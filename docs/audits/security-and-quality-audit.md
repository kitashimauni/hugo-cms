# セキュリティ・品質監査

最終確認日: 2026-07-06

## 概要

Hugo CMSの認証、記事編集、メディア管理、Git連携、Hugoプレビュー、および開発・デプロイ手順を確認した結果をまとめる。

初回監査では、特に認可、SSHリモート利用時のGit認証、メディアパス検証を本番運用前の必須対応として確認した。各項目の現在の対応状態は以下に記録する。

## 優先度: 重大

### 1. 許可リスト未設定時にすべてのGitHubユーザーを許可する

- 状態: 対応済み
- 該当箇所:
  - `pkg/config/config.go` の `AllowedGitHubUsers`
  - `pkg/config/config.go` の `IsUserAllowed`
  - READMEのクイックスタート
- 影響:
  - `ALLOWED_GITHUB_USERS` が空の場合、GitHubアカウントを持つすべてのユーザーがCMSへログインできる。
  - ログイン後は記事の保存・削除、メディア操作、同期、公開、Hugo再起動などを実行できる。
  - 現在のローカル `.env` にも `ALLOWED_GITHUB_USERS` は設定されていない。
- 推奨対応:
  - 本番環境では許可リストを必須にし、空の場合は起動を失敗させる。
  - 開発時だけ明示的な設定で全員許可を選べるようにする。
  - READMEと`.env.example`の初期設定を安全側へ変更する。
  - 各リクエストでも保存済みユーザー名を再認可し、許可リスト変更後に既存セッションを失効できるようにする。

### 2. SSHリモートではログインユーザーのOAuth権限で公開されない

- 状態: 対応済み
- 該当箇所: `pkg/services/git.go` の `ExecuteGitWithToken`
- 影響:
  - OAuthトークンを使う処理はHTTPSリモートを前提としている。
  - SSHまたはscp形式のリモートでは、Gitはサーバー側のSSH鍵・SSH設定を使用する。この場合、ログインユーザー本人に対象リポジトリの書き込み権限がなくても、サーバーの鍵に権限があれば公開できる。
  - 現在の`repo`のリモートも `github:kitashimauni/techblog.git` というSSH形式である。
  - `GIT_REMOTE`を変更しても、リモートURL取得処理が`origin`に固定されている。
- 推奨対応:
  - ユーザーのOAuth権限で公開する場合はHTTPSリモートのみを許可し、起動時に検証する。
  - SSH鍵で公開する設計にする場合は、その事実を明示し、OAuthログインとは別に厳格なCMS認可を行う。
  - リモート名のハードコードを廃止し、すべて`config.GitRemote`を使用する。
  - 実装では`github.com`のHTTPSリモートだけを許可し、credential helperを無効化してログインユーザーのOAuthトークンを使用する。
  - 既存のSSH形式リモートはHTTPS形式へ移行する必要がある。

### 3. メディアアップロード先がリポジトリ外へ脱出できる

- 状態: 対応済み
- 該当箇所:
  - `pkg/handlers/media.go`
  - `pkg/services/media.go` の `ListMediaFiles`
  - `pkg/services/media.go` の `SaveMediaFile`
- 影響:
  - contentモードの`articlePath`が検証されていない。
  - `../../`を含むパスにより、許可された拡張子のファイルをリポジトリ外の任意ディレクトリへ書き込める。
  - メディア一覧でもリポジトリ外のディレクトリを走査できる。
- 推奨対応:
  - `articlePath`を必ず`content`配下へ安全に解決する。
  - 設定由来の`ARTICLE_MEDIA_DIR`と`STATIC_MEDIA_DIR`も絶対パスや親ディレクトリ参照を拒否する。
  - シンボリックリンクを含め、実パスが許可ルート内にあることを確認する。
  - 正常系、`../`、絶対パス、Windows形式パス、シンボリックリンクのテストを追加する。

### 4. GitHubアクセストークンがCookie内で暗号化されていない

- 状態: 対応済み（暗号化Cookie。サーバー側セッションストアへの移行は将来改善）
- 該当箇所:
  - `main.go` のCookie Store初期化
  - `pkg/handlers/auth.go` の`access_token`保存
- 影響:
  - Cookie Storeへ認証鍵だけを渡しているため、Cookieは署名されるが暗号化されない。
  - Cookieが漏えい・取得された場合、CMSセッションだけでなくGitHub OAuthトークン自体も露出し、トークンに許可された他リポジトリまで影響が広がる。
  - READMEと設定ガイドの「セッション暗号化キー」という説明と実装が一致していない。
- 推奨対応:
  - サーバー側セッションストアへ移行し、CookieにはランダムなセッションIDだけを保存する。
  - Cookie Storeを継続する場合は、署名鍵と独立したAES暗号化鍵を設定する。
  - `Secure`、`HttpOnly`、`SameSite`設定を維持し、鍵のローテーション方法も定義する。

## 優先度: 高

### 5. 自動保存完了前に公開できる

- 状態: 未対応
- 該当箇所:
  - `static/js/editor.js` の3秒後の自動保存
  - `static/js/app.js` の`publishFile`および`runPublish`
- 影響:
  - 編集直後に公開すると、ディスクへ保存される前の内容がコミットされる。
  - 公開成功表示の後に自動保存が動き、最新内容が未公開の変更として残る。
- 推奨対応:
  - 公開前に保留中のタイマーを停止し、最新内容の保存完了を待つ。
  - 保存失敗時は公開を中止する。
  - 公開ボタンの多重実行と、自動保存の同時実行を直列化する。

### 6. `git commit`失敗後もpushし、成功扱いになる

- 状態: 未対応
- 該当箇所: `pkg/services/git.go` の `PublishChanges`
- 影響:
  - コミット失敗が警告文字列へ変換されるだけで、処理はpushへ進む。
  - pushが成功すればAPIは成功を返すため、変更がコミットされていないのに「公開成功」と表示される。
- 推奨対応:
  - 「変更なし」以外のコミット失敗では直ちに処理を中止する。
  - 「変更なし」の場合も、対象変更が本当に存在しないことを確認して明示的な結果を返す。
  - 同期・保存・公開を排他制御し、Git indexと作業ツリーの競合を防ぐ。

### 7. JSON Front Matterで本文が失われる

- 状態: 未対応
- 該当箇所: `pkg/services/frontmatter.go`
- 影響:
  - JSON Front Matterの読み込みはファイル全体をJSONとして解析するため、本文付きの記事を解析できない。
  - JSON形式の書き出しでは本文が出力されない。
  - 対象記事の保存時に本文を失う可能性がある。
- 推奨対応:
  - Hugoが扱うJSON Front Matterと本文の境界を正しく解析・生成する。
  - YAML、TOML、JSONすべてについて、読み込み後の保存で本文が保持されるラウンドトリップテストを追加する。

### 8. Hugoプレビューと管理画面が同一オリジンである

- 状態: 対応済み（別オリジンへの分離は将来改善）
- 該当箇所:
  - `main.go` のHugoリバースプロキシ
  - `templates/index.html` のプレビューiframe
- 影響:
  - iframeに`sandbox`がなく、HugoテーマやレイアウトのJavaScriptは管理画面と同一オリジンで実行される。
  - 同期したリポジトリに悪意あるJavaScriptが含まれる場合、CSRFトークンを取得し、ログインユーザー権限でCMS APIを操作できる。
- 推奨対応:
  - プレビューを管理画面とは異なるオリジンへ分離する。
  - 分離できない場合はiframeの`sandbox`と厳格なContent Security Policyを導入する。ただし、必要なプレビュー機能との互換性を検証する。
  - 現在はiframeを`sandbox="allow-forms allow-scripts"`でopaque originとして扱い、`allow-same-origin`、トップレベル遷移、ポップアップを許可していない。
  - プレビューへの直接アクセスにも管理画面と同じ認証・トークン再検証を適用している。

## 優先度: 中

### 9. Hugo再起動時にプロセス管理が競合する

- 状態: 未対応
- 該当箇所: `pkg/services/hugo.go`
- 影響:
  - 旧プロセスの監視goroutineが、新プロセス起動後に共有変数を`nil`へ戻す可能性がある。
  - Hugoの二重起動、稼働状態の誤判定、停止不能につながる。
- 推奨対応:
  - 監視goroutineが、自分の監視対象と現在のプロセスが同一の場合だけ共有状態をクリアする。
  - 固定時間の`sleep`ではなく、プロセス終了を明示的に待つ。
  - 起動、異常終了、連続再起動のテストを追加する。

### 10. `CACHE_CONCURRENCY`の値によって停止またはpanicする

- 状態: 対応済み
- 該当箇所:
  - `pkg/config/config.go`
  - `pkg/services/cache.go`
- 影響:
  - `CACHE_CONCURRENCY=0`では記事一覧のキャッシュ構築がデッドロックする。
  - 負数ではチャネル作成時にpanicする。
- 推奨対応:
  - 設定読み込み時に1以上の上限付き整数へ制限する。
  - 不正値では安全な既定値を使用するか、起動を失敗させる。

### 11. コレクションのFront Matter形式指定を無視する

- 状態: 未対応
- 該当箇所:
  - `pkg/models/cms.go`
  - `pkg/services/frontmatter.go` の `GenerateContentFromCollection`
- 影響:
  - コレクションモデルに`format`がなく、新規記事は常にTOMLで生成される。
  - YAMLまたはJSONを指定したCMS設定と一致しない。
- 推奨対応:
  - CMS設定モデルに`format`を追加する。
  - `toml-frontmatter`、`yaml-frontmatter`、`json-frontmatter`などの値を内部形式へ正規化する。
  - 未対応形式は暗黙にTOMLへ変換せず、設定エラーとして通知する。

### 12. Goバージョンとデプロイ文書が一致していない

- 状態: 未対応
- 該当箇所:
  - `go.mod`: Go 1.24.0、toolchain Go 1.24.11
  - README: Go 1.21以上
  - `docs/guides/deployment.md`: `golang:1.21-alpine`
- 影響:
  - 記載されたDockerfileでは現在の`go.mod`をビルドできない。
  - 開発者ごとに異なるGoバージョンが使われる可能性がある。
- 推奨対応:
  - README、Dockerfile例、CI、ローカル開発環境をGo 1.24.11へ統一する。
  - バージョン管理にはmiseの採用を推奨する。

## その他の改善点

- 記事取得APIのパスをフロントエンド側でURLエンコードしていないため、`&`、`#`、`?`などを含むファイル名を正しく扱えない。
- Git statusのporcelain出力を文字列操作で解析しており、リネームや引用された日本語ファイル名のdirty判定を誤る可能性がある。
- 複数タブや並行リクエスト間の更新競合を検出するバージョン番号・ETag・排他制御がなく、後勝ちで記事を上書きする。
- 複数のGoファイルが`gofmt`未適用である。

## 検証結果

以下は成功した。

- `go test ./...`
- `go vet ./...`
- `go build -buildvcs=false .`
- JavaScriptファイルの構文検査

テストカバレッジ:

- `pkg/handlers`: 19.8%
- `pkg/services`: 11.8%
- `main`、`pkg/config`: 0%

完走できなかった検査:

- `go test -race ./...`
  - Windows環境にCコンパイラがなく、race detectorを有効化できなかった。
- `staticcheck ./...`
  - インストール済みのstaticcheckがGo 1.24の解析中にpanicした。

重要な異常系の大半が未テストであるため、修正時は各問題の再現テストを先に追加することを推奨する。

## mise導入の検討

### 結論

このプロジェクトではmiseの採用を推奨する。

`go.mod`がGo 1.24.11のtoolchainを指定している一方、READMEやDocker例はGo 1.21のままであり、環境差がすでに問題として現れている。miseを使うことで、開発者・CI・ローカル検証で使用するツールのバージョンと基本コマンドを一か所へ寄せられる。

### miseで管理する対象

- Go 1.24.11
- Hugo Extended
  - 実サイトと本番環境で使用するバージョンを確認してから固定する。
- staticcheck
  - Go 1.24対応版へ更新して固定する。
- 必要に応じてgolangci-lint

### miseタスクとして揃えるコマンド

- `test`: `go test ./...`
- `test-race`: LinuxまたはCコンパイラを用意した環境で`go test -race ./...`
- `vet`: `go vet ./...`
- `lint`: `staticcheck ./...`、またはgolangci-lint
- `fmt-check`: `gofmt`未適用ファイルがないことを確認
- `build`: `go build .`
- `check`: format、vet、lint、test、buildをまとめて実行

### 導入時の注意

- `go.mod`の`go`・`toolchain`指定は残し、Go側でも最低要件を検証する。
- miseと`go.mod`、Docker、CIのバージョンを同時に更新し、二重管理のずれを防ぐ。
- OAuth Client Secretやセッション鍵などの秘密情報を`mise.toml`へ直接書かない。
- `.env`は引き続きGit管理外とし、本番ではサービス管理基盤のSecret機能を使用する。
- HugoはExtended版が必要かをサイト側で確認し、通常版と混在させない。
- race detectorはmiseだけでは解決しないため、Linux CIで実行するのが扱いやすい。

### 推奨する導入順序

1. `mise.toml`でGo 1.24.11とHugo Extendedを固定する。
2. READMEとデプロイ文書をmise前提の手順へ更新する。
3. `mise run test`、`mise run vet`、`mise run build`を定義する。
4. Go 1.24対応のlintツールを固定する。
5. CIでもmiseを使用するか、少なくとも同じバージョンとタスクを再現する。
6. DockerのbuilderイメージをGo 1.24系へ更新する。
