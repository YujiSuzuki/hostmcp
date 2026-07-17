# HostMCP

[English README is here](README.md)

**AI エージェントのためのホスト OS への制御されたアクセス（MCP 経由）**

HostMCP は、ホスト OS 上で動作する MCP サーバーです。AI Sandbox 内の AI アシスタント（Claude Code、Gemini Code Assist など）が、Docker コンテナ・ホストツール・ホスト OS コマンドといったホスト環境全体に、セキュリティポリシーに基づいて安全にアクセスできるようにします。

HostMCP を使用する AI Sandbox テンプレートについては [ai-sandbox](https://github.com/YujiSuzuki/ai-sandbox) を参照してください。

---

## 目次

- [機能](#機能)
- [インストール](#インストール)
- [サーバー起動](#サーバー起動)
- [AIアシスタントとの接続](#aiアシスタントとの接続)
  - [パターン A: ai-sandbox テンプレートを使う場合](#パターン-a-ai-sandbox-テンプレートを使う場合)
  - [パターン B: 任意の DevContainer](#パターン-b-任意の-devcontainer-で使う場合ai-sandbox-なし)
  - [パターン C: Claude Desktop など](#パターン-c-claude-desktop-などのデスクトップ-ai-アプリ)
- [CLIコマンド](#cliコマンド)
  - [セットアップコマンド](#セットアップコマンド)
  - [カレントコンテナコマンド](#カレントコンテナコマンド)
  - [ホストツール管理コマンド](#ホストツール管理コマンド)
  - [ホストOSコマンド（直接Dockerアクセス）](#ホストosコマンド直接dockerアクセス)
  - [クライアントコマンド（HTTP API経由）](#クライアントコマンドhttp-api経由)
- [セキュリティモード](#セキュリティモード)
- [認証](#認証)
- [設定リファレンス](#設定リファレンス)
  - [ファイルアクセスブロック（blocked_paths）](#ファイルアクセスブロックblocked_paths)
  - [出力マスキング](#出力マスキング)
  - [ホストパスマスキング](#ホストパスマスキング)
  - [パーミッション](#パーミッション)
  - [デフォルトコマンド](#デフォルトコマンドexec_whitelist-)
  - [危険モード（exec_dangerously）](#危険モードexec_dangerously)
  - [ホストツール（host_access.host_tools）](#ホストツールhost_accesshost_tools)
  - [ホストコマンド（host_access.host_commands）](#ホストコマンドhost_accesshost_commands)
  - [ロギング](#ロギング)
  - [監査ログ（audit）](#監査ログaudit)
- [アーキテクチャ](#アーキテクチャ)
- [設計思想](#設計思想)
- [提供されるMCPツール](#提供されるmcpツール)
- [トラブルシューティング](#トラブルシューティング)
- [ライセンス](#ライセンス)
- [謝辞](#謝辞)

---

## 機能

- 🐳 **Docker コンテナアクセス** — ログ取得・コマンド実行・inspect・stats・ライフサイクル管理（起動/停止/再起動）
- 🔧 **ホストツール実行** — `.sandbox/host-tools/` の承認済みスクリプトを人のレビュー付きで実行
- 💻 **ホスト OS コマンド** — ホワイトリスト登録された CLI コマンドをホスト OS 上で実行
- 🔒 **セキュリティ第一の設計** — ホワイトリストベースのアクセス制御・出力マスキング・パスブロック
- 🤖 **マルチ AI サポート** — Claude Code、Gemini Code Assist で動作
- 🚀 **依存関係ゼロ** — 単一バイナリ、ランタイム要件なし
- 🌐 **クロスプラットフォーム** — Windows、macOS（Intel & Apple Silicon）、Linux
- 📝 **監査ログ** — コンプライアンス対応のため全操作を記録可能

## インストール

ホストOS上で実行してください。

**前提条件:** Go 1.25以降（`go install`やソースからのビルドが必要な場合のみ）、およびホストOS上で動作しているDocker Engine（Docker DesktopまたはOrbStack）。

**Go Install（推奨）**
```bash
go install github.com/YujiSuzuki/hostmcp@latest
```

**ビルド済みバイナリ（Go不要）**

**macOS (Apple Silicon)**
```bash
curl -L https://github.com/YujiSuzuki/hostmcp/releases/latest/download/hostmcp_darwin_arm64 -o hostmcp
chmod +x hostmcp
sudo mv hostmcp /usr/local/bin/
```

**macOS (Intel)**
```bash
curl -L https://github.com/YujiSuzuki/hostmcp/releases/latest/download/hostmcp_darwin_amd64 -o hostmcp
chmod +x hostmcp
sudo mv hostmcp /usr/local/bin/
```

**Windows**
1. [Releases](https://github.com/YujiSuzuki/hostmcp/releases)から `hostmcp_windows_amd64.exe` をダウンロード
2. フォルダに配置（例: `C:\tools\`）
3. PATHに追加するか、フルパスで使用

**Linux**
```bash
curl -L https://github.com/YujiSuzuki/hostmcp/releases/latest/download/hostmcp_linux_amd64 -o hostmcp
chmod +x hostmcp
sudo mv hostmcp /usr/local/bin/
```

**ソースからビルド**
```bash
git clone https://github.com/YujiSuzuki/hostmcp.git
cd hostmcp
make install  # ~/go/bin/ にインストール
```

## サーバー起動

### 設定ファイルの準備

`hostmcp init` を使って、ワークスペースに設定ファイルを生成できます：

```bash
hostmcp init --workspace /path/to/workspace
```

ワークスペース内の `.sandbox/config/hostmcp.yaml` が作成されます。ポート番号を同時に指定することもできます：

```bash
hostmcp init --workspace /path/to/workspace --port 18080
```

| フラグ | 説明 |
|--------|------|
| `--workspace` | 対象ワークスペースディレクトリ（必須） |
| `--port` | 生成する設定ファイルのポート番号（デフォルト: 18080） |
| `--force` | 既存の設定ファイルを上書き |

または、サンプル設定を手動でコピーすることもできます：

```bash
cp configs/hostmcp.example.yaml hostmcp.yaml
nano hostmcp.yaml
```

設定例:
```yaml
server:
  port: 18080
  host: "127.0.0.1"

security:
  mode: "moderate"  # strict, moderate, または permissive

  allowed_containers:
    - "myapp-*"
    - "mydb-*"

  exec_whitelist:
    "myapp-api":
      - "npm test"
      - "pytest /app/tests"
    "*":
      - "pwd"
```

### 起動

```bash
# ホストOSで実行
hostmcp serve --config .sandbox/config/hostmcp.yaml
```

> **代替方法:** `hostmcp init` の代わりにサンプル設定を手動でコピーした場合は、`--config` にそのファイルを指定してください（例: `hostmcp serve --config hostmcp.yaml`）。

> **スポンサーメッセージ:** `hostmcp serve` は起動時に短いスポンサーメッセージを表示します。非表示にするには `--no-thanks` を指定してください。

以下のような出力が表示されます:
```
2026-01-22 12:55:17 INFO  Starting HostMCP server version=dev security_mode=moderate port=18080 log_level=info
2026-01-22 12:55:17 INFO  Found accessible containers count=3
2026-01-22 12:55:17 INFO  MCP server listening url=http://127.0.0.1:18080 health_check=http://127.0.0.1:18080/health sse_endpoint=http://127.0.0.1:18080/sse
2026-01-22 12:55:17 INFO  Press Ctrl+C to stop
```

### Verbosity レベル

デバッグ用にログの詳細度を上げるには `-v` フラグを使用します：

```bash
hostmcp serve --config hostmcp.yaml -v      # レベル1: JSONリクエスト/レスポンス出力
hostmcp serve --config hostmcp.yaml -vv     # レベル2: DEBUGレベル + JSON出力
hostmcp serve --config hostmcp.yaml -vvv    # レベル3: フルデバッグ（全ノイズ表示）
hostmcp serve --config hostmcp.yaml -vvvv   # レベル4: フルデバッグ + HTTPヘッダー
```

| レベル | フラグ | 説明 |
|--------|--------|------|
| 0 | (なし) | 通常のINFOレベル、最小出力 |
| 1 | `-v` | JSONリクエスト/レスポンス表示、未初期化接続をフィルタ |
| 2 | `-vv` | DEBUGレベル + JSON出力、未初期化接続をフィルタ |
| 3 | `-vvv` | フルデバッグ、ノイズを含む全接続を表示 |
| 4 | `-vvvv` | フルデバッグに加え、生のHTTPヘッダーも表示 |

**注:** 「ノイズ」とは未初期化のSSE接続（例：VS Code拡張機能のプローブ）を指します。レベル0-2ではこれらをフィルタしてログをきれいに保ちます。

### ログ機能

**リクエスト番号:** 各リクエストには一意の番号 `[#N]` が割り当てられ、複数のリクエストが同時に処理される際の追跡に使えます：

```
═══ [#1] ═══════════════════════════════════════════════════════════
▼ REQUEST client=claude-code method=tools/call tool=list_containers id=2
...
═══ [#1] ═══════════════════════════════════════════════════════════
```

**クライアント識別:** サーバーはログにクライアント名（MCP `clientInfo` から取得）を表示します：
- `claude-code` - Claude Code拡張機能
- `hostmcp-go-client` - HostMCP CLIクライアント（`--client-suffix` でカスタムサフィックス対応）

**グレースフルシャットダウン:** サーバー停止時（Ctrl+C）：
- アクティブな接続が閉じるまで最大2秒待機
- タイムアウト後は残りの接続を強制クローズ
- 未初期化接続のUser-Agentサマリーを表示：
  ```
  Uninitialized connection summary: claude-code/2.1.7: 81, node: 1
  ```

### 複数インスタンスの起動

ポートと設定ファイルを分けることで、用途別に複数のHostMCPサーバーを同時に起動できます：

```bash
# 開発インスタンス（寛容）
hostmcp serve --port 18080 --config dev.yaml

# 別プロジェクト用（厳格）
hostmcp serve --port 8081 --config strict.yaml
```

## AIアシスタントとの接続

HostMCP は MCP に対応した任意の AI クライアントと連携できます。環境に合わせたパターンを選んでください。

### パターン A: ai-sandbox テンプレートを使う場合

[ai-sandbox](https://github.com/YujiSuzuki/ai-sandbox) テンプレートを使うと、`setup-hostmcp.sh` で MCP 登録が自動化されます。詳細は ai-sandbox の README を参照してください。

### パターン B: 任意の DevContainer で使う場合（ai-sandbox なし）

既存の DevContainer にそのまま導入できます。テンプレートの移行は不要です。

**1. ホスト OS で HostMCP を起動**
```bash
hostmcp init --workspace /path/to/your-project
hostmcp serve --config /path/to/your-project/.sandbox/config/hostmcp.yaml
```

**2. DevContainer 内で MCP サーバーを登録**
```bash
# Claude Code
claude mcp add --transport sse --scope user hostmcp \
  http://host.docker.internal:18080/sse

# Gemini CLI
gemini mcp add --transport sse hostmcp \
  http://host.docker.internal:18080/sse
```

**3. 再接続**

Claude Code の場合: `/mcp` → 「Reconnect」を選択

> `host.docker.internal` は Docker Desktop / OrbStack 環境であれば、コンテナ内から自動的にホスト OS に解決されます。追加設定は不要です。

### パターン C: Claude Desktop などのデスクトップ AI アプリ

Claude Desktop は MCP 経由でしか外部ツールにアクセスできないため、`docker` コマンドを直接実行できません。HostMCP がそのギャップを埋めます。

**1. ホスト OS で HostMCP を起動**（上記と同じ）

**2. Claude Desktop に追加**

Claude Desktop の設定 → MCP サーバー → サーバーを追加:

```json
{
  "mcpServers": {
    "hostmcp": {
      "url": "http://localhost:18080/sse"
    }
  }
}
```

または CLI で:
```bash
claude mcp add --transport sse hostmcp http://localhost:18080/sse
```

> Claude Desktop はホスト OS 上で直接動作するため、`host.docker.internal` ではなく `localhost` を使います。

---

接続後、AI アシスタントがコンテナにアクセスできるようになります:

```
ユーザー: "myapp-api コンテナのログを確認して"
Claude: [HostMCP を使用] "ログに 500 エラーが見えます..."

ユーザー: "API コンテナでテストを実行して"
Claude: [HostMCP を使用] "npm test を実行中... 3 つのテストが通過"
```

## CLIコマンド

HostMCPは2種類のCLIコマンドを提供します:

### セットアップコマンド

```bash
# ワークスペースに設定ファイルを生成
hostmcp init --workspace /path/to/workspace

# ポート番号を指定して生成
hostmcp init --workspace /path/to/workspace --port 18080

# 既存の設定ファイルを上書き
hostmcp init --workspace /path/to/workspace --force
```

> **`--config` と `--workspace` の違い:** `hostmcp serve`、`hostmcp tools sync`、`hostmcp tools list` はいずれも `--config <path>` または `--workspace <path>` のどちらかが必要です（併用不可）。`--workspace` を指定した場合、設定ファイルは `{workspace}/.sandbox/config/hostmcp.yaml` として自動的に導出されます。`workspace_root`（[ホストツール](#ホストツールhost_accesshost_tools)を参照）は、ホストツールのステージング・承認や相対パス解決の基準となる「プロジェクトルート」ディレクトリです。通常は `--workspace` から決まりますが、`--config` を直接指定した場合は設定ファイル自身の `host_access.workspace_root` の値が使われます。同じ `hostmcp.yaml`（`--config`）を使い回しつつ別のワークスペースディレクトリを指定したい場合は、代わりに `--workspace-root` を使用してください。こちらは `workspace_root` のみを上書きし、設定ファイルの解決には影響しません。

### カレントコンテナコマンド

「カレントコンテナ」機能（設定の `cli.current_container`、デフォルト有効）を使うと、`logs`・`stats`・`exec` などのコマンドで毎回 `-c`/`--container` を指定する必要がなくなります：

```bash
# カレントコンテナを設定
hostmcp use myapp-api

# logs/stats/exec はカレントコンテナに対して実行される
hostmcp logs
hostmcp stats
hostmcp exec "npm test"

# カレントコンテナをクリア
hostmcp use --clear
```

> **セキュリティ注意:** AIアシスタントがMCPを経由せずホストOS上で直接CLIコマンドを実行できる非サンドボックス環境では、カレントコンテナ機能が予期しない動作につながる可能性があります。その場合は `cli.current_container.enabled: false` を設定してください。

### ホストツール管理コマンド

ステージングディレクトリ（例：`.sandbox/host-tools/`）に配置されたホストツールは、HostMCPが実行する前に承認が必要です：

```bash
# ステージング済みのホストツールをインタラクティブにレビュー・承認
hostmcp tools sync

# 同上、承認後すぐにサーバーを起動
hostmcp serve --sync

# 承認済み・承認待ちのホストツールを一覧表示
hostmcp tools list
```

> **注意:** `--workspace`/`--workspace-root` を明示的に指定せず（つまり`workspace_root`が`--config`の設定ファイルの値そのまま）に`hostmcp tools sync`を実行すると、承認処理の前に解決済みのワークスペースを確認するプロンプト（`Continue with this workspace? [y/N]`）が表示されます。無入力やEOFの場合はデフォルトで**拒否（no）**になるため、CIなどで`--workspace`/`--workspace-root`を指定せずに非対話実行すると、同期されずに中断されます。

> **開発モード:** `hostmcp serve --dev` を使うと、`hostmcp tools sync` による承認ステップを経ずに、`staging_dirs` からツールを直接読み込みます（優先順位: staging > approved > common）。ホストツールをローカルで反復開発する際には便利ですが、人によるレビューを省略するため、共有環境や本番相当の環境では避けてください。

### ホストOSコマンド（直接Dockerアクセス）

Dockerソケットに直接アクセスするため、**ホストOS上**で実行します:

```bash
# アクセス可能なコンテナを一覧表示
hostmcp list

# コンテナログを取得
hostmcp logs myapp-api --tail 100

# ホワイトリスト登録されたコマンドを実行
hostmcp exec -c myapp-api "npm test"

# コンテナ詳細をサマリー付きで表示（デフォルト）
hostmcp inspect myapp-api

# コンテナ詳細をフルJSON出力で表示
hostmcp inspect myapp-api --json

# コンテナ統計を取得
hostmcp stats myapp-api
```

> **注意:** `logs`/`inspect`/`stats`と異なり、`exec`は実行するコマンドを位置引数に取るため、コンテナは`-c`/`--container`で指定する必要があります（[カレントコンテナ](#カレントコンテナコマンド)が設定されていれば省略可能）。

**`list` 出力例：**
```
NAME              ID            IMAGE           STATE    STATUS          PORTS
myapp-api         4a2e541171d9  node:18-alpine  running  Up 5 minutes    0.0.0.0:3000->3000/tcp
myapp-proxy       8b3f621283e1  nginx:alpine    running  Up 5 minutes    0.0.0.0:80->80/tcp
```

**`inspect` サマリー出力例：**
```
=== Container Summary: myapp-api ===

State:    running
Started:  2024-01-15T10:30:00Z
Image:    node:18-alpine

--- Network ---
  bridge:
    IP:      172.17.0.2
    Gateway: 172.17.0.1

--- Ports ---
  0.0.0.0:3000 -> 3000/tcp

--- Mounts ---
  /path/to/workspace -> /app (rw)

--- Full Details (JSON) ---
{ ... }
```

### クライアントコマンド（HTTP API経由）

HostMCPサーバーにHTTP経由で接続するため、**AI Sandbox内**でも使用できます:

```bash
# HostMCPサーバー経由でコンテナを一覧表示
hostmcp client list

# サーバー経由でコンテナログを取得
hostmcp client logs securenote-api --tail 100

# サーバー経由でコンテナ詳細を表示（デフォルトはサマリー）
hostmcp client inspect securenote-api

# サーバー経由でコンテナ詳細を表示（フルJSON）
hostmcp client inspect securenote-api --json

# サーバー経由でコンテナ統計を取得
hostmcp client stats securenote-api

# サーバー経由でコマンドを実行
hostmcp client exec securenote-api "npm test"

# カスタムサーバーURL
hostmcp client list --url http://localhost:18080

# または環境変数を使用
export HOSTMCP_SERVER_URL=http://host.docker.internal:18080
hostmcp client list

# タイムアウトを指定（秒）
hostmcp client --timeout 120 exec securenote-api "npm run build"

# または環境変数を使用
export HOSTMCP_TIMEOUT=120
hostmcp client exec securenote-api "npm run build"
```

**クライアントコマンド 共通フラグ:**

| フラグ | 環境変数 | デフォルト | 説明 |
|--------|---------|-----------|------|
| `--url` | `HOSTMCP_SERVER_URL` | `http://host.docker.internal:18080` | HostMCPサーバーのURL |
| `--client-suffix` / `-s` | `HOSTMCP_CLIENT_SUFFIX` | (なし) | クライアント名に追加するサフィックス |
| `--timeout` | `HOSTMCP_TIMEOUT` | `30` | HTTPリクエストおよびツール呼び出しレスポンスのタイムアウト（秒） |

> **タイムアウトについて:** `--timeout` はHTTPリクエスト送信の待機時間とSSE経由のレスポンス受信の待機時間の両方に適用されます。`npm run build` のように時間のかかるコマンドを実行する場合は、適切に延長してください。SSE接続自体（サーバーとのセッション維持）にはタイムアウトは適用されません。

**どちらを使うべきか:**
- **ホストOSコマンド**: Dockerソケットへの直接アクセスがある場合
- **クライアントコマンド**: AI Sandbox内、またはDockerソケットアクセスがない環境

## セキュリティモード

### Strictモード
- 読み取り専用操作（logs、inspect、stats）
- コマンド実行不可
- 最も制限が厳しく安全

### Moderateモード（推奨）
- 読み取り操作許可
- コマンド実行はホワイトリストに限定
- 安全性と機能性のバランスが良い

### Permissiveモード
- すべての操作許可
- 信頼された開発環境でのみ使用

## 認証

現在のバージョンでは認証機能は**実装されていません**。

HostMCPはローカル開発環境での使用を想定しており、サーバーはデフォルトでlocalhostにバインドされます。

**将来の計画:**
- 設定ファイルによるオプション認証
- リモートアクセス用のトークンベース認証
- ユーザーの需要に応じて実装予定

認証機能が必要な場合は、[Discussions](https://github.com/YujiSuzuki/hostmcp/discussions)でリクエストしてください。

## 設定リファレンス

完全な設定オプションについては [configs/hostmcp.example.yaml](configs/hostmcp.example.yaml) を参照してください:
- コンテナホワイトリストパターン
- コンテナごとのコマンドホワイトリスト
- 監査ログ
- ポートとホスト設定

### ファイルアクセスブロック（blocked_paths）


#### 設定例

```yaml
security:
  blocked_paths:
    # 手動でブロックするパス
    manual:
      "securenote-api":
        - "/.env"
        - "/secrets/*"
      "*":  # 全コンテナに適用
        - "*.key"
        - "*.pem"

    # DevContainer設定からの自動インポート
    auto_import:
      enabled: true
      workspace_root: "."

      # スキャンするファイル
      scan_files:
        - ".devcontainer/docker-compose.yml"
        - ".devcontainer/devcontainer.json"

      # グローバルパターン（全コンテナに適用）
      global_patterns:
        - ".env"
        - "*.key"
        - "secrets/*"

      # Claude Code設定からのインポート
      claude_code_settings:
        enabled: true
        max_depth: 1  # サブディレクトリをスキャンする深度
        settings_files:
          - ".claude/settings.json"
          - ".claude/settings.local.json"

      # Gemini Code Assist除外ファイル（.aiexclude, .geminiignore）からのインポート
      # 上記claude_code_settingsと同じ形式。デフォルトで有効
      gemini_settings:
        enabled: true
        max_depth: 1
        settings_files:
          - ".aiexclude"
          - ".geminiignore"
```

#### max_depth の動作

`max_depth` はClaude Code設定ファイルをスキャンする深度を制御します：

```
/workspace/                          ← hostmcp serve 起動位置
├── .claude/settings.json            ← ✅ 見る (depth 0)
├── demo-apps/
│   └── .claude/settings.json        ← ✅ 見る (depth 1)
├── demo-apps-ios/
│   └── .claude/settings.json        ← ✅ 見る (depth 1)
└── demo-apps/subproject/
    └── .claude/settings.json        ← ❌ 見ない (depth 2)
```

| max_depth | 動作 |
|-----------|------|
| 0 | workspace_root のみ |
| 1 | 1階層下まで |
| 2 | 2階層下まで |

#### Claude Code設定との連携

Claude Codeの `.claude/settings.json` にある `permissions.deny` パターンを自動的にインポートできます：

```json
{
  "permissions": {
    "deny": [
      "Read(securenote-api/.env)",
      "Read(**/*.key)",
      "Read(**/secrets/**)"
    ]
  }
}
```

これにより、DevContainerでのClaude Code設定とHostMCPのブロックポリシーを統一できます。

#### ブロック時のレスポンス

アクセスがブロックされると、詳細な理由が返されます：

```json
{
  "blocked": true,
  "reason": "claude_code_settings_deny",
  "pattern": "**/*.key",
  "source": "demo-apps/.claude/settings.json",
  "hint": "This path is blocked by Claude Code settings (permissions.deny)..."
}
```

### 出力マスキング

HostMCPはツール出力内の機密データ（パスワード、APIキー、トークン）をAIアシスタントに返す前に自動的にマスクします。

```yaml
security:
  output_masking:
    enabled: true
    replacement: "[MASKED]"

    # 機密データを検出する正規表現パターン
    patterns:
      - '(?i)(password|passwd|pwd)\s*[=:]\s*["'']?[^\s"''\n]+["'']?'
      - '(?i)(api[_-]?key|secret[_-]?key)\s*[=:]\s*["'']?[^\s"''\n]+["'']?'
      - '(?i)bearer\s+[a-zA-Z0-9._-]+'
      - 'sk-[a-zA-Z0-9]{20,}'
      - '(?i)(postgres|mysql|mongodb|redis)://[^:]+:[^@]+@'

    # マスクする出力
    apply_to:
      logs: true      # get_logs, search_logs
      exec: true      # exec_command
      inspect: true   # inspect_container（環境変数）
```

**例：**
```
# 生の出力
DATABASE_URL=postgres://admin:secret123@db:5432/app

# マスク後
DATABASE_URL=[MASKED]db:5432/app
```

### ホストパスマスキング

ホストOSのパスにユーザーのホームディレクトリが含まれる場合、ホームディレクトリ部分をマスクしてAIから見えないようにします。

```yaml
security:
  host_path_masking:
    enabled: true           # パスマスキングを有効化（デフォルト: true）
    replacement: "[HOST_PATH]"
```

**対応パス：**
- macOS: `/Users/username/...` → `[HOST_PATH]/...`
- Linux: `/home/username/...` → `[HOST_PATH]/...`
- Windows: `C:\Users\username\...` → `[HOST_PATH]\...`

**例（inspect_container出力）：**
```json
// 生の出力
{"Source": "/Users/john/workspace/myapp/.env"}

// マスク後
{"Source": "[HOST_PATH]/workspace/myapp/.env"}
```

> **注意:** このマスキングはMCPツール出力にのみ適用されます。CLIコマンド（`hostmcp inspect`）はユーザー向けにフルパスを表示します。

### パーミッション

グローバルに許可する操作を制御：

```yaml
security:
  permissions:
    logs: true       # ログ取得を許可（get_logs, search_logs）
    inspect: true    # コンテナ検査を許可
    stats: true      # リソース統計を許可
    exec: true       # exec実行を許可（exec_whitelistの対象）
    lifecycle: false # start/stop/restart_containerを許可（デフォルト無効）
```

### デフォルトコマンド（exec_whitelist `"*"`）

`"*"` をコンテナ名として使用すると、全コンテナで利用可能なコマンドを定義できます：

```yaml
security:
  exec_whitelist:
    # コンテナ固有のコマンド
    "myapp-api":
      - "npm test"
      - "npm run lint"

    # 全コンテナのデフォルトコマンド
    "*":
      - "pwd"
      - "whoami"
      - "date"
```

> ⚠️ **セキュリティ警告:** `env`、`printenv`、`echo *` をデフォルトホワイトリストに追加しないでください。これらは秘匿情報を含む全ての環境変数を露出させる可能性があります。

> **一時的なコマンド許可:** `hostmcp serve --allow-exec <container:command>` を使うと、設定ファイルを編集せずに、起動中のサーバーに限定して `exec_whitelist` にコマンドを追加できます。一時的なデバッグ作業に便利です。複数指定する場合はフラグを繰り返してください。

### 危険モード（exec_dangerously）

ホワイトリストにない `tail`、`grep`、`cat` などのコマンドがデバッグに必要な場合、HostMCPは `blocked_paths` の制限を維持しながらこれらのコマンドを許可する「危険モード」を提供します。

#### なぜ危険モードが必要か？

Dockerの `get_logs` はstdout/stderrのみを表示します。`/var/log/app.log` のようなログファイルを見るには `tail` や `cat` などが必要です。しかし、これらを `exec_whitelist` に追加すると、秘匿情報を含む任意のファイルを読めてしまいます。

危険モードはこれを解決します：
1. 特定のコマンドを許可（例：`tail`、`cat`、`grep`）
2. ファイルパスは引き続き `blocked_paths` でチェック
3. パイプ（`|`）、リダイレクト（`>`）、パストラバーサル（`..`）はブロック

#### 設定

```yaml
security:
  exec_dangerously:
    enabled: false  # グローバル有効/無効
    commands:
      # コンテナ固有のコマンド
      "securenote-api":
        - "tail"
        - "head"
        - "cat"
        - "grep"
      # グローバルコマンド（全コンテナ）
      "*":
        - "tail"
        - "ls"
```

#### サーバー起動フラグ

設定ファイルを変更せずに起動時に危険モードを有効化：

```bash
# 特定のコンテナで有効化
hostmcp serve --dangerously=securenote-api,demo-app

# 全コンテナで有効化
hostmcp serve --dangerously-all
```

これらのフラグは：
- `exec_dangerously.enabled = true` を設定
- デフォルトコマンドを追加：`tail`、`head`、`cat`、`grep`、`less`、`wc`、`ls`、`find`

| フラグ | 動作 |
|-------|------|
| `--dangerously=container1,container2` | 既存の `exec_dangerously.commands` 設定を**クリア**し、指定コンテナのみ有効化 |
| `--dangerously-all` | 既存の設定と**マージ**し、`"*"`（全コンテナ）にコマンドを追加 |

> 💡 特定のコンテナのみに危険モードを厳密に制限したい場合は `--dangerously=container` を使用します。設定ファイルのコンテナ固有の設定を保持しながら広範囲に有効化したい場合は `--dangerously-all` を使用します。

#### 使用方法

**MCPツール（Claude Code）：**
```json
{
  "tool": "exec_command",
  "arguments": {
    "container": "securenote-api",
    "command": "tail -100 /var/log/app.log",
    "dangerously": true
  }
}
```

**CLI：**
```bash
# 直接（ホストOS）
hostmcp exec --dangerously -c securenote-api "tail -100 /var/log/app.log"

# クライアント（AI Sandbox）
hostmcp client exec --dangerously --url http://host.docker.internal:18080 securenote-api "tail -100 /var/log/app.log"
```

#### セキュリティモデル

```
dangerously=true でリクエスト
    ↓
1. exec_dangerously.enabled = true か？（サーバー設定）
    ↓
2. ベースコマンドが exec_dangerously.commands にあるか？
    ↓
3. パイプ（|）、リダイレクト（>）、パストラバーサル（..）チェック
    ↓
4. コマンドからファイルパスを抽出
    ↓
5. 各パスを blocked_paths と照合
    ↓
全チェック通過で実行
```

**設計上ブロックされる例：**
- `cat /secrets/key.pem` → `blocked_paths` でブロック
- `cat /etc/passwd | grep root` → パイプ禁止
- `cat ../secrets/key` → パストラバーサル禁止
- `rm /var/log/app.log` → `rm` は `exec_dangerously.commands` にない

> ⚠️ **セキュリティ注意:** クライアントは明示的に `dangerously=true` を設定する必要があります。この「オプトイン」設計により、危険モード使用時の意識的な承認が保証されます。

#### エラーのヒントメッセージ

ホワイトリストにないが危険モードで使用可能なコマンドを実行しようとすると、ヒント付きのエラーが表示されます：

```
command not whitelisted: tail (hint: this command is available with dangerously=true)
```

#### 利用可能なコマンドの確認

`hostmcp client commands` を使用して、ホワイトリストコマンドと危険コマンドの両方を確認できます：

```bash
$ hostmcp client commands
CONTAINER           ALLOWED COMMANDS
---------           ----------------
* (all containers)  pwd
                    whoami
securenote-api      npm test
                    npm run lint

CONTAINER           DANGEROUS COMMANDS (requires dangerously=true)
---------           ----------------------------------------------
* (all containers)  tail
                    ls
securenote-api      tail
                    cat
                    grep

Note: Commands with '*' wildcard match any suffix. Dangerous commands require dangerously=true parameter.
```

### ホストツール（host_access.host_tools）

ホストツールは、AIアシスタントが `run_host_tool` 経由でホストOS上で実行できる承認済みスクリプトです。ワークスペース内のディレクトリにステージングされたツールは、実行前に `hostmcp tools sync` で承認する必要があります（[ホストツール管理コマンド](#ホストツール管理コマンド)を参照）。

```yaml
host_access:
  workspace_root: "."

  host_tools:
    enabled: true

    # 承認済みツールディレクトリ（ワークスペース外、AIから書き込み不可）
    # ツールはプロジェクトごとに整理されます: <approved_dir>/<project-id>/
    # <project-id>はワークスペースパスから自動的に導出されます
    # （形式: <ディレクトリ名>-<短いハッシュ>、例: "my-project-a1b2c3d4"）。
    # 実際の値は `hostmcp tools list` で確認できます。
    approved_dir: "~/.hostmcp/host-tools"

    # ステージングディレクトリ：新しいツールが提案される場所（ワークスペース内）
    staging_dirs:
      - ".sandbox/host-tools"

    # approved_dirの_common/サブディレクトリからのツール読み込みを有効化（全プロジェクト共通）
    common: true

    allowed_extensions:
      - ".sh"
      - ".go"
      - ".py"
    timeout: 60  # 秒

    max_output_bytes: 102400  # 100KB。0で無効化
    large_output_dir: ".sandbox/tmp"  # workspace_root からの相対パス
```

#### 大きな出力の処理

ホストツールの出力が `max_output_bytes` を超えると、HostMCP は全出力をファイルに保存し、AIにはパスとプレビューを返します。大きなビルドログやテストレポートが AI のコンテキストを圧迫するのを防ぎます。

AI が受け取るメッセージの例:

```
Tool: my-build.sh
Exit Code: 0

⚠️  Output was large (N bytes) and has been saved to a file.
File: [HOST_PATH]/workspace/.sandbox/tmp/hostmcp-my-build-last.log
Use the Read or Grep tool to inspect the full output.

--- Preview (first/last 20 lines) ---
...
```

> **注意:** ツールを実行するたびに同じファイル（`hostmcp-<toolname>-last.log`）が上書きされます。保持されるのは最新の出力のみです。

### ホストコマンド（host_access.host_commands）

ホストツールとは別に、HostMCP は `exec_host_command` MCPツール経由で、ホワイトリスト登録されたCLIコマンドをホストOS上で直接実行できます。デフォルトでは無効です。

```yaml
host_access:
  host_commands:
    enabled: false

    # ホワイトリストコマンド（ベースコマンド → 許可された引数パターン）
    # "*"サフィックスでプレフィックスマッチング（例: "-i *" は "-i :8080" にマッチ）
    whitelist:
      "df":
        - "-h"
      "free":
        - "-m"
      "lsof":
        - "-i *"

    # 拒否リスト（ホワイトリストを上書き）
    # deny:
    #   "some-command":
    #     - "dangerous-subcommand *"

    # 危険モード: exec_host_commandがdangerously=trueで呼ばれた場合のみ許可
    dangerously:
      enabled: false
      commands:
        "kill":
          - "*"
```

> **サーバー起動フラグ:** `hostmcp serve --host-dangerously` を使うと、設定ファイルを編集せずに `host_access.host_commands.dangerously.enabled = true` を起動時に有効化できます。`exec_dangerously` 用の `--dangerously`/`--dangerously-all` と同じ、明示的なオプトイン方式です。

> **注意:** `docker`/`docker-compose` コマンドはここでは不要です。監視・調査系は `list_containers`・`get_logs`・`get_stats`・`inspect_container` で既にカバーされています。ライフサイクル操作（`docker-compose up/down/build`）にはホストツールを使ってください（例：`.sandbox/host-tools/app-up.sh` に包む）。

### ロギング

サーバーのログ設定は、設定ファイルの `logging.level` だけでなく、主にCLIフラグで行います：

```bash
hostmcp serve --config hostmcp.yaml --log-level debug --log-file server.log --log-also-stdout
```

| フラグ | 説明 |
|--------|------|
| `--log-level` | `debug`、`info`、`warn`、`error` のいずれか |
| `--log-file` | 指定したファイルパスにログを書き込む |
| `--log-also-stdout` | `--log-file` 指定時、標準出力にも同時に出力 |

### 監査ログ（audit）

監査ログは、監視とコンプライアンスのため、セキュリティ関連イベント（ツール呼び出し、アクセス拒否、クライアント接続、セキュリティポリシー照会）を構造化JSONとして記録します。デフォルトでは無効です。

```yaml
audit:
  enabled: false

  # enabled が true のとき必須。常にファイルに出力（stdoutには出力しない）
  # "~/"プレフィックスでホームディレクトリ展開をサポート
  file: "~/.hostmcp/audit.log"

  # サーバー起動時のローテーション: audit.log -> audit.log.1 -> audit.log.2 ...
  # keep 件を超えたファイルは削除。keep: 0 でローテーション無効化
  rotation:
    keep: 3

  events:
    tool_calls: true          # exec_command, get_logs, read_file など
    access_denied: true       # ブロックパス、不許可コマンド、権限エラー
    client_connections: true  # クライアントの接続/切断
    security_policy: false    # get_security_policy, get_blocked_paths の照会
```

監査ログエントリの例:

```json
{
  "time": "2024-01-15T10:30:45.123Z",
  "level": "INFO",
  "msg": "audit_event",
  "event_type": "tool_call",
  "tool": "exec_command",
  "container": "myapp-api",
  "result": "success",
  "details": {"command": "npm test", "duration_ms": 1234}
}
```

## アーキテクチャ

```
┌─────────────────────────────────┐
│ ホストOS                         │
│  ┌──────────────────────────┐   │
│  │ HostMCP (Goバイナリ)    │   │
│  │ - MCPサーバー(HTTP/SSE)  │   │
│  │ - セキュリティポリシー    │   │
│  └────────┬─────────────────┘   │
│           │ :18080                │
│  ┌────────┴─────────────────┐   │
│  │ Docker Engine            │   │
│  │  ├─ AI Sandbox            │   │
│  │  │   └─ Claude Code ─┐   │   │
│  │  ├─ app-api ←─────────┘   │   │
│  │  └─ app-db              │   │
│  └─────────────────────────┘   │
└─────────────────────────────────┘
```

## 設計思想

**なぜ HostMCP は `docker-compose up/down` やイメージリビルドをサポートしないのか？**

HostMCP は AI と人の責任を明確に分離しています。AI は観察と提案を担い、人がインフラ変更を実行します。アクセスは段階的に提供され、各レベルをオプトインで有効化できます。

### 基本設計原則

```
AI = 目と口（観察する、提案する）
人間 = 手（インフラ変更を実行する）
```

**AI ができること（デフォルト）：**
- ログ、統計情報、コンテナ情報の読み取り
- ホワイトリストに登録されたコマンドの実行（テスト、リント）
- ファイルの読み取り（blocked_paths による保護付き）
- 変更や解決策の提案

**AI ができること（オプトイン）：**
- コンテナの起動/停止/再起動（`lifecycle: true`）
- 承認済みホストツールの実行（host_tools — デフォルト有効）
- ホワイトリスト登録されたホストコマンドの実行（host_commands）

**人がやること：**
- イメージのリビルド（`docker-compose build`）
- コンテナの再作成（`docker-compose up`）
- ホストツールの承認（`hostmcp tools sync`）
- インフラの変更

### 段階的アクセスモデル

HostMCP は 5 段階のアクセスレベルを提供します。上位ほど権限が広くなります。

| レベル | 操作 | デフォルト | リスク |
|--------|------|-----------|--------|
| **読み取り** | ログ、統計、inspect、ファイル一覧 | 有効 | なし |
| **コマンド実行** | コンテナ内のホワイトリストコマンド | 有効（moderate モード） | 低 |
| **ライフサイクル** | コンテナの起動/停止/再起動 | **無効** | 中 |
| **ホストツール** | 承認済みホストツールスクリプト | 有効 | 中 |
| **ホストコマンド** | ホワイトリスト登録されたホスト CLI コマンド | **無効** | 高 |

ライフサイクルとホストコマンドはデフォルトで無効であり、`hostmcp.yaml` で明示的にオプトインする必要があります。ホストツールはデフォルトで有効ですが、実行前に人の承認（`hostmcp tools sync`）が必要です。

### なぜビルド/再作成は人のみなのか？

#### 1. Dockerfile の変更にはリビルドが必要

Dockerfile を変更した場合、単純な `restart` では変更が反映されません：

```bash
# これでは Dockerfile の変更は反映されない
docker restart myapp  # 古いイメージのまま

# 実際に必要なのはこれ
docker-compose build myapp
docker-compose up -d myapp
```

コンテナの再起動はクラッシュからの復旧や設定変更の反映には有効ですが、フルリビルドの代わりにはなりません。HostMCP が `docker-compose build` や `docker-compose up` を MCP ツールとして直接提供しないのは、再起動ですべて解決するという誤解を防ぐためです。

> **補足:** ホストツールを使えば、これらの操作を人がレビューしたスクリプト（`demo-build.sh`、`demo-up.sh` など）に包んで AI に実行させることができます。スクリプトは明示的に承認されたものだけが実行されるため、制御されたアクセスが確保されます。

#### 2. ほとんどの開発作業にコンテナ操作は不要

| アクション | 対応方法 | コンテナ操作が必要？ |
|-----------|---------|-------------------|
| コード変更 | ホットリロード / `exec npm run dev` | 不要 |
| 設定ファイル変更 | アプリ再読み込みコマンド | 不要 |
| テスト実行 | `exec npm test` | 不要 |
| ログ確認 | `get_logs` | 不要 |
| コンテナがクラッシュ | `restart_container`（オプトイン） | 必要 |
| Dockerfile 変更 | リビルド + 再作成 | **必要、人間が実行** |

イメージリビルドが本当に必要なケース（Dockerfile 変更、docker-compose.yml 変更）は**インフラの変更**であり、人間のレビューを経るべきです。

#### 3. リスク vs 頻度のトレードオフ

| 操作レベル | リスク | 開発中の頻度 |
|-----------|-------|-------------|
| ログ/統計の読み取り | なし | 非常に高い |
| ホワイトリストコマンド実行 | 低 | 高い |
| コンテナ再起動 | 中 | 低い |
| ビルド/再作成 | 高 | 非常に低い |

コンテナの再起動は、本当に必要な場面（クラッシュからの復旧、環境変数変更の反映）に対応するためオプトインで提供されています。ビルド/再作成は高リスクかつ低頻度のため、人のみが実行します。

#### 4. AI は調査し、人がインフラを操作する

**良いワークフロー：**
1. AI がログ、統計、エラーパターンを調査
2. AI が問題を特定し、解決策を提案
3. `lifecycle` が有効なら、AI が単純な復旧としてコンテナを再起動
4. インフラ変更が必要な場合は、**人が** 判断・実行

**リスクのあるワークフロー：**
1. AI がエラーを検知して即座にイメージをリビルド/再作成
2. ビルドに時間がかかり、問題は解決しない
3. 人が何が変わったのか把握できない

### exec_command について

`exec_command` はホワイトリストで許可するコマンドを絞り込めます：

```yaml
exec_whitelist:
  "myapp-api":
    - "npm test"
    - "npm run lint"
    - "npm run dev"  # 開発サーバーの再起動が可能
```

これにより可能になること：
- テストとリントの実行
- 開発サーバーの再起動（プロセスマネージャー経由）
- ヘルスチェックとデバッグコマンド

許可されないこと：
- 任意のコマンド実行
- ファイルシステムの変更
- ネットワーク設定の変更

### まとめ

HostMCP は段階的なアクセスを提供します：
- コンテナ情報への**読み取り専用アクセス**（ログ、統計、inspect）
- ホワイトリストによる**制御されたコマンド実行**
- blocked_paths 保護付きの**ファイルアクセス**
- **コンテナライフサイクル**（起動/停止/再起動）— オプトイン、デフォルト無効
- **ホストツール** — デフォルト有効（ツールごとに人間の承認が必要）
- **ホストコマンド** — オプトイン、デフォルト無効
- **イメージビルド/再作成は不可** — 常に人間のみ

各レベルを独立して有効化でき、環境に応じた AI の自律性と人間の制御のバランスを選択できます。

## 提供されるMCPツール

| ツール | 説明 |
|------|------|
| `list_containers` | アクセス可能なコンテナを一覧表示 |
| `get_logs` | コンテナログを取得 |
| `get_stats` | リソース使用統計を取得 |
| `exec_command` | ホワイトリスト登録されたコマンドを実行（`dangerously`モード対応） |
| `inspect_container` | 詳細なコンテナ情報を取得 |
| `get_allowed_commands` | コンテナごとのホワイトリストコマンドを一覧表示 |
| `get_security_policy` | 現在のセキュリティ設定を表示 |
| `search_logs` | パターンマッチでコンテナログを検索 |
| `list_files` | コンテナ内のディレクトリをリスト表示（ブロック機能付き） |
| `read_file` | コンテナ内のファイルを読み取り（ブロック機能付き） |
| `get_blocked_paths` | ブロックされているファイルパスを表示 |
| `restart_container` | コンテナを再起動（`lifecycle: true` が必要） |
| `stop_container` | コンテナを停止（`lifecycle: true` が必要） |
| `start_container` | コンテナを起動（`lifecycle: true` が必要） |
| `list_host_tools` | ホストツールの一覧を表示 |
| `get_host_tool_info` | ホストツールの詳細情報を表示 |
| `run_host_tool` | 承認済みホストツールを実行 |
| `exec_host_command` | ホワイトリスト登録されたホストコマンドを実行 |

> **動的な機能情報:** MCPの`initialize`レスポンスには`instructions`フィールドも含まれ、ホストツールの最新状態（有効化済みツール、承認待ちの新規ツール、承認後に内容が変更されたツール）を接続のたびに動的に要約します。この静的な表だけに頼らず、常に最新の状態を伝えます。

## トラブルシューティング

### HostMCPサーバーが認識されない

1. **ホストでHostMCPが実行されていることを確認：**
   ```bash
   curl http://localhost:18080/health
   # 200 OK を返すはず
   ```

2. **AI Sandbox内でMCP設定を確認：**
   ```bash
   cat ~/.claude.json | jq '.mcpServers.hostmcp'
   # "url": "http://host.docker.internal:18080/sse" であること
   ```

3. **MCP再接続を試す：**
   Claude Codeで `/mcp` → 「Reconnect」を選択

4. **VS Codeを完全に再起動：**
   macOS: `Cmd + Q` / Windows・Linux: `Alt + F4`

### HostMCPサーバーを再起動した場合

HostMCPサーバーを再起動するとSSE接続が切断されるため、AIアシスタント側で再接続が必要です：

- **Claude Code:** `/mcp` → 「Reconnect」を選択
- **それでも解決しない場合:** VS Codeを完全に再起動（Cmd+Q / Alt+F4）

### "Connection refused" エラー

- HostMCPがホストで動作しているか？ → `ps aux | grep hostmcp`
- URLに `host.docker.internal` を使っているか？（`localhost` はNG）
- ポート18080がファイアウォールでブロックされていないか？ → `lsof -i :18080`

### "Container not in allowed list"

設定の `allowed_containers` にコンテナ名またはパターンを追加:
```yaml
security:
  allowed_containers:
    - "your-container-name"
```

### "Command not whitelisted"

設定の `exec_whitelist` にコマンドを追加:
```yaml
security:
  exec_whitelist:
    "container-name":
      - "your command here"
```

## ライセンス

MIT License - [LICENSE](LICENSE) ファイルを参照

## 謝辞

- [Model Context Protocol](https://modelcontextprotocol.io/) で構築
- Docker統合は [docker/docker](https://github.com/docker/docker) 経由
- CLIは [spf13/cobra](https://github.com/spf13/cobra) で駆動

---

**注意**: HostMCPは制御されたアクセスを提供しますが、責任を持って使用してください。AIアシスタントに公開する前に、必ずセキュリティ設定を確認してください。
