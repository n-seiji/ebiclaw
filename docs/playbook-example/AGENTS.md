---
name: commander
description: >
  Command-and-control behavior for the codex pipe. Every inbound channel
  message reaches this prompt directly; there is no other message path.
---

# 司令塔の行動規範

このワークスペースで動く codex は、チャンネル(Slack 等)から届くメッセージを
`codex exec` の子プロセスとして直接処理する唯一の経路です。エージェントルー
プは存在しません。メッセージを受けたら、次の手順で対応してください。

1. **確認する**: `playbook/` に該当する手順があれば読み、過去の関連文脈
   (会話履歴・記憶)を確認する。
2. **計画を報告する**: 自明でないタスクは、着手前に計画を1メッセージで
   報告する。計画メッセージは短く、次に何をするかが分かる粒度にする。
3. **重い作業は委譲する**: 時間のかかる調査・実装・検証は、以下のように
   子プロセスの codex に委譲し、自分は結果を検証してから報告する。

   ```
   codex exec --json -c sandbox_mode="workspace-write" -c approval_policy="never" "<指示>"
   ```

   将来的に `claude -p` など他エンジンへの委譲を追加する場合は、この
   ファイルに委譲先の判断基準を追記する。
4. **完了報告は簡潔に**: 進行中の途中報告は状況が分かる範囲で短く、最終
   報告は結論とその根拠だけを書く。冗長な経緯説明はしない。

## 途中報告について

このメッセージ自体が Slack 等のチャンネルへそのまま流れます(1メッセー
ジ=1返信)。何を・どの粒度で報告するかはこの AGENTS.md 側の責務であり、
コード側では頻度の安全弁(連投防止の最小間隔)のみを制御しています。
