# Local Live Preview設定ガイド

> Issue #32 Phase 1時点では設定・URL生成・Host validation・process lifecycle契約まで実装済みです。generator serverの実起動、proxy、LiveReload、editor連携はPhase 2以降で実装します。

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

`PREVIEW_DOMAIN`には`*.`、scheme、path、portを含めません。

```text
OK: preview.example.com
NG: *.preview.example.com
NG: https://preview.example.com
NG: preview.example.com:443
```

`PREVIEW_SCHEME`は`http`または`https`だけを使用できます。

## Site Registry

`LOCAL_LIVE_PREVIEW_ENABLED`はsite全体の既定値です。site単位でoverrideできます。

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

有効なsiteについて、`GET /admin/api/sites`のsite設定にはderived valueとして次のようなURLが含まれます。

```json
{
  "preview": {
    "local_preview": {
      "enabled": true,
      "url": "https://tech.preview.example.com/"
    }
  }
}
```

`url`は`sites.yml`へ保存する値ではありません。

## site ID / hostname制約

Local Live Previewで有効にするsite IDは、そのままDNS labelになります。

許可例:

```text
tech
daily-blog
blog2
```

拒否例:

```text
Tech
foo.bar
-tech
tech-
tech_blog
```

Local Live Previewを無効にしているsiteにはこのDNS label制約を追加しません。

site ID単体だけでなく、`<site-id>.<preview-domain>`を連結したhostname全体も有効なDNS名で、253文字以内である必要があります。設定検証、URL生成、Host resolverは同じ制約を使用します。

## wildcard DNS / TLS

想定DNSは1レコードです。

```text
*.preview.example.com -> preview ingress
```

site追加ごとにDNS recordを追加する必要はありません。

hugo-cms自身はTLS certificateやDNS provider credentialを管理しません。たとえば以下の構成を利用できます。

- Tailscale内だけでpreview ingressへ到達可能にする
- DNS-01 challengeで`*.preview.example.com`のwildcard certificateを取得する
- Caddy / Traefik / NginxでTLS terminateする
- 必要に応じてCloudflare等を利用する

これらは外部ingressの責務です。

## 閲覧者認可

**wildcard DNSとHost validationは閲覧者認可ではありません。** Local Live Previewには未公開contentが含まれ、site repository由来のJavaScriptも実行され得るため、preview ingressは必ず独立したアクセス制御の内側に置いてください。

次のいずれかを必須とします。

- Tailscale等のprivate networkからのみ到達可能にする
- Internet reachableにする場合はCloudflare Access等のpreview専用viewer authenticationで保護する

CMSの既存session cookieを`*.preview.example.com`へ共有して認証に使わないでください。preview site由来のJavaScriptへCMS sessionを露出させないためです。

hugo-cmsは外部ingressの認証方式を自動検出・設定しません。この閲覧制御はdeployment時にoperatorが満たす契約です。

## Host validation

Phase 2ではingressから受けたHostをCMSのresolverへ渡します。

`PREVIEW_DOMAIN=preview.example.com`の場合、次を許可します。

```text
tech.preview.example.com
tech.preview.example.com:443
```

次は拒否します。

```text
preview.example.com
foo.tech.preview.example.com
unknown.preview.example.com
tech.preview.example.com.evil.example
foo.example.com
```

Hostからrepository path、generator command、internal portを作ることはありません。Site Registryに登録されたsite IDとのlookupだけに使用します。

## 旧preview設定との違い

`PREVIEW_URL`、`HUGO_SERVER_PORT`、`HUGO_SERVER_BIND`はIssue #30以前のpath-prefix preview用legacy設定です。新Local Live Previewの公開URL・port管理には使用しません。

旧方式:

```text
/admin/preview/tech/...
```

新方式:

```text
https://tech.preview.example.com/...
```

root-relative assetやLiveReloadがsite自身のorigin rootで動作できることが新方式の重要な違いです。

## 内部port

Local Live Previewのgenerator processはPhase 2で`127.0.0.1`だけへbindします。外部URLには内部portを含めません。

初期の自動予約範囲は次です。

```text
14100-14999
```

同一siteには同じ予約slotを返し、別siteへの重複割当を避けます。OS上で使用中のportをprobeし、generator bindに失敗した場合は別portでretryする処理をPhase 2で接続します。

## 編集中データ

Phase 1で、未保存editor stateはproduction working treeやGit worktreeへ書かず、**shadow content workspace**へ反映する方針を確定しました。workspaceの作成・同期・cleanup自体はPhase 3で実装します。

初期契約では同一siteのactive Local Live Preview sessionは1つに制限し、別tab/draftから同時更新された場合は混在させず競合として扱います。

詳細は[Local Live Preview設計](../architecture/local-live-preview-design.md)を参照してください。
