# リリース前チェックリスト

このCMSをリリース可能な状態にする前に確認する項目です。機能追加PRではなく、リリース候補を作るPRではこのチェックリストを基準にします。

## 1. 自動検証

ローカルでは次を通します。

```powershell
mise run check
git diff --check
```

`mise` を使わない場合は個別に実行します。

```powershell
go test ./...
go vet ./...
go build -buildvcs=false ./...
npm run test:js
git diff --check
```

## 2. 設定検証

代表的なサイト構成で `/admin/api/config` を確認します。

- `.homecms.yml` の `_cms.config_source` が `.homecms.yml`
- legacy `static/admin/config.yml` の `_cms.config_source` が実ファイルに対応
- warningがない設定では `_cms.warnings` が `[]`
- warningがある設定ではサイドバーに表示される
- `content_dir` / `static_dir` / `public_dir` / `site_generator` / `site_id` が選択サイトの値

## 3. 単一サイトの基本動作

- ログインできる
- 記事一覧が表示される
- 記事を開く、保存する、Diffを見る、削除する
- 新規作成が `content_dir` 配下に作成される
- メディア一覧、アップロード、削除、Markdown挿入が動く
- スニペット挿入が動く
- 安全な本文プレビューが保存前入力へ追従する
- 明示操作だけがdraft branchをcommit/pushする
- ready deploymentからPRを作成し、production branchへ直接pushしない

## 4. 複数サイトの基本動作

Site Registryありで、少なくとも2サイトを用意して確認します。

- Site selectorで切り替えられる
- 記事一覧、記事取得、作成、保存、削除が選択サイトだけを対象にする
- APIの `?site=<site_id>` と `X-CMS-Site` が同じ結果になる
- default siteの `/ready` と選択site APIが混ざらない
- siteごとの本文preview image pathとdeployment stateが混ざらない
- siteごとの snippets / media / Git設定が使われる

## 5. Hugo / Eleventy

Hugoサイトでは次を確認します。

- `CONTENT_DIR` / Site Registryの `content_dir` を変えた場合も `hugo build` が対象contentを読む
- `hugo new content <path>` のfallback作成が期待通り

Eleventyサイトでは次を確認します。

- `package.json` とlockfileがある
- lockfileに対応するpackage managerで明示buildされる

デプロイプレビューでは次を確認します。

- Cloudflare Pagesが`cms-preview/*`をbuild対象にする
- CMS statusとimmutable URLが同じcommit SHAに対応する
- Access未保護警告、failed retry、discard cleanupが動く

## 6. Docker

- `bash -n deploy/*.sh`と`docker compose config --quiet`が成功する
- imageを実際にbuildできる
- appが非rootで動作し、repoを再帰`chown`しない
- base image内に存在するGIDを指定してもbuildでき、runtimeの数値UID/GIDが指定値と一致する
- UID/GIDの`0`と`00`などのゼロ埋め表現がbuild時に拒否される
- `.env`が必須で、container内`PORT=8080`、host公開がloopbackのみ
- 推奨配置が`$HOME/hugo-cms`で、`/opt`や`/srv`の親directory全体を`chown`する手順がない
- `mise-data`がnamed volumeとして永続化する
- `tool-bootstrap`が`tools` profileのone-shotで、app secretを受け取らない
- `HUGO_CMS_REPOS`へUnixの`:`区切りで列挙したrepoだけをtrust・準備する
- Hugoの`mise install`と、Eleventyのlockfile別frozen installを確認する
- app再起動ではbootstrapが暗黙実行されない
- `/health`のcontainer smoke testが成功する

## 7. PR確認

PR本文には次を含めます。

- 変更概要
- 動作確認コマンド
- 手動確認した範囲
- リリース時の注意点や既知の未対応

リリース候補PRは、CIとレビューが通ったあとに draft を解除します。
