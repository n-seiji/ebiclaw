# BigQuery 集計手順(サンプル)

依頼例:「先月のアクティブユーザー数を教えて」「◯◯の売上を集計して」

## 前提

- `bq` コマンドが利用可能であること
- プロジェクトは `--project_id=miive-prod-data` を明示する(暗黙のデフォ
  ルトプロジェクトに依存しない)
- 読み取り専用(`query` / `ls` / `show` / `head`)のみ実行する。書き込み・
  削除系のコマンドはこの手順の対象外。

## 主要テーブル

- `miive-prod-data.analytics.events` — イベントログ(1 行 = 1 イベント)
- `miive-prod-data.analytics.users` — ユーザーマスタ

## 手順

1. 対象期間・対象イベントを依頼文から特定する。曖昧な場合は先に確認する。
2. クエリを組み立てて実行する:

   ```
   bq query --use_legacy_sql=false --project_id=miive-prod-data '
   SELECT DATE(event_ts) AS d, COUNT(DISTINCT user_id) AS active_users
   FROM `miive-prod-data.analytics.events`
   WHERE event_ts BETWEEN "2026-06-01" AND "2026-06-30"
   GROUP BY d
   ORDER BY d
   '
   ```

3. 集計結果を要約して報告する。生の行データをそのまま貼らず、依頼に対
   する結論(合計・傾向)を先に書く。

## attribution ラベル

社内のコスト按分ルールに従い、`bq` 実行時は attribution ラベル
(`--label=team:xxx`)を付与する運用がある場合は、workspace の他の設定
ファイルを確認してから付与する。
