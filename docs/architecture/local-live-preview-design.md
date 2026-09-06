# Local Live Preview設計

## ステータス

Issue #32のLocal Live Previewは段階的に実装している。

- Phase 1 (#33): 設定model、derived URL、Host validation、process lifecycle/port reservation
- Phase 2 (#34): Hugo lazy start/stop、loopback port、hostname reverse proxy、redirect補正、WebSocket/LiveReload中継
- Phase 3: **shadow content workspace + editor debounce連携**
- Phase 4: UI/運用導線

Local Live Previewは既存のMarkdown本文プレビュー、Deployment Previewを置き換えない。

```text
Markdown本文プレビュー
  -> generatorを実行しない安全な即時確認

Local Live Preview
  -> Hugo/theme/layout/shortcode/CSS/JSを使う編集中確認

Deployment Preview
  -> remote buildした特定commitを公開前に最終確認
```

## URLとingress境界

Local Live Previewはpath prefixではなくsiteごとのhostnameをorigin rootとして使用する。

```text
PREVIEW_DOMAIN=preview.example.com
PREVIEW_SCHEME=https

tech  -> https://tech.preview.example.com/
daily -> https://daily.preview.example.com/
```

DNSは`*.preview.example.com`のwildcardを想定する。TLS、wildcard certificate、DNS-01、Cloudflare、Tailscale、Caddy/Traefik/Nginx等はpreview ingressの責務であり、hugo-cmsへ証明書秘密鍵やDNS provider credentialを持たせることを必須にしない。

### 閲覧者認可

wildcard DNSやHost validationは閲覧者認可ではない。Local Live Previewは未公開contentとrepository由来JavaScriptを配信し得るため、preview ingressは次のいずれかで保護する。

- Tailscale等のprivate networkからのみ到達可能にする
- Internet reachableの場合はCloudflare Access等のpreview専用viewer authenticationを使う

CMS session cookieを`*.preview.example.com`へ共有しない。preview hostname requestはCMS session middlewareより前で処理する。

## 設定契約

```env
LOCAL_LIVE_PREVIEW_ENABLED=false
PREVIEW_DOMAIN=preview.example.com
PREVIEW_SCHEME=https
```

Site Registryではsite単位でoverrideできる。

```yaml
sites:
  - id: tech
    repo_path: /data/repos/tech-blog
    generator: hugo
    preview:
      local_preview:
        enabled: true
```

有効siteのIDはlowercase DNS labelでなければならない。`<site-id>.<preview-domain>`全体も253文字以内の有効なDNS名である必要がある。

HTTPSではport省略または`:443`、HTTPではport省略または`:80`だけを受け付ける。non-standard external portは初期契約ではサポートしない。

## Host routing

HTTP `Host`を`ResolveLocalPreviewHost`へ渡し、Site Registryに登録済みかつLocal Live Previewが有効なsiteだけへ解決する。

preview namespaceに属するinvalid/unknown HostはCMS admin routeへfall throughさせず拒否する。Hostからrepository path、generator command、internal portを生成しない。

## Hugo process lifecycle

`LocalPreviewManager`がsiteごとのHugo processを管理する。

```text
stopped -> starting -> ready -> stopping -> stopped
                 \-> failed <-/
```

- 最初のpreview requestでlazy start
- `127.0.0.1`だけへbind
- internal port rangeは`14100-14999`
- OS上のport availabilityをprobeし、bind/start raceでは有限回retry
- child environmentはgenerator allowlistのみ
- CMS shutdown開始後は新規lazy startを拒否
- HTTP server drain後にchild processを停止
- 異常終了後は次requestで再起動可能

Hugoは概ね次相当で起動する。

```text
hugo server
  --source .
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

## Reverse proxy / LiveReload

preview requestはpathを書き換えずHugoへproxyする。

```text
https://tech.preview.example.com/css/main.css
  -> http://127.0.0.1:<internal-port>/css/main.css
```

proxyはexternal Hostを保持し、`X-Forwarded-Host` / `X-Forwarded-Proto`を再構成する。内部`127.0.0.1:<port>` / `localhost:<port>`を指すabsolute `Location`だけをexternal preview originへ書き換える。HTTP Upgradeを透過してLiveReload WebSocketを通す。

## Phase 3: shadow content workspace

### 目的

editorの250ms debounce更新をproduction working treeへ直接書かず、CMS管理の一時content directoryへ反映する。

```text
Editor input
  -> 250ms debounce
  -> POST /admin/api/preview/local
  -> shadow content directory
  -> Hugo watcher
  -> rebuild / LiveReload
```

既存の3秒autosaveは保存機能として残るが、Local Live Previewの更新経路自体はGit working treeへ書かない。

### workspace構成

初回update時にsiteの`content_dir`全体を一時directoryへmirrorする。

```text
OS temporary directory/
  <site-id>/
    <local-preview-session-id>/
      content/
        ... mirrored site content ...
```

Hugoのworking directory/source rootは元repositoryのまま維持するため、次は元repoから使用する。

- Hugo config
- theme / layout
- static
- assets
- modules

`--contentDir`だけをshadow directoryのabsolute pathへ差し替える。これにより未保存contentとrepositoryのsite構造を分離する。

workspaceは`PREVIEW_STATE_DIR`へ永続化しない。CMS processごとのephemeral stateであり、session releaseまたはCMS shutdown時に削除する。

### update ordering

browserはLocal Preview専用session IDと単調増加`revision`を送る。

- `revision == 0`は拒否
- server側の現在revision以下のupdateはno-op
- HTTP response順が逆転しても古いrequestは新しい内容を上書きしない
- 対象記事はatomic replacementする

### session競合

初期実装では**同一siteにつきactive Local Live Preview sessionは1つ**とする。

- 同じsiteを別tab/sessionからupdate -> `409 Conflict`
- active sessionと異なるarticle path -> conflict
- 別siteは独立workspaceを持てる
- stale tabは別tabのworkspaceをreleaseできない

site/article切替では、browserがin-flight update完了を待ってからreleaseする。serverはHugo processを先に停止し、その後shadow directoryを削除する。

### filesystem境界

- article pathは既存`SafeJoin`境界で検証
- shadow content copyではsymlinkを拒否
- regular fileだけをmirror
- target articleはtemporary fileからreplace
- production contentはLocal Preview updateによって変更しない

### 現時点の制約

初回mirror後に**content directory内へ新規作成されたpage resource**（例: article bundleへ後からuploadした画像）は自動mirrorしない。既存resourceは初回mirrorに含まれる。Phase 3はまずeditor本文/front matterの未保存反映を対象とし、必要ならmedia upload/delete連携を後続で追加する。

また、Hugo Modulesで`content`をcustom mountしているsiteはlegacy `contentDir`とは異なる解決経路を使う場合がある。実blog repositoryでのsmoke test後、必要ならmount-awareな方式を追加する。

## API

### editor state update

```text
POST /admin/api/preview/local
```

主なrequest fields:

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

初回updateでworkspaceを作成した場合、保存済みcontentを使っていた既存Hugo processを一度停止する。次のpreview hostname requestがshadow `contentDir`でHugoをlazy startする。その後のupdateは同じdirectoryを書き換えるためHugo watcherが再buildする。

### release

```text
POST /admin/api/preview/local/release
```

release時はHugo process停止後にworkspaceを削除する。次のpreview requestはactive shadow sessionがなければ保存済みrepository contentを使用する。

## Phase 4への契約

Phase 4は以下を実装する。

- Local Live Preview表示/新規tab導線
- starting/ready/failed状態表示
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
