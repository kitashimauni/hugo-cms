# デプロイガイド

Hugo CMSを本番環境にデプロイする方法について説明します。

## 前提条件

- デプロイ先サーバー (Linux推奨)
- ドメインとSSL証明書
- GitHub OAuth App (本番用)
- Git設定済みのHugoリポジトリ

## デプロイ方法

### 1. バイナリデプロイ

最もシンプルな方法です。

#### ビルド

```bash
# Linux用にクロスコンパイル
GOOS=linux GOARCH=amd64 go build -o hugo-cms-linux .

# または対象サーバー上でビルド
go build -o hugo-cms .
```

#### サーバー設定

1. バイナリをサーバーにアップロード:
```bash
scp hugo-cms-linux user@server:/opt/hugo-cms/hugo-cms
scp .env.example user@server:/opt/hugo-cms/.env
```

2. 環境変数を設定:
```bash
ssh user@server
cd /opt/hugo-cms
vim .env
```

3. Hugoリポジトリを準備:
```bash
git clone https://github.com/username/hugo-site.git /opt/hugo-cms/repo
```

4. systemdサービスを作成:

```ini
# /etc/systemd/system/hugo-cms.service
[Unit]
Description=Hugo CMS
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/hugo-cms
ExecStart=/opt/hugo-cms/hugo-cms
Restart=always
RestartSec=5
Environment=GIN_MODE=release

[Install]
WantedBy=multi-user.target
```

5. サービスを開始:
```bash
sudo systemctl daemon-reload
sudo systemctl enable hugo-cms
sudo systemctl start hugo-cms
```

### 2. Dockerデプロイ

#### Dockerfile

```dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o hugo-cms .

FROM alpine:latest

# Hugoをインストール
RUN apk add --no-cache hugo git

WORKDIR /app
COPY --from=builder /app/hugo-cms .
COPY --from=builder /app/static ./static
COPY --from=builder /app/templates ./templates

EXPOSE 8080

CMD ["./hugo-cms"]
```

#### docker-compose.yml

```yaml
version: '3.8'

services:
  hugo-cms:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./repo:/app/repo
      - ./.env:/app/.env:ro
    environment:
      - GIN_MODE=release
      - HUGO_SERVER_BIND=0.0.0.0
    restart: unless-stopped
```

#### 起動

```bash
docker-compose up -d
```

### 3. リバースプロキシ設定

#### Nginx

```nginx
server {
    listen 80;
    server_name cms.example.com;
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name cms.example.com;

    ssl_certificate /etc/letsencrypt/live/cms.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/cms.example.com/privkey.pem;

    # セキュリティヘッダー
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;

    # クライアント最大ボディサイズ (アップロード用)
    client_max_body_size 50M;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # タイムアウト設定
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 300s;
    }
}
```

#### Caddy

```caddyfile
cms.example.com {
    reverse_proxy localhost:8080

    # アップロードサイズ制限
    request_body {
        max_size 50MB
    }
}
```

## 本番環境の設定

### 環境変数

```env
# 必須
GITHUB_CLIENT_ID=本番用のClient ID
GITHUB_CLIENT_SECRET=本番用のClient Secret
SESSION_SECRET=32文字以上のランダムな文字列

# セキュリティ
ALLOWED_GITHUB_USERS=your-username
ALLOW_ALL_GITHUB_USERS=false

# アプリケーション
PORT=8080
APP_URL=https://cms.example.com
REPO_PATH=/opt/hugo-cms/repo

# Gin
GIN_MODE=release
```

### GitHub OAuth App設定

本番用のOAuth Appを作成:

1. [GitHub Developer Settings](https://github.com/settings/developers)
2. "New OAuth App"
3. 設定:
   - **Homepage URL**: `https://cms.example.com`
   - **Authorization callback URL**: `https://cms.example.com/admin/auth/callback`

### Gitリモートの設定

Gitのpush/pullは、ログインユーザーのGitHub OAuthトークンを使用します。認証主体がサーバーのSSH鍵へ切り替わることを防ぐため、リモートURLには`https://github.com/`形式だけを使用してください。

```bash
git clone https://github.com/username/hugo-site.git repo
git -C repo remote get-url origin
```

SSH、scp形式、GitHub以外のHTTPSホスト、認証情報を埋め込んだURLはCMSからの同期・公開時に拒否されます。既存リポジトリがSSH形式の場合は次のように変更します。

```bash
git -C repo remote set-url origin https://github.com/username/hugo-site.git
```

## 監視とログ

### ログの確認

```bash
# systemdの場合
journalctl -u hugo-cms -f

# Dockerの場合
docker-compose logs -f hugo-cms
```

### ヘルスチェック

```bash
# 基本チェック
curl https://cms.example.com/health

# 詳細チェック
curl https://cms.example.com/ready
```

### 監視設定例 (Prometheus)

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'hugo-cms'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/health'
```

## バックアップ

### Gitリポジトリ

コンテンツはGitHubにプッシュされるため、自動的にバックアップされます。

### 環境設定

```bash
# 定期バックアップ
cp /opt/hugo-cms/.env /backup/hugo-cms-env-$(date +%Y%m%d).bak
```

## アップデート

### バイナリの更新

```bash
# 新しいバイナリをビルド/アップロード
scp hugo-cms-linux user@server:/opt/hugo-cms/hugo-cms.new

# サービス停止
sudo systemctl stop hugo-cms

# バイナリ入れ替え
mv /opt/hugo-cms/hugo-cms /opt/hugo-cms/hugo-cms.old
mv /opt/hugo-cms/hugo-cms.new /opt/hugo-cms/hugo-cms
chmod +x /opt/hugo-cms/hugo-cms

# サービス再開
sudo systemctl start hugo-cms
```

### Dockerの更新

```bash
docker-compose pull
docker-compose up -d --build
```

## トラブルシューティング

### よくある問題

#### OAuth認証が失敗する

- Callback URLが正確か確認
- `APP_URL` が正しいか確認
- HTTPSを使用しているか確認

#### Git pushが失敗する

- GitHub OAuth スコープが `public_repo` または `repo` か確認
- リポジトリへの書き込み権限があるか確認

#### Hugoサーバーが起動しない

- `hugo` コマンドがPATHにあるか確認
- リポジトリパスが正しいか確認
- `/ready` エンドポイントで詳細を確認

#### アップロードが失敗する

- `MAX_UPLOAD_SIZE_MB` の設定を確認
- Nginxの `client_max_body_size` を確認

### ログレベルの変更

現在のバージョンでは `log/slog` を使用しています。
デバッグ情報が必要な場合は、環境変数で設定できます:

```bash
# 将来的なサポート
LOG_LEVEL=debug
```

## セキュリティチェックリスト

- [ ] HTTPS (SSL/TLS) が有効
- [ ] `SESSION_SECRET` が32文字以上のランダムな値
- [ ] `ALLOWED_GITHUB_USERS` で許可ユーザーを制限
- [ ] `ALLOW_ALL_GITHUB_USERS=false` を設定
- [ ] `GIN_MODE=release` を設定
- [ ] ファイアウォールで不要なポートを閉じる
- [ ] 定期的なセキュリティアップデート
