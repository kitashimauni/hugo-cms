# Docker + mise デプロイガイド

新規サーバーでは、CMS本体とサイト別ツールチェーンをDocker内で実行する構成を推奨します。ホストOSへHugo、Node.js、miseをインストールする必要はありません。

この構成では、アプリケーションの起動とツール準備を分離します。

- `hugo-cms` は非rootでCMSだけを起動し、`mise install`や所有者変更を行わない
- `tool-bootstrap` は`tools` profileのone-shot serviceとして、管理者が明示したときだけ実行する
- `HUGO_CMS_REPOS`にUnixの`:`区切りで列挙したリポジトリだけを準備する
- tool bootstrapにはGitHub OAuthやセッションの秘密情報を渡さない
- miseのtools/cacheは`mise-data` named volumeへ保存する
- CMSはホストのloopbackだけに公開し、外部公開はリバースプロキシ経由にする

## セキュリティ境界

サイトリポジトリはMarkdown置き場ではなく、実行コードとして扱います。mise設定は環境変数やhookを定義でき、Node.js依存のinstall scriptもコードを実行できます。

Composeのruntimeと`tool-bootstrap`は次の境界を守ります。

- `/data/repos`を一括探索せず、`HUGO_CMS_REPOS`の明示allowlistだけを処理する
- appのgenerator runtimeとbootstrapがtrustできるpathを同じallowlistへ制限する
- `.env`を補間に利用しても、`tool-bootstrap`コンテナには`GITHUB_CLIENT_SECRET`、`SESSION_SECRET`などのapp環境変数を渡さない
- appコンテナは依存準備を行わず、準備失敗によってCMS自体を再起動ループにしない

allowlistへ追加する前に、リポジトリの`mise.toml`、`package.json`、lockfile、install scriptをレビューしてください。

## 前提条件

- Docker Engine
- Docker Compose plugin 2.x
- Git
- GitHub OAuth App

```bash
docker version
docker compose version
```

## サーバーへ配置

単一の管理者が`git`と`docker compose`を実行する構成では、管理者のhome directory配下を推奨します。`/opt`全体の所有者を変更したり、通常運用で`sudo`を付けたりする必要がありません。

```bash
git clone https://github.com/kitashimauni/hugo-cms.git "$HOME/hugo-cms"
cd "$HOME/hugo-cms"
mkdir -p repos
cp deploy/.env.example .env
```

`.env`は必須です。Composeは設定ファイルがない状態では起動しません。

system-managedな配置として`/srv`または`/opt`を使う場合は、親directoryではなくCMS専用directoryだけを管理ユーザーへ委譲します。以後の`git`と`docker compose`は`sudo`なしで実行します。

```bash
sudo install -d -o "$(id -un)" -g "$(id -gn)" /srv/hugo-cms
git clone https://github.com/kitashimauni/hugo-cms.git /srv/hugo-cms
cd /srv/hugo-cms
```

`/opt/hugo-cms`を選ぶ場合も同様に、`sudo install -d ... /opt/hugo-cms`で対象directoryだけを作成してください。`sudo chown ... /opt`のように`/opt`全体の所有者を変更してはなりません。

すでに`/opt/hugo-cms`へ配置済みでhome directoryへ移す場合は、containerを停止してから移動します。named volumeを保持するため`--volumes`は付けません。

```bash
cd /opt/hugo-cms
docker compose down
cd "$HOME"
test ! -e "$HOME/hugo-cms"
sudo mv /opt/hugo-cms "$HOME/hugo-cms"
sudo chown -R "$(id -un):$(id -gn)" "$HOME/hugo-cms"
cd "$HOME/hugo-cms"
docker compose up -d hugo-cms
```

配置場所にかかわらず`docker compose`だけがpermission errorになる場合は、filesystemではなくDocker daemonへのアクセス権を確認します。

```bash
docker info
```

Docker groupを利用する場合、そのメンバーは実質的にroot相当の操作が可能です。サーバーの権限方針に応じて、管理ユーザーをDocker groupへ追加するかrootless Dockerを利用してください。日常運用で`sudo docker compose`を使うと、作業treeへroot所有のファイルを作る原因になるため推奨しません。

## コンテナUID/GID

appはbuild時に指定したUID/GIDの非rootユーザーで動作し、bind mountしたリポジトリを`chown`しません。Linuxではホスト上の所有者に合わせます。

```bash
sed -i "s/^HUGO_CMS_UID=.*/HUGO_CMS_UID=$(id -u)/" .env
sed -i "s/^HUGO_CMS_GID=.*/HUGO_CMS_GID=$(id -g)/" .env
```

UID/GIDを変更した場合はimageを再buildしてください。`root`相当の`0`は、`00`などのゼロ埋め表現を含めて指定できません。Docker Desktopでは通常、配布時の既定値を利用できます。

指定したUID/GIDがbase image内ですでに使われている場合、imageはその数値IDを再利用します。container内のuser/group名ではなく、`HUGO_CMS_UID`と`HUGO_CMS_GID`の数値が実行権限の基準です。

すでに`mise-data` volumeを作成したあとでUID/GIDを変更すると、volume内に以前の所有権が残ることがあります。toolchainを再取得できる環境では、意図した初期化として`docker compose down --volumes`を実行してからimageを再buildし、bootstrapをやり直してください。既存volumeを保持する必要がある場合は、削除せず管理者がvolume内の所有権を新しいUID/GIDへ移行してください。

サイトリポジトリはappユーザーが記事やGit metadataを書き込める権限で配置します。コンテナは権限を自動修復しないため、permission errorはホスト側のUID/GIDとディレクトリ権限を確認してください。

## サイトリポジトリ

```bash
git clone https://github.com/kitashimauni/techblog.git repos/techblog
git -C repos/techblog remote get-url origin
```

Git remoteはCMSがOAuth tokenを安全に利用できる`https://github.com/owner/repository.git`形式にしてください。

Hugoサイトの`mise.toml`例:

```toml
[tools]
hugo = "0.148.2"
```

EleventyサイトではNode.jsと使用するpackage managerを固定し、`package.json`と対応するlockfileをコミットします。

```toml
[tools]
node = "22"
pnpm = "10"
```

## `.env`

単一Hugoサイトの例です。

```env
APP_URL=https://cms.example.com
GIN_MODE=release

GITHUB_CLIENT_ID=...
GITHUB_CLIENT_SECRET=...
SESSION_SECRET=32文字以上のランダムな値
ALLOWED_GITHUB_USERS=kitashimauni
ALLOW_ALL_GITHUB_USERS=false
GITHUB_OAUTH_SCOPES=public_repo

REPO_PATH=/data/repos/techblog
SITE_GENERATOR=hugo
GENERATOR_RUNTIME=mise
CONTENT_DIR=content
STATIC_DIR=static
PUBLIC_DIR=public

# tool-bootstrapが処理してよいリポジトリ。Unixの":"区切り。
HUGO_CMS_REPOS=/data/repos/techblog

# ホスト側のloopback公開ポート。コンテナ内PORTは常に8080。
HUGO_CMS_HOST_PORT=8080
HUGO_CMS_UID=1000
HUGO_CMS_GID=1000

HUGO_SERVER_BIND=127.0.0.1
HUGO_SERVER_PORT=1314

GIT_REMOTE=origin
GIT_BRANCH=main
```

private repositoryには`GITHUB_OAUTH_SCOPES=repo`を使用します。スコープを変更した利用者は再ログインが必要です。

`PORT`はComposeがappコンテナ内で`8080`に固定します。ホスト側のポートだけを`HUGO_CMS_HOST_PORT`で変更します。たとえば`HUGO_CMS_HOST_PORT=18080`なら、`127.0.0.1:18080`からコンテナの8080へ接続します。

## ツールとNode.js依存関係の準備

appを起動する前に、secret-freeなone-shot serviceを明示実行します。

```bash
docker compose build
docker compose --profile tools run --rm tool-bootstrap
```

`tool-bootstrap`は`HUGO_CMS_REPOS`をUnixの`:`で分割し、空要素、相対パス、`/data/repos`外のパス、存在しないディレクトリを拒否します。カンマや空白区切り、自動globは使用できません。

各allowlist対象では、mise設定を明示的にtrustして`mise install`を実行します。Node.jsプロジェクトではlockfileに応じて再現可能な依存インストールも実行します。

| lockfile | 準備処理 |
|---|---|
| `package-lock.json` / `npm-shrinkwrap.json` | `npm ci` |
| `pnpm-lock.yaml` | frozen lockfileを使った`pnpm install` |
| `yarn.lock` | immutable/frozen lockfileを使った`yarn install` |
| `bun.lock` / `bun.lockb` | frozen lockfileを使った`bun install` |

Node.jsプロジェクトには対応するlockfileをちょうど1種類だけコミットしてください。`package.json`があるのに対応lockfileがない場合や、複数種類を混在させた場合は、意図しない依存解決を避けるためbootstrapが失敗します。エラーやrepo設定を修正したあとは同じコマンドを再実行できます。named volumeと各repoの依存ディレクトリは再利用されます。

サイトのmise設定、lockfile、package manager、依存関係を更新したときも、appを再起動する前にこのone-shotを再実行します。app起動時に自動bootstrapは行われません。

## appの起動

```bash
docker compose up -d hugo-cms
docker compose logs -f hugo-cms
```

状態確認:

```bash
curl --fail http://127.0.0.1:8080/health
curl --fail http://127.0.0.1:8080/ready
docker compose exec hugo-cms mise exec -C /data/repos/techblog -- hugo version
```

ホストの公開先は`127.0.0.1`固定です。外部からはNginxやCaddyでHTTPS終端し、loopbackへproxyしてください。
`HUGO_CMS_HOST_PORT`を既定値から変更した場合は、上の確認URLも同じポートへ置き換えてください。

## 複数サイト

`sites.yml`を使う場合は、`compose.yml`のread-only bind mountを有効にします。

```yaml
volumes:
  - ./repos:/data/repos
  - mise-data:/data/mise
  - ./sites.yml:/app/sites.yml:ro
```

`.env`では全対象を明示列挙します。

```env
SITES_CONFIG_PATH=/app/sites.yml
GENERATOR_RUNTIME=mise
HUGO_CMS_REPOS=/data/repos/techblog:/data/repos/docs
```

`sites.yml`例:

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

各サイトのpreview portは重複させないでください。Site Registryへ登録しただけではbootstrap対象になりません。実行を承認したrepoだけを`HUGO_CMS_REPOS`にも追加します。

## 更新

```bash
git pull --ff-only
docker compose build --pull
docker compose --profile tools run --rm tool-bootstrap
docker compose up -d hugo-cms
```

appだけの更新でもbootstrapを再実行して問題ありません。サイトの実行コードを再評価したくない場合は、変更内容を確認したうえでappだけを再作成します。

`mise-data`はnamed volumeです。通常の`docker compose down`では残ります。`docker compose down --volumes`はtoolchainを削除するため、意図した初期化時だけ実行してください。

## トラブルシューティング

### `tool-bootstrap`が失敗する

one-shotの出力を確認します。

```bash
docker compose --profile tools run --rm tool-bootstrap
```

- `HUGO_CMS_REPOS`がUnixの`:`区切りか
- 各値が`/data/repos/<name>`の絶対パスか
- repoのmise設定とlockfileをレビュー済みか
- host側repoとnamed volumeへ指定UID/GIDで書き込めるか
- 対応するNode.js lockfileが1種類だけコミットされているか

### `permission denied`

appはbind mountを`chown`しません。build argsとホスト所有者を比較します。

```bash
id -u
id -g
docker compose run --rm --no-deps --entrypoint id hugo-cms
```

値を修正した場合は`docker compose build --no-cache`でappユーザーを作り直します。

UID/GIDを変更済みで`mise-data`に以前の所有権が残っている場合は、上記「コンテナUID/GID」のvolume移行手順も確認してください。

### `hugo`またはpackage managerが見つからない

対象repoが`HUGO_CMS_REPOS`に含まれ、そのrepoの`mise.toml`に必要なtoolが固定されていることを確認し、bootstrapを再実行します。app再起動だけではtoolchainは更新されません。
