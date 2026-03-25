# claude-forge

コードの前に検証を — forgeは仕様のエラーがデバッグセッションになる前に捕捉します。

[![CI](https://github.com/imtemp-dev/claude-forge/actions/workflows/ci.yml/badge.svg)](https://github.com/imtemp-dev/claude-forge/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/imtemp-dev/claude-forge)](https://github.com/imtemp-dev/claude-forge/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://go.dev)

[English](README.md) | [한국어](README.ko.md) | [中文](README.zh.md)

## Why

Claude Codeを本格的に使っているなら、おそらく独自のプロセスを構築しているはずです：AIに全体アーキテクチャを思い出させ、出力をレビューさせ、エッジケースを確認する。これは正しいアプローチです。

しかし、毎回手動で行うと現実的な限界があります：

- **一貫性がない。** あるセッションではレビューを依頼し、別のセッションでは忘れる。品質はその日の注意深さに左右されます。
- **エラーは早く見つけるほど安い。** 計画のミスがそのままコードになると複数ファイルに広がり、修正にはビルドとデバッグが必要です。計画段階で見つければテキスト修正で済みます。しかし検証ステップがなければ、計画レベルのエラーはコードの問題になる前に発見される機会がありません。
- **実装がゴールを飲み込む。** AIは計画を立てられますが、コードに深く入ると — 型エラーの修正、テスト失敗の追跡 — 完成したシステムが全体としてどうあるべきかを見失います。forgeがインテント、スコープ、ワイヤーフレームから始めるのはこのためです：詳細がコンテキストを埋め尽くす前に全体像を確立します。

パターンは常に同じです：会話で品質管理をしていますが、毎セッションゼロからやり直し、前回発見したことが今回も発見される保証はありません。

## forgeがやること

forgeはClaude Codeのライフサイクルフックに接続するCLIツールです。すでに行っているプロセスを構造化し — 自動的に、追跡可能に、独立したAIコンテキストで検証します。

**構造化された全体像優先。** コードの前に、forgeはインテント探索、スコープ定義、ワイヤーフレーム設計を行います。これにより以降のすべてのステップ — ドラフト、検証、実装 — が参照できるゴールを持ち、AIが全体を犠牲にして目の前の問題に最適化するのを防ぎます。

**独立した検証。** 同じセッションでAIが自分の出力をレビューすると、同じブラインドスポットを共有します。forgeは別のエージェントコンテキストで検証を実行します — ドキュメントを生成した会話履歴を共有しない別のAIインスタンスです。

**セッション間の状態追跡。** forgeは検証で発見されたすべての問題を記録し、解決状況を追跡し、セッションやコンテキスト圧縮を越えて永続化します。セッションが再開されると、どこまで進んだか、何が未解決かを正確に把握しています。

**完了ゲート。** 検証をパスしなければ仕様を確定できません。テスト合格、レビュー完了、仕様とコードの乖離が文書化されるまで実装を完了できません。これらのゲートは自動的に適用されます — あなたが確認を忘れても関係ありません。

基本的なアイデアはシンプルです：**エラーをコードではなくドキュメントで捕捉する。** 仕様の修正はテキスト編集。コードの修正はビルド-テスト-デバッグサイクル。実装前にフィルタリングされるエラーが多いほど、実装後の手戻りが減ります。

## クイックスタート

[Claude Code](https://docs.anthropic.com/en/docs/claude-code)が必要です。

```bash
# Homebrew (macOS / Linux)
brew tap imtemp-dev/tap
brew install forge

# またはワンラインインストール
curl -fsSL https://raw.githubusercontent.com/imtemp-dev/claude-forge/main/install.sh | bash

# またはソースからビルド (Go 1.22+)
git clone https://github.com/imtemp-dev/claude-forge.git && cd claude-forge && make install

# プロジェクトで初期化
cd your-project
forge init .

# Claude Codeを起動
claude
```

Claude Code内で：

```bash
# 完璧な仕様を作成 → 実装 → テスト → 完了
/forge-recipe-blueprint OAuth2認証を追加

# 既知のバグを修正
/forge-recipe-fix ログインbcryptハッシュ比較失敗

# 未知の問題をデバッグ
/forge-recipe-debug 5分後にセッションが切断される
```

## 仕組み

forgeは作業を**仕様**と**実装**の2フェーズに分けます。各レシピタイプは独自の仕様フェーズを持ちますが、すべて同じ実装ループを共有します。

仕様フェーズでは、forgeはドキュメントを反復します — インテントの探索、コードベースの調査、詳細な設計の起草、独立したAIコンテキストでの複数ラウンドの検証。ここで発見されたエラーはテキスト編集で修正できます。

実装フェーズでは、forgeは確定した仕様からコードを生成し、テストを実行（失敗時はリトライ）し、コードパスをシミュレーションし、品質をレビューし、差異を仕様に同期します。各ステップには要件が満たされるまで完了をブロックする自動ゲートがあります。

各レシピタイプの詳細なフローは[レシピライフサイクル](#レシピライフサイクル)を参照してください。

## レシピ

| レシピ | 用途 | 出力 |
|--------|------|------|
| `/forge-recipe-blueprint` | 完全な実装仕様 | Level 3 仕様 → コード → テスト |
| `/forge-recipe-design` | 機能設計 | Level 2 設計ドキュメント |
| `/forge-recipe-analyze` | 既存システムの理解 | Level 1 分析ドキュメント |
| `/forge-recipe-fix` | 既知のバグ修正 | 修正仕様 → コード → テスト |
| `/forge-recipe-debug` | 未知のバグ調査 | 6視点分析 → 仕様 → コード |

マルチ機能プロジェクトでは、forgeが作業を**ビジョン + ロードマップ**に分解します。各レシピはロードマップ項目にマッピングされ、完了は自動的に追跡されます。

## 機能

### 21スキル

| カテゴリ | スキル |
|----------|--------|
| **レシピ** | blueprint, design, analyze, fix, debug |
| **発見** | discover, wireframe |
| **検証** | verify, cross-check, audit, assess, sync-check |
| **分析** | research, simulate, debate, adjudicate |
| **実装** | implement, test, sync, status |
| **品質** | review (basic / security / performance / patterns) |

### ライフサイクルフック

| フック | 用途 |
|--------|------|
| session-start | コンテキスト認識再開（レシピ状態 + 次ステップヒントを注入） |
| stop | 完了ゲート（完了前に仕様、テスト、レビューを検証） |
| pre-compact | コンテキスト圧縮前にワークステートをスナップショット |
| session-end | セッション間再開のためワークステートを永続化 |
| post-tool-use | ツール使用メトリクス追跡（ツール名、ファイル、成功/失敗） |
| subagent-start/stop | サブエージェントライフサイクルメトリクス追跡 |

### メトリクス＆コスト推定

```
forge stats
```

```
Project Overview
────────────────────────────────────────
  Recipes:     3 complete, 1 active, 4 total
  Sessions:    12 total, 5 compactions
  Models:      claude-opus-4-6, claude-sonnet-4-6

Estimated Cost
────────────────────────────────────────
  Total:       $4.52
  Input:       $1.23
  Output:      $2.89
```

セッション別・レシピ別のトークン追跡とモデル固有のコスト推定。CSV（`--csv`）またはJSON（`--json`）で外部分析ツールにエクスポート可能。

### ステータスライン

```
forge v0.1.0 │ JWT auth │ implement 3/5 │ ctx 60%
```

Claude Codeのステータスバーでレシピの進捗、フェーズ、コンテキスト使用量をリアルタイムで確認できます。

### ドキュメント可視化

```bash
forge graph              # プロジェクト全体のドキュメント関係図
forge graph <recipe-id>  # レシピ別ドキュメントグラフ
```

ドキュメントの依存関係、ディベート結論、検証チェーンを示すmermaidダイアグラムを生成します。

## レシピライフサイクル

各レシピタイプは独自の仕様フェーズを持ちます。コードを生成するレシピはすべて同じ実装フェーズを共有します。

### 仕様フェーズ（レシピ別）

**Blueprint** — 新機能のための完全な仕様：

```mermaid
flowchart LR
    DIS["発見"] --> SC["スコープ"] --> R["調査"] --> W["ワイヤーフレーム"]
    W --> D["ドラフト"] --> V["検証"]
    V -->|"問題"| D
    V -->|"合格"| F["確定"]
```

**Fix** — 軽量な診断：

```mermaid
flowchart LR
    DIAG["診断"] --> SPEC["修正仕様"] --> F["確定"]
```

**Debug** — 多角的な根本原因分析：

```mermaid
flowchart LR
    BP["6 ブループリント"] --> CROSS["クロスリファレンス"] --> SPEC["修正仕様"] --> F["確定"]
```

**Design** / **Analyze** — 仕様のみ、実装なし：

```mermaid
flowchart LR
    R["調査"] --> D["ドラフト"] --> V["検証"]
    V -->|"問題"| D
    V -->|"合格"| F["確定"]
```

### 実装フェーズ（共通）

コードを生成するすべてのレシピは `/forge-implement` を通じて同じ実装ループに入ります：

```mermaid
flowchart LR
    F["確定仕様"] --> IMP["実装"] --> T["テスト"]
    T -->|"失敗"| IMP
    T -->|"合格"| SIM["シミュレーション"] --> RV["レビュー"] --> SY["同期"] --> DONE["完了"]
```

## アーキテクチャ

**Goバイナリ** — 単一の静的リンクバイナリ（約5ms起動）。状態管理、完了検証、テンプレートデプロイ、メトリクス追跡。Go以外のランタイム依存はゼロです。

**Claude Code統合** — 21スキルがレシピプロトコルを、8ライフサイクルフックがセッションイベント（再開、完了ゲート、メトリクス）を、6ルールが制約を処理します。検証は常に独立したエージェントコンテキストで実行されます。

## コア原則

- **ドキュメントファースト** — コードではなく仕様を反復する
- **自己検証禁止** — 検証は独立したエージェントコンテキストで実行
- **コンテキストが接着剤** — スキルはルール強制ではなく状況認識を提供
- **差異 = フォローアップ** — 仕様とコードの違いはレポートであり、ゲートではない
- **クラッシュ回復** — JSONでワークステートを永続化；セッションは自動再開
- **階層的マップ** — 軽量なプロジェクト概要、必要に応じて詳細表示
- **高速** — 単一Goバイナリ、ランタイム依存ゼロ、約5ms起動

## CLI

```
forge init [dir]              プロジェクト初期化（スキル、フック、ルールをデプロイ）
forge doctor [recipe-id]      ヘルスチェック（システム、レシピ、ドキュメント）
forge validate [recipe-id]    JSONスキーマ準拠チェック
forge verify <file>           ドキュメント一貫性チェック、レベル評価
forge recipe status           アクティブレシピ表示
forge recipe list             全レシピ一覧
forge recipe create           新しいレシピを作成
forge recipe log <id>         アクション / フェーズ / イテレーション記録
forge recipe cancel           アクティブレシピキャンセル
forge stats [recipe-id]       メトリクスとコスト推定 (--json, --csv)
forge graph [recipe-id]       ドキュメント関係可視化 (--all)
forge sync-check <id>         レシピ内ドキュメント同期確認
forge update                  バイナリバージョンに合わせてテンプレート更新
forge version                 バイナリおよびテンプレートバージョン表示
```

## 要件

- **Go** 1.22+（[インストール](https://go.dev/dl/)）
- **Claude Code**（[インストール](https://docs.anthropic.com/en/docs/claude-code)）
- **OS**：macOS、Linux（WindowsはWSL経由）

インストール後、`forge doctor`を実行して環境を確認してください。

## コントリビューション

コントリビューション歓迎です。バグ報告や機能リクエストは[issue](https://github.com/imtemp-dev/claude-forge/issues)を作成してください。

```bash
# 開発環境セットアップ
git clone https://github.com/imtemp-dev/claude-forge.git
cd claude-forge
make install          # ビルドして ~/.local/bin にインストール
go test -race ./...   # テスト実行
```

## ライセンス

MIT
