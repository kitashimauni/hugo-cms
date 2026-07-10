# 設定ガイド

Hugo CMSの詳細な設定オプションについて説明します。

## 環境変数

### 必須設定

#### `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET`

GitHub OAuth Appの認証情報です。

1. [GitHub Developer Settings](https://github.com/settings/developers) にアクセス
2. "New OAuth App" をクリック
3. 設定を入力:
   - **Homepage URL**: アプリケーションのURL
   - **Authorization callback URL**: `{APP_URL}/admin/auth/callback`

#### `SESSION_SECRET`

セッションCookieの署名鍵と暗号化鍵の導出に使用する、32文字以上の秘密値です。

```bash
# 生成例
openssl rand -base64 32
```

**注意**: `GIN_MODE=release`では必須です。開発モードで設定しない場合は起動時にランダムなキーを生成するため、サーバー再起動でセッションが無効になります。

### 認証・セキュリティ

#### `ALLOWED_GITHUB_USERS`

CMSへのアクセスを許可するGitHubユーザー名のリスト(カンマ区切り)。

```env
# 特定のユーザーのみ許可
ALLOWED_GITHUB_USERS=user1,user2,user3

# 1人以上のユーザー指定が必須
ALLOWED_GITHUB_USERS=user1
```

未設定または空の場合、CMSは安全のため起動を拒否します。ローカル開発で全GitHubユーザーを明示的に許可する場合だけ、次を設定できます。

```env
ALLOW_ALL_GITHUB_USERS=true
```

この設定を本番環境で使用してはいけません。

#### `GITHUB_OAUTH_SCOPES`

GitHubへのアクセス権限を指定します。

| スコープ | 説明 |
|---------|------|
| `public_repo` | 公開リポジトリのみ (デフォルト・推奨) |
| `repo` | 公開・非公開リポジトリ両方 |

```env
# プライベートリポジトリを使用する場合
GITHUB_OAUTH_SCOPES=repo
```

**重要**: スコープを変更した場合、既存ユーザーは再ログインが必要です。

### アプリケーション設定

#### `PORT`

サーバーが待ち受けるポート番号。デフォルト: `8080`

#### `APP_URL`

アプリケーションの公開URL。OAuth リダイレクトやプレビューURLの生成に使用。

```env
# ローカル開発
APP_URL=http://localhost:8080

# 本番環境
APP_URL=https://cms.example.com
```

#### `REPO_PATH`

Hugoサイトのリポジトリパス。相対パスまたは絶対パスで指定。

```env
# 相対パス (hugo-cmsディレクトリからの相対)
REPO_PATH=./repo

# 絶対パス
REPO_PATH=/var/www/my-hugo-site
```

#### `SITE_GENERATOR`

対象サイトのジェネレーター。デフォルトは`hugo`です。

```env
SITE_GENERATOR=hugo
# または
SITE_GENERATOR=eleventy
```

`eleventy`を指定する場合、対象リポジトリには`package.json`とlockファイル
（`package-lock.json`、`pnpm-lock.yaml`、`yarn.lock`など）が必要です。
CMSは初期対応として任意のnpm scriptではなく、lockfileに対応するpackage managerでローカル依存のEleventyを実行します。

#### `CONTENT_DIR` / `STATIC_DIR` / `PUBLIC_DIR`

対象リポジトリ内の主要ディレクトリ。Hugoの標準構成では変更不要です。

```env
CONTENT_DIR=content
STATIC_DIR=static
PUBLIC_DIR=public
```

Eleventyなどで入力・出力ディレクトリが異なる場合は明示します。

```env
SITE_GENERATOR=eleventy
CONTENT_DIR=src
STATIC_DIR=public-assets
PUBLIC_DIR=_site
```

#### `SITES_CONFIG_PATH`

複数サイト対応へ向けたSite Registry設定ファイル。未指定の場合は従来通り、環境変数で定義した単一のdefault siteだけを使います。

```env
SITES_CONFIG_PATH=./sites.yml
```

設定例:

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
    snippet_paths:
      - .vscode/md.code-snippets

  - id: notes
    name: Notes
    repo_path: D:/sites/notes
    generator: eleventy
    content_dir: src
    static_dir: public-assets
    public_dir: _site
    hugo_server_port: "1315"
    snippet_paths:
      - .vscode/md.code-snippets
```

Site Registryを設定すると、管理画面のサイドバーにサイトセレクタが表示されます。記事、CMS設定、メディア、スニペット、Git操作、プレビューは選択中サイトを対象にします。

HTTP APIでは、`?site=<site_id>`または`X-CMS-Site: <site_id>`で対象サイトを指定できます。未指定の場合は`default_site`が使われます。

### Preview

プレビューはサイトごとに独立したプロセスとして管理されます。選択中サイトのiframeは次の内部proxyを参照します。

```text
/admin/preview/<site_id>/<page-path>
```

サイトごとに`hugo_server_bind`と`hugo_server_port`の組み合わせを重複しない値にしてください。重複がある場合、CMSは起動時にSite Registryを不正として拒否します。Hugoでは`hugo server`、Eleventyではlockfileに対応するpackage manager経由の`eleventy --serve`を使用します。

初回preview requestで対象サイトのpreview processを起動した場合、CMSはproxyする前にpreview portが実際に接続可能になるまで短時間待機します。これにより、process起動直後にiframeが`502 Preview unavailable`になる揺らぎを抑えます。

preview iframe内のページ・画像・CSSなどが`/images/foo.png`のようなroot-relative URLを要求した場合も、直前のpreview URLから選択サイトを判定し、同じ`/admin/preview/<site_id>/...`配下へリダイレクトします。これにより非defaultサイトのpreviewがdefaultサイトのroot proxyへ落ちることを避けます。

### site registry項目

| 項目 | 必須 | 説明 |
|---|---:|---|
| `id` | はい | サイトID。UI、API、preview routeで使用 |
| `name` | いいえ | UI表示名。未指定時は`id` |
| `repo_path` | はい | 対象リポジトリ |
| `generator` | いいえ | `hugo`または`eleventy` |
| `content_dir` | いいえ | Markdown記事のルート。デフォルト`content` |
| `static_dir` | いいえ | 静的ファイルのルート。デフォルト`static` |
| `public_dir` | いいえ | build出力先。デフォルト`public` |
| `hugo_server_port` | いいえ | previewプロセスのポート。`hugo_server_bind`との組み合わせはサイト間で重複不可 |
| `hugo_server_bind` | いいえ | preview bind address。デフォルト`127.0.0.1` |
| `snippet_paths` | いいえ | スニペットファイル。相対パスは`repo_path`基準 |

### スニペット設定

#### `SNIPPET_PATHS`

Markdown編集時に使用するスニペットファイルのパス。カンマ区切りで複数指定可能。
ファイル形式は VS Code のスニペット形式 (`.code-snippets` または `.json`) に準拠します。
`scope` プロパティに `markdown` が含まれるか、`scope` が未指定（グローバル）のスニペットのみが読み込まれます。

単一サイト構成のデフォルト: `<REPO_PATH>/.vscode/md.code-snippets`

Site Registry利用時は、サイトごとに`snippet_paths`を指定できます。未指定の場合は各サイトの`<repo_path>/.vscode/md.code-snippets`を読み込みます。

```env
# 複数ファイルを指定
SNIPPET_PATHS=repo/.vscode/global.code-snippets,repo/.vscode/md.code-snippets
```

Site Registryでの例:

```yaml
sites:
  - id: techblog
    repo_path: D:/sites/techblog
    snippet_paths:
      - .vscode/global.code-snippets
      - .vscode/md.code-snippets
```

### メディア設定

#### `MAX_UPLOAD_SIZE_MB`

アップロード可能な最大ファイルサイズ (MB単位)。デフォルト: `10`

```env
# 50MBまで許可
MAX_UPLOAD_SIZE_MB=50
```

#### `ARTICLE_MEDIA_DIR`

記事のPage Bundle内で画像を保存するサブディレクトリ。

```env
# content/posts/my-post/src/image.jpg に保存
ARTICLE_MEDIA_DIR=src

# content/posts/my-post/image.jpg に保存 (空の場合)
ARTICLE_MEDIA_DIR=
```

#### `STATIC_MEDIA_DIR`

`static/` 内で画像を保存するサブディレクトリ。

```env
# static/images/uploaded.jpg に保存
STATIC_MEDIA_DIR=images

# static/uploaded.jpg に保存 (空の場合)
STATIC_MEDIA_DIR=
```

### Hugoサーバー設定

#### `HUGO_SERVER_PORT`

内部Hugoサーバーのポート。デフォルト: `1314`

CMSはHugoサーバーをバックグラウンドで起動し、プレビュー機能を提供します。

#### `HUGO_SERVER_BIND`

Hugoサーバーがバインドするアドレス。デフォルト: `127.0.0.1`

```env
# ローカルのみ (デフォルト・推奨)
HUGO_SERVER_BIND=127.0.0.1

# 全インターフェース (Docker等で必要な場合)
HUGO_SERVER_BIND=0.0.0.0
```

### Git設定

#### `GIT_USER_NAME` / `GIT_USER_EMAIL`

コミット時に使用するGit identity。

```env
GIT_USER_NAME="Hugo CMS Bot"
GIT_USER_EMAIL="bot@hugo-cms.local"
```

#### `GIT_BRANCH`

操作対象のGitブランチ。デフォルト: `main`

#### `GIT_REMOTE`

リモートリポジトリ名。デフォルト: `origin`

指定したリモートのURLは`https://github.com/owner/repository.git`形式である必要があります。SSH形式や認証情報を埋め込んだURLは、ログインユーザー以外の認証情報利用やトークン送信先のすり替えを防ぐため拒否されます。

## タイムアウト設定

以下のタイムアウトはソースコード内で定義されています:

| 操作 | タイムアウト | 説明 |
|------|-------------|------|
| Gitローカルコマンド | 60秒 | status, diff等 |
| Gitネットワーク操作 | 5分 | push, pull |
| Hugoビルド | 5分 | サイト全体のビルド |
| Hugo新規コンテンツ | 60秒 | `hugo new` コマンド |
| GitHubトークン検証 | 5分 | 定期的なトークン有効性確認 |

## 設定例

### ローカル開発

```env
GITHUB_CLIENT_ID=Iv1.xxxxxxxx
GITHUB_CLIENT_SECRET=xxxxxxxxxxxxxxxx
SESSION_SECRET=dev-secret-key-change-in-production
ALLOWED_GITHUB_USERS=your-username

PORT=8080
APP_URL=http://localhost:8080
REPO_PATH=./repo

HUGO_SERVER_PORT=1314
```

### 本番環境 (単一ユーザー)

```env
GITHUB_CLIENT_ID=Iv1.xxxxxxxx
GITHUB_CLIENT_SECRET=xxxxxxxxxxxxxxxx
SESSION_SECRET=32文字以上のランダムな文字列

ALLOWED_GITHUB_USERS=your-username

PORT=8080
APP_URL=https://cms.example.com
REPO_PATH=/var/www/hugo-site

MAX_UPLOAD_SIZE_MB=20
ARTICLE_MEDIA_DIR=images
STATIC_MEDIA_DIR=uploads

GIT_USER_NAME="Your Name"
GIT_USER_EMAIL="you@example.com"
GIT_BRANCH=main
```

### Docker環境

```env
GITHUB_CLIENT_ID=Iv1.xxxxxxxx
GITHUB_CLIENT_SECRET=xxxxxxxxxxxxxxxx
SESSION_SECRET=replace-with-at-least-32-random-characters
ALLOWED_GITHUB_USERS=your-username

PORT=8080
APP_URL=http://localhost:8080
REPO_PATH=/app/repo

HUGO_SERVER_BIND=0.0.0.0
HUGO_SERVER_PORT=1314
```
