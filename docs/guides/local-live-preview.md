# Local Live Preview設定ガイド

> Issue #32ではPhase 1/2までmainへmerge済みです。Phase 3では、editorの未保存内容をproduction working treeとは別のshadow content workspaceへdebounce反映し、Hugo watcher/LiveReloadへつなぎます。

## 基本設定

Local Live PreviewのURLはsite IDと共通preview domainから自動生成します。

```env
LOCAL_LIVE_PREVIEW_ENABLED=true
PREVIEW_DOMAIN=preview.example.com
PREVIEW_SCHEME=https
```

`tech` siteなら次のURLになります。

```text
https://tech.preview.example.com/
```

`PREVIEW_DOMAIN`には`*.`、scheme、path、portを含めません。`PREVIEW_SCHEME`は`http`または`https`だけを使用できます。

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

  - id: daily
    repo_path: /data/repos/daily-blog
    generator: hugo
    preview:
      local_preview:
        enabled: false
```

`preview.local_preview.enabled`を省略すると`LOCAL_LIVE_PREVIEW_ENABLED`を継承します。有効siteの`GET /admin/api/sites`にはderived `preview.local_preview.url`が含まれます。URLはSite Registryへ永続化しません。

## hostname制約

Local Live Previewで有効にするsite IDはlowercase DNS labelとして有効でなければなりません。`<site-id>.<preview-domain>`全体も253文字以内の有効なDNS名である必要があります。

HTTPSではport省略または`:443`、HTTPではport省略または`:80`だけを許可します。non-standard external portはサポートしません。

## wildcard DNS / TLS / viewer authentication

想定DNS:

```text
*.preview.example.com -> preview ingress
```

hugo-cms自身はTLS certificateやDNS provider credentialを管理しません。たとえば次の運用を利用できます。

- Tailscale内だけでpreview ingressへ到達可能にする
- DNS-01で`*.preview.example.com`のwildcard certificateを取得する
- Caddy / Traefik / NginxでTLS terminateする
- 必要に応じてCloudflareを利用する

**wildcard DNSとHost validationは閲覧者認可ではありません。** preview ingressはprivate network内に置くか、Internet reachableならCloudflare Access等の独立viewer authenticationで保護してください。

CMS session cookieを`*.preview.example.com`へ共有して認証に使わないでください。

## Host routing

`PREVIEW_DOMAIN=preview.example.com`の場合、次を許可します。

```text
tech.preview.example.com
tech.preview.example.com:443
```

次はpreview namespaceとしてfail closedします。

```text
preview.example.com
foo.tech.preview.example.com
unknown.preview.example.com
tech.preview.example.com:8443
tech.preview.example.com:evil
```

Hostからrepository path、generator command、internal portを作ることはありません。Site Registry lookupだけに使用します。

## Hugo process

最初のpreview hostname requestでsiteごとのHugo serverをlazy startします。

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

Hugoは`127.0.0.1`だけへbindします。内部portは`14100-14999`から自動予約し、起動raceでは別portで有限回retryします。child environmentは既存generator allowlistを使用し、CMS secretを継承しません。

CMS shutdown開始後は新規preview processを起動せず、HTTP serverをdrainしてからchild processを停止します。

## Reverse proxy / LiveReload

preview requestはpath prefixを書き換えません。

```text
https://tech.preview.example.com/css/main.css
  -> http://127.0.0.1:<internal-port>/css/main.css
```

内部upstreamを指すabsolute `Location`だけを外部preview originへ補正します。HTTP Upgradeも透過するため、Hugo LiveReload WebSocketを同じhostnameで利用できます。

## Phase 3: 未保存editor内容

### 動作

Local Live Previewが有効なsiteでは、editor変更を約250ms debounceして次のAPIへ送ります。

```text
POST /admin/api/preview/local
```

初回update時にrepositoryの`content_dir`をOS temporary directoryへmirrorし、対象記事だけshadow側へ反映します。

```text
Editor
  -> 250ms debounce
  -> shadow content workspace
  -> Hugo watcher
  -> rebuild / LiveReload
```

Hugoのsource rootは元repositoryのままなので、theme/layout/config/static/assetsは通常どおり元repoを利用します。`--contentDir`だけshadow directoryのabsolute pathへ切り替えます。

既存の3秒autosaveは保存機能として残りますが、250msのLocal Preview update経路はproduction working treeへ書き込みません。

### revision

各Local Preview updateには単調増加`revision`を付けます。network responseの順序が逆転しても、serverは現在revision以下の古いupdateを適用しません。

### tab / draft競合

初期実装では同一siteにつきactive Local Live Preview sessionは1つです。

別tabから同じsiteを編集し始めた場合、後から来たsessionは`409 Conflict`となり、既存workspaceを上書きしません。別siteは独立して利用できます。

### article/site切替

記事またはsiteを切り替える際は、in-flight update完了を待ってからLocal Preview sessionをreleaseします。

releaseは次の順で行います。

```text
Hugo process stop
  -> shadow workspace delete
```

これによりHugo watcherが参照中のdirectoryを先に削除しません。

CMS shutdownでもHugo child停止後にtemporary workspaceをcleanupします。

### workspaceの永続性

shadow workspaceは`PREVIEW_STATE_DIR`へ保存しません。CMS process単位のephemeral temporary directoryです。Git branch/ref/indexも使用しません。

### page resourceの注意

workspace作成時点のcontent directoryはresource画像等も含めてmirrorされます。ただしworkspace作成後にarticle bundleへ新規upload/deleteしたpage resourceはPhase 3初期実装では自動同期しません。必要性を実環境で確認後、media API連携として追加します。

Hugo Modulesでcontentをcustom mountしているsiteも実blog repositoryでのsmoke test対象です。必要ならmount-awareなpreview方式を追加します。

## 旧preview方式との違い

旧設定`PREVIEW_URL`、`HUGO_SERVER_PORT`、`HUGO_SERVER_BIND`はIssue #30以前のpath-prefix preview用です。新Local Live Previewには使用しません。

旧:

```text
/admin/preview/tech/...
```

新:

```text
https://tech.preview.example.com/...
```

詳細は[Local Live Preview設計](../architecture/local-live-preview-design.md)を参照してください。
