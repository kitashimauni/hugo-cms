# Smoke Test Checklist

最終更新日: 2026-07-10

大きめの変更やSite Registry変更後に、最低限確認する項目です。

## 1. 共通

```powershell
go test ./...
go vet ./...
go build -buildvcs=false ./...
git diff --check
```

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
- スニペットが選択サイトの`snippet_paths`または`<repo_path>/.vscode/md.code-snippets`から読み込まれる
- Preview iframe が `/admin/preview/<site_id>/...` を参照する
- 初回previewでも、preview process起動直後のport未listenによる一時的な`502`にならない
- preview内のroot-relative URL (`/images/foo.png`, `/about/`など) が同じsiteの`/admin/preview/<site_id>/...`へリダイレクトされる
- サイトごとに別のpreview processが起動する
- Restart Previewが選択サイトのpreviewだけを再起動する
- `hugo_server_bind` + `hugo_server_port` が重複しているSite Registryは起動時に拒否される

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

## 5. Git操作

- Syncが対象サイトのrepoで実行される
- 単一記事publishが対象サイトのcontent pathをcommit対象にする
- static directoryが未作成でも単一記事publishが失敗しない
- push失敗後の再publishで、既存commitがremoteへpushされる

## 6. 注意点

- preview bind/portはサイト間で重複させないでください。重複したSite Registryは起動時に拒否されます。
- preview processはCMSサーバー上でサイトのコードを実行します。Site Registryには信頼済みリポジトリだけを登録してください。
- 現在は互換性維持のため、内部サービスの一部がprocess-wide runtime bridgeを使います。scope外のruntime readsはlockで保護し、今後は明示的な`SiteRuntime`引数へ段階移行します。
