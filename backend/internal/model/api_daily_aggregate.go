package model

import "time"

// APISessionAggregate 对应 API 模式下按 session 粒度缓存的聚合结果。
// 同时缓存列表指标与详情页 bundle，避免实时解析失败时整页为空。
type APISessionAggregate struct {
	ID                 uint64     `gorm:"column:id;primaryKey;autoIncrement;comment:自增主键"`
	SessionID          string     `gorm:"column:session_id;type:varchar(128);not null;uniqueIndex:uk_session_id;comment:session 唯一标识"`
	ArtifactID         string     `gorm:"column:artifact_id;type:varchar(64);not null;default:'';index:idx_artifact_id;comment:artifact_id"`
	AggregateDate      time.Time  `gorm:"column:aggregate_date;type:date;not null;index:idx_aggregate_date;index:idx_date_started,priority:1;index:idx_user_date,priority:2;comment:聚合归属自然日"`
	UserID             string     `gorm:"column:user_id;type:varchar(64);not null;default:'';index:idx_user_date,priority:1;comment:用户 ID"`
	UserName           string     `gorm:"column:user_name;type:varchar(128);not null;default:'';comment:用户名"`
	StartedAtMs        int64      `gorm:"column:started_at_ms;not null;default:0;index:idx_started_at_ms;index:idx_date_started,priority:2;comment:会话起始时间戳(ms)"`
	StartedAt          *time.Time `gorm:"column:started_at;type:datetime(3);comment:会话起始时间"`
	DurationMs         int64      `gorm:"column:duration_ms;not null;default:0;comment:总耗时(ms)"`
	TraceID            string     `gorm:"column:trace_id;type:varchar(128);not null;default:'';comment:主 trace_id"`
	Title              string     `gorm:"column:title;type:varchar(512);not null;default:'';comment:列表展示标题"`
	Chip               string     `gorm:"column:chip;type:varchar(64);not null;default:'';index:idx_chip;comment:异常标签或健康标签"`
	InputTokens        int64      `gorm:"column:input_tokens;not null;default:0;comment:输入 token"`
	OutputTokens       int64      `gorm:"column:output_tokens;not null;default:0;comment:输出 token"`
	TotalTokens        int64      `gorm:"column:total_tokens;not null;default:0;comment:总 token"`
	AvgTokensPerTurn   int64      `gorm:"column:avg_tokens_per_turn;not null;default:0;comment:单轮平均 token"`
	Turns              int        `gorm:"column:turns;not null;default:0;comment:轮次"`
	TraceCount         int        `gorm:"column:trace_count;not null;default:0;comment:trace 数"`
	ToolCalls          int        `gorm:"column:tool_calls;not null;default:0;comment:工具调用次数"`
	UniqueTools        int        `gorm:"column:unique_tools;not null;default:0;comment:唯一工具数"`
	ToolFailures       int        `gorm:"column:tool_failures;not null;default:0;comment:工具失败次数"`
	ToolFailRateBP     int        `gorm:"column:tool_fail_rate_bp;not null;default:0;comment:工具失败率bp"`
	ToolRetries        int        `gorm:"column:tool_retries;not null;default:0;comment:重复工具调用次数"`
	MaxSerialRun       int        `gorm:"column:max_serial_run;not null;default:0;comment:同名工具最长连续调用次数"`
	HasRootFail        bool       `gorm:"column:has_root_fail;not null;default:false;comment:是否主流程失败"`
	HasLoop            bool       `gorm:"column:has_loop;not null;default:false;comment:是否疑似死循环"`
	HasFinalAnswer     bool       `gorm:"column:has_final_answer;not null;default:false;comment:是否产出最终答复"`
	NoOpStreak         int        `gorm:"column:no_op_streak;not null;default:0;comment:连续空转次数"`
	Score              int        `gorm:"column:score;not null;default:0;comment:综合分"`
	ResponseScore      int        `gorm:"column:response_score;not null;default:0;comment:响应维度分"`
	StabilityScore     int        `gorm:"column:stability_score;not null;default:0;comment:稳定性分"`
	ThinkingScore      int        `gorm:"column:thinking_score;not null;default:0;comment:思考质量分"`
	ResourceScore      int        `gorm:"column:resource_score;not null;default:0;comment:资源使用分"`
	OrchestrationScore int        `gorm:"column:orchestration_score;not null;default:0;comment:编排分"`
	AbnormalLevel      int        `gorm:"column:abnormal_level;not null;default:0;index:idx_abnormal_level;comment:异常等级"`
	RulesJSON          string     `gorm:"column:rules_json;type:longtext;comment:规则检查结果JSON"`
	FeaturesJSON       string     `gorm:"column:features_json;type:longtext;comment:特征明细JSON"`
	BundleJSON         string     `gorm:"column:bundle_json;type:longtext;comment:详情页完整bundle缓存JSON"`
	SourceCreateAt     *time.Time `gorm:"column:source_create_at;type:datetime(3);comment:上游 create_at"`
	SourceUpdateAt     *time.Time `gorm:"column:source_update_at;type:datetime(3);comment:上游 update_at"`
	AggregatedAt       time.Time  `gorm:"column:aggregated_at;type:datetime(3);not null;comment:聚合完成时间"`
	CreatedAt          time.Time  `gorm:"column:created_at;comment:创建时间"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;comment:更新时间"`
}

func (APISessionAggregate) TableName() string { return "api_session_aggregates" }

// APIDailyAggregateStatus 记录 API 模式下某一天的补库状态，避免重复触发。
type APIDailyAggregateStatus struct {
	ID               uint64     `gorm:"column:id;primaryKey;autoIncrement;comment:自增主键"`
	AggregateDate    time.Time  `gorm:"column:aggregate_date;type:date;not null;uniqueIndex:uk_aggregate_date;comment:自然日"`
	Status           string     `gorm:"column:status;type:varchar(32);not null;default:'pending';index:idx_status;comment:pending/running/completed/failed"`
	SessionCount     int        `gorm:"column:session_count;not null;default:0;comment:成功聚合的 session 数"`
	SuccessCount     int        `gorm:"column:success_count;not null;default:0;comment:成功数"`
	FailCount        int        `gorm:"column:fail_count;not null;default:0;comment:失败数"`
	RetryCount       int        `gorm:"column:retry_count;not null;default:0;comment:重试次数"`
	ListTotal        int        `gorm:"column:list_total;not null;default:0;comment:上游list返回总数"`
	FetchConcurrency int        `gorm:"column:fetch_concurrency;not null;default:2;comment:本次聚合detail并发"`
	LastError        string     `gorm:"column:last_error;type:text;comment:最后一次错误"`
	LastErrorAt      *time.Time `gorm:"column:last_error_at;type:datetime(3);comment:最后错误时间"`
	StartedAt        *time.Time `gorm:"column:started_at;type:datetime(3);comment:本轮开始时间"`
	FinishedAt       *time.Time `gorm:"column:finished_at;type:datetime(3);comment:本轮结束时间"`
	CostMs           int64      `gorm:"column:cost_ms;not null;default:0;comment:本轮耗时(ms)"`
	LastAggregatedAt *time.Time `gorm:"column:last_aggregated_at;type:datetime(3);index:idx_last_aggregated_at;comment:最后成功聚合时间"`
	CreatedAt        time.Time  `gorm:"column:created_at;comment:创建时间"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;comment:更新时间"`
}

func (APIDailyAggregateStatus) TableName() string { return "api_daily_aggregate_status" }

// APIDailySummary 记录按天汇总后的大盘指标，避免每次前端打开页面现算。
type APIDailySummary struct {
	ID                    uint64    `gorm:"column:id;primaryKey;autoIncrement;comment:自增主键"`
	AggregateDate         time.Time `gorm:"column:aggregate_date;type:date;not null;uniqueIndex:uk_aggregate_date;comment:自然日"`
	SessionCount          int       `gorm:"column:session_count;not null;default:0;comment:session总数"`
	ActiveUserCount       int       `gorm:"column:active_user_count;not null;default:0;comment:活跃用户数"`
	AbnormalSessionCount  int       `gorm:"column:abnormal_session_count;not null;default:0;comment:异常session数"`
	FailedSessionCount    int       `gorm:"column:failed_session_count;not null;default:0;comment:主流程失败session数"`
	LoopSessionCount      int       `gorm:"column:loop_session_count;not null;default:0;comment:死循环session数"`
	TotalInputTokens      int64     `gorm:"column:total_input_tokens;not null;default:0;comment:输入token总量"`
	TotalOutputTokens     int64     `gorm:"column:total_output_tokens;not null;default:0;comment:输出token总量"`
	TotalTokens           int64     `gorm:"column:total_tokens;not null;default:0;comment:总token"`
	TotalToolCalls        int64     `gorm:"column:total_tool_calls;not null;default:0;comment:工具调用总量"`
	TotalToolFailures     int64     `gorm:"column:total_tool_failures;not null;default:0;comment:工具失败总量"`
	AvgDurationMs         int64     `gorm:"column:avg_duration_ms;not null;default:0;comment:平均耗时"`
	AvgTurns              float64   `gorm:"column:avg_turns;type:decimal(10,2);not null;default:0;comment:平均轮次"`
	AvgScore              float64   `gorm:"column:avg_score;type:decimal(10,2);not null;default:0;comment:平均综合分"`
	P50DurationMs         int64     `gorm:"column:p50_duration_ms;not null;default:0;comment:P50耗时"`
	P90DurationMs         int64     `gorm:"column:p90_duration_ms;not null;default:0;comment:P90耗时"`
	P95DurationMs         int64     `gorm:"column:p95_duration_ms;not null;default:0;comment:P95耗时"`
	ResponseScoreAvg      float64   `gorm:"column:response_score_avg;type:decimal(10,2);not null;default:0;comment:响应平均分"`
	StabilityScoreAvg     float64   `gorm:"column:stability_score_avg;type:decimal(10,2);not null;default:0;comment:稳定性平均分"`
	ThinkingScoreAvg      float64   `gorm:"column:thinking_score_avg;type:decimal(10,2);not null;default:0;comment:思考平均分"`
	ResourceScoreAvg      float64   `gorm:"column:resource_score_avg;type:decimal(10,2);not null;default:0;comment:资源平均分"`
	OrchestrationScoreAvg float64   `gorm:"column:orchestration_score_avg;type:decimal(10,2);not null;default:0;comment:编排平均分"`
	AggregatedAt          time.Time `gorm:"column:aggregated_at;type:datetime(3);not null;comment:聚合完成时间"`
	CreatedAt             time.Time `gorm:"column:created_at;comment:创建时间"`
	UpdatedAt             time.Time `gorm:"column:updated_at;comment:更新时间"`
}

func (APIDailySummary) TableName() string { return "api_daily_summary" }
