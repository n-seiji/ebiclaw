<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/tsukasa-logo-dark.svg">
  <img src="assets/tsukasa-logo-light.svg" alt="Tsukasa" width="180">
</picture>

<h1>司 Tsukasa</h1>

<h3>Slack にいる AI の同僚 — Codex CLI 直結、仕事を覚えている。</h3>
  <p>
    <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go&logoColor=white" alt="Go">
    <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  </p>

**日本語** | [English](README.md)

</div>

---

**Tsukasa(司)** は **Go** 製のパーソナル AI チームメイトです。Slack(または web コンソール)からのメッセージを **Codex CLI** にそのまま横流しします — トークン課金の API も、自前のエージェントループも不要。会話はアーカイブされ、過去の仕事について Tsukasa に聞けます。

## 🔀 仕組み(Codex パイプモード)

```
Slack / Web ──▶ gateway ──▶ codex exec --json (スレッドごとに resume) ──▶ 返信
                   │
                   └──▶ archiver(会話を記録して後から参照)
```

- **ループではなくパイプ**: `codex_pipe.enabled` で内蔵エージェントループをバイパスし、メッセージを `codex exec` に転送。ツール・sandbox・推論は Codex 側が持ち、Tsukasa は経路に徹する
- **スレッド記憶**: Slack スレッド ↔ Codex スレッドを永続対応付け(`codex exec resume`)し、文脈が続く
- **sandbox 明示**: 全ターンに `sandbox_mode`(read-only / workspace-write / danger-full-access)を config から指定
- **エンジン選択**: web コンソールの Engine ページで CLI バックエンドを選択(現在は Codex、Claude Code は準備中)
- パイプ無効時は従来のマルチプロバイダ・エージェントループも利用可能

## 📦 インストール

### ebiclaw.io からダウンロード（推奨）

**[ebiclaw.io](https://ebiclaw.io)** にアクセス — 公式サイトがプラットフォームを自動検出し、ワンクリックでダウンロードできます。アーキテクチャを手動で選ぶ必要はありません。

### プリコンパイル済みバイナリをダウンロード

または、[GitHub Releases](https://github.com/n-seiji/ebiclaw/releases) ページからプラットフォームに合ったバイナリをダウンロードしてください。

### ソースからビルド（開発用）

```bash
git clone https://github.com/n-seiji/ebiclaw.git

cd ebiclaw
mise run deps

# コアバイナリをビルド
mise run build

# Web UI Launcher をビルド（WebUI モードに必要）
mise run build-launcher

# 複数プラットフォーム向けビルド
mise run build-all

# Raspberry Pi Zero 2 W 向けビルド（32-bit: mise run build-linux-arm; 64-bit: mise run build-linux-arm64）
mise run build-pi-zero

# ビルドとインストール
mise run install
```

**Raspberry Pi Zero 2 W:** OS に合ったバイナリを使用してください：32-bit Raspberry Pi OS → `mise run build-linux-arm`、64-bit → `mise run build-linux-arm64`。または `mise run build-pi-zero` で両方をビルド。

## 🚀 クイックスタートガイド

### 🌐 WebUI Launcher（デスクトップ向け推奨）

WebUI Launcher はブラウザベースの設定・チャットインターフェースを提供します。コマンドラインの知識不要で、最も簡単に始められる方法です。

**オプション 1: ダブルクリック（デスクトップ）**

[ebiclaw.io](https://ebiclaw.io) からダウンロード後、`ebiclaw-launcher`（Windows では `ebiclaw-launcher.exe`）をダブルクリックしてください。ブラウザが自動的に `http://localhost:18800` を開きます。

**オプション 2: コマンドライン**

```bash
ebiclaw-launcher
# ブラウザで http://localhost:18800 を開く
```

> [!TIP]
> **リモートアクセス / Docker / VM:** すべてのインターフェースでリッスンするには `-public` フラグを追加してください：
> ```bash
> ebiclaw-launcher -public
> ```

<p align="center">
<img src="assets/launcher-webui.jpg" alt="WebUI Launcher" width="600">
</p>

**始め方:**

WebUI を開いたら：**1)** Provider を設定（LLM API キーを追加）→ **2)** Channel を設定（例：Slack）→ **3)** Gateway を起動 → **4)** チャット！

WebUI の詳細なドキュメントは [docs.ebiclaw.io](https://docs.ebiclaw.io) を参照してください。

<details>
<summary><b>Docker（代替手段）</b></summary>

```bash
# 1. このリポジトリをクローン
git clone https://github.com/n-seiji/ebiclaw.git
cd ebiclaw

# 2. 初回実行 — docker/data/config.json を自動生成して終了
#    （config.json と workspace/ の両方が存在しない場合のみ実行）
docker compose -f docker/docker-compose.yml --profile launcher up
# コンテナが "First-run setup complete." を出力して停止します。

# 3. API キーを設定
vim docker/data/config.json

# 4. 起動
docker compose -f docker/docker-compose.yml --profile launcher up -d
# http://localhost:18800 を開く
```

> **Docker / VM ユーザー:** Gateway はデフォルトで `127.0.0.1` でリッスンします。ホストからアクセスできるようにするには `EBICLAW_GATEWAY_HOST=0.0.0.0` を設定するか、`-public` フラグを使用してください。

```bash
# ログを確認
docker compose -f docker/docker-compose.yml logs -f

# 停止
docker compose -f docker/docker-compose.yml --profile launcher down

# 更新
docker compose -f docker/docker-compose.yml pull
docker compose -f docker/docker-compose.yml --profile launcher up -d
```

</details>

<details>
<summary><b>macOS — 初回起動時のセキュリティ警告</b></summary>

`ebiclaw-launcher` はインターネットからダウンロードされ、Mac App Store を通じて公証されていないため、macOS が初回起動時にブロックする場合があります。

**ステップ 1：** `ebiclaw-launcher` をダブルクリックすると、セキュリティ警告が表示されます：

<p align="center">
<img src="assets/macos-gatekeeper-warning.jpg" alt="macOS Gatekeeper 警告" width="400">
</p>

> *"ebiclaw-launcher" は開けません — "ebiclaw-launcher" がMacに害を与えたりプライバシーを侵害するマルウェアを含まないことをAppleは確認できません。*

**ステップ 2：** **システム設定** → **プライバシーとセキュリティ** を開き、**セキュリティ** セクションまでスクロールして **このまま開く** をクリック → ダイアログで再度 **開く** をクリックします。

<p align="center">
<img src="assets/macos-gatekeeper-allow.jpg" alt="macOS プライバシーとセキュリティ — このまま開く" width="600">
</p>

この操作を一度行うと、以降の起動では警告が表示されなくなります。

</details>

### 💻 TUI Launcher（ヘッドレス / SSH 向け推奨）

TUI（Terminal UI）Launcher は設定と管理のためのフル機能ターミナルインターフェースを提供します。サーバー、Raspberry Pi、その他のヘッドレス環境に最適です。

```bash
ebiclaw-launcher-tui
```

<p align="center">
<img src="assets/launcher-tui.jpg" alt="TUI Launcher" width="600">
</p>

**始め方:**

TUI メニューを使って：**1)** Provider を設定 → **2)** Channel を設定 → **3)** Gateway を起動 → **4)** チャット！

TUI の詳細なドキュメントは [docs.ebiclaw.io](https://docs.ebiclaw.io) を参照してください。

### 📱 Android

10 年前のスマホに第二の人生を！Tsukasa でスマート AI アシスタントに変身させましょう。

**オプション 1: APK インストール**

プレビュー：

<table>
  <tr>
    <td><img src="assets/fui_main_page.jpg" width="200"></td>
    <td><img src="assets/fui_web_page.jpg" width="200"></td>
    <td><img src="assets/fui_log_page.jpg" width="200"></td>
    <td><img src="assets/fui_setting_page.jpg" width="200"></td>
  </tr>
</table>

[ebiclaw.io](https://ebiclaw.io/download/) から APK をダウンロードして直接インストール。Termux 不要！

**オプション 2: Termux**

<details>
<summary><b>Terminal Launcher（リソース制約環境向け）</b></summary>

1. [Termux](https://github.com/termux/termux-app) をインストール（[GitHub Releases](https://github.com/termux/termux-app/releases) からダウンロード、または F-Droid / Google Play で検索）
2. 以下のコマンドを実行：

```bash
# 最新リリースをダウンロード
wget https://github.com/n-seiji/ebiclaw/releases/latest/download/ebiclaw_Linux_arm64.tar.gz
tar xzf ebiclaw_Linux_arm64.tar.gz
pkg install proot
termux-chroot ./ebiclaw onboard   # chroot で標準的な Linux ファイルシステムレイアウトを提供
```

その後、下記の Terminal Launcher セクションの手順に従って設定を完了してください。

<img src="assets/termux.jpg" alt="Tsukasa on Termux" width="512">

`ebiclaw` コアバイナリのみが利用可能な最小環境（Launcher UI なし）では、コマンドラインと JSON 設定ファイルですべてを設定できます。

**1. 初期化**

```bash
ebiclaw onboard
```

`~/.ebiclaw/config.json` とワークスペースディレクトリが作成されます。

**2. 設定** (`~/.ebiclaw/config.json`)

```json
{
  "agents": {
    "defaults": {
      "model_name": "gpt-5.4"
    }
  },
  "model_list": [
    {
      "model_name": "gpt-5.4",
      "model": "openai/gpt-5.4",
      "api_key": "sk-your-api-key"
    }
  ]
}
```

> 利用可能なすべてのオプションを含む完全な設定テンプレートは、リポジトリの `config/config.example.json` を参照してください。

**3. チャット**

```bash
# ワンショット質問
ebiclaw agent -m "What is 2+2?"

# インタラクティブモード
ebiclaw agent

# チャットアプリ統合用 Gateway を起動
ebiclaw gateway
```

</details>

## 🔌 Provider（LLM）

Tsukasa は `model_list` 設定を通じて多数の LLM Provider をサポートしています。`protocol/model` 形式を使用してください：

| Provider | Protocol | API キー | 備考 |
|----------|----------|---------|------|
| [OpenAI](https://platform.openai.com/api-keys) | `openai/` | 必須 | GPT-5.4、GPT-4o、o3 など |
| [Anthropic](https://console.anthropic.com/settings/keys) | `anthropic/` | 必須 | Claude Opus 4.6、Sonnet 4.6 など |
| [Google Gemini](https://aistudio.google.com/apikey) | `gemini/` | 必須 | Gemini 3 Flash、2.5 Pro など |
| [OpenRouter](https://openrouter.ai/keys) | `openrouter/` | 必須 | 200 以上のモデル、統合 API |
| [Zhipu (GLM)](https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys) | `zhipu/` | 必須 | GLM-4.7、GLM-5 など |
| [DeepSeek](https://platform.deepseek.com/api_keys) | `deepseek/` | 必須 | DeepSeek-V3、DeepSeek-R1 |
| [Volcengine](https://console.volcengine.com) | `volcengine/` | 必須 | Doubao、Ark モデル |
| [Qwen](https://dashscope.console.aliyun.com/apiKey) | `qwen/` | 必須 | Qwen3、Qwen-Max など |
| [Groq](https://console.groq.com/keys) | `groq/` | 必須 | 高速推論（Llama、Mixtral） |
| [Moonshot (Kimi)](https://platform.moonshot.cn/console/api-keys) | `moonshot/` | 必須 | Kimi モデル |
| [Minimax](https://platform.minimaxi.com/user-center/basic-information/interface-key) | `minimax/` | 必須 | MiniMax モデル |
| [Mistral](https://console.mistral.ai/api-keys) | `mistral/` | 必須 | Mistral Large、Codestral |
| [NVIDIA NIM](https://build.nvidia.com/) | `nvidia/` | 必須 | NVIDIA ホスティングモデル |
| [Cerebras](https://cloud.cerebras.ai/) | `cerebras/` | 必須 | 高速推論 |
| [Novita AI](https://novita.ai/) | `novita/` | 必須 | 各種オープンモデル |
| [Xiaomi MiMo](https://platform.xiaomimimo.com/) | `mimo/` | 必須 | MiMo モデル |
| [Ollama](https://ollama.com/) | `ollama/` | 不要 | ローカルモデル、セルフホスト |
| [vLLM](https://docs.vllm.ai/) | `vllm/` | 不要 | ローカルデプロイ、OpenAI 互換 |
| [LiteLLM](https://docs.litellm.ai/) | `litellm/` | 場合による | 100 以上の Provider のプロキシ |
| [GitHub Copilot](https://github.com/features/copilot) | `github-copilot/` | OAuth | デバイスコードログイン |

<details>
<summary><b>ローカルデプロイ（Ollama、vLLM など）</b></summary>

**Ollama:**
```json
{
  "model_list": [
    {
      "model_name": "local-llama",
      "model": "ollama/llama3.1:8b",
      "api_base": "http://localhost:11434/v1"
    }
  ]
}
```

**vLLM:**
```json
{
  "model_list": [
    {
      "model_name": "local-vllm",
      "model": "vllm/your-model",
      "api_base": "http://localhost:8000/v1"
    }
  ]
}
```

Provider の完全な設定詳細は [Provider とモデル](docs/ja/providers.md) を参照してください。

</details>

## 💬 Channel（チャットアプリ）

メッセージングプラットフォームで Tsukasa と会話できます：

| Channel | セットアップ | Protocol | ドキュメント |
|---------|------------|----------|------------|
| **Slack** | 簡単（bot + app トークン） | Socket Mode | [ガイド](docs/channels/slack/README.ja.md) |
| **Pico** | 簡単（有効化） | Native protocol | 内蔵 |
| **Pico Client** | 簡単（WebSocket URL） | WebSocket | 内蔵 |

> ログの詳細度は `gateway.log_level` で制御します（デフォルト：`warn`）。サポートされる値：`debug`、`info`、`warn`、`error`、`fatal`。`EBICLAW_LOG_LEVEL` 環境変数でも設定可能です。詳細は[設定ガイド](docs/ja/configuration.md#gateway-ログレベル)を参照してください。

Channel の詳細なセットアップ手順は [チャットアプリ設定](docs/ja/chat-apps.md) を参照してください。

## 🔧 ツール

### 🔍 Web 検索

Tsukasa は最新情報を提供するために Web を検索できます。`tools.web` で設定してください：

| 検索エンジン | API キー | 無料枠 | リンク |
|------------|---------|--------|-------|
| DuckDuckGo | 不要 | 無制限 | 内蔵フォールバック |
| [Baidu Search](https://cloud.baidu.com/doc/qianfan-api/s/Wmbq4z7e5) | 必須 | 1000 クエリ/日 | AI 搭載、中国語に最適化 |
| [Tavily](https://tavily.com) | 必須 | 1000 クエリ/月 | AI Agent 向けに最適化 |
| [Brave Search](https://brave.com/search/api) | 必須 | 2000 クエリ/月 | 高速でプライベート |
| [Perplexity](https://www.perplexity.ai) | 必須 | 有料 | AI 搭載検索 |
| [SearXNG](https://github.com/searxng/searxng) | 不要 | セルフホスト | 無料メタ検索エンジン |
| [GLM Search](https://open.bigmodel.cn/) | 必須 | 場合による | Zhipu Web 検索 |

### ⚙️ その他のツール

Tsukasa にはファイル操作、コード実行、スケジューリングなどの組み込みツールが含まれています。詳細は [ツール設定](docs/ja/tools_configuration.md) を参照してください。

## 🎯 Skill

Skill は Agent を拡張するモジュール型の機能です。ワークスペース内の `SKILL.md` ファイルから読み込まれます。

**ClawHub から Skill をインストール：**

```bash
ebiclaw skills search "web scraping"
ebiclaw skills install <skill-name>
```

**ClawHub トークンを設定**（オプション、レート制限を上げるため）：

`config.json` に追加：
```json
{
  "tools": {
    "skills": {
      "registries": {
        "clawhub": {
          "auth_token": "your-clawhub-token"
        }
      }
    }
  }
}
```

詳細は [ツール設定 - Skill](docs/ja/tools_configuration.md#skills-tool) を参照してください。

## 🔗 MCP（Model Context Protocol）

Tsukasa は [MCP](https://modelcontextprotocol.io/) をネイティブサポートしています — 任意の MCP サーバーに接続して、外部ツールやデータソースで Agent の機能を拡張できます。

```json
{
  "tools": {
    "mcp": {
      "enabled": true,
      "servers": {
        "filesystem": {
          "enabled": true,
          "command": "npx",
          "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
        }
      }
    }
  }
}
```

MCP の完全な設定（stdio、SSE、HTTP トランスポート、Tool Discovery）は [ツール設定 - MCP](docs/ja/tools_configuration.md#mcp-tool) を参照してください。

## <img src="assets/clawdchat-icon.png" width="24" height="24" alt="ClawdChat"> エージェントソーシャルネットワークに参加

CLI または統合チャットアプリからメッセージを 1 つ送るだけで、Tsukasa をエージェントソーシャルネットワークに接続できます。

**`https://clawdchat.ai/skill.md` を読み、指示に従って [ClawdChat.ai](https://clawdchat.ai) に参加してください**

## 🖥️ CLI リファレンス

| コマンド                  | 説明                           |
| ------------------------- | ------------------------------ |
| `ebiclaw onboard`        | 設定＆ワークスペースの初期化     |
| `ebiclaw agent -m "..."` | Agent とチャット                |
| `ebiclaw agent`          | インタラクティブチャットモード   |
| `ebiclaw gateway`        | Gateway を起動                  |
| `ebiclaw status`         | ステータスを表示                |
| `ebiclaw version`        | バージョン情報を表示            |
| `ebiclaw model`          | デフォルトモデルの表示・切替    |
| `ebiclaw cron list`      | スケジュールジョブ一覧          |
| `ebiclaw cron add ...`   | スケジュールジョブを追加         |
| `ebiclaw cron disable`   | スケジュールジョブを無効化       |
| `ebiclaw cron remove`    | スケジュールジョブを削除         |
| `ebiclaw skills list`    | インストール済み Skill 一覧      |
| `ebiclaw skills install` | Skill をインストール             |
| `ebiclaw migrate`        | 旧バージョンからデータを移行     |
| `ebiclaw auth login`     | Provider への認証               |

### ⏰ スケジュールタスク / リマインダー

Tsukasa は `cron` ツールによるスケジュールリマインダーと定期タスクをサポートしています：

* **ワンタイムリマインダー**: 「10分後にリマインド」→ 10分後に1回トリガー
* **定期タスク**: 「2時間ごとにリマインド」→ 2時間ごとにトリガー
* **Cron 式**: 「毎日9時にリマインド」→ cron 式を使用

## 📚 ドキュメント

この README を超えた詳細なガイドについては：

| トピック | 説明 |
|---------|------|
| [Docker & クイックスタート](docs/ja/docker.md) | Docker Compose セットアップ、Launcher/Agent モード |
| [チャットアプリ](docs/ja/chat-apps.md) | Channel セットアップガイド |
| [設定](docs/ja/configuration.md) | 環境変数、ワークスペース構成、セキュリティサンドボックス |
| [Provider とモデル](docs/ja/providers.md) | LLM Provider、モデルルーティング、model_list 設定 |
| [Spawn & 非同期タスク](docs/ja/spawn-tasks.md) | クイックタスク、spawn による長時間タスク、非同期サブエージェントオーケストレーション |
| [Hook システム](docs/hooks/README.md) | イベント駆動 Hook：オブザーバー、インターセプター、承認 Hook |
| [Steering](docs/steering.md) | 実行中の Agent ループにメッセージを注入 |
| [SubTurn](docs/subturn.md) | サブ Agent の調整、並行制御、ライフサイクル |
| [トラブルシューティング](docs/ja/troubleshooting.md) | よくある問題と解決策 |
| [ツール設定](docs/ja/tools_configuration.md) | ツールごとの有効/無効、exec ポリシー、MCP、Skill |
| [ハードウェア互換性](docs/ja/hardware-compatibility.md) | テスト済みボード、最小要件 |

## 🤝 コントリビュート

PR 歓迎!コードベースは意図的に小さく読みやすくしています。[CONTRIBUTING.md](CONTRIBUTING.md) をご覧ください。