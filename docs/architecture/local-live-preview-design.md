# Local Live Preview設計

## ステータス

Issue #32 Phase 1で設計・設定契約を確定し、設定model、derived URL、Host validation、process lifecycle/port reservationの基盤を実装した。

Phase 2ではHugoを対象に、generator processのlazy start/stop、readiness、loopback port retry、hostname reverse proxy、redirect補正、WebSocket/LiveReload中継まで実装する。未保存editor stateをshadow workspaceへ反映する処理はPhase 3の責務として分離する。

Local Live Previewは既存のMarkdown本文プレビュー、Deployment Previewを置き換えず、第3のpreview段階として追加する。

```text
Markdown本文プレビュー
  -> generatorを実行しない即時確認

Local Live Preview
  -> 実際のgenerator/theme/layout/shortcode/CSS/JSを使う編集中確認

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

DNSは`*.preview.example.com`のwildcardを想定し、site追加ごとのDNS変更を要求しない。

TLS、wildcard certificate、DNS-01 challenge、Cloudflare、Tailscale、Caddy/Traefik/Nginx等はpreview ingressの責務であり、hugo-cmsへ証明書秘密鍵やDNS provider credentialを持たせることを必須にしない。

### 閲覧者認可

wildcard DNSやHost validationは閲覧者認可ではない。Local Live Previewは未公開contentとrepository由来のJavaScriptを配信し得るため、**preview ingressは必ずCMSとは独立した閲覧制御の内側に置く**。

許容する運用は次のいずれかとする。

- Tailscale等のprivate networkからのみpreview ingressへ到達可能にする
- Internet reachableにする場合は、Cloudflare Access等のpreview専用viewer authenticationを必須にする

CMSの既存session cookieを`*.preview.example.com`へ共有して認証に使わない。preview側で実行されるsite由来JavaScriptへCMS sessionを露出させないためである。

hugo-cmsは外部ingressの認証方式そのものを実装・検出しないため、この要件はoperatorが満たすdeployment contractとする。preview hostnameのrequestはCMS session middlewareより前で処理し、preview contentへadmin sessionを渡さない。

## 設定契約

全体既定値:

```env
LOCAL_LIVE_PREVIEW_ENABLED=false
PREVIEW_DOMAIN=preview.example.com
PREVIEW_SCHEME=https
```

Site Registryでは全体既定値をoverrideできる。

```yaml
sites:
  - id: tech
    repo_path: /data/repos/tech-blog
    generator: hugo
    preview:
      local_preview:
        enabled: true
```

`preview.local_preview.enabled`を省略した場合は`LOCAL_LIVE_PREVIEW_ENABLED`を継承する。

Local Live Previewを有効にするsite IDはDNS labelとしてそのままhostnameへ使用するため、次を必須とする。

- lowercase ASCII
- `a-z`、`0-9`、`-`のみ
- 先頭・末尾の`-`は禁止
- 1〜63文字

`PREVIEW_DOMAIN`はscheme、`*.`、path、portを含まないDNS名だけを受け付ける。`PREVIEW_SCHEME`は`http`または`https`だけを許可する。

さらに`<site-id>.<preview-domain>`を連結したhostname全体がDNS名として有効で、253文字以内であることを必須とする。設定検証、URL生成、Host resolverは同じ制約を使用する。

Site RegistryをAPIへ返す際、Local Live Previewが有効ならderived valueとして`preview.local_preview.url`を返す。URLはYAMLへ永続化しない。

## Host routing

HTTP `Host`を`ResolveLocalPreviewHost`へ渡し、Site Registryに登録済みかつLocal Live Previewが有効なsiteだけへ解決する。

許可例:

```text
tech.preview.example.com
tech.preview.example.com:443
```

拒否例:

```text
preview.example.com
foo.tech.preview.example.com
tech.preview.example.com.evil.example
unknown.preview.example.com
tech.example.com
```

preview namespaceに属するがinvalid/unknownなHostはCMS admin routeへfall throughさせず404相当で拒否する。Hostからrepository path、generator command、internal portを生成してはならない。Hostから得られる値はSite Registry lookup keyだけとする。

## Generator process lifecycle

`LocalPreviewManager`がPhase 1のstate machineを実processへ接続する。

```text
stopped
  -> starting
  -> ready
  -> stopping
  -> stopped

starting/ready/stopping
  -> failed

failed
  -> starting
  -> stopped
```

### 起動契約

- Local Live Previewは設定だけでCMS起動時に全siteを起動しない
- 最初のpreview requestでlazy startする
- Host validationとSite Registry lookupが成功する前にprocessを起動しない
- Phase 2のruntime実装対象はHugoのみとする
- generator processは`127.0.0.1`へだけbindする
- generator portをhost/public interfaceへ直接公開しない
- child processへ渡す環境変数は既存generator allowlistを使用する
- CMSのOAuth/session/provider secretは継承させない
- CMS shutdown時には起動済みprocessを停止し、終了を待つ
- processが異常終了した場合は`failed`へ遷移し、次requestで再起動できる

Hugoは概ね次相当で起動する。

```text
hugo server
  --source .
  --contentDir <content_dir>
  --bind 127.0.0.1
  --port <internal-port>
  --baseURL https://<site-id>.<preview-domain>/
  --appendPort=false
  --liveReloadPort 443
  --renderToMemory
  --buildDrafts
  --buildFuture
  --watch
  --noHTTPCache
```

HTTP previewでは`--liveReloadPort 80`を使用する。外部preview URLへinternal portを露出させない。

### port allocation

旧`hugo_server_port`のようなsiteごとの手動port指定をLocal Live Previewの標準契約にはしない。

managerが内部rangeからsiteごとにportを予約する。

```text
14100-14999
```

予約候補はloopback bind可能性をprobeしてからprocessを起動する。probeとchild process bindの間のraceで起動に失敗した場合はslotを解放し、別portで有限回retryする。port番号は外部URLへ現れないため、retryしてもbrowser側URLは変化しない。

## Reverse proxy / LiveReload

preview requestはpath prefixを追加・除去せず、そのままHugoへproxyする。

```text
https://tech.preview.example.com/css/main.css
  -> http://127.0.0.1:<internal-port>/css/main.css
```

これによりroot-relative CSS/JS/imageはsite自身のorigin root `/` のまま扱える。

proxyは次を行う。

- external Hostをupstream requestにも保持する
- `X-Forwarded-Host` / `X-Forwarded-Proto`をCMS側で再構成する
- internal `127.0.0.1:<port>` / `localhost:<port>`を指すabsolute `Location`だけをexternal preview originへ書き換える
- unrelatedなexternal redirectは書き換えない
- HTTP Upgradeを透過し、Hugo LiveReloadのWebSocketを通す

## 編集中state: shadow content workspace

### 決定

Local Live Previewの未保存編集はproduction repositoryのworking treeへキー入力ごとに書き込まず、**shadow content workspace**へ反映する。

Git worktreeをLocal Live Previewの基本方式にはしない。

理由:

- Live Previewはcommit/branchを必要としない編集UI機能であり、Git ref/index操作を増やす必要がない
- Deployment Previewはすでにtemporary index + draft branchというGit境界を持っており、Live PreviewまでGit lifecycleへ結合しない方が責務が明確
- 保存済みworking treeと未保存editor stateを分離できる
- preview破棄時に一時directoryを削除するだけでcleanupできる

### workspaceの内容

初期実装ではsiteの`content_dir`をshadow workspaceへmirrorし、editorの未保存変更はshadow側の記事だけへ適用する。generatorのsource、layout、theme、config、static/assets等は登録済みrepositoryを基準にする。

Phase 2時点のHugo processは保存済みrepository contentを使用する。Phase 3でshadow content directoryをpreview commandへ渡し、editor変更をdebounce反映する。

shadow workspaceのrootはCMS管理領域に置き、site IDやarticle pathを直接filesystem pathとして連結せず既存のpath validationと同等の境界を設ける。

### 同一siteの複数editor

`<site-id>.<preview-domain>`はsiteごとに1つのhostnameなので、初期実装では**同一siteにつきactive Local Live Preview sessionを1つ**に制限する。別draft/tabから同時にshadow stateを更新しようとした場合は混在させず競合として拒否する。

将来、複数sessionを同一hostnameで安全にroutingする仕組みを導入する場合のみ、この制約を緩和する。

## Phase 3への契約

Phase 3は以下を実装する。

- shadow content workspace作成/同期/cleanup
- editor変更のdebounce反映
- Hugo watcherによる再build
- 保存操作とshadow stateの同期
- 同一site concurrent preview sessionの競合拒否
- abnormal shutdown後のstale workspace cleanup

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
- unknown site IDは404相当で拒否する
- Local Live Preview無効siteは起動しない
- Host値をcommand/path/portへ直接変換しない
- generatorは明示的に登録されたrepositoryだけで実行する
- generator child environmentはallowlistし、CMS secretを渡さない
- internal generator serverはloopback bindのみ
- TLS/DNS credentialをCMSへ要求しない
- Local Live Previewはrepository内generator codeを実行するため、Markdown本文プレビューより広いtrust boundaryである
