# 本文プレビューとデプロイプレビュー

## ステータス

現在の実装では、Hugo CMSのプレビューをCMS内の安全な「本文プレビュー」と、外部providerが生成する「デプロイプレビュー」に分離している。Issue #30で旧`/admin/preview/:site/*` path-prefix proxyとローカルgenerator preview processを廃止したため、現時点ではCMSプロセスはHugo/Eleventyのpreview serverを常駐起動しない。

Issue #32では、この2段階を維持したまま、`https://<site-id>.<preview-domain>/`をorigin rootとして使う任意の「Local Live Preview」を第3段階として追加する方針を検討している。これは旧path-prefix proxyの復活ではない。TLS終端、wildcard DNS、DNS-01、Tailscale等のpreview ingressはCMS本体から分離する。

## 現行の決定

本文プレビューは編集中のMarkdownを即時に確認する機能であり、サイトプレビューではない。GoldmarkのGFM拡張で見出し、段落、リスト、引用、コードブロック、表、リンク、画像をHTML化し、Bluemondayでsanitizeする。raw HTML、`javascript:` URL、event handlerは許可しない。既存のローカル画像は、認証済みの`/admin/api/media/raw`へ書き換える。Hugo shortcode、generator固有template、サイトCSS/JavaScript、taxonomy、menu、外部dataは再現しない。

デプロイプレビューは次の境界を持つ。

```text
Editor
  -> save working tree
  -> explicit deployment update
  -> temporary Git index
  -> cms-preview/<draft-id> commit and push
  -> PreviewDeploymentProvider
  -> exact commit deployment status and immutable URL
```

一時Git indexを使うため、CMSはproduction branchのworktreeをcheckoutしない。draftごとにbranchとstateを分離し、同じrepositoryに対するGit writeはrepository lockで直列化する。autosaveやキー入力ではremote buildを起動せず、「デプロイプレビューを更新」の明示操作だけがcommit/pushを行う。

## Local Live Previewとの役割分担

Issue #32の実装後も、previewは次の3段階として扱う。

1. **Markdown本文プレビュー**
   - generatorを実行しない
   - 編集入力を即時に安全表示する
   - theme/layout/shortcodeの再現は目的としない
2. **Local Live Preview**（Issue #32、未実装）
   - Hugo等のgenerator preview processを実際に起動する
   - `<site-id>.<preview-domain>`をsite自身のorigin rootとして利用する
   - theme/layout/shortcode/CSS/JS/LiveReloadを含む編集中の確認を目的とする
   - remote commit/pushを要求しない
3. **Deployment Preview**
   - 明示操作でdraft branchをcommit/pushする
   - 外部providerでbuildし、特定commitの本番同等表示を確認する
   - 公開前の最終確認を目的とする

Local Live PreviewはDeployment Previewの置き換えではなく、Markdown本文プレビューとDeployment Previewの間を補う機能とする。

## Provider契約

providerはhandler/UIから分離し、次の責務を持つ。

- `Trigger`: branch push後のdeployment追跡を開始する
- `Status`: deployment IDまたはcommit SHAから状態を取得する
- `URL`: readyかつ同一commitのimmutable URLを返す
- `Delete`: 不要なdeploymentを削除する
- `Retry`: failed deploymentを明示操作で再試行する（optional拡張）

CMSが公開する状態は`queued`、`building`、`ready`、`failed`に正規化する。ready URLはproviderが返したdeployment固有URLのみを採用し、branch aliasは採用しない。build中・失敗時に過去のready URLを新しいcommitの結果として表示してはならない。

stateにはsite ID、draft ID、正規化済みarticle path、commit対象paths、branch、commit SHA、deployment ID、status、URL、作成・更新時刻を保存する。publish時はrequestのpathから対象を再計算せず、stateをsource of truthとする。tokenはstate、ログ、APIレスポンスへ保存しない。

## Publishとcleanup

production branchへ直接commit/pushする従来Publishは使用しない。readyになったdraft branchからproduction branchへのPull Requestを作成し、レビュー後のmergeを公開操作とする。publishでは同一draft lockの下でprovider status、stateに保存したpathsとworking treeの一致、remote branch SHA、既存または作成したPRの`head.sha`を順に確認し、すべてがpreview済みcommitと一致する場合だけPR URLを返す。draft破棄時はremote branchとprovider deploymentをcleanupする。cleanup失敗はstateに残し、同じ明示操作で再試行できる。

## セキュリティ

### Markdown本文プレビュー / Deployment Preview

- provider tokenは設定された環境変数名からserver側だけで読み、browserへ返さない
- Cloudflare tokenは対象Pages projectに必要な最小権限とする
- preview deploymentは既定で公開され得るため、未公開contentにはCloudflare Access等のアクセス制御を設定する
- `noindex`は認証の代替にしない
- Access付きURLはiframeへ埋めず、新しいtabで開く
- production secretやproduction DB更新権限をpreview buildへ渡さない
- UIはAccess未保護設定を明示的に警告する

### 将来のLocal Live Preview

Issue #32では、ローカルgenerator codeを再び実行するため、次を追加の境界とする。

- Site Registryでallowlistされたsiteだけを起動対象にする
- Host値から任意repository path、port、commandを生成しない
- generatorへ渡す環境変数をallowlistし、CMSのOAuth/session/provider secretを継承させない
- generator portを直接公開せず、preview ingress経由でのみ到達させる
- unknownな`<site-id>.<preview-domain>`は拒否する
- previewをInternet公開することを必須とせず、Tailscale等のprivate network内だけで運用できる
- TLS証明書やDNS provider tokenをCMS自身が保持することを必須にしない

## ライフサイクルと競合

draft IDはbrowser sessionとsite/article pathの組み合わせごとに生成する。`crypto.randomUUID()`が利用できない非HTTPS環境では`crypto.getRandomValues()`からUUID v4を生成し、暗号学的乱数源がない場合はfail closedとする。同じdraftの同時更新は拒否し、複数site・複数draftのstateを混在させない。同じdraft IDを別記事へ再利用することも拒否する。

preview更新は既存preview commitではなく、その時点のlocal production branchをbaseにして対象pathsだけを一時indexへ適用する。production branchが進んでnon-fast-forward更新になる場合は、前回commitを期待値とする`--force-with-lease`でdraft branchだけを更新し、失敗時はlocal draft refをrollbackする。commit SHAでdeploymentを照合するため、providerがbranch aliasを新しいbuildへ切り替えている途中でも誤ったpreviewを表示しない。

Local Live Preview側の編集中stateをworking tree、shadow directory、worktreeのどこへ保持するかはIssue #32で決定する。少なくとも未保存入力がproduction branchのGit状態を意図せず破壊せず、site/draft間でstateが混在しない構成とする。

## 移行

`preview_url`、`hugo_server_bind`、`hugo_server_port`はIssue #30以前のpath-prefix型ローカルpreview用の旧設定であり、現行の本文/デプロイプpreviewでは使用しない。`/admin/preview/:site/*`、`POST /admin/api/build`、`POST /admin/api/build/restart`も廃止済みであり、Issue #32でもこれらをそのまま復活させない。

Issue #32のLocal Live Previewでは、`PREVIEW_DOMAIN`等から`https://<site-id>.<preview-domain>/`を生成する新しい設定契約を別途導入する予定であり、旧`preview_url`等との互換性を前提にしない。実装完了まではgenerator runtimeは明示的なbuild/content作成で引き続き使用できるが、CMS起動時にLocal Live Previewを自動起動する現行仕様は存在しない。
