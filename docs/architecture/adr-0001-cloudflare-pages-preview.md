# ADR-0001: 初期デプロイproviderにCloudflare Pagesを採用する

- 状態: Accepted
- 日付: 2026-08-01

## Context

Git branchから本番相当のstatic siteを確認し、特定commitに対応するdeployment statusとimmutable URLをCMSへ返す必要がある。候補はCloudflare Pages、Workers Builds、Netlify、Vercel、GitHub Actionsである。

## Decision

初期providerはGit連携されたCloudflare Pagesとする。CMSは`cms-preview/<draft-id>`をpushし、Pagesのdeployment APIから`deployment_trigger.metadata.commit_hash`が一致するpreview deploymentを取得する。`latest_stage`を共通statusへ変換し、ready時はdeployment固有の`url`を表示する。

Pagesは非production branchの自動build、commit metadata、deployment固有URL、status/list/get/retry/delete APIを提供し、今回必要な責務と一致する。Workers Buildsは将来providerとして追加できるが、初期実装ではstatic site generatorとの統合が単純なPagesを優先する。

## Consequences

- Pages projectはGitHub repositoryと接続し、`cms-preview/*`をpreview build対象にする必要がある
- API tokenはPages Read/Writeの必要最小権限でserver環境変数へ保存する
- Git pushとPages API反映には時間差があるため、deployment未検出は`queued`として扱う
- branch aliasではなくdeployment固有URLを使用する
- provider interfaceを維持し、Workers Builds等をhandler/UI変更なしで追加できるようにする
