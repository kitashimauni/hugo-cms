# 本文プレビューとデプロイプレビュー

## 決定

HomeCMSのプレビューは、CMS内の安全な「本文プレビュー」と、外部providerが生成する「デプロイプレビュー」に分離する。CMSプロセスはHugo/Eleventyのpreview serverを起動せず、`/admin/preview/:site/*` reverse proxyも提供しない。

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

- provider tokenは設定された環境変数名からserver側だけで読み、browserへ返さない
- Cloudflare tokenは対象Pages projectに必要な最小権限とする
- preview deploymentは既定で公開され得るため、未公開contentにはCloudflare Accessを設定する
- `noindex`は認証の代替にしない
- Access付きURLはiframeへ埋めず、新しいtabで開く
- production secretやproduction DB更新権限をpreview buildへ渡さない
- UIはAccess未保護設定を明示的に警告する

## ライフサイクルと競合

draft IDはbrowser sessionとsite/article pathの組み合わせごとに生成する。`crypto.randomUUID()`が利用できない非HTTPS環境では`crypto.getRandomValues()`からUUID v4を生成し、暗号学的乱数源がない場合はfail closedとする。同じdraftの同時更新は拒否し、複数site・複数draftのstateを混在させない。同じdraft IDを別記事へ再利用することも拒否する。

preview更新は既存preview commitではなく、その時点のlocal production branchをbaseにして対象pathsだけを一時indexへ適用する。production branchが進んでnon-fast-forward更新になる場合は、前回commitを期待値とする`--force-with-lease`でdraft branchだけを更新し、失敗時はlocal draft refをrollbackする。commit SHAでdeploymentを照合するため、providerがbranch aliasを新しいbuildへ切り替えている途中でも誤ったpreviewを表示しない。

## 移行

`preview_url`、`hugo_server_bind`、`hugo_server_port`はローカルpreview用の旧設定であり、本文/デプロイプpreviewでは使用しない。`/admin/preview/:site/*`、`POST /admin/api/build`、`POST /admin/api/build/restart`は廃止する。generator runtimeは明示的なbuild/content作成で引き続き使用できるが、CMS起動時にはサイト固有コードを実行しない。
