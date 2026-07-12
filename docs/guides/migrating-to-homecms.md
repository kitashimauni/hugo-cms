# `.homecms.yml` 移行ガイド

既存の `static/admin/config.yml` は互換読み込みできますが、新しい機能はリポジトリ直下の `.homecms.yml` を基準にします。新規サイトや複数サイト構成では、`.homecms.yml` への移行を推奨します。

## 移行の考え方

`static/admin/config.yml` は「CMSの管理画面用の静的ファイル」としてサイト内に置かれていました。`.homecms.yml` は「HomeCMSが読むサイト設定」としてリポジトリ直下に置き、Hugo / Eleventy どちらでも同じ場所から読みます。

| 旧設定 | 新設定 | 備考 |
|---|---|---|
| `collections` | `content.collections` | collection定義はほぼそのまま移せます |
| `collections[].folder` | `content.collections[].folder` | リポジトリルート基準。通常は `content/posts` や `src/posts` |
| `collections[].path` | `content.collections[].path` | `{{slug}}` などの変数を使う場合、同名fieldが必要 |
| `collections[].format` | `content.collections[].frontmatter` | `yaml-frontmatter` は `yaml` に寄せます |
| `media_folder` | `media.folder` | リポジトリルート基準。例: `static/images` |
| `public_folder` / `public_path` | `media.public_path` | Markdownへ挿入する公開URL。例: `/images` |
| なし | `preview.url_field` | permalink等からpreview URLを決めたい場合に指定 |

## 手順

1. 対象サイトのリポジトリ直下に `.homecms.yml` を作成する
2. 旧 `collections` を `content.collections` へ移す
3. `media_folder` / `public_folder` を `media.folder` / `media.public_path` へ移す
4. `path` テンプレートで使う変数が `fields` に含まれているか確認する
5. CMSを起動し、`/admin/api/config` の `_cms.warnings` とサイドバーのwarningを確認する
6. 問題がなければ旧 `static/admin/config.yml` を削除する

## 例

旧 `static/admin/config.yml`:

```yaml
media_folder: static/images
public_folder: /images
collections:
  - name: posts
    label: Posts
    folder: content/posts
    path: "{{slug}}"
    format: yaml-frontmatter
    fields:
      - { name: slug, label: Slug, widget: string }
      - { name: title, label: Title, widget: string }
      - { name: body, label: Body, widget: markdown }
```

新 `.homecms.yml`:

```yaml
version: 1

content:
  collections:
    - name: posts
      label: Posts
      folder: content/posts
      path: "{{slug}}"
      frontmatter: yaml
      fields:
        - { name: slug, label: Slug, widget: string }
        - { name: title, label: Title, widget: string }
        - { name: permalink, label: Permalink, widget: string }
        - { name: body, label: Body, widget: markdown }

media:
  folder: static/images
  public_path: /images

preview:
  url_field: permalink
```

## 複数サイトでの注意点

Site Registryを使う場合、`.homecms.yml` は各サイトの `repo_path` 直下に置きます。`folder` や `media.folder` はそのサイトのリポジトリルート基準です。

```yaml
default_site: techblog
sites:
  - id: techblog
    repo_path: D:/sites/techblog
    content_dir: content

  - id: docs
    repo_path: D:/sites/docs
    generator: eleventy
    content_dir: src
```

この例では、`docs` サイトの新規 `.homecms.yml` では `src/posts` のように、そのサイトの `content_dir` 配下を明示することを推奨します。

一方、既存の legacy config から移行する場合、`folder: content/posts` のような旧Hugo前提の値は互換処理で `src/posts` として扱われます。そのため、動作している既存設定を急いで書き換える必要はありません。新しく設定を書く場合や、互換挙動に依存したくない場合は、現在の `content_dir` に合わせた `folder` へ整理してください。

## 移行後の確認

最低限、次を確認してください。

- 記事一覧が表示される
- 新規作成時に期待したパスへ `.md` が作られる
- front matterのlabelやwidgetが維持されている
- static media uploadの保存先と挿入URLが期待通り
- `preview.url_field` を使うサイトでは、保存・preview・restart後も同じURLが表示される
- `_cms.warnings` が空配列 `[]` になる、または残ったwarningの理由を説明できる
