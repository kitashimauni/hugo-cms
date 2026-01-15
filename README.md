# Hugo CMS

Hugoサイト用のセルフホスト型ヘッドレスCMSです。GitHub OAuthによる認証、リアルタイムプレビュー、Gitベースのコンテンツ管理を提供します。

## 特徴

- 🔐 **GitHub OAuth認証** - 安全なログインとユーザー制限
- 📝 **リアルタイムプレビュー** - 組み込みHugoサーバーによる即座のプレビュー
- 🖼️ **メディア管理** - ドラッグ&ドロップでの画像アップロード
- 🔄 **Gitワークフロー** - 変更の同期・公開をワンクリックで
- ⚡ **高速キャッシュ** - 並列処理による記事一覧の高速表示
- 🛡️ **セキュリティ** - CSRF保護、パストラバーサル対策、入力検証

## クイックスタート

### 前提条件

- Go 1.21以上
- Hugo (Extended版推奨)
- Git
- GitHub OAuth App

### 1. リポジトリのクローン

```bash
git clone https://github.com/your-username/hugo-cms.git
cd hugo-cms
```

### 2. GitHub OAuth Appの作成

1. [GitHub Developer Settings](https://github.com/settings/developers) にアクセス
2. "New OAuth App" をクリック
3. 以下を設定:
   - **Application name**: Hugo CMS (任意)
   - **Homepage URL**: `http://localhost:8080`
   - **Authorization callback URL**: `http://localhost:8080/admin/auth/callback`
4. Client IDとClient Secretをメモ

### 3. 環境設定

```bash
cp .env.example .env
```

`.env` を編集:

```env
GITHUB_CLIENT_ID=your_client_id
GITHUB_CLIENT_SECRET=your_client_secret
SESSION_SECRET=ランダムな文字列
```

### 4. Hugoサイトの準備

`repo/` ディレクトリにHugoサイトを配置するか、既存のサイトへのパスを `REPO_PATH` で指定します。

CMSの設定ファイルを作成:

```bash
mkdir -p repo/static/admin
```

`repo/static/admin/config.yml` を作成 ([設定例](docs/cms-config.md)):

```yaml
collections:
  - name: "posts"
    label: "記事"
    folder: "content/posts"
    create: true
    slug: "{{year}}{{month}}{{day}}-{{slug}}"
    fields:
      - { label: "タイトル", name: "title", widget: "string" }
      - { label: "日付", name: "date", widget: "datetime" }
      - { label: "下書き", name: "draft", widget: "boolean", default: true }
      - { label: "本文", name: "body", widget: "markdown" }
```

### 5. ビルドと起動

```bash
go build -o hugo-cms .
./hugo-cms
```

ブラウザで http://localhost:8080/admin にアクセス

## 設定リファレンス

すべての設定は環境変数または `.env` ファイルで指定します。

| 環境変数 | 説明 | デフォルト |
|----------|------|-----------|
| `GITHUB_CLIENT_ID` | GitHub OAuth Client ID | (必須) |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth Client Secret | (必須) |
| `SESSION_SECRET` | セッション暗号化キー | (自動生成・非推奨) |
| `ALLOWED_GITHUB_USERS` | 許可するGitHubユーザー名(カンマ区切り) | (空=全員許可) |
| `GITHUB_OAUTH_SCOPES` | OAuthスコープ | `public_repo` |
| `PORT` | サーバーポート | `8080` |
| `APP_URL` | アプリケーションURL | `http://localhost:8080` |
| `REPO_PATH` | Hugoリポジトリのパス | `./repo` |
| `SNIPPET_PATHS` | スニペットファイルのパス(カンマ区切り) | `repo/.vscode/md.code-snippets` |
| `MAX_UPLOAD_SIZE_MB` | 最大アップロードサイズ(MB) | `10` |
| `ARTICLE_MEDIA_DIR` | 記事バンドル内の画像ディレクトリ | (空) |
| `STATIC_MEDIA_DIR` | static内の画像ディレクトリ | (空) |
| `HUGO_SERVER_PORT` | Hugoサーバーポート | `1314` |
| `GIT_USER_NAME` | Gitコミット用ユーザー名 | `Hugo CMS Bot` |
| `GIT_USER_EMAIL` | Gitコミット用メール | `bot@hugo-cms.local` |
| `GIT_BRANCH` | Gitブランチ | `main` |

詳細は [設定ガイド](docs/configuration.md) を参照してください。

## ドキュメント

- [設定ガイド](docs/configuration.md) - 詳細な設定オプション
- [CMS設定](docs/cms-config.md) - コレクションとフィールドの設定
- [スニペット機能](docs/snippets.md) - スニペットの使い方と設定
- [アーキテクチャ](docs/architecture.md) - システム構成と実装詳細
- [API リファレンス](docs/api.md) - REST API仕様
- [デプロイ](docs/deployment.md) - 本番環境へのデプロイ方法

## プロジェクト構造

```
hugo-cms/
├── main.go              # エントリーポイント、ルーティング
├── pkg/
│   ├── config/          # 設定管理
│   ├── handlers/        # HTTPハンドラー
│   │   ├── api.go       # 記事API
│   │   ├── auth.go      # 認証・CSRF
│   │   ├── media.go     # メディアAPI
│   │   ├── health.go    # ヘルスチェック
│   │   └── errors.go    # エラーレスポンス
│   ├── models/          # データモデル
│   └── services/        # ビジネスロジック
│       ├── cache.go     # 記事キャッシュ
│       ├── file.go      # ファイル操作
│       ├── frontmatter.go # Front Matter処理
│       ├── git.go       # Git操作
│       ├── hugo.go      # Hugoサーバー管理
│       └── media.go     # メディアファイル管理
├── static/              # フロントエンド静的ファイル
│   ├── css/
│   └── js/
│       ├── app.js       # メインアプリケーション
│       ├── api.js       # APIクライアント
│       ├── editor.js    # Markdownエディタ
│       └── ui.js        # UI操作
├── templates/           # HTMLテンプレート
└── repo/                # Hugoサイト (デフォルト)
```

## 開発

### テストの実行

```bash
go test ./pkg/... -v
```

### ビルド

```bash
go build -o hugo-cms .
```

## セキュリティ

- **認証**: GitHub OAuthによるシングルサインオン
- **認可**: `ALLOWED_GITHUB_USERS` によるアクセス制限
- **CSRF保護**: 全てのPOST/PUT/DELETEリクエストにCSRFトークンを要求
- **パス検証**: パストラバーサル攻撃を防止
- **トークン検証**: GitHubトークンの定期的な有効性確認

## ライセンス

MIT License