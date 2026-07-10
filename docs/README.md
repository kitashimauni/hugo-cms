# ドキュメント

Hugo CMSの利用方法、仕様、設計、および監査結果を目的別に整理している。

## ガイド

- [設定ガイド](guides/configuration.md) - 環境変数と実行設定
- [デプロイガイド](guides/deployment.md) - サーバー、Docker、リバースプロキシ
- [Smoke Test Checklist](guides/smoke-tests.md) - 変更後の最低限確認項目

## リファレンス

- [APIリファレンス](reference/api.md) - HTTP APIの仕様
- [CMS設定](reference/cms-config.md) - コレクション、フィールド、メディア設定
- [スニペット機能](reference/snippets.md) - VS Code形式スニペットの設定

## アーキテクチャ

- [現行アーキテクチャ](architecture/current-architecture.md) - 現在のHugo向け実装
- [マルチサイト・マルチジェネレーター設計](architecture/multi-site-generator-design.md) - 複数HugoサイトとEleventy等へ対応するための提案

## 監査

- [セキュリティ・品質監査](audits/security-and-quality-audit.md) - 既知の問題、推奨対応、mise導入方針
