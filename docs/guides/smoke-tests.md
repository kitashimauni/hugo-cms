# Smoke Test Checklist

最終更新日: 2026-07-13

大きめの変更やSite Registry変更後に、最低限確認する項目です。

リリース候補PRでは、ここに加えて[リリース前チェックリスト](release-checklist.md)も確認してください。

## 1. 共通

```powershell
go test ./...
go vet ./...
go build -buildvcs=false ./...
npm run test:js
git diff --check
```

miseを使う場合は、同じ検証をまとめて実行できます。

```powershell
mise run check
```

`mise run check`はGoのtest/vet/buildに加えて、Node標準テストランナーによるフロントエンドの軽量テストも実行します。

ブラウザでは以下を確認します。

- `/admin/login` からログインできる
- `/health` が `200`
- `/ready` がdefault siteの状態を返す
- `/admin/api/sites` がSite Registryを返す

## 2. 単一Hugoサイト

`SITES_CONFIG_PATH`を未設定にして確認します。

- 記事一覧が表示される
- 記事を開ける
- front matter formが表示される
- 保存できる
- Diffが表示される
- 画像メディア一覧が表示される
- スニペットが `<REPO_PATH>/.vscode/md.code-snippets` から読み込まれる
- Preview iframe が `/admin/preview/default/...` で表示される
- Restart Previewが成功する

## 3. 複数サイト

例:

```yaml
default_site: techblog
sites:
  - id: techblog
    name: Tech Blog
    repo_path: D:/sites/techblog
    generator: hugo
    content_dir: content
    static_dir: static
    public_dir: public
    hugo_server_port: "1314"

  - id: notes
    name: Notes
    repo_path: D:/sites/notes
    generator: eleventy
    content_dir: src
    static_dir: public-assets
    public_dir: _site
    hugo_server_port: "1315"
```

確認項目:

- サイドバーのSite selectorで切り替えられる
- 切り替え後、記事一覧が選択サイトの内容になる
- `/admin/api/articles?site=<id>` が対象サイトの内容を返す
- `/admin/api/config?site=<id>` の `_cms.site_id` が対象サイトになる
- `.homecms.yml`があるサイトでは`/admin/api/config?site=<id>` の `_cms.config_source` が `.homecms.yml` になる
- 設定に注意点がある場合、`/admin/api/config?site=<id>` の `_cms.warnings` と管理画面サイドバーに表示される
- legacy `static/admin/config.yml` を使うサイトでは互換読み込みwarningが表示される
- 記事の作成、保存、Diff、削除が選択サイトの`content_dir`配下だけを対象にする
- メディア一覧、アップロード、削除、raw配信が選択サイトの`static_dir`/記事別メディア設定だけを対象にする
- `static_media_dir`未指定かつ`.homecms.yml`の`media.folder`があるサイトでは、そのフォルダへstatic media uploadされ、`media.public_path`がMarkdown挿入パスに使われる
- スニペットが選択サイトの`snippet_paths`または`<repo_path>/.vscode/md.code-snippets`から読み込まれる
- Preview iframe が `/admin/preview/<site_id>/...` を参照する
- 初回previewでも、preview process起動直後のport未listenによる一時的な`502`にならない
- preview内のroot-relative URL (`/images/foo.png`, `/about/`など) が同じsiteの`/admin/preview/<site_id>/...`へリダイレクトされる
- サイトごとに別のpreview processが起動する
- Restart Previewが選択サイトのpreviewだけを再起動する
- `hugo_server_bind` + `hugo_server_port` が重複しているSite Registryは起動時に拒否される
- `0.0.0.0`や`::`のwildcard bindは、同じportの具体bindとも衝突として拒否される

## 4. Eleventy

対象リポジトリには以下が必要です。

- `package.json`
- `@11ty/eleventy` dependency
- lockfile (`package-lock.json`, `pnpm-lock.yaml`, `yarn.lock`, `bun.lock`, `bun.lockb`)
- `content_dir` と `public_dir` の明示

確認項目:

- 記事一覧が`content_dir`配下から取得される
- 記事を保存できる
- Previewが`eleventy --serve`で起動する
- Buildがlockfileに対応するpackage manager経由で実行される
- Dockerでは`tool-bootstrap`がlockfileに対応するfrozen installを完了してからPreview/Buildを実行できる

## 5. Docker

- `.env`がない場合はComposeがapp起動前に明確に失敗する
- appと`tool-bootstrap`のUID/GIDが非rootで、app起動によってhost repoの所有者が変わらない
- host GIDがbase image内の既存groupと重複してもimageをbuildでき、指定した数値UID/GIDで動作する
- UID/GIDに`0`または`00`などのゼロ埋め表現を指定するとimage buildが失敗する
- host portが`127.0.0.1:${HUGO_CMS_HOST_PORT:-8080}`だけに公開され、container内`PORT`が`8080`
- `mise-data`がnamed volumeで、app再作成後も準備済みtoolchainを利用できる
- app起動・再起動では`mise install`やNode.js依存installが実行されない
- `docker compose --profile tools run --rm tool-bootstrap`が`HUGO_CMS_REPOS`のUnix `:`区切りallowlistだけを処理する
- allowlist外の`/data/repos`配下repoはbootstrapされない
- `tool-bootstrap`に`GITHUB_CLIENT_SECRET`、`SESSION_SECRET`などのapp secretが存在しない
- mise設定とlockfileを更新してbootstrapを再実行すると、準備済み環境が安全に更新される
- bootstrap失敗時も既に起動しているappは停止・再起動されない

## 6. Git操作

- Syncが対象サイトのrepoで実行される
- 単一記事publishが対象サイトのcontent pathをcommit対象にする
- publish時に対象サイトの`git_remote`/`git_branch`/`static_dir`設定が使われる
- static directoryが未作成でも単一記事publishが失敗しない
- push失敗後の再publishで、既存commitがremoteへpushされる

## 7. 注意点

- preview bind/portはサイト間で重複させないでください。重複したSite Registryは起動時に拒否されます。
- preview processはCMSサーバー上でサイトのコードを実行します。Site Registryには信頼済みリポジトリだけを登録してください。
- 主要なsite-aware APIは`SiteRuntime`を明示的にサービスへ渡します。default site向け起動・readiness経路と選択siteのAPI結果が混ざらないことを重点的に確認してください。
