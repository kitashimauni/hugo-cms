# Local Live Preview設定ガイド

> Issue #32ではPhase 1〜4が実装済みです。session lease/recovery、status/stop APIに加え、CMS内の埋め込みpreviewを主導線とするUIを提供します。実blogとwildcard ingressを使った受け入れ確認はIssue #37で追跡します。

## 基本設定

```env
LOCAL_LIVE_PREVIEW_ENABLED=true
PREVIEW_DOMAIN=preview.example.com
PREVIEW_SCHEME=https
```

`tech` siteならpreview URLは次になります。

```text
https://tech.preview.example.com/
```

`PREVIEW_DOMAIN`には`*.`、scheme、path、portを含めません。`PREVIEW_SCHEME`は`http`または`https`だけです。

## Site Registry

```yaml
default_site: tech
sites:
  - id: tech
    repo_path: /data/repos/tech-blog
    generator: hugo
    preview:
      local_preview:
        enabled: true
```

`preview.local_preview.enabled`を省略すると`LOCAL_LIVE_PREVIEW_ENABLED`を継承します。有効siteの`GET /admin/api/sites`にはderived `preview.local_preview.url`が含まれます。

有効site IDはlowercase DNS labelで、`<site-id>.<preview-domain>`全体も253文字以内の有効DNS名である必要があります。

## wildcard DNS / TLS / viewer authentication

想定DNS:

```text
*.preview.example.com -> preview ingress
```

TLS certificate、DNS-01、Cloudflare、Tailscale、Caddy/Traefik/Nginx等は外部preview ingressの責務です。

**wildcard DNSとHost validationは閲覧者認可ではありません。** Tailscale等のprivate network内に置くか、Internet reachableならCloudflare Access等の独立viewer authenticationで保護してください。CMS session cookieをpreview subdomainへ共有しません。

## Host routing

HTTPSでは次を許可します。

```text
tech.preview.example.com
tech.preview.example.com:443
```

次はfail closedします。

```text
preview.example.com
foo.tech.preview.example.com
unknown.preview.example.com
tech.preview.example.com:8443
tech.preview.example.com:evil
```

HTTP設定ではport省略または`:80`だけを許可します。HostはSite Registry lookupにだけ使います。

## Generator process

最初のpreview hostname requestで、siteの`generator`に対応した開発サーバーをlazy startします。現在はHugoとEleventyに対応しています。

```text
hugo server
  --source .
  --contentDir <content-dir>
  --bind 127.0.0.1
  --port <internal-port>
  --baseURL https://tech.preview.example.com/
  --appendPort=false
  --liveReloadPort 443
  --renderToMemory
  --buildDrafts
  --buildFuture
  --buildExpired
  --watch
  --noHTTPCache
```

内部portは`14100-14999`から予約します。Hugoはloopbackだけへbindし、child environmentはgenerator allowlistを使います。

CMS shutdown開始後は新規preview processを起動せず、HTTP serverをdrainしてからchild processを停止します。

Eleventy siteでは、対象siteのlock fileから検出したpackage manager経由で次のように起動します。

```text
<package-manager> exec eleventy
  --serve
  --input <shadow-content-dir>
  --output <temporary-preview-output-dir>
  --port <internal-port>
```

Eleventyのconfig、layout、data、assetはproduction repositoryを作業ディレクトリとして読み込み、生成出力だけをOS temporary directoryへ分離します。Eleventy自身のdev server/watch/live reloadを使用するため、production working treeやGitへ生成物を書き込みません。

## Reverse proxy / LiveReload

path prefixは追加しません。

```text
https://tech.preview.example.com/css/main.css
  -> http://127.0.0.1:<internal-port>/css/main.css
```

内部upstreamを指すabsolute `Location`だけを外部preview originへ補正し、HTTP Upgradeを透過してLiveReload WebSocketを通します。

## 未保存editor内容

Local Live Previewが有効なsiteでは、editor変更を約250ms debounceして次へ送ります。

```text
POST /admin/api/preview/local
```

初回update時にrepositoryの`content_dir`をOS temporary directoryへmirrorします。

```text
Editor
  -> 250ms debounce
  -> shadow content workspace
  -> generator watcher
  -> rebuild / LiveReload
```

generatorの作業ディレクトリは元repositoryのままです。Hugoは`--contentDir`、Eleventyは`--input`だけshadow directoryのabsolute pathへ切り替え、theme/layout/config/data/static/assets/modulesなどgeneratorが管理する規則は元repoから読み込みます。Eleventyの生成出力はtemporary directoryへ分離します。

既存の3秒autosaveは保存機能として残りますが、Local Previewの250ms update経路はproduction working tree/Git index/refへ書き込みません。

### revision

各updateには単調増加`revision`を付けます。serverは現在revision以下の古いrequestをno-opにするため、network順序が逆転しても古い本文で上書きされません。

### article bundle resource

workspace作成時点のcontent resourceは初回mirrorに含まれます。その後CMS media APIでcontent directory配下へupload/deleteした画像等もactive shadow workspaceへ同期します。

static配下は元repositoryをHugoが直接参照するためshadow同期しません。media本体の保存成功後にpreview同期だけ失敗した場合は、media操作を失敗扱いにせずserver logへ記録します。

## session lease / recovery

Local Preview ownership IDはbrowser document/tabのmemory上だけに保持します。複製tabが同じIDを引き継がないため、同一siteの別tab/sessionは`409 Conflict`になります。

一方、tab reload、browser crash、network断ではrelease requestを確実に送れません。そのためactive workspaceにはlast-seen leaseを持たせます。

- lease TTL: 2分
- CMS editorは30秒ごとにheartbeat
- editor update自体もleaseを更新
- lease切れworkspaceは`stale`としてstatus APIへ表示
- stale workspaceは明示的なreclaim APIでCMS再起動なしに回収可能
- liveなsessionはreclaimできない

recovery時もgenerator processを先にstopしてからshadow workspaceを削除します。

## UI

Local Live Preview panelでは次を利用できます。

- `埋め込み表示`: 記事を選択するとCMS内のsandbox付きiframeを主表示として自動表示
- `新規タブで開く`: iframeが利用できない場合や補助的な確認に使う
- `簡易Markdownを表示`: generatorを使わない補助/fallback表示
- `管理操作` > `停止`: 自分が所有するsession、またはworkspaceを伴わないsaved-content preview processを停止
- `管理操作` > `期限切れsessionを回収`: stale leaseだけを安全にreclaim
- stopped / starting / ready / failed / conflict / staleの状態表示

Local Live Previewが有効なsiteではheaderのview切替を次のように扱います。

- `Edit`: Editorのみを全幅表示
- `Preview`: Local Live Previewを全幅表示
- `Split`: EditorとLocal Live Previewを左右（狭い画面では上下）に表示

記事選択時はdesktopでは`Split`を初期viewにし、generatorが解決した記事ページを表示します。CMSは`slug`、`url`、permalink、page bundleの規則を推測せず、generatorのURL resolverへ解決を委譲します。Local Live Previewが無効なsiteでは、従来どおり`Preview`と`Split`の右側に簡易Markdown Previewを表示します。

記事選択直後の初回表示では、現在の記事をshadow workspaceへ反映した後、generator自身のURL解決結果を取得します。Hugoは`hugo list all`の`permalink`、Eleventyは`eleventy --to=json`の`inputPath`/`url` metadataを使います。取得したURLはpath、query、fragmentを保持したままLocal Preview originへ変換し、iframeと新規タブへ直接設定します。CMSはslug、`url`、permalink、page bundle、Data Cascade、paginationなどの規則を再実装しません。通常の本文編集ではiframeの現在URLを維持してgeneratorのwatch/live reloadを利用し、URLに影響するfront matter変更時だけ再解決します。

初回URL解決のnetwork error、408/425/429、5xxは250ms・750msのbackoffで最大3試行します。409（別session、stale、記事不一致）やその他の4xxは再試行せず、通常のsession recovery表示へ委譲します。URLを解決できない場合はpreview rootへフォールバックせず、エラー状態を表示します。

iframeの読み込み中はloading表示を出し、`load`または対応するpreview bridgeのready通知を一定時間確認できない場合は、エラーと「新規タブで開く」fallbackを表示します。これはbest-effortの判定であり、CSPや`X-Frame-Options`などによるiframe拒否をブラウザAPIだけで確実に判定するものではありません。埋め込み表示はボタンから閉じられ、記事を切り替えるかstale sessionを回収すると再び自動表示されます。狭い画面では編集画面とpreviewを上下に配置します。

preview側を管理できる場合は、正常表示後に親ウィンドウへ `window.parent.postMessage({ type: 'homecms-local-preview-ready' }, '<CMS origin>')` を送ると、CMSが明示的なready通知として扱います。第2引数のtarget originはpreview originではなく、親フレームであるCMSのorigin（例: `https://cms.example.com`）を指定し、`*`は使用しません。

### iframe埋め込み

preview hostnameはCMSとは別originなので、iframe埋め込み自体は可能です。CMSはpreview subdomainへsession cookieを共有しません。

ただし、外部preview ingressが次のheaderでframeを禁止している場合は埋め込めません。

```text
X-Frame-Options: DENY
X-Frame-Options: SAMEORIGIN
Content-Security-Policy: frame-ancestors 'none'
```

Cloudflare Access等のviewer authenticationのlogin画面がiframeを拒否する構成もあります。そのため**埋め込みを主導線にしつつ、新規タブ表示を必ずfallbackとして残します**。

CMS側iframeにはsandboxを付け、top-level navigation等を許可しません。Hugo/LiveReloadに必要なscriptとsame-origin権限だけを許可します。

## article/site切替とcleanup

article/site切替時はin-flight update完了を待ってsessionをreleaseします。

```text
generator process stop
  -> shadow workspace delete
```

CMS shutdownでもgenerator child停止後にtemporary workspaceとEleventy temporary outputを削除します。workspaceは`PREVIEW_STATE_DIR`へ永続化しません。

## Hugo Modules

Hugo Modulesで`content`をcustom mountしているsiteは実blog repositoryでsmoke testしてください。必要ならmount-awareなpreview方式を追加します。

## API

editor update:

```text
POST /admin/api/preview/local
```

記事選択後の初回URL解決:

```text
POST /admin/api/preview/local/navigate
```

```json
{
  "draft_id": "<local-preview-session-id>",
  "path": "posts/example.md"
}
```

このAPIはactive sessionの所有者と記事pathを検証し、shadow workspaceを使ってgeneratorの実ページURLを解決します。成功時は`article_url`、`revision`、`session_id`を返し、production content、Git working tree、editorのrevisionは変更しません。解決に失敗した場合はエラーを返し、preview rootへフォールバックしません。

release:

```text
POST /admin/api/preview/local/release
```

lifecycle:

```text
GET  /admin/api/preview/local/status
POST /admin/api/preview/local/heartbeat
POST /admin/api/preview/local/stop
POST /admin/api/preview/local/reclaim
```

すべて既存admin auth + CSRF境界の内側です。status APIは別tabのowner session IDを返しません。

## 旧preview方式との違い

`PREVIEW_URL`、`HUGO_SERVER_PORT`、`HUGO_SERVER_BIND`はIssue #30以前のpath-prefix preview用legacy設定で、新Local Live Previewには使用しません。

```text
旧: /admin/preview/tech/...
新: https://tech.preview.example.com/...
```

詳細は[Local Live Preview設計](../architecture/local-live-preview-design.md)を参照してください。
