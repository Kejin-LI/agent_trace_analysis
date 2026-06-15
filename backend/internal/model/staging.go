package model

import "time"

// StgSessionSource 对应 stg_session_sources 表：TOS 实时数据源索引。
// 仅存 session 元信息与 TOS JSONL 地址（obj_url），不存对话内容；
// 详情页对话/思考/工具调用由后端实时拉取 obj_url 解析得到。
//
// 唯一键为 artifact_id（CSV 中天然唯一，UUID 格式）。
// session_id 可选：仅 .jsonl 文件可从文件名提取（ses_xxx.jsonl）。
// 当前阶段仅纳入 .jsonl 格式日志，旧 .json 格式（流式裸日志、缺 promptId）不入库。
type StgSessionSource struct {
	ID              uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	ArtifactID      string     `gorm:"column:artifact_id;type:varchar(64);uniqueIndex:uk_artifact_id"`
	SessionID       string     `gorm:"column:session_id;type:varchar(128);index:idx_session_id"`
	UserID          string     `gorm:"column:user_id;type:varchar(64);index:idx_user_id"`
	UserName        string     `gorm:"column:user_name;type:varchar(128)"`
	ObjURL          string     `gorm:"column:obj_url;type:varchar(1024)"`
	ObjFormat       string     `gorm:"column:obj_format;type:varchar(16)"`
	SourceCreatedAt *time.Time `gorm:"column:source_created_at"`
	SourceUpdatedAt *time.Time `gorm:"column:source_updated_at;index:idx_source_updated_at"`
	Extra           string     `gorm:"column:extra;type:json"`
	ImportBatch     string     `gorm:"column:import_batch;type:varchar(64)"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
}

func (StgSessionSource) TableName() string { return "stg_session_sources" }

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

// StgSessionManualReviewRequest 对应人工校准送审记录表。
type StgSessionManualReviewRequest struct {
	ID                uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	ReviewID          string     `gorm:"column:review_id"`
	SessionID         string     `gorm:"column:session_id"`
	TraceID           string     `gorm:"column:trace_id"`
	ArtifactID        string     `gorm:"column:artifact_id"`
	SessionTitle      string     `gorm:"column:session_title"`
	SessionUser       string     `gorm:"column:session_user"`
	SessionUserID     string     `gorm:"column:session_user_id"`
	SessionStartedAt  *time.Time `gorm:"column:session_started_at"`
	SessionDurationMs int64      `gorm:"column:session_duration_ms"`
	SessionTurns      int        `gorm:"column:session_turns"`
	SessionTraceCount int        `gorm:"column:session_trace_count"`
	ReviewType        string     `gorm:"column:review_type"`
	Status            string     `gorm:"column:status"`
	ReasonCode        string     `gorm:"column:reason_code"`
	ReasonText        string     `gorm:"column:reason_text"`
	SubmitNote        string     `gorm:"column:submit_note"`
	Submitter         string     `gorm:"column:submitter"`
	Reviewer          string     `gorm:"column:reviewer"`
	RulePassed        int        `gorm:"column:rule_passed"`
	RuleTotal         int        `gorm:"column:rule_total"`
	LLMJudgeScore     int        `gorm:"column:llm_judge_score"`
	LLMJudgeModel     string     `gorm:"column:llm_judge_model"`
	LLMJudgeResult    string     `gorm:"column:llm_judge_result;type:json"`
	RuleEvalResult    string     `gorm:"column:rule_eval_result;type:json"`
	EvidenceSnapshot  string     `gorm:"column:evidence_snapshot;type:json"`
	HumanResult       string     `gorm:"column:human_result;type:json"`
	HumanScore        int        `gorm:"column:human_score"`
	HumanComment      string     `gorm:"column:human_comment"`
	CompletedAt       *time.Time `gorm:"column:completed_at"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
	IsDeleted         int        `gorm:"column:is_deleted"`
}

func (StgSessionManualReviewRequest) TableName() string {
	return "stg_session_manual_review_requests"
}

// StgSessionQualityEvaluation 对应 Session 质量评估快照表。
// 同一 session_id 仅保留最新评估结果；重新触发 GPT-5.5 评估时更新该行并递增 llm_eval_version。
type StgSessionQualityEvaluation struct {
	ID                        uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	SessionID                 string     `gorm:"column:session_id"`
	TraceID                   string     `gorm:"column:trace_id"`
	ArtifactID                string     `gorm:"column:artifact_id"`
	SessionTitle              string     `gorm:"column:session_title"`
	SessionUser               string     `gorm:"column:session_user"`
	SessionUserID             string     `gorm:"column:session_user_id"`
	SessionStartedAt          *time.Time `gorm:"column:session_started_at"`
	SessionDurationMs         int64      `gorm:"column:session_duration_ms"`
	SessionTurns              int        `gorm:"column:session_turns"`
	SessionTraceCount         int        `gorm:"column:session_trace_count"`
	RuleScore                 *int       `gorm:"column:rule_score"`
	RuleGrade                 string     `gorm:"column:rule_grade"`
	RuleTags                  string     `gorm:"column:rule_tags;type:json"`
	RuleSummary               string     `gorm:"column:rule_summary"`
	RuleSuggestions           string     `gorm:"column:rule_suggestions;type:json"`
	RuleEvalResult            string     `gorm:"column:rule_eval_result;type:json"`
	RuleEvalAt                *time.Time `gorm:"column:rule_eval_at"`
	LLMScore                  *int       `gorm:"column:llm_score"`
	LLMGrade                  string     `gorm:"column:llm_grade"`
	LLMModel                  string     `gorm:"column:llm_model"`
	LLMEvalVersion            int        `gorm:"column:llm_eval_version"`
	LLMEvalStatus             string     `gorm:"column:llm_eval_status"`
	LLMTriggeredBy            string     `gorm:"column:llm_triggered_by"`
	LLMEvaluatedAt            *time.Time `gorm:"column:llm_evaluated_at"`
	LLMSentiment              string     `gorm:"column:llm_sentiment"`
	LLMSentimentScore         *int       `gorm:"column:llm_sentiment_score"`
	LLMResolved               string     `gorm:"column:llm_resolved"`
	LLMResolvedScore          *int       `gorm:"column:llm_resolved_score"`
	LLMIntentMatch            string     `gorm:"column:llm_intent_match"`
	LLMIntentMatchScore       *int       `gorm:"column:llm_intent_match_score"`
	LLMEfficiencyFeel         string     `gorm:"column:llm_efficiency_feel"`
	LLMEfficiencyFeelScore    *int       `gorm:"column:llm_efficiency_feel_score"`
	LLMRepeatLoop             string     `gorm:"column:llm_repeat_loop"`
	LLMRepeatLoopScore        *int       `gorm:"column:llm_repeat_loop_score"`
	LLMActionability          string     `gorm:"column:llm_actionability"`
	LLMActionabilityScore     *int       `gorm:"column:llm_actionability_score"`
	LLMHallucinationRisk      string     `gorm:"column:llm_hallucination_risk"`
	LLMHallucinationRiskScore *int       `gorm:"column:llm_hallucination_risk_score"`
	LLMTags                   string     `gorm:"column:llm_tags;type:json"`
	LLMSummary                string     `gorm:"column:llm_summary"`
	LLMScoreBasis             string     `gorm:"column:llm_score_basis"`
	LLMSuggestions            string     `gorm:"column:llm_suggestions;type:json"`
	LLMEvidence               string     `gorm:"column:llm_evidence;type:json"`
	LLMEvalResult             string     `gorm:"column:llm_eval_result;type:json"`
	LLMRawResult              string     `gorm:"column:llm_raw_result;type:json"`
	LLMError                  string     `gorm:"column:llm_error"`
	CombinedScore             *int       `gorm:"column:combined_score"`
	CombinedGrade             string     `gorm:"column:combined_grade"`
	CombinedTags              string     `gorm:"column:combined_tags;type:json"`
	CombinedSummary           string     `gorm:"column:combined_summary"`
	CombinedSuggestions       string     `gorm:"column:combined_suggestions;type:json"`
	CombinedScoreBasis        string     `gorm:"column:combined_score_basis"`
	CreatedAt                 time.Time  `gorm:"column:created_at"`
	UpdatedAt                 time.Time  `gorm:"column:updated_at"`
	IsDeleted                 int        `gorm:"column:is_deleted"`
}

func (StgSessionQualityEvaluation) TableName() string {
	return "stg_session_quality_evaluations"
}
