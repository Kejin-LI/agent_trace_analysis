package model

import "time"

// StgSyncJob 对应 stg_sync_jobs 表：记录每次同步任务
type StgSyncJob struct {
	ID            uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	SyncJobID     string     `gorm:"column:sync_job_id;uniqueIndex"`
	BatchName     string     `gorm:"column:batch_name"`
	SourceFile    string     `gorm:"column:source_file"`
	ArtifactCount int        `gorm:"column:artifact_count"`
	SessionCount  int        `gorm:"column:session_count"`
	TraceCount    int        `gorm:"column:trace_count"`
	SpanCount     int64      `gorm:"column:span_count"`
	Status        string     `gorm:"column:status"`
	ErrorMessage  string     `gorm:"column:error_message"`
	StartedAt     *time.Time `gorm:"column:started_at"`
	FinishedAt    *time.Time `gorm:"column:finished_at"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at"`
}

func (StgSyncJob) TableName() string { return "stg_sync_jobs" }

// StgArtifactSession 对应 stg_artifact_sessions 表：artifact 与 session 映射
type StgArtifactSession struct {
	ID                 uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	SyncJobID          string     `gorm:"column:sync_job_id"`
	ArtifactID         string     `gorm:"column:artifact_id;uniqueIndex:uk_artifact_session"`
	SessionID          string     `gorm:"column:session_id;uniqueIndex:uk_artifact_session"`
	UserID             string     `gorm:"column:user_id"`
	SessionCreatedAtMs *int64     `gorm:"column:session_created_at_ms"`
	SessionCreatedAt   *time.Time `gorm:"column:session_created_at"`
	Status             string     `gorm:"column:status"`
	ErrorMessage       string     `gorm:"column:error_message"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

func (StgArtifactSession) TableName() string { return "stg_artifact_sessions" }

// StgArtifactTrace 对应 stg_artifact_traces 表：trace 摘要
type StgArtifactTrace struct {
	ID                 uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	SyncJobID          string     `gorm:"column:sync_job_id"`
	ArtifactID         string     `gorm:"column:artifact_id"`
	SessionID          string     `gorm:"column:session_id"`
	TraceID            string     `gorm:"column:trace_id;uniqueIndex"`
	RootSpanID         string     `gorm:"column:root_span_id"`
	UserID             string     `gorm:"column:user_id"`
	StartedAtMs        *int64     `gorm:"column:started_at_ms"`
	StartedAt          *time.Time `gorm:"column:started_at"`
	DurationMs         *int64     `gorm:"column:duration_ms"`
	LLMDurationMs      *int64     `gorm:"column:llm_duration_ms"`
	ToolDurationMs     *int64     `gorm:"column:tool_duration_ms"`
	TurnCount          *int       `gorm:"column:turn_count"`
	SpanCount          *int       `gorm:"column:span_count"`
	Status             string     `gorm:"column:status"`
	ModelName          string     `gorm:"column:model_name"`
	InputTokens        *int64     `gorm:"column:input_tokens"`
	OutputTokens       *int64     `gorm:"column:output_tokens"`
	TotalTokens        *int64     `gorm:"column:total_tokens"`
	FinalResult        string     `gorm:"column:final_result"`
	UserRequestPreview string     `gorm:"column:user_request_preview"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

func (StgArtifactTrace) TableName() string { return "stg_artifact_traces" }

// StgArtifactSpan 对应 stg_artifact_spans 表：span 摘要
type StgArtifactSpan struct {
	ID            uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	SyncJobID     string     `gorm:"column:sync_job_id"`
	ArtifactID    string     `gorm:"column:artifact_id"`
	SessionID     string     `gorm:"column:session_id"`
	TraceID       string     `gorm:"column:trace_id;uniqueIndex:uk_trace_span"`
	SpanID        string     `gorm:"column:span_id;uniqueIndex:uk_trace_span"`
	ParentID      string     `gorm:"column:parent_id"`
	SpanName      string     `gorm:"column:span_name"`
	SpanType      string     `gorm:"column:span_type"`
	StartedAtMs   *int64     `gorm:"column:started_at_ms"`
	StartedAt     *time.Time `gorm:"column:started_at"`
	DurationMs    *int64     `gorm:"column:duration_ms"`
	Status        string     `gorm:"column:status"`
	ModelName     string     `gorm:"column:model_name"`
	InputTokens   *int64     `gorm:"column:input_tokens"`
	OutputTokens  *int64     `gorm:"column:output_tokens"`
	TotalTokens   *int64     `gorm:"column:total_tokens"`
	HasInput      bool       `gorm:"column:has_input"`
	HasOutput     bool       `gorm:"column:has_output"`
	InputPreview  string     `gorm:"column:input_preview"`
	OutputPreview string     `gorm:"column:output_preview"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at"`
}

func (StgArtifactSpan) TableName() string { return "stg_artifact_spans" }
