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

セッションCookieの暗号化に使用するキーです。

```bash
# 生成例
openssl rand -base64 32
```

**注意**: 設定しない場合、起動時にランダムなキーが生成されますが、サーバー再起動でセッションが無効になります。

### 認証・セキュリティ

#### `ALLOWED_GITHUB_USERS`

CMSへのアクセスを許可するGitHubユーザー名のリスト(カンマ区切り)。

```env
# 特定のユーザーのみ許可
ALLOWED_GITHUB_USERS=user1,user2,user3

# 空の場合は全認証ユーザーを許可 (非推奨)
ALLOWED_GITHUB_USERS=
```

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

#### `CSRF_SECRET`

CSRFトークン生成用のシークレット。設定しない場合は自動生成されます。

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

PORT=8080
APP_URL=http://localhost:8080
REPO_PATH=./repo

HUGO_SERVER_PORT=1314
```

### 本番環境 (単一ユーザー)

```env
GITHUB_CLIENT_ID=Iv1.xxxxxxxx
GITHUB_CLIENT_SECRET=xxxxxxxxxxxxxxxx
SESSION_SECRET=長くてランダムな文字列

ALLOWED_GITHUB_USERS=your-username
CSRF_SECRET=別のランダム文字列

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
SESSION_SECRET=docker-secret

PORT=8080
APP_URL=http://localhost:8080
REPO_PATH=/app/repo

HUGO_SERVER_BIND=0.0.0.0
HUGO_SERVER_PORT=1314
```
