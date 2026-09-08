# Local Live Preview設計

## ステータス

Issue #32のLocal Live Previewは段階的に実装する。

- Phase 1 (#33): 設定model、derived URL、Host validation、process lifecycle/port reservation
- Phase 2 (#34): Hugo lazy start/stop、loopback port、hostname reverse proxy、redirect補正、WebSocket/LiveReload中継
- Phase 3 (#35): shadow content workspace + editor debounce + content resource同期
- Phase 4: UI/運用導線、generator別URL解決
- Issue #46: Eleventy `--serve`、shadow input、generator metadata URL解決
- Issue #37: 実blog・wildcard ingressでのHugo/Eleventy受け入れ確認は実環境で継続する

Local Live PreviewはMarkdown本文プレビューとDeployment Previewを置き換えず、両者の中間を担う。

```text
Markdown本文プレビュー
  -> generatorを実行しない安全な即時確認

Local Live Preview
  -> Hugo/theme/layout/shortcode/CSS/JSを使う編集中確認

Deployment Preview
  -> remote buildした特定commitを公開前に最終確認
```

## URL / ingress / viewer authentication

Local Live Previewはpath prefixではなくsiteごとのhostnameをorigin rootとして使う。

```text
PREVIEW_DOMAIN=preview.example.com
PREVIEW_SCHEME=https

tech  -> https://tech.preview.example.com/
daily -> https://daily.preview.example.com/
```

DNSは`*.preview.example.com`のwildcardを想定する。TLS、DNS-01、Cloudflare、Tailscale、Caddy/Traefik/Nginx等はpreview ingressの責務であり、hugo-cmsへ証明書秘密鍵やDNS provider credentialを持たせることを必須にしない。

wildcard DNSやHost validationは閲覧者認可ではない。preview ingressはTailscale等のprivate network内に置くか、Internet reachableならCloudflare Access等の独立viewer authenticationで保護する。CMS session cookieはpreview subdomainへ共有しない。

## 設定 / Host routing

```env
LOCAL_LIVE_PREVIEW_ENABLED=false
PREVIEW_DOMAIN=preview.example.com
PREVIEW_SCHEME=https
```

```yaml
sites:
  - id: tech
    repo_path: /data/repos/tech-blog
    generator: hugo
    preview:
      local_preview:
        enabled: true
```

有効site IDはlowercase DNS labelで、`<site-id>.<preview-domain>`全体も253文字以内の有効DNS名でなければならない。

HTTPSではport省略または`:443`、HTTPではport省略または`:80`だけを許可する。preview namespace内のinvalid/unknown HostはCMS admin routeへfall throughさせない。HostはSite Registry lookupにだけ使い、repository path、command、internal portへ変換しない。

## Generator process lifecycle

`LocalPreviewManager`がsiteごとのgenerator processを管理する。generatorごとの差異はprocess command factoryと`PreviewURLResolver`へ閉じ込め、lifecycle、port reservation、proxy、shutdownは共通化する。

```text
stopped -> starting -> ready -> stopping -> stopped
                 \-> failed <-/
```

- preview hostnameへの最初のrequestでlazy start
- `127.0.0.1`だけへbind
- internal port rangeは`14100-14999`
- port probe後のbind raceでは有限回retry
- child environmentはgenerator allowlistのみ
- CMS shutdown開始後は新規lazy startを拒否
- HTTP server drain後にchild processを停止
- 異常終了後は次requestで再起動可能

Hugoは概ね次相当で起動する。

```text
hugo server
  --source .
  --environment development
  --contentDir <content-dir>
  --bind 127.0.0.1
  --port <internal-port>
  --baseURL https://<site-id>.<preview-domain>/
  --appendPort=false
  --liveReloadPort 443
  --renderToMemory
  --buildDrafts
  --buildFuture
  --buildExpired
  --watch
  --noHTTPCache
```

Eleventyは対象siteのpackage managerを再利用し、production repositoryをcwdにしたままshadow contentを入力にする。

```text
<package-manager> exec eleventy
  --serve
  --input <shadow-content-dir>
  --output <OS-temporary-output-dir>
  --port <internal-port>
```

Eleventyの出力ディレクトリはproductionの`public`/`_site`を上書きしない。内部serverはloopback portだけでproxyから利用し、停止時にtemporary outputを削除する。Eleventy dev serverのwatch/live reloadをそのまま使う。

記事選択時の初回起動では、shadow workspaceをgeneratorの入力として使う。CMSはgeneratorに依存しない`PreviewURLResolver`契約を介して解決する。Hugo実装はserverと同じ`--environment development`を指定し、`HUGO_CONTENTDIR`と`HUGO_BASEURL`のenvironment variableでshadow contentとLocal Preview URLをoverrideし、`--noBuildLock`を渡す。Eleventy実装は同じrepository cwdとshadow `--input`で`--to=json --quiet`を実行し、Eleventyが返す`inputPath`と`url`を対応づける。取得したURLはpath、query、fragmentを保持してLocal Preview originへ変換し、CMSはpermalink、slug、Data Cascade、paginationを再実装しない。以降の同一記事の編集はgeneratorのwatch/live reloadを利用する。

## Reverse proxy / LiveReload

preview requestはpathを書き換えずHugoへproxyする。

```text
https://tech.preview.example.com/css/main.css
  -> http://127.0.0.1:<internal-port>/css/main.css
```

external Hostを保持し、`X-Forwarded-Host` / `X-Forwarded-Proto`を再構成する。内部loopbackを指すabsolute `Location`だけをexternal preview originへ書き換え、HTTP Upgradeを透過してLiveReload WebSocketを通す。

## Phase 3: shadow content workspace

### 目的

editorの約250ms debounce更新をproduction working treeへ直接書かず、CMS管理の一時content directoryへ反映する。

```text
Editor input
  -> 250ms debounce
  -> POST /admin/api/preview/local
  -> shadow content directory
  -> generator watcher
  -> rebuild / LiveReload
```

既存の3秒autosaveは保存機能として残るが、Local Live Previewのupdate経路自体はGit working tree/index/refへ書かない。

### workspace

初回update時にsiteの`content_dir`全体をOS temporary directoryへmirrorする。

```text
OS temporary directory/
  <site-id>/
    <local-preview-session-id>/
      content/
        ... mirrored site content ...
```

generatorのworking directoryは元repositoryのまま維持する。Hugo config、theme/layout、static、assets、modulesは元repoから読み、`--contentDir`だけをshadow directoryのabsolute pathへ差し替える。Eleventyもconfig、layout、data、passthrough assetは元repoから読み、`--input`だけをshadow directoryへ差し替え、生成出力はtemporary directoryへ分離する。

workspaceは`PREVIEW_STATE_DIR`へ永続化しない。session releaseまたはCMS shutdown時に削除する。

### editor update ordering

browserはLocal Preview専用session IDと単調増加`revision`を送る。

- `revision == 0`は拒否
- serverの現在revision以下のupdateはno-op
- response順が逆転しても古いrequestは新しい内容を上書きしない
- 対象記事はtemporary fileからreplaceする

### content resource同期

初回mirrorにはarticle bundle内のpage resourceも含まれる。workspace作成後にCMS media APIでcontent directory配下へupload/deleteしたresourceは、active shadow workspaceへ同じ変更を同期する。

static配下はHugoが元repositoryを直接参照するためshadow同期しない。media本体の保存が成功した後にpreview-only同期が失敗した場合、media APIを失敗扱いにはせずserver logへ残す。

### session競合

初期実装では**同一siteにつきactive Local Live Preview sessionは1つ**とする。

- 同一siteへの別tab/session update -> `409 Conflict`
- active sessionと異なるarticle path -> conflict
- 別siteは独立workspaceを利用可能
- stale tabは別tabのworkspaceをreleaseできない

article/site切替ではbrowserがin-flight update完了を待ってreleaseする。serverはgenerator processを先に停止し、その後shadow directoryを削除する。

ブラウザtabを切替操作なしで閉じた場合の確実なlease解放はPhase 4の停止UI/lease運用で扱う。それまではCMS shutdownで全workspaceをcleanupする。

### filesystem境界

- article/resource pathは既存`SafeJoin`境界で検証
- shadow initial copyではsymlinkを拒否
- regular fileだけをmirror
- production contentはLocal Preview updateによって変更しない
- site IDとsession IDは事前validation済みの値だけをworkspace pathに使用する

### Hugo Modules

Hugo Modulesで`content`をcustom mountしているsiteはlegacy `contentDir`とは異なる解決経路を使う場合がある。実blog repositoryでのsmoke test後、必要ならmount-aware方式を追加する。

## API

### editor state update

```text
POST /admin/api/preview/local
```

```json
{
  "draft_id": "<local-preview-session-id>",
  "revision": 12,
  "path": "posts/example.md",
  "frontmatter": {"title": "Draft"},
  "body": "editing...",
  "format": "yaml"
}
```

初回updateでworkspaceを作った場合、保存済みcontentを使っていた既存generator processを一度停止する。次のpreview hostname requestでshadow inputを使ってlazy startし、その後はgenerator watcherが変更を拾う。

### 初回記事URL解決

```text
POST /admin/api/preview/local/navigate
```

記事選択時にCMSが`draft_id`と記事pathを`/admin/api/preview/local/navigate`へ送り、serverはactive sessionの所有者・記事pathを検証する。検証後、`PreviewURLResolver`がshadow workspaceを使ってgeneratorへURL解決を依頼し、`article_url`を返す。Hugo実装は`hugo list all`の`path`と`permalink`を対応づけ、production originではなくLocal Preview originを返す。この処理はproduction content、Git、editor revisionを変更しない。network error、408/425/429、5xxに限ってclientが250ms・750msのbackoffで最大3回まで再試行し、409やその他の4xxは再試行しない。通常の本文編集はLiveReloadを利用し、URL関連front matter変更時は再解決する。解決失敗時はpreview rootへ黙ってフォールバックしない。

### release

```text
POST /admin/api/preview/local/release
```

release時はgenerator process停止後にworkspaceを削除する。active shadow sessionがなければ次のpreview requestは保存済みrepository contentを使う。Eleventyのtemporary outputも同時に削除する。

## Phase 4への契約

- Local Live Previewを開く/新規tab導線（埋め込みを主導線、新規tabをfallback）
- desktopの編集+埋め込みpreview並列表示と狭い画面での上下配置
- iframe loading、応答未確認のbest-effort表示、新規tab fallback。preview側がCMS originをtarget originに指定して`homecms-local-preview-ready`の`postMessage`を送る場合は明示的なready通知として扱う
- 記事選択時の`PreviewURLResolver`によるgenerator準拠の実ページURL解決と、解決済みURLのiframe/新規タブ表示
- Local Preview有効siteの`Edit` / `Preview` / `Split`統合。無効siteではMarkdown Previewを維持
- starting / ready / failed / conflict / stale状態表示
- Local Previewの明示停止とstale session recovery/lease方針
- private network/Tailscale運用例
- wildcard DNS / TLS ingress構成例
- Deployment Previewとの役割差のUI明記

## セキュリティ要点

- wildcard DNSはauthorizationではない
- preview ingressにはprivate networkまたは独立viewer authenticationを必須とする
- CMS session cookieをpreview subdomainと共有しない
- unknown site IDは拒否
- generator child environmentはallowlist
- internal generator serverはloopback bindのみ
- TLS/DNS credentialをCMSへ要求しない
- Local Live Previewはrepository内generator codeを実行するため、Markdown本文プレビューより広いtrust boundaryである
