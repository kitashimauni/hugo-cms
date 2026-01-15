# スニペット機能

Hugo CMSは、Markdown編集時に定型文を簡単に挿入できるスニペット機能をサポートしています。
VS Code互換のスニペット形式 (`.code-snippets` または `.json`) を使用します。

## 設定

スニペットファイルの場所は環境変数 `SNIPPET_PATHS` で指定します。

```env
# デフォルト
SNIPPET_PATHS=repo/.vscode/md.code-snippets

# 複数ファイルを指定する場合（カンマ区切り）
SNIPPET_PATHS=repo/.vscode/global.code-snippets,repo/.vscode/team.code-snippets
```

## スニペットファイルの形式

標準的な VS Code スニペット形式 (JSON with Comments / JSONC) をサポートしています。

```jsonc
{
  "Shortcode Example": {
    "prefix": "shortcode",
    "body": [
      "{{< ${1:shortcode_name} >}}",
      "$0",
      "{{< /${1:shortcode_name} >}}"
    ],
    "description": "Insert a Hugo shortcode"
  },
  "Markdown Only Snippet": {
    "prefix": "md-note",
    "body": "> **Note:** $1",
    "scope": "markdown" // scopeを指定すると、markdownが含まれる場合のみ表示されます
  }
}
```

### スコープ (Scope)

`scope` プロパティを使用することで、特定言語向けのスニペットのみを表示するように制御できます。

- **グローバルスニペット**: `scope` プロパティがない、または空の場合。常に表示されます。
- **Markdown専用**: `scope` に `markdown` が含まれている場合（例: `"scope": "markdown, plaintext"`）。表示されます。
- **その他**: `scope` が定義されており、かつ `markdown` が含まれていない場合（例: `"scope": "javascript"`）。**無視されます**。

これにより、プロジェクト内の既存の `.vscode` フォルダにあるスニペットファイルをそのまま流用しつつ、Markdown編集に関係のないスニペット（JavaScriptやGoなど）を除外することができます。

## 使い方

1. 記事編集画面を開きます。
2. ツールバーの "Snippet" ボタン（📝）をクリックするか、ショートカットキー（設定されている場合）を使用します。
3. 利用可能なスニペットの一覧が表示されます。
4. 挿入したいスニペットを選択します。
5. スニペットにプレースホルダー（`${1:default}` など）が含まれている場合、入力フォームが表示されます。
