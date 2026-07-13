# Docker + mise デプロイガイド

新規サーバーでは、Hugo CMS本体・mise・Hugo/Nodeなどのサイト別ツールチェーンをDocker内に閉じ込める構成を推奨します。

この構成では、ホストOSにHugoやmiseを直接インストールしません。サイトリポジトリごとの `mise.toml` / `.mise.toml` / `.tool-versions` をコンテナ内のmiseが読み、HugoやNode.jsを `/data/mise` volumeへインストールします。

## 方針

- CMSコンテナ内に `mise` を同梱する
- generator実行は `GENERATOR_RUNTIME=mise` で `mise exec -C <repo> -- ...` 経由にする
- サイトリポジトリは `/data/repos` にmountする
- miseのtools/cacheは `/data/mise` にmountする
- ホスト上では `mise` を実行しない

## サーバー側の準備

```bash
sudo mkdir -p /opt
sudo chown "$USER:$USER" /opt
```

DockerとDocker Compose pluginを用意してください。

```bash
docker version
docker compose version
```

## CMSリポジトリを配置

CMSリポジトリをサーバーへcloneします。rootの `compose.yml` はローカルbuild前提なので、GHCRなどのimage配布を待たずに使えます。

```bash
git clone https://github.com/kitashimauni/hugo-cms.git /opt/hugo-cms
cd /opt/hugo-cms
mkdir -p repos mise-data
cp deploy/.env.example .env
```

## サイトリポジトリをclone

```bash
git clone https://github.com/kitashimauni/techblog.git repos/techblog
```

remote URLはHTTPS形式にしてください。

```bash
git -C repos/techblog remote get-url origin
```

期待値:

```text
https://github.com/kitashimauni/techblog.git
```

## サイトリポジトリに `mise.toml` を置く

Hugoサイトの例:

```toml
[tools]
hugo = "0.148.2"
```

Node.jsも必要なサイトでは追加します。

```toml
[tools]
hugo = "0.148.2"
node = "22"
```

Eleventyサイトでは、`node` と使用するpackage managerをサイト側で固定してください。

```toml
[tools]
node = "22"
pnpm = "10"
```

そのうえで、Eleventy本体やプラグインは `package.json` とlockfileで固定します。

`.env` の最小例:

```env
APP_URL=https://cms.example.com
PORT=8080
GIN_MODE=release

GITHUB_CLIENT_ID=...
GITHUB_CLIENT_SECRET=...
SESSION_SECRET=...
ALLOWED_GITHUB_USERS=kitashimauni
ALLOW_ALL_GITHUB_USERS=false
GITHUB_OAUTH_SCOPES=public_repo

REPO_PATH=/data/repos/techblog
SITE_GENERATOR=hugo
GENERATOR_RUNTIME=mise
CONTENT_DIR=content
STATIC_DIR=static
PUBLIC_DIR=public

HUGO_SERVER_BIND=127.0.0.1
HUGO_SERVER_PORT=1314

GIT_REMOTE=origin
GIT_BRANCH=main
```

private repositoryを使う場合は次にします。

```env
GITHUB_OAUTH_SCOPES=repo
```

## 起動

```bash
docker compose up -d --build
```

entrypointは起動前に次を実行します。

1. `/data/repos` と `/data/mise` の権限をコンテナ内ユーザーへ合わせる
2. `REPO_PATH` と `/data/repos/*` を検出する
3. `mise.toml` / `.mise.toml` / `.tool-versions` があるrepoで `mise install` を実行する
4. CMSを起動する

ログ確認:

```bash
docker compose logs -f
```

miseの状態確認:

```bash
docker compose exec hugo-cms mise ls
docker compose exec hugo-cms mise exec -C /data/repos/techblog -- hugo version
```

## 複数サイト

`sites.yml` を使う場合は、`compose.yml` のbind mountを有効化します。

```yaml
volumes:
  - ./repos:/data/repos
  - ./mise-data:/data/mise
  - ./sites.yml:/app/sites.yml:ro
```

`.env`:

```env
SITES_CONFIG_PATH=/app/sites.yml
GENERATOR_RUNTIME=mise
```

`sites.yml`:

```yaml
default_site: techblog
sites:
  - id: techblog
    name: Tech Blog
    repo_path: /data/repos/techblog
    generator: hugo
    runtime: mise
    content_dir: content
    static_dir: static
    public_dir: public
    hugo_server_port: "1314"

  - id: docs
    name: Docs
    repo_path: /data/repos/docs
    generator: eleventy
    runtime: mise
    content_dir: src
    static_dir: public-assets
    public_dir: _site
    hugo_server_port: "1315"
```

各サイトのpreview portは重複させないでください。

## 運用ルール

- ホストでは `mise` を実行しない
- `mise install` や `hugo version` の確認は `docker compose exec hugo-cms ...` で行う
- `mise-data` volumeは削除しない。削除すると次回起動時にtoolchainを再インストールします
- `mise.toml` は信頼済みサイトリポジトリにだけ置く

## トラブルシューティング

### `hugo: command not found`

対象repoの `mise.toml` にHugoが定義されているか確認してください。

```bash
docker compose exec hugo-cms cat /data/repos/techblog/mise.toml
docker compose exec hugo-cms mise exec -C /data/repos/techblog -- hugo version
```

### `mise install` が失敗する

ログを確認します。

```bash
docker compose logs hugo-cms
```

`/data/mise` が書き込み可能か確認します。

```bash
docker compose exec hugo-cms sh -lc 'touch /data/mise/.write-test && rm /data/mise/.write-test'
```

### pnpm/yarn/bunが見つからない

Eleventyサイトでnpm以外のlockfileを使う場合、サイトの `mise.toml` に対応するpackage managerも追加してください。

```toml
[tools]
node = "22"
pnpm = "10"
```
