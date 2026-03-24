# claude-forge — 防弾技術仕様

[English](README.md) | [한국어](README.ko.md) | [中文](README.zh.md)

```
╔════════════════════════════════════════════════════════════════╗
║                                                                ║
║   Ralph Mode                    Lisa Mode                      ║
║                                                                ║
║   code -> fail                  spec -> verify                 ║
║     -> code -> fail               -> spec -> verify            ║
║       -> code -> fail               -> spec -> verify          ║
║         -> code -> fail               -> bulletproof spec      ║
║           -> ...                        -> code                ║
║             -> works?                     -> works. first try. ║
║                                                                ║
║   Loop the CODE (expensive)     Loop the DOCS (safe to fail)   ║
║   builds, tests, side effects   no builds, no tests, no breakage║
║                                                                ║
║                    claude-forge is Lisa Mode.                           ║
║                                                                ║
╚════════════════════════════════════════════════════════════════╝
```

> **ラルフはコードをループする。リサはドキュメントをループする。**
> どちらも成功するまで反復する——しかしドキュメントの変更コストはゼロ。
> ビルドなし、テストなし、副作用なし。仕様が完璧であれば、
> AIは初回で動作するコードを生成します。

## フルライフサイクル

```mermaid
flowchart LR
    subgraph Blueprint
        S["スコーピング"] --> R["リサーチ"] --> D["ドラフト"] --> V["検証ループ"]
        V --> SIM["シミュレーション"] --> DB["ディベート"] --> F["確定"]
    end
    subgraph Implement
        IMP["実装"] --> T["テスト"] --> CSM["シミュレーション"] --> RV["レビュー"] --> SY["同期"] --> ST["ステータス"]
    end
    F --> IMP
    ST --> DONE["完了"]
```

```mermaid
flowchart LR
    subgraph Fix
        DG["診断"] --> FS["修正仕様"] --> SM["シミュレーション"]
        SM --> ER["エキスパートレビュー"] --> VR["検証"] --> IM["実装"] --> TE["テスト"]
    end
    TE --> FD["完了"]
```

```mermaid
flowchart LR
    subgraph Debug
        P["6つの視点"] --> CR["クロスリファレンス"] --> HY["仮説"]
        HY --> S2["シミュレーション"] --> D2["ディベート"] --> V2["検証"] --> FM["final.md"]
    end
    FM --> IMP2["/implement"] --> DONE2["完了"]
```

forgeは**企画 → 構築 → 検証**を単一の自動化パイプラインとしてカバーします。

## インストール

```bash
# ワンラインインストール（macOS / Linux）
curl -fsSL https://raw.githubusercontent.com/imtemp-dev/claude-forge/main/install.sh | bash

# またはソースからビルド（Go 1.22+）
git clone https://github.com/imtemp-dev/claude-forge.git
cd claude-forge
make install    # ~/.local/bin/forgeにインストール
```

`~/.local/bin`がPATHにない場合、`.zshrc`または`.bashrc`に追加：
```bash
export PATH="$HOME/.local/bin:$PATH"
```

アップデート：
```bash
git pull && make install
```

## クイックスタート

```bash
# プロジェクトを初期化
forge init .

# Claude Codeを起動
claude

# 完璧な仕様を作成
/recipe blueprint "add OAuth2 authentication"

# 既知のバグを修正
/recipe fix "login bcrypt hash comparison fails"

# 未知の問題をデバッグ
/recipe debug "session drops after 5 minutes"

# コード品質レビュー
/forge-review
/forge-review security src/auth/

# プロジェクトの健全性チェック
forge doctor
```

## レシピ

| レシピ | 用途 | 出力 |
|--------|------|------|
| `/recipe analyze` | 既存システムの理解 | Level 1 分析ドキュメント |
| `/recipe design` | 機能設計 | Level 2 設計ドキュメント |
| `/recipe blueprint` | 完全な実装仕様 | Level 3 仕様 → コード → テスト |
| `/recipe fix` | 既知のバグ修正（軽量） | 修正仕様 → コード → テスト |
| `/recipe debug` | 未知のバグ調査 | 6視点分析 → 仕様 → コード |

## スキル（19個）

| カテゴリ | スキル |
|----------|--------|
| **レシピ** | blueprint, design, analyze, fix, debug |
| **検証** | verify, cross-check, audit, assess, sync-check |
| **分析** | research, simulate, debate, adjudicate |
| **実装** | implement, test, sync, status |
| **品質** | review（基本 / セキュリティ / パフォーマンス / パターン）|

## コア原則

- **ドキュメントファースト**：コードではなく仕様を反復する
- **自己検証禁止**：検証は独立したエージェントコンテキストで実行
- **コンテキストが接着剤**：スキルはルール強制ではなく状況認識を提供
- **差異 = フォローアップ**：仕様とコードの違いはレポートであり、ゲートではない
- **クラッシュ回復**：tasks.json + work-state.jsonでワークステートを永続化
- **高速**：単一Goバイナリ、ランタイム依存ゼロ、約5ms起動

## ビジョン＆ロードマップ

大規模な機能リクエストは管理可能なレシピ単位に自動分解されます：
- `vision.md`：最終製品のビジョン（目的、コンポーネント、制約）
- `roadmap.md`：チェックボックスベースの順序付きレシピ分解

レシピ完了時にロードマップ項目は自動的に `[x]` にマークされ、
次のセッションで次の項目を案内します。

## CLI

```
forge init [dir]              プロジェクト初期化
forge doctor [recipe-id]      レシピ健全性チェック（ドキュメント、マニフェスト、フロー、ビジョン/ロードマップ）
forge validate [recipe-id]    JSONスキーマ準拠チェック
forge version                 バイナリおよびテンプレートバージョン表示
forge update                  最新テンプレートのデプロイ
forge recipe status           アクティブレシピ表示
forge recipe list             全レシピ一覧
forge recipe log <id>         アクション/フェーズ/イテレーション記録
forge recipe cancel           アクティブレシピキャンセル
```

## ライセンス

MIT
