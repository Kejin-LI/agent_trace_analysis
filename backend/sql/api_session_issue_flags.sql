-- 统一异常口径：命中失败规则 OR GPT 问题标签。
-- 执行顺序：
-- 1. 先执行 DDL；
-- 2. 再执行全量重算；
-- 3. 最后重算 api_daily_summary.abnormal_session_count，让大盘和异常页同口径。

ALTER TABLE `api_session_aggregates`
  ADD COLUMN `has_issue` TINYINT(1) NOT NULL DEFAULT 0 COMMENT '统一标签口径异常标记：失败规则或GPT问题标签' AFTER `abnormal_level`,
  ADD KEY `idx_issue_started` (`has_issue`, `started_at_ms`, `id`);

UPDATE `api_session_aggregates` a
LEFT JOIN (
  SELECT e.*
  FROM `stg_session_quality_evaluations` e
  INNER JOIN (
    SELECT `session_id`, MAX(`updated_at`) AS `updated_at`
    FROM `stg_session_quality_evaluations`
    WHERE `is_deleted` = 0
    GROUP BY `session_id`
  ) latest
    ON latest.`session_id` = e.`session_id`
   AND latest.`updated_at` = e.`updated_at`
  WHERE e.`is_deleted` = 0
) qe
  ON qe.`session_id` = a.`session_id`
SET a.`has_issue` =
  CASE
    WHEN COALESCE(a.`rules_json`, '') REGEXP '\"passed\"[[:space:]]*:[[:space:]]*false' THEN 1
    WHEN qe.`id` IS NOT NULL AND (
      qe.`llm_resolved_score` < 70 OR
      qe.`llm_intent_match_score` < 70 OR
      qe.`llm_sentiment_score` < 60 OR
      qe.`llm_efficiency_feel_score` < 70 OR
      qe.`llm_actionability_score` < 70 OR
      qe.`llm_hallucination_risk_score` < 70
    ) THEN 1
    ELSE 0
  END;

UPDATE `api_daily_summary` s
LEFT JOIN (
  SELECT `aggregate_date`, SUM(CASE WHEN `has_issue` = 1 THEN 1 ELSE 0 END) AS `issue_count`
  FROM `api_session_aggregates`
  GROUP BY `aggregate_date`
) x
  ON x.`aggregate_date` = s.`aggregate_date`
SET s.`abnormal_session_count` = COALESCE(x.`issue_count`, 0),
    s.`aggregated_at` = NOW(),
    s.`updated_at` = NOW();
