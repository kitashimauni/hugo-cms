# API リファレンス

Hugo CMSのREST API仕様です。

## 認証

すべての `/admin/api/*` エンドポイントは認証が必要です。
`/admin/preview/*` も認証済みadmin route配下で提供されます。

### セッション認証

GitHub OAuthでログイン後、セッションCookieが発行されます。

### CSRF保護

`POST`, `PUT`, `DELETE` リクエストには `X-CSRF-Token` ヘッダーが必要です。

```javascript
// CSRFトークンの取得
const response = await fetch('/admin/api/csrf');
const { token } = await response.json();

// リクエスト時にヘッダーに含める
fetch('/admin/api/article', {
    method: 'PUT',
    headers: {
        'Content-Type': 'application/json',
        'X-CSRF-Token': token
    },
    body: JSON.stringify(data)
});
```

---

## サイト指定

Site Registryを使う場合、site-aware APIはクエリパラメータまたはヘッダーで対象サイトを指定できます。

```http
GET /admin/api/articles?site=techblog
X-CMS-Site: techblog
```

両方が指定された場合はクエリパラメータが優先されます。未指定の場合は`default_site`が使われます。

対象API:

- `/admin/api/articles`
- `/admin/api/article`
- `/admin/api/create`
- `/admin/api/delete`
- `/admin/api/diff`
- `/admin/api/config`
- `/admin/api/snippets`
- `/admin/api/sync`
- `/admin/api/publish`
- `/admin/api/media`
- `/admin/api/media/delete`
- `/admin/api/media/raw`
- `/admin/api/build`
- `/admin/api/build/restart`

---

## 認証API

### GET /admin/auth/login

GitHub OAuth認証を開始します。

**レスポンス**: GitHubの認証ページへリダイレクト

### GET /admin/auth/callback

OAuth認証コールバック。GitHubから呼び出されます。

**クエリパラメータ**:
- `code`: 認証コード
- `state`: CSRF用ステート

**レスポンス**: `/admin` へリダイレクト

### GET /admin/auth/logout

セッションを破棄してログアウトします。

**レスポンス**:
```json
{
    "status": "ok",
    "log": "Logged out"
}
```

### GET /admin/api/csrf

CSRFトークンを取得します。

**レスポンス**:
```json
{
    "token": "abc123..."
}
```

---

## 記事API

### GET /admin/api/articles

全記事の一覧を取得します。

**レスポンス**:
```json
[
    {
        "path": "posts/2026-01-10-hello/index.md",
        "title": "Hello World",
        "date": "2026-01-10T12:00:00Z",
        "draft": true,
        "dirty": false
    },
    // ...
]
```

### GET /admin/api/article

単一の記事を取得します。

**クエリパラメータ**:
- `path` (必須): 記事のパス (content相対)

**レスポンス**:
```json
{
    "path": "posts/2026-01-10-hello/index.md",
    "frontmatter": {
        "title": "Hello World",
        "date": "2026-01-10T12:00:00Z",
        "draft": true,
        "tags": ["blog", "hello"]
    },
    "body": "# Hello\n\nThis is my first post.",
    "format": "yaml"
}
```

**エラーレスポンス**:
```json
{
    "status": "error",
    "code": "NOT_FOUND",
    "message": "Article not found"
}
```

### PUT /admin/api/article

記事を保存します。

**リクエストボディ**:
```json
{
    "path": "posts/2026-01-10-hello/index.md",
    "frontmatter": {
        "title": "Updated Title",
        "date": "2026-01-10T12:00:00Z",
        "draft": false
    },
    "body": "# Updated Content\n\nNew content here."
}
```

**レスポンス**:
```json
{
    "status": "ok",
    "log": "Saved"
}
```

### POST /admin/api/article

新規記事を作成します。

**リクエストボディ**:
```json
{
    "collection": "posts",
    "fields": {
        "title": "New Article",
        "slug": "new-article"
    }
}
```

**レスポンス**:
```json
{
    "status": "ok",
    "data": {
        "path": "posts/20260110-new-article/index.md"
    }
}
```

**エラーレスポンス** (既に存在する場合):
```json
{
    "status": "error",
    "code": "CONFLICT",
    "message": "File already exists"
}
```

### DELETE /admin/api/article

記事を削除します。

**クエリパラメータ**:
- `path` (必須): 記事のパス

**レスポンス**:
```json
{
    "status": "ok",
    "log": "Deleted"
}
```

### GET /admin/api/diff

記事の差分を取得します。

**クエリパラメータ**:
- `path` (必須): 記事のパス

**リクエストボディ** (オプション):
```json
{
    "content": "現在のエディタ内容"
}
```

**レスポンス**:
```json
{
    "diff": "--- HEAD\n+++ Current\n@@ -1,3 +1,3 @@\n...",
    "type": "git"  // "git", "unsaved", or "none"
}
```

---

## サイトAPI

### GET /admin/api/sites

読み込まれたSite Registryを取得します。

**レスポンス**:

```json
{
  "default_site": "techblog",
  "selected_site": "techblog",
  "sites": [
    {
      "id": "techblog",
      "name": "Tech Blog",
      "repo_path": "D:/sites/techblog",
      "generator": "hugo",
      "content_dir": "content",
      "static_dir": "static",
      "public_dir": "public",
      "hugo_server_port": "1314",
      "snippet_paths": ["D:/sites/techblog/.vscode/md.code-snippets"]
    }
  ]
}
```

---

## Preview

### GET /admin/preview/:site/*

選択サイトのpreview processへproxyします。管理画面のiframeが使用する内部routeです。

例:

```text
/admin/preview/techblog/posts/hello/
```

このrouteは必要に応じて対象サイトのpreview processを起動し、Site Registryの`hugo_server_bind`と`hugo_server_port`へproxyします。初回起動時はpreview portが接続可能になるまで短時間待機してからproxyします。サイトIDが存在しない場合は`400`、preview processを起動できない場合やport readiness timeout時は`502`を返します。

iframe内でroot-relative URLへの遷移やasset requestが発生した場合、CMSは`Referer`の`/admin/preview/:site/...`から選択サイトを復元し、同じpreview route配下へ一時リダイレクトします。

---

## Git API

### POST /admin/api/sync

リモートリポジトリから最新の変更を取得します。

**レスポンス**:
```json
{
    "status": "ok",
    "log": "Already up to date."
}
```

### POST /admin/api/publish

変更をリモートリポジトリにプッシュします。

**リクエストボディ** (オプション):
```json
{
    "path": "posts/2026-01-10-hello/index.md"
}
```

- `path`を指定: その記事のみ公開
- `path`省略: 全変更を公開

**レスポンス**:
```json
{
    "status": "ok",
    "log": "--- Git Add ---\n(Success)\n\n--- Git Commit ---\n[main abc1234] Update posts/2026-01-10-hello/index.md via HomeCMS\n..."
}
```

### POST /admin/api/restart

Hugoサーバーを再起動します。

**レスポンス**:
```json
{
    "status": "ok",
    "log": "Hugo Server Restarted"
}
```

### POST /admin/api/build

サイトをビルドします (Hugoサーバー使用時は通常不要)。

**レスポンス**:
```json
{
    "status": "ok",
    "log": "Preview managed by Hugo Server"
}
```

---

## メディアAPI

### GET /admin/api/media

メディアファイル一覧を取得します。

**クエリパラメータ**:
- `mode`: `static` または `content`
- `path` (modeがcontentの場合): 記事のパス

**レスポンス**:
```json
[
    {
        "name": "image.jpg",
        "path": "/images/image.jpg",
        "size": 102400,
        "url": "/admin/api/media/raw?path=static/images/image.jpg",
        "repo_path": "static/images/image.jpg"
    }
]
```

### POST /admin/api/media

メディアファイルをアップロードします。

**Content-Type**: `multipart/form-data`

**フォームフィールド**:
- `file` (必須): アップロードするファイル
- `mode`: `static` または `content`
- `path` (modeがcontentの場合): 記事のパス

**制限**:
- 最大サイズ: `MAX_UPLOAD_SIZE_MB` (デフォルト10MB)
- 許可される拡張子: `.jpg`, `.jpeg`, `.png`, `.gif`, `.webp`, `.mp4`, `.webm`, `.pdf`
- 拡張子と実際のContent-Typeが一致しないファイル、および実行可能コンテンツを含められるSVGは拒否

**レスポンス**:
```json
{
    "name": "image_1704844800.jpg",
    "path": "/images/image_1704844800.jpg",
    "size": 102400,
    "url": "/admin/api/media/raw?path=static/images/image_1704844800.jpg",
    "repo_path": "static/images/image_1704844800.jpg"
}
```

**エラーレスポンス**:
```json
{
    "status": "error",
    "code": "BAD_REQUEST",
    "message": "File too large. Maximum size is 10MB"
}
```

### POST /admin/api/media/delete

メディアファイルを削除します。

**リクエストボディ**:
```json
{
    "repo_path": "static/images/image.jpg"
}
```

**制限**:
- `static/` または `content/` 内のファイルのみ削除可能

**レスポンス**:
```json
{
    "status": "deleted"
}
```

### GET /admin/api/media/raw

メディアファイルを直接取得します (プレビュー用)。

**クエリパラメータ**:
- `path` (必須): ファイルのリポジトリ相対パス

**レスポンス**: ファイルの内容 (適切なContent-Type)

---

## 設定API

### GET /admin/api/config

CMS設定 (config.yml) を取得します。

**レスポンス**:
```json
{
    "collections": [
        {
            "name": "posts",
            "label": "ブログ記事",
            "folder": "content/posts",
            "create": true,
            "fields": [...]
        }
    ],
    "media_folder": "/static/images",
    "public_folder": "/images"
}
```

---

## ヘルスチェックAPI

### GET /health

基本的なヘルスステータスを返します (認証不要)。

**レスポンス**:
```json
{
    "status": "ok",
    "timestamp": "2026-01-10T12:00:00Z",
    "uptime": "1h30m45s",
    "version": "1.0.0"
}
```

### GET /ready

詳細なヘルスチェックを実行します (認証不要)。

**レスポンス (正常時)**:
```json
{
    "status": "ok",
    "timestamp": "2026-01-10T12:00:00Z",
    "uptime": "1h30m45s",
    "checks": {
        "hugo_server": {
            "healthy": true,
            "message": "Hugo server is running"
        },
        "content_dir": {
            "healthy": true,
            "path": "./repo/content"
        },
        "git_repo": {
            "healthy": true
        }
    },
    "system": {
        "goroutines": 15,
        "memory_alloc": 12
    }
}
```

**レスポンス (異常時)**: HTTP 503
```json
{
    "status": "degraded",
    "checks": {
        "hugo_server": {
            "healthy": false,
            "message": "Hugo server is not running"
        }
    }
}
```

---

## エラーコード

| コード | HTTPステータス | 説明 |
|--------|---------------|------|
| `UNAUTHORIZED` | 401 | 認証が必要 |
| `FORBIDDEN` | 403 | アクセス拒否 |
| `BAD_REQUEST` | 400 | 不正なリクエスト |
| `NOT_FOUND` | 404 | リソースが見つからない |
| `CONFLICT` | 409 | リソースが既に存在 |
| `INTERNAL_ERROR` | 500 | サーバー内部エラー |
| `INVALID_CSRF` | 403 | CSRFトークンが無効 |
| `USER_NOT_ALLOWED` | 403 | ユーザーが許可リストにない |
