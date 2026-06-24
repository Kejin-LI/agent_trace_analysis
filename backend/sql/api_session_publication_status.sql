-- 异常聚类按发布状态(已发布/未发布)分视图与对比所需列。
-- 设计要点：
-- 1. 发布状态在系统里本非存储字段，而是由上游列表查询参数 OnlyUnpublishedArtifacts 推断；
--    这里把聚合时捕获到的状态落库成快照，使异常页首屏能从 DB 秒出，再由前端做一次上游校准。
-- 2. 懒回填：不做全量重算，历史行保留默认 'unknown'，新聚合/详情写入时自然补齐为
--    published/unpublished。读路径对 unknown/历史空值按“全部”处理，不影响既有视图。
-- 3. 复合索引覆盖异常页主查询：按发布状态 + 异常标记 + 起始时间过滤排序。
--
-- 执行顺序：只需执行 DDL。无需全量回填，无需重算 api_daily_summary。

ALTER TABLE `api_session_aggregates`
  ADD COLUMN `artifact_publication_status` VARCHAR(16) NOT NULL DEFAULT 'unknown' COMMENT '产物发布状态 published/unpublished/unknown' AFTER `has_issue`,
  ADD KEY `idx_pub_issue_started` (`artifact_publication_status`, `has_issue`, `started_at_ms`);
