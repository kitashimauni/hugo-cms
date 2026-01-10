# CMS設定 (config.yml)

Hugo CMSのコレクションとフィールドの設定方法について説明します。

設定ファイルは `{REPO_PATH}/static/admin/config.yml` に配置します。

## 基本構造

```yaml
# (オプション) バックエンド設定 - 互換性のため残していますが、Hugo CMSでは無視されます
backend:
  name: github
  repo: username/repo
  branch: main

# (オプション) ロゴURL
logo_url: "https://example.com/logo.png"

# (オプション) デフォルトのメディアフォルダ
media_folder: "/static/images"
public_folder: "/images"

# コレクション定義 (必須)
collections:
  - name: "posts"
    # ... コレクション設定
```

## コレクション設定

### 必須フィールド

```yaml
collections:
  - name: "posts"           # 内部識別子 (URLに使用)
    label: "ブログ記事"      # UI表示名
    folder: "content/posts" # コンテンツの保存先
    fields:                 # フィールド定義
      - { label: "タイトル", name: "title", widget: "string" }
```

### オプションフィールド

```yaml
collections:
  - name: "posts"
    label: "ブログ記事"
    folder: "content/posts"
    
    # 新規作成を許可
    create: true
    
    # ファイル名のパターン (変数使用可)
    slug: "{{year}}{{month}}{{day}}-{{slug}}"
    
    # Page Bundleを使用する場合のパス
    path: "{{slug}}/index"
    
    # ファイル拡張子
    extension: "md"
    
    # Front Matterのフォーマット
    format: "yaml-frontmatter"  # yaml-frontmatter, toml-frontmatter, json
    
    # コレクション固有のメディアフォルダ
    media_folder: "{{dirname}}/images"
    public_folder: "images"
    
    fields:
      # ...
```

### スラッグ変数

`slug` と `path` で使用できる変数:

| 変数 | 説明 | 例 |
|------|------|-----|
| `{{slug}}` | タイトルからの自動生成スラッグ | `hello-world` |
| `{{year}}` | 4桁の年 | `2026` |
| `{{month}}` | 2桁の月 | `01` |
| `{{day}}` | 2桁の日 | `10` |
| `{{hour}}` | 2桁の時 | `14` |
| `{{minute}}` | 2桁の分 | `30` |
| `{{second}}` | 2桁の秒 | `00` |
| `{{field_name}}` | 任意のフィールド値 | フィールドによる |

## フィールドウィジェット

### string - テキスト入力

```yaml
- label: "タイトル"
  name: "title"
  widget: "string"
  required: true      # デフォルト: true
  default: ""         # デフォルト値
```

### text - 複数行テキスト

```yaml
- label: "説明"
  name: "description"
  widget: "text"
```

### markdown - Markdownエディタ

```yaml
- label: "本文"
  name: "body"
  widget: "markdown"
```

**注意**: `body` という名前のフィールドはFront Matterではなく、本文として扱われます。

### datetime - 日時選択

```yaml
- label: "公開日"
  name: "date"
  widget: "datetime"
  format: "YYYY-MM-DDTHH:mm:ssZ"  # 保存形式
  date_format: "YYYY-MM-DD"       # 表示形式 (日付)
  time_format: "HH:mm"            # 表示形式 (時刻)
```

### boolean - チェックボックス

```yaml
- label: "下書き"
  name: "draft"
  widget: "boolean"
  default: true
```

### number - 数値入力

```yaml
- label: "優先度"
  name: "weight"
  widget: "number"
  default: 0
  min: 0
  max: 100
  step: 1
  value_type: "int"  # int または float
```

### select - ドロップダウン

```yaml
- label: "カテゴリ"
  name: "category"
  widget: "select"
  options:
    - { label: "技術", value: "tech" }
    - { label: "日記", value: "diary" }
    - { label: "その他", value: "other" }
  default: "tech"
```

### list - リスト入力

```yaml
# シンプルなリスト (タグなど)
- label: "タグ"
  name: "tags"
  widget: "list"
  required: false

# オブジェクトのリスト
- label: "著者"
  name: "authors"
  widget: "list"
  fields:
    - { label: "名前", name: "name", widget: "string" }
    - { label: "メール", name: "email", widget: "string" }
```

### object - ネストしたオブジェクト

```yaml
- label: "SEO設定"
  name: "seo"
  widget: "object"
  fields:
    - { label: "メタ説明", name: "description", widget: "text" }
    - { label: "OGP画像", name: "image", widget: "string" }
```

### image - 画像選択

```yaml
- label: "アイキャッチ"
  name: "featured_image"
  widget: "image"
  required: false
  media_folder: "/static/images"
  public_folder: "/images"
```

### hidden - 隠しフィールド

```yaml
- label: "タイプ"
  name: "type"
  widget: "hidden"
  default: "post"
```

## 設定例

### ブログ記事 (Page Bundle)

```yaml
collections:
  - name: "posts"
    label: "ブログ記事"
    folder: "content/posts"
    create: true
    path: "{{year}}{{month}}{{day}}-{{slug}}/index"
    media_folder: "{{dirname}}/images"
    public_folder: "images"
    format: "yaml-frontmatter"
    fields:
      - { label: "タイトル", name: "title", widget: "string" }
      - { label: "スラッグ", name: "slug", widget: "string" }
      - { label: "公開日", name: "date", widget: "datetime" }
      - { label: "更新日", name: "lastmod", widget: "datetime", required: false }
      - { label: "下書き", name: "draft", widget: "boolean", default: true }
      - { label: "タグ", name: "tags", widget: "list", required: false }
      - { label: "カテゴリ", name: "categories", widget: "list", required: false }
      - { label: "説明", name: "description", widget: "text", required: false }
      - { label: "本文", name: "body", widget: "markdown" }
```

### 固定ページ

```yaml
collections:
  - name: "pages"
    label: "固定ページ"
    folder: "content"
    create: true
    path: "{{slug}}/_index"
    format: "yaml-frontmatter"
    fields:
      - { label: "タイトル", name: "title", widget: "string" }
      - { label: "メニュー順", name: "weight", widget: "number", default: 0 }
      - { label: "本文", name: "body", widget: "markdown" }
```

### 複数コレクション

```yaml
collections:
  - name: "posts"
    label: "ブログ"
    folder: "content/posts"
    create: true
    fields:
      - { label: "タイトル", name: "title", widget: "string" }
      - { label: "日付", name: "date", widget: "datetime" }
      - { label: "本文", name: "body", widget: "markdown" }
  
  - name: "news"
    label: "お知らせ"
    folder: "content/news"
    create: true
    fields:
      - { label: "タイトル", name: "title", widget: "string" }
      - { label: "日付", name: "date", widget: "datetime" }
      - { label: "重要", name: "important", widget: "boolean", default: false }
      - { label: "本文", name: "body", widget: "markdown" }
  
  - name: "authors"
    label: "著者"
    folder: "content/authors"
    create: true
    path: "{{slug}}/_index"
    fields:
      - { label: "名前", name: "title", widget: "string" }
      - { label: "プロフィール", name: "bio", widget: "text" }
      - { label: "アバター", name: "avatar", widget: "image" }
```

## フィルター設定

記事一覧にフィルターを追加できます:

```yaml
view_filters:
  - label: "下書き"
    field: "draft"
    pattern: true
  
  - label: "公開済み"
    field: "draft"
    pattern: false
```

## Netlify CMS / Decap CMSとの互換性

Hugo CMSの設定形式はNetlify CMS (現Decap CMS) と互換性があります。
既存の `config.yml` をそのまま使用できる場合が多いですが、以下の点に注意してください:

### サポートされている機能

- コレクション定義
- 主要なウィジェット (string, text, markdown, datetime, boolean, number, select, list, object, image, hidden)
- スラッグ変数
- メディアフォルダ設定
- フィルター

### サポートされていない機能

- Editorial Workflow
- Git Gateway バックエンド
- 外部メディアライブラリ (Cloudinary, Uploadcare等)
- カスタムウィジェット
- i18n (多言語)

これらの機能が必要な場合は、Decap CMSの使用を検討してください。
