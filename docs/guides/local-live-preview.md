# Local Live Preview設定ガイド

> Issue #32ではPhase 1〜4が実装済みです。site-scoped runtimeのstatus/stop APIと、CMS内の埋め込みpreviewを主導線とするUIを提供します。実blogとwildcard ingressを使った受け入れ確認はIssue #37で追跡します。

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

Eleventy siteでは、対象siteのlock fileから検出したpackage manager経由で、production repositoryを元にしたtemporary project-root overlayをcwdとして次のように起動します。

```text
<package-manager> exec node <CMS>/scripts/eleventy-local-preview.cjs
  --serve
  --input <project-relative-content-dir>
  --output <temporary-project-public-dir>
  --port <internal-port>
  --host 127.0.0.1
```

CMSのNodeラッパーはtemporary project-root overlayをcwdにしてEleventyのprogrammatic `watch`を実行します。overlayでは`content_dir`をshadow workspaceへ、`public_dir`をtemporary outputへ置き換え、package.json、node_modules、config、includes/layouts/data、その他のproject-root相対パスはproduction repositoryを参照します。HTTP配信とLiveReload WebSocketはCMS側のloopback serverが担当し、Eleventyの初回build前にlistenerを`127.0.0.1`へbindします。`/__hugo_cms_ready`はbuild中に503、初回build完了後に200を返すため、重いsiteでも起動処理が固定秒数で同じbuildを繰り返しません。Eleventy標準Dev Serverの未指定hostや`HOST`環境変数には依存しません。

process停止はgeneratorの`cmd.Wait()`完了を成功条件とし、temporary outputのfilesystem cleanup完了を待ちません。workspaceが所有するEleventyのproject/outputはsite runtimeのStopまたはidle cleanupで削除し、process cleanupとの二重削除や次世代workspaceとの競合を避けます。workspace外のEleventy previewは起動ごとに一意なtemporary project rootを割り当て、停止後に所有projectだけを非同期cleanupします。cleanupの遅延・失敗はserver logへ記録しますが、generator processの停止失敗とは区別します。

Eleventy設定はoverlayのproject rootで通常どおり解決します。そのため`getFilteredByGlob("src/posts/**")`、`addPassthroughCopy("src/images")`、pluginの`outputDir: "./public/img/"`のようなproject-root相対指定も、CMS側で解析・推定せずpreview側のcontent/publicを参照します。通常の記事URL解決では、稼働中wrapperが`eleventy.after`の`inputPath`/`url`を保持する`/__hugo_cms_metadata?path=...`を利用します。workspace updateはshadow fileを書き換える直前にmetadataをinvalidateし、次のwatch buildが完了するまで旧mapを503で隠します。watch rebuildごとにmapを更新するため、resolver専用の追加full buildを発生させません。既存の直接resolver呼び出しにはJSONモードのfallbackを残します。
repo外のabsolute pathや環境変数で指定された外部pathはこのfilesystem overlayの保証対象外です。

初回buildとwatch rebuild後のmetadata URL解決の待機上限はデフォルト2分です。必要に応じて`HUGO_CMS_LOCAL_PREVIEW_STARTUP_TIMEOUT`へGoのduration（例: `5m`）を設定できます。Eleventyの初回build失敗時は同じ重いbuildを自動で繰り返さず、stderrの末尾を含む診断を返します。

## Reverse proxy / LiveReload

path prefixは追加しません。

```text
https://tech.preview.example.com/css/main.css
  -> http://127.0.0.1:<internal-port>/css/main.css
```

内部upstreamを指すabsolute `Location`だけを外部preview originへ補正し、HTTP Upgradeを透過してLiveReload WebSocketを通します。Eleventyの`/__hugo_cms_ready`、`/__hugo_cms_metadata`、`/__hugo_cms_invalidate`はCMSとwrapper間のloopback専用制御endpointであり、preview hostnameからは404として遮断します。LiveReload用のpathは引き続きproxyします。

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

generatorの作業ディレクトリは、Hugoでは元repository、Eleventyでは一時project-root overlayです。Hugoは`--contentDir`をshadowへ差し替え、Eleventyはproject-relativeな`--input`と`--output`を使います。theme/layout/config/data/static/assets/modulesなどgeneratorが管理する規則は、それぞれ元repoまたはoverlay内のproduction referenceから読み込み、Eleventyの生成出力はtemporary directoryへ分離します。

既存の3秒autosaveは保存機能として残りますが、Local Previewの250ms update経路はproduction working tree/Git index/refへ書き込みません。

### revision / 複数tab

各updateにはbrowser documentごとの`revision`を付けます。serverはこの値をtab間のordering判定には使わず、受信したrequestをsite workspaceへ適用してserver側のrevisionを採番します。同じsiteを複数tabから編集しても`409`にはならず、preview shadowはlast-write-winsです。production contentとGitの整合性は通常のsave側で管理します。

### article bundle resource

workspace作成時点のcontent resourceは初回mirrorに含まれます。その後CMS media APIでcontent directory配下へupload/deleteした画像等もactive shadow workspaceへ同期します。

static配下は元repositoryをHugoが直接参照するためshadow同期しません。media本体の保存成功後にpreview同期だけ失敗した場合は、media操作を失敗扱いにせずserver logへ記録します。

## site runtime activity / cleanup

Local Previewはbrowser tabを所有者として扱いません。update、記事URL解決、preview ingress、content resource同期をsite runtimeの`lastActivity`として記録します。idle timeoutは`HUGO_CMS_LOCAL_PREVIEW_IDLE_TIMEOUT`で設定でき、初期値は30分、`0`で無効化できます。

idle timeoutまたは明示Stopでは、site gateで新しいupdate/ingressを止めてからgenerator processを停止し、workspace rootを一意なcleanup領域へrenameして物理削除を非同期で行います。長寿命のHTTP/WebSocket配信前にgateを解放するため、cleanupの遅延が配信完了をブロックしません。CMS shutdownでも同じcleanup pathを利用します。

## UI

Local Live Preview panelでは次を利用できます。

- `埋め込み表示`: 記事を選択するとCMS内のsandbox付きiframeを主表示として自動表示
- `新規タブで開く`: iframeが利用できない場合や補助的な確認に使う
- `簡易Markdownを表示`: generatorを使わない補助/fallback表示
- `停止`: site runtime、generator process、shadow workspaceを停止・cleanup
- stopped / starting / ready / failedの状態表示

Local Live Previewが有効なsiteではheaderのview切替を次のように扱います。

- `Edit`: Editorのみを全幅表示
- `Preview`: Local Live Previewを全幅表示
- `Split`: EditorとLocal Live Previewを左右（狭い画面では上下）に表示

記事選択時はdesktopでは`Split`を初期viewにし、generatorが解決した記事ページを表示します。CMSは`slug`、`url`、permalink、page bundleの規則を推測せず、generatorのURL resolverへ解決を委譲します。Local Live Previewが無効なsiteでは、従来どおり`Preview`と`Split`の右側に簡易Markdown Previewを表示します。

記事選択直後の初回表示では、現在の記事をshadow workspaceへ反映した後、generator自身のURL解決結果を取得します。Hugoは`hugo list all`の`permalink`、Eleventyは稼働中wrapperの`eleventy.after` metadata map（`inputPath`/`url`）を使います。取得したURLはpath、query、fragmentを保持したままLocal Preview originへ変換し、iframeと新規タブへ直接設定します。CMSはslug、`url`、permalink、page bundle、Data Cascade、paginationなどの規則を再実装しません。通常の本文編集ではiframeの現在URLを維持してgeneratorのwatch/live reloadを利用し、URLに影響するfront matter変更時だけ再解決します。

初回URL解決のnetwork error、408/425/429、5xxは250ms・750msのbackoffで最大3試行します。その他の4xxは再試行せず、エラー状態を表示します。URLを解決できない場合はpreview rootへフォールバックしません。

iframeの読み込み中はloading表示を出し、`load`または対応するpreview bridgeのready通知を一定時間確認できない場合は、エラーと「新規タブで開く」fallbackを表示します。これはbest-effortの判定であり、CSPや`X-Frame-Options`などによるiframe拒否をブラウザAPIだけで確実に判定するものではありません。埋め込み表示はボタンから閉じられ、記事を切り替えると再び自動表示されます。狭い画面では編集画面とpreviewを上下に配置します。

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

article切替では、現在記事のproduction保存とin-flight Local Preview updateの完了を待ってから、同じsite workspaceへ新しい記事pathを反映します。generator process、project overlay、shadow workspaceは再利用し、watch rebuildとgenerator準拠のURL解決だけを行います。browser tab ownershipを持たないため、記事切替でstop/releaseは実行しません。Previewを開く、または埋め込みpreviewを再表示する明示操作では、clientの同期済み判定やstatus pollingに依存せず、現在のeditor payloadを必ずsite-scoped shadowへ再同期してからURLを解決します。複数tab共有のshadowとclient状態がずれていてもこの再同期で収束し、同一contentの場合はserver-side no-opにより不要なupdate/invalidation/buildを発生させません。

UIの明示的な停止では、clientがdebounce済みの更新をキャンセルし、送信済みの更新完了を待ってからsite runtimeを停止します。idle timeout、CMS shutdownを含め、site runtimeは次の順で終了します。

```text
generator process stop
  -> workspace root detach/rename
  -> shadow workspace delete (async)
```

CMS shutdownでもgenerator child停止後にtemporary workspaceとEleventy temporary outputを削除します。workspaceは`PREVIEW_STATE_DIR`へ永続化しません。

cleanup中に到着またはcleanup待ちになった古いpreview requestはgenerationで無効化し、停止完了直後にworkspaceやgeneratorを復活させません。cleanup後に新しく到着したrequestだけがsite runtimeを再開できます。

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
  "path": "posts/example.md"
}
```

このAPIはsite単位のshadow workspaceから要求された記事pathを使ってgeneratorの実ページURLを解決します。成功時は`article_url`とserver側の`revision`を返し、production content、Git working tree、browserのrevisionは変更しません。解決に失敗した場合はエラーを返し、preview rootへフォールバックしません。

lifecycle:

```text
GET  /admin/api/preview/local/status
POST /admin/api/preview/local/stop
```

すべて既存admin auth + CSRF境界の内側です。status APIはprocess、workspace、last activityだけを返し、browser tabのowner情報を持ちません。

## 旧preview方式との違い

`PREVIEW_URL`、`HUGO_SERVER_PORT`、`HUGO_SERVER_BIND`はIssue #30以前のpath-prefix preview用legacy設定で、新Local Live Previewには使用しません。

```text
旧: /admin/preview/tech/...
新: https://tech.preview.example.com/...
```

詳細は[Local Live Preview設計](../architecture/local-live-preview-design.md)を参照してください。
