# Hugo CMS

Hugoサイト用のセルフホスト型ヘッドレスCMSです。GitHub OAuthによる認証、安全なMarkdown本文プレビュー、デプロイプレビュー、Gitベースのコンテンツ管理を提供します。

## 特徴

- 🔐 **GitHub OAuth認証** - 安全なログインとユーザー制限
- 📝 **本文プレビュー** - 編集中のMarkdownをsanitizeして即座に確認
- 🚀 **デプロイプレビュー** - draft branchを外部providerでbuildし、公開前に特定commitを確認
- 🖼️ **メディア管理** - ドラッグ&ドロップでの画像アップロード
- 🔄 **Gitワークフロー** - 変更の同期・公開をワンクリックで
- ⚡ **高速キャッシュ** - 並列処理による記事一覧の高速表示
- 🛡️ **セキュリティ** - CSRF保護、パストラバーサル対策、入力検証

> Hugoのtheme/layout/shortcode/CSS/JSを含むローカルLive Previewは現在未実装です。Issue #32で、`https://<site-id>.<preview-domain>/`形式のwildcard subdomainを使う方式を検討しています。旧`/admin/preview/:site/*` path-prefix proxyは復活させません。

## クイックスタート

### 前提条件

- mise（Go 1.24.11のセットアップに使用）
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
SESSION_SECRET=32文字以上のランダムな文字列
ALLOWED_GITHUB_USERS=your-github-username
```

### 4. Hugoサイトの準備

`repo/` ディレクトリにHugoサイトを配置するか、既存のサイトへのパスを `REPO_PATH` で指定します。

CMSの設定ファイルを作成:

```bash
mkdir -p repo/static/admin
```

`repo/static/admin/config.yml` を作成 ([設定例](docs/reference/cms-config.md)):

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

### 5. 開発環境の準備と起動

```bash
mise install
mise run dev
```

ブラウザで http://localhost:8080/admin にアクセス

## Dockerでの起動

Docker構成ではapp起動とサイトtoolchainの準備を分離します。`.env`へapp設定、host UID/GID、`HUGO_CMS_REPOS`の明示allowlistを設定したあと、secret-freeなone-shot serviceを実行します。

```bash
cp deploy/.env.example .env
docker compose build
docker compose --profile tools run --rm tool-bootstrap
docker compose up -d hugo-cms
```

appは非rootで動作し、bind mountしたrepoを`chown`しません。mise tools/cacheはnamed volumeに保持され、appを再起動しても`mise install`やNode.js依存のインストールは自動実行されません。既定の公開先は`127.0.0.1:8080`です。詳しい設定と安全境界は[Docker + mise デプロイガイド](docs/guides/docker-mise-deployment.md)を参照してください。

## 設定リファレンス

すべての設定は環境変数または `.env` ファイルで指定します。

| 環境変数 | 説明 | デフォルト |
|----------|------|-----------|
| `GITHUB_CLIENT_ID` | GitHub OAuth Client ID | (必須) |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth Client Secret | (必須) |
| `SESSION_SECRET` | セッション暗号化キー | (自動生成・非推奨) |
| `ALLOWED_GITHUB_USERS` | 許可するGitHubユーザー名(カンマ区切り) | (必須) |
| `ALLOW_ALL_GITHUB_USERS` | 全GitHubユーザーを許可する開発用設定 | `false` |
| `GITHUB_OAUTH_SCOPES` | OAuthスコープ | `public_repo` |
| `PORT` | サーバーポート。Docker container内は固定 | `8080` |
| `APP_URL` | アプリケーションURL | `http://localhost:8080` |
| `REPO_PATH` | Hugoリポジトリのパス | `./repo` |
| `SITE_GENERATOR` | サイトジェネレーター (`hugo` / `eleventy`) | `hugo` |
| `GENERATOR_RUNTIME` | generatorコマンドの実行方式 (`direct` / `mise`) | `direct` |
| `CONTENT_DIR` | リポジトリ内の記事ディレクトリ | `content` |
| `STATIC_DIR` | リポジトリ内の静的ファイルディレクトリ | `static` |
| `PUBLIC_DIR` | リポジトリ内のビルド出力ディレクトリ | `public` |
| `SITES_CONFIG_PATH` | 複数サイト設定ファイルのパス | (空) |
| `SNIPPET_PATHS` | スニペットファイルのパス(カンマ区切り) | `repo/.vscode/md.code-snippets` |
| `MAX_UPLOAD_SIZE_MB` | 最大アップロードサイズ(MB) | `10` |
| `ARTICLE_MEDIA_DIR` | 記事バンドル内の画像ディレクトリ | (空) |
| `STATIC_MEDIA_DIR` | static内の画像ディレクトリ | (空) |
| `MARKDOWN_PREVIEW_ENABLED` | 安全な本文プレビュー | `true` |
| `PREVIEW_DEPLOYMENT_PROVIDER` | デプロイprovider (`cloudflare_pages`または空) | (空) |
| `CLOUDFLARE_PAGES_ACCOUNT_ID` | Cloudflare account ID | (provider使用時必須) |
| `CLOUDFLARE_PAGES_PROJECT_NAME` | Cloudflare Pages project | (provider使用時必須) |
| `CLOUDFLARE_PAGES_API_TOKEN_ENV` | API tokenを保持する環境変数名 | `CLOUDFLARE_API_TOKEN` |
| `PREVIEW_DEPLOYMENT_ACCESS_PROTECTED` | Cloudflare Access設定済みの申告 | `false` |
| `PREVIEW_STATE_DIR` | draft/deployment state保存先 | `data/preview-deployments` |
| `GIT_USER_NAME` | Gitコミット用ユーザー名 | `Hugo CMS Bot` |
| `GIT_USER_EMAIL` | Gitコミット用メール | `bot@hugo-cms.local` |
| `GIT_BRANCH` | Gitブランチ | `main` |

詳細は [設定ガイド](docs/guides/configuration.md) を参照してください。

## ドキュメント

- [ドキュメント一覧](docs/README.md) - 目的別の索引
- [設定ガイド](docs/guides/configuration.md) - 詳細な設定オプション
- [Docker + mise デプロイガイド](docs/guides/docker-mise-deployment.md) - secret-free bootstrapと非root appによる推奨デプロイ
- [CMS設定](docs/reference/cms-config.md) - コレクションとフィールドの設定
- [現行アーキテクチャ](docs/architecture/current-architecture.md) - 現在のシステム構成
- [マルチサイト・マルチジェネレーター設計](docs/architecture/multi-site-generator-design.md) - 複数HugoサイトとEleventy等への対応方針
- [本文プレビューとデプロイプレビュー](docs/architecture/preview-deployment-design.md) - 安全な本文表示、draft deployment、Local Live Previewとの役割分担
- [セキュリティ・品質監査](docs/audits/security-and-quality-audit.md) - 既知の問題と推奨対応

## プロジェクト構造

```
hugo-cms/
├── Dockerfile           # 非root app/tool bootstrap用image
├── compose.yml          # app、tools profile、named volume
├── main.go              # エントリーポイント、ルーティング
├── mise.toml            # 開発ツールと共通タスク
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
│       ├── generator.go # ジェネレーター共通インターフェース
│       ├── hugo_adapter.go # Hugoアダプター
│       ├── process_manager.go # プレビュープロセス管理
│       └── media.go     # メディアファイル管理
├── docs/
│   ├── guides/          # 設定・デプロイ手順
│   ├── reference/       # API・CMS設定リファレンス
│   ├── architecture/    # 現行・将来アーキテクチャ
│   └── audits/          # セキュリティ・品質監査
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
mise run test
```

### ビルド

```bash
mise run build
```

## セキュリティ

- **認証**: GitHub OAuthによるシングルサインオン
- **認可**: `ALLOWED_GITHUB_USERS` によるアクセス制限
- **CSRF保護**: 全てのPOST/PUT/DELETEリクエストにCSRFトークンを要求
- **パス検証**: パストラバーサル攻撃を防止
- **トークン検証**: GitHubトークンの定期的な有効性確認

## ライセンス

MIT License