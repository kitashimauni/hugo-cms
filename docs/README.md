# ドキュメント

Hugo CMSの利用方法、仕様、設計、および監査結果を目的別に整理している。

## ガイド

- [設定ガイド](guides/configuration.md) - 環境変数と実行設定
- [デプロイガイド](guides/deployment.md) - サーバー、Docker、リバースプロキシ
- [Docker + mise デプロイガイド](guides/docker-mise-deployment.md) - secret-free one-shot bootstrap、非root app、サイト別toolchainの運用
- [`.homecms.yml` 移行ガイド](guides/migrating-to-homecms.md) - legacy `static/admin/config.yml` からの移行
- [リリース前チェックリスト](guides/release-checklist.md) - リリース候補PRで確認する項目
- [Smoke Test Checklist](guides/smoke-tests.md) - 変更後の最低限確認項目

## リファレンス

- [APIリファレンス](reference/api.md) - HTTP APIの仕様
- [CMS設定](reference/cms-config.md) - コレクション、フィールド、メディア設定
- [スニペット機能](reference/snippets.md) - VS Code形式スニペットの設定

## アーキテクチャ

- [現行アーキテクチャ](architecture/current-architecture.md) - 現在のHugo向け実装
- [マルチサイト・マルチジェネレーター設計](architecture/multi-site-generator-design.md) - 複数HugoサイトとEleventy等へ対応するための提案
- [本文プレビューとデプロイプレビュー](architecture/preview-deployment-design.md) - preview責務、draft branch、provider、security、cleanup
- [ADR-0001: Cloudflare Pages preview](architecture/adr-0001-cloudflare-pages-preview.md) - 初期providerの選定理由

## 監査

- [セキュリティ・品質監査](audits/security-and-quality-audit.md) - 既知の問題、推奨対応、mise導入方針
