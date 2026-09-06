# Local Live Preview設定ガイド

> Issue #32ではPhase 1/2がmainへmerge済みです。Phase 3 (#35)では、editorの未保存内容をproduction working treeとは別のshadow content workspaceへdebounce反映し、Hugo watcher/LiveReloadへつなぎます。

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

## Hugo process

最初のpreview hostname requestでHugo serverをlazy startします。

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

## Reverse proxy / LiveReload

path prefixは追加しません。

```text
https://tech.preview.example.com/css/main.css
  -> http://127.0.0.1:<internal-port>/css/main.css
```

内部upstreamを指すabsolute `Location`だけを外部preview originへ補正し、HTTP Upgradeを透過してLiveReload WebSocketを通します。

## Phase 3: 未保存editor内容

Local Live Previewが有効なsiteでは、editor変更を約250ms debounceして次へ送ります。

```text
POST /admin/api/preview/local
```

初回update時にrepositoryの`content_dir`をOS temporary directoryへmirrorします。

```text
Editor
  -> 250ms debounce
  -> shadow content workspace
  -> Hugo watcher
  -> rebuild / LiveReload
```

Hugoのsource rootは元repositoryのままです。theme/layout/config/static/assets/modulesは元repoを利用し、`--contentDir`だけshadow directoryのabsolute pathへ切り替えます。

既存の3秒autosaveは保存機能として残りますが、Local Previewの250ms update経路はproduction working tree/Git index/refへ書き込みません。

### revision

各updateには単調増加`revision`を付けます。serverは現在revision以下の古いrequestをno-opにするため、network順序が逆転しても古い本文で上書きされません。

### article bundle resource

workspace作成時点のcontent resourceは初回mirrorに含まれます。その後CMS media APIでcontent directory配下へupload/deleteした画像等もactive shadow workspaceへ同期します。

static配下は元repositoryをHugoが直接参照するためshadow同期しません。media本体の保存成功後にpreview同期だけ失敗した場合は、media操作を失敗扱いにせずserver logへ記録します。

### tab / draft競合

初期実装では同一siteにつきactive Local Live Preview sessionは1つです。別tab/sessionから同siteを更新すると`409 Conflict`となり、既存workspaceを上書きしません。別siteは独立して利用できます。

### article/site切替とcleanup

article/site切替時はin-flight update完了を待ってsessionをreleaseします。

```text
Hugo process stop
  -> shadow workspace delete
```

CMS shutdownでもHugo child停止後にtemporary workspaceを削除します。workspaceは`PREVIEW_STATE_DIR`へ永続化しません。

ブラウザtabを切替操作なしで閉じた場合の確実なreleaseはPhase 4の明示停止/lease recoveryで扱います。

### Hugo Modules

Hugo Modulesで`content`をcustom mountしているsiteは実blog repositoryでsmoke testしてください。必要ならmount-awareなpreview方式を追加します。

## API

editor update:

```text
POST /admin/api/preview/local
```

release:

```text
POST /admin/api/preview/local/release
```

両方とも既存admin auth + CSRF境界の内側です。

## 旧preview方式との違い

`PREVIEW_URL`、`HUGO_SERVER_PORT`、`HUGO_SERVER_BIND`はIssue #30以前のpath-prefix preview用legacy設定で、新Local Live Previewには使用しません。

```text
旧: /admin/preview/tech/...
新: https://tech.preview.example.com/...
```

詳細は[Local Live Preview設計](../architecture/local-live-preview-design.md)を参照してください。
