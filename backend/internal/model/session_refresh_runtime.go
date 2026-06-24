package model

import "time"

// StgIncrementalSyncCursor 对应热增量扫描游标表。
// 仅记录扫描水位、窗口和最近错误，不存任何对话内容。
type StgIncrementalSyncCursor struct {
	ID                  uint64     `gorm:"column:id;primaryKey;autoIncrement;comment:自增主键"`
	JobName             string     `gorm:"column:job_name;type:varchar(64);not null;uniqueIndex:uk_job_name;comment:任务名，唯一，如 session_hot_incremental"`
	CursorKind          string     `gorm:"column:cursor_kind;type:varchar(32);not null;default:'source_updated_at';comment:游标类型 source_updated_at/update_at"`
	WatermarkAt         *time.Time `gorm:"column:watermark_at;type:datetime(3);index:idx_watermark_at;comment:上次成功推进到的时间水位"`
	LookbackSeconds     int        `gorm:"column:lookback_seconds;not null;default:1200;comment:回看窗口秒数，默认20分钟"`
	Status              string     `gorm:"column:status;type:varchar(16);not null;default:'idle';index:idx_status;comment:idle/running/succeeded/failed"`
	LastScanWindowStart *time.Time `gorm:"column:last_scan_window_start;type:datetime(3);comment:最近一次扫描窗口起点"`
	LastScanWindowEnd   *time.Time `gorm:"column:last_scan_window_end;type:datetime(3);comment:最近一次扫描窗口终点"`
	LastScanStartedAt   *time.Time `gorm:"column:last_scan_started_at;type:datetime(3);comment:最近一次扫描开始时间"`
	LastScanFinishedAt  *time.Time `gorm:"column:last_scan_finished_at;type:datetime(3);comment:最近一次扫描结束时间"`
	LastSuccessAt       *time.Time `gorm:"column:last_success_at;type:datetime(3);comment:最近一次成功时间"`
	LastError           string     `gorm:"column:last_error;type:varchar(1024);not null;default:'';comment:最近一次错误信息"`
	LastErrorAt         *time.Time `gorm:"column:last_error_at;type:datetime(3);comment:最近一次错误时间"`
	CreatedAt           time.Time  `gorm:"column:created_at;comment:创建时间"`
	UpdatedAt           time.Time  `gorm:"column:updated_at;comment:更新时间"`
}

func (StgIncrementalSyncCursor) TableName() string { return "stg_incremental_sync_cursors" }

// StgSessionRefreshQueue 对应 session 变更刷新队列表。
// 只记录待重聚合对象和任务状态，不存对话内容。
type StgSessionRefreshQueue struct {
	ID                         uint64     `gorm:"column:id;primaryKey;autoIncrement;comment:自增主键"`
	SessionID                  string     `gorm:"column:session_id;type:varchar(128);not null;uniqueIndex:uk_session_id;comment:Session ID"`
	ArtifactID                 string     `gorm:"column:artifact_id;type:varchar(128);not null;default:'';comment:Artifact ID 快照"`
	TriggerSource              string     `gorm:"column:trigger_source;type:varchar(32);not null;default:'hot_incremental';index:idx_trigger_source;comment:触发来源 hot_incremental/detail_freshness/nightly_reconcile/manual"`
	Priority                   int8       `gorm:"column:priority;not null;default:0;comment:优先级 0 low, 1 normal, 2 high"`
	Status                     string     `gorm:"column:status;type:varchar(16);not null;default:'pending';index:idx_status_retry_priority,priority:1;comment:pending/running/succeeded/failed/paused/cancelled"`
	DiscoveredSourceUpdatedAt  *time.Time `gorm:"column:discovered_source_updated_at;type:datetime(3);index:idx_discovered_source_updated_at;comment:发现变更时的上游 source_updated_at"`
	DiscoveredTraceFingerprint string     `gorm:"column:discovered_trace_fingerprint;type:varchar(64);not null;default:'';comment:发现变更时的trace指纹，可为空"`
	AggregateVersion           int        `gorm:"column:aggregate_version;not null;default:0;comment:入队时聚合版本号，便于幂等控制"`
	RetryCount                 int        `gorm:"column:retry_count;not null;default:0;comment:重试次数"`
	MaxRetryCount              int        `gorm:"column:max_retry_count;not null;default:3;comment:最大重试次数"`
	NextRetryAt                *time.Time `gorm:"column:next_retry_at;type:datetime(3);index:idx_status_retry_priority,priority:2;comment:下次可重试时间"`
	LeaseOwner                 string     `gorm:"column:lease_owner;type:varchar(64);not null;default:'';comment:当前处理者标识"`
	LeaseExpiresAt             *time.Time `gorm:"column:lease_expires_at;type:datetime(3);index:idx_lease_expires_at;comment:租约到期时间，防止僵死running"`
	LastError                  string     `gorm:"column:last_error;type:varchar(1024);not null;default:'';comment:最近一次错误信息"`
	LastErrorAt                *time.Time `gorm:"column:last_error_at;type:datetime(3);comment:最近一次错误时间"`
	StartedAt                  *time.Time `gorm:"column:started_at;type:datetime(3);comment:本次处理开始时间"`
	FinishedAt                 *time.Time `gorm:"column:finished_at;type:datetime(3);comment:本次处理结束时间"`
	CreatedAt                  time.Time  `gorm:"column:created_at;comment:创建时间"`
	UpdatedAt                  time.Time  `gorm:"column:updated_at;comment:更新时间"`
}

func (StgSessionRefreshQueue) TableName() string { return "stg_session_refresh_queue" }
