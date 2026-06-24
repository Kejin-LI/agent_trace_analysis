-- Session 历史变更增量刷新所需的游标表、队列表与聚合表增强字段。
-- 设计要点：
-- 1. 高频任务只做“元数据增量发现”，不拉对话内容；重活仍交给后端异步 worker。
-- 2. 刷新队列表负责去重、重试、租约与状态可观测，避免仅靠用户打开详情页被动兜底。
-- 3. api_session_aggregates 增加 trace_fingerprint / aggregate_invalidated 等字段，
--    支撑详情页 freshness check、列表页“待重算”展示与后端幂等判断。
--
-- 执行顺序：
-- 1. 先执行两张新表 DDL；
-- 2. 再执行 api_session_aggregates 的 ALTER；
-- 3. 无需全量回填；历史行默认 aggregate_invalidated=0、trace_fingerprint='' 即可上线。

CREATE TABLE IF NOT EXISTS `stg_incremental_sync_cursors` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '自增主键',
  `job_name` VARCHAR(64) NOT NULL COMMENT '任务名，唯一，如 session_hot_incremental',
  `cursor_kind` VARCHAR(32) NOT NULL DEFAULT 'source_updated_at' COMMENT '游标类型：source_updated_at / update_at',
  `watermark_at` DATETIME(3) DEFAULT NULL COMMENT '上次成功推进到的时间水位',
  `lookback_seconds` INT NOT NULL DEFAULT 1200 COMMENT '回看窗口秒数，默认20分钟',
  `status` VARCHAR(16) NOT NULL DEFAULT 'idle' COMMENT 'idle/running/succeeded/failed',
  `last_scan_window_start` DATETIME(3) DEFAULT NULL COMMENT '最近一次扫描窗口起点',
  `last_scan_window_end` DATETIME(3) DEFAULT NULL COMMENT '最近一次扫描窗口终点',
  `last_scan_started_at` DATETIME(3) DEFAULT NULL COMMENT '最近一次扫描开始时间',
  `last_scan_finished_at` DATETIME(3) DEFAULT NULL COMMENT '最近一次扫描结束时间',
  `last_success_at` DATETIME(3) DEFAULT NULL COMMENT '最近一次成功时间',
  `last_error` VARCHAR(1024) NOT NULL DEFAULT '' COMMENT '最近一次错误信息',
  `last_error_at` DATETIME(3) DEFAULT NULL COMMENT '最近一次错误时间',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_job_name` (`job_name`),
  KEY `idx_status` (`status`),
  KEY `idx_watermark_at` (`watermark_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Session 增量扫描游标表';

CREATE TABLE IF NOT EXISTS `stg_session_refresh_queue` (
  `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '自增主键',
  `session_id` VARCHAR(128) NOT NULL COMMENT 'Session ID',
  `artifact_id` VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'Artifact ID 快照',
  `trigger_source` VARCHAR(32) NOT NULL DEFAULT 'hot_incremental' COMMENT '触发来源：hot_incremental/detail_freshness/nightly_reconcile/manual',
  `priority` TINYINT NOT NULL DEFAULT 0 COMMENT '优先级：0 low, 1 normal, 2 high',
  `status` VARCHAR(16) NOT NULL DEFAULT 'pending' COMMENT 'pending/running/succeeded/failed/paused/cancelled',
  `discovered_source_updated_at` DATETIME(3) DEFAULT NULL COMMENT '发现变更时的上游 source_updated_at',
  `discovered_trace_fingerprint` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '发现变更时的 trace 指纹，可为空',
  `aggregate_version` INT NOT NULL DEFAULT 0 COMMENT '入队时聚合版本号，便于幂等控制',
  `retry_count` INT NOT NULL DEFAULT 0 COMMENT '重试次数',
  `max_retry_count` INT NOT NULL DEFAULT 3 COMMENT '最大重试次数',
  `next_retry_at` DATETIME(3) DEFAULT NULL COMMENT '下次可重试时间',
  `lease_owner` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '当前处理者标识',
  `lease_expires_at` DATETIME(3) DEFAULT NULL COMMENT '租约到期时间，防止僵死 running',
  `last_error` VARCHAR(1024) NOT NULL DEFAULT '' COMMENT '最近一次错误信息',
  `last_error_at` DATETIME(3) DEFAULT NULL COMMENT '最近一次错误时间',
  `started_at` DATETIME(3) DEFAULT NULL COMMENT '本次处理开始时间',
  `finished_at` DATETIME(3) DEFAULT NULL COMMENT '本次处理结束时间',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_session_id` (`session_id`),
  KEY `idx_status_retry_priority` (`status`, `next_retry_at`, `priority`, `id`),
  KEY `idx_trigger_source` (`trigger_source`),
  KEY `idx_discovered_source_updated_at` (`discovered_source_updated_at`),
  KEY `idx_lease_expires_at` (`lease_expires_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Session 变更刷新队列表';

ALTER TABLE `api_session_aggregates`
  ADD COLUMN `trace_fingerprint` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '当前聚合结果对应的 trace 指纹' AFTER `source_update_at`,
  ADD COLUMN `aggregate_invalidated` TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否检测到上游变更、当前聚合结果已失效' AFTER `trace_fingerprint`,
  ADD COLUMN `aggregate_invalidated_at` DATETIME(3) DEFAULT NULL COMMENT '聚合结果被标记失效时间' AFTER `aggregate_invalidated`,
  ADD COLUMN `last_change_detected_at` DATETIME(3) DEFAULT NULL COMMENT '最近一次检测到上游内容变更时间' AFTER `aggregate_invalidated_at`,
  ADD KEY `idx_invalidated_started` (`aggregate_invalidated`, `started_at_ms`, `id`),
  ADD KEY `idx_source_update_trace` (`source_update_at`, `trace_fingerprint`);
