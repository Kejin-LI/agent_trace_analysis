package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/upstream/modellog"
)

const (
	sessionRefreshTriggerHotIncremental  = "hot_incremental"
	sessionRefreshTriggerDetailFreshness = "detail_freshness"
	sessionRefreshTriggerNightly         = "nightly_reconcile"
	sessionRefreshTriggerManual          = "manual"

	sessionRefreshPriorityLow    = int8(0)
	sessionRefreshPriorityNormal = int8(1)
	sessionRefreshPriorityHigh   = int8(2)
)

type sessionRefreshRequest struct {
	SessionID                  string
	ArtifactID                 string
	TriggerSource              string
	Priority                   int8
	DiscoveredSourceUpdatedAt  *time.Time
	DiscoveredTraceFingerprint string
}

func (a *Aggregator) refreshQueueLoop() {
	if a == nil || a.db == nil || !sessionRefreshQueueTableExists(a.db) {
		return
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := a.processOneSessionRefreshQueue(); err != nil && err != gorm.ErrRecordNotFound {
			log.Printf("session refresh queue: process failed: %v", err)
		}
	}
}

func (a *Aggregator) EnqueueSessionRefresh(req sessionRefreshRequest) error {
	if a == nil || a.db == nil || !sessionRefreshQueueTableExists(a.db) {
		return nil
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return nil
	}
	triggerSource := strings.TrimSpace(req.TriggerSource)
	if triggerSource == "" {
		triggerSource = sessionRefreshTriggerHotIncremental
	}
	now := time.Now()
	row := model.StgSessionRefreshQueue{
		SessionID:                  trimDBString(sessionID, 128),
		ArtifactID:                 trimDBString(req.ArtifactID, 128),
		TriggerSource:              trimDBString(triggerSource, 32),
		Priority:                   req.Priority,
		Status:                     "pending",
		DiscoveredSourceUpdatedAt:  req.DiscoveredSourceUpdatedAt,
		DiscoveredTraceFingerprint: trimDBString(req.DiscoveredTraceFingerprint, 64),
		NextRetryAt:                &now,
	}
	return a.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"artifact_id":                  row.ArtifactID,
			"trigger_source":               row.TriggerSource,
			"priority":                     gorm.Expr("GREATEST(priority, ?)", row.Priority),
			"status":                       "pending",
			"discovered_source_updated_at": row.DiscoveredSourceUpdatedAt,
			"discovered_trace_fingerprint": row.DiscoveredTraceFingerprint,
			"next_retry_at":                now,
			"last_error":                   "",
			"last_error_at":                nil,
			"finished_at":                  nil,
			"lease_owner":                  "",
			"lease_expires_at":             nil,
			"updated_at":                   now,
		}),
	}).Create(&row).Error
}

func (a *Aggregator) processOneSessionRefreshQueue() error {
	job, ok, err := a.claimNextSessionRefreshJob()
	if err != nil || !ok {
		return err
	}
	if err := a.runSessionRefreshJob(job); err != nil {
		return a.failSessionRefreshJob(job, err)
	}
	return a.finishSessionRefreshJob(job)
}

func (a *Aggregator) claimNextSessionRefreshJob() (model.StgSessionRefreshQueue, bool, error) {
	if a == nil || a.db == nil {
		return model.StgSessionRefreshQueue{}, false, nil
	}
	now := time.Now()
	tx := a.db.Begin()
	if tx.Error != nil {
		return model.StgSessionRefreshQueue{}, false, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()
	var row model.StgSessionRefreshQueue
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("(status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?)", "pending", now, "running", now).
		Order("priority DESC, updated_at ASC, id ASC").
		First(&row).Error
	if err != nil {
		tx.Rollback()
		if err == gorm.ErrRecordNotFound {
			return model.StgSessionRefreshQueue{}, false, nil
		}
		return model.StgSessionRefreshQueue{}, false, err
	}
	leaseOwner := fmt.Sprintf("%s:%d", hostnameOrDefault(), os.Getpid())
	leaseExpiresAt := now.Add(sessionRefreshLeaseTTL)
	if err := tx.Model(&model.StgSessionRefreshQueue{}).
		Where("id = ?", row.ID).
		Updates(map[string]interface{}{
			"status":           "running",
			"lease_owner":      leaseOwner,
			"lease_expires_at": leaseExpiresAt,
			"started_at":       now,
			"updated_at":       now,
		}).Error; err != nil {
		tx.Rollback()
		return model.StgSessionRefreshQueue{}, false, err
	}
	if err := tx.Commit().Error; err != nil {
		return model.StgSessionRefreshQueue{}, false, err
	}
	row.Status = "running"
	row.LeaseOwner = leaseOwner
	row.LeaseExpiresAt = &leaseExpiresAt
	row.StartedAt = &now
	return row, true, nil
}

func (a *Aggregator) runSessionRefreshJob(job model.StgSessionRefreshQueue) error {
	if a == nil || a.db == nil {
		return nil
	}
	sessionID := strings.TrimSpace(job.SessionID)
	if sessionID == "" {
		return fmt.Errorf("empty session id")
	}

	indexedSrc, indexedStatus, hasIndexed, err := a.latestRefreshIndexedSource(job)
	if err != nil {
		return err
	}

	// 优先回源上游拿最新文件 URL，再与本地索引比对：
	// 用户在原对话里追加多轮后，上游会产出文件名时间戳更新的新 JSONL，但
	// stg_session_sources 仅由离线导入写入、运行期从不更新 obj_url，索引会永远指向旧文件。
	// 因此必须以上游为准重抓，并把新 obj_url 回写索引，否则"刷新了个寂寞"。
	hit, hitStatus := a.resolveUpstreamSessionForRefresh(job, sessionID, hasIndexed, indexedSrc)
	if hit != nil && upstreamLatestFileURL(hit) != "" {
		if !hasIndexed || indexedSourceStaleVsUpstream(indexedSrc, hit) {
			src := sessionToStgSource(*hit)
			status := hitStatus
			if status == "" {
				status = indexedStatus
			}
			if err := a.refreshSessionFromSource(src, status); err != nil {
				return err
			}
			a.writebackIndexedSource(src)
			return nil
		}
	}

	// 上游未见更新（或不可用）：退回到本地索引重抓。
	if hasIndexed {
		return a.refreshSessionFromSource(indexedSrc, indexedStatus)
	}
	if hit == nil {
		return fmt.Errorf("session not found in upstream window and no indexed source")
	}
	return fmt.Errorf("upstream session has no jsonl file")
}

// resolveUpstreamSessionForRefresh 尽力回源上游拿到该 session 的最新 Session 记录（含最新 FileList）。
// best-effort：上游不可用 / 无 Cookie / 鉴权失效时返回 (nil, "")，让上层退回本地索引重抓，不让队列任务失败。
func (a *Aggregator) resolveUpstreamSessionForRefresh(job model.StgSessionRefreshQueue, sessionID string, hasIndexed bool, indexedSrc model.StgSessionSource) (*modellog.Session, string) {
	if a.upstream == nil || a.fetcher == nil {
		return nil, ""
	}
	cookie := strings.TrimSpace(a.currentCookie())
	if cookie == "" {
		return nil, ""
	}
	statusHint := ""
	anchor := time.Time{}
	if row, ok, err := a.latestAggregateForRefresh(sessionID, strings.TrimSpace(job.ArtifactID)); err == nil && ok {
		statusHint = row.ArtifactPublicationStatus
		if row.StartedAtMs > 0 {
			anchor = time.UnixMilli(row.StartedAtMs)
		}
	}
	if anchor.IsZero() && hasIndexed {
		if indexedSrc.SourceCreatedAt != nil && !indexedSrc.SourceCreatedAt.IsZero() {
			anchor = *indexedSrc.SourceCreatedAt
		} else if indexedSrc.SourceUpdatedAt != nil && !indexedSrc.SourceUpdatedAt.IsZero() {
			anchor = *indexedSrc.SourceUpdatedAt
		}
	}
	tr := modellog.TimeRange{}
	if !anchor.IsZero() {
		tr.StartTime = anchor.Add(-detailLookupHalfWindow).Format("2006-01-02 15:04:05")
		tr.EndTime = anchor.Add(detailLookupHalfWindow).Format("2006-01-02 15:04:05")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	handler := &Handler{upstream: a.upstream}
	hit, hitStatus, err := handler.resolveSessionPublicationStatus(ctx, pickFirstNonEmpty(sessionID, job.ArtifactID), cookie, tr, statusHint)
	if err != nil {
		if isUpstreamAuthMissing(err) {
			a.forgetCookie()
		}
		log.Printf("session refresh: upstream resolve failed session=%s err=%v", sessionID, err)
		return nil, ""
	}
	return hit, hitStatus
}

// writebackIndexedSource 把上游最新文件 URL 回写到 stg_session_sources（OnConflict on artifact_id），
// 使后续详情读/freshness 判定不再指向旧文件。运行期唯一更新该表 obj_url 的入口。
func (a *Aggregator) writebackIndexedSource(src model.StgSessionSource) {
	if a == nil || a.db == nil {
		return
	}
	if strings.TrimSpace(src.ArtifactID) == "" || strings.TrimSpace(src.ObjURL) == "" {
		return
	}
	if strings.TrimSpace(src.ObjFormat) == "" {
		src.ObjFormat = "jsonl"
	}
	if err := a.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "artifact_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"session_id", "user_id", "user_name", "obj_url", "obj_format",
			"source_created_at", "source_updated_at", "updated_at",
		}),
	}).Create(&src).Error; err != nil {
		log.Printf("session refresh: writeback indexed source failed artifact=%s err=%v", src.ArtifactID, err)
	}
}

func (a *Aggregator) latestRefreshIndexedSource(job model.StgSessionRefreshQueue) (model.StgSessionSource, string, bool, error) {
	if a == nil || a.db == nil {
		return model.StgSessionSource{}, "", false, nil
	}
	var src model.StgSessionSource
	q := a.db.Model(&model.StgSessionSource{})
	switch {
	case strings.TrimSpace(job.SessionID) != "" && strings.TrimSpace(job.ArtifactID) != "":
		q = q.Where("(session_id = ? OR artifact_id = ?)", job.SessionID, job.ArtifactID)
	case strings.TrimSpace(job.SessionID) != "":
		q = q.Where("session_id = ?", job.SessionID)
	case strings.TrimSpace(job.ArtifactID) != "":
		q = q.Where("artifact_id = ?", job.ArtifactID)
	default:
		return model.StgSessionSource{}, "", false, nil
	}
	if err := q.Order("source_updated_at DESC, id DESC").First(&src).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.StgSessionSource{}, "", false, nil
		}
		return model.StgSessionSource{}, "", false, err
	}
	var agg model.APISessionAggregate
	status := ""
	if err := a.db.Select("artifact_publication_status").Where("session_id = ? OR artifact_id = ?", src.SessionID, src.ArtifactID).Order("updated_at DESC, id DESC").First(&agg).Error; err == nil {
		status = bundlePublicationStatusFromStored(agg.ArtifactPublicationStatus)
	}
	return src, status, true, nil
}

func (a *Aggregator) latestAggregateForRefresh(sessionID, artifactID string) (model.APISessionAggregate, bool, error) {
	if a == nil || a.db == nil {
		return model.APISessionAggregate{}, false, nil
	}
	var row model.APISessionAggregate
	q := a.db.Model(&model.APISessionAggregate{})
	switch {
	case sessionID != "" && artifactID != "":
		q = q.Where("(session_id = ? OR artifact_id = ?)", sessionID, artifactID)
	case sessionID != "":
		q = q.Where("session_id = ?", sessionID)
	case artifactID != "":
		q = q.Where("artifact_id = ?", artifactID)
	default:
		return model.APISessionAggregate{}, false, nil
	}
	if err := q.Order("updated_at DESC, id DESC").First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.APISessionAggregate{}, false, nil
		}
		return model.APISessionAggregate{}, false, err
	}
	return row, true, nil
}

func (a *Aggregator) refreshSessionFromSource(src model.StgSessionSource, status string) error {
	if a == nil || a.fetcher == nil {
		return nil
	}
	if src.ObjFormat != "jsonl" || strings.TrimSpace(src.ObjURL) == "" {
		return fmt.Errorf("indexed source invalid obj_format=%s obj_url_empty=%t", src.ObjFormat, strings.TrimSpace(src.ObjURL) == "")
	}
	pr, err := a.fetcher.FetchAndParse(src.ObjURL)
	if err != nil {
		return err
	}
	bundle := buildBundleFromTOS(src, pr)
	if status != "" {
		bundle.ArtifactPublicationStatus = status
	}
	return a.PersistBundle(src, bundle)
}

func (a *Aggregator) finishSessionRefreshJob(job model.StgSessionRefreshQueue) error {
	if a == nil || a.db == nil {
		return nil
	}
	now := time.Now()
	return a.db.Model(&model.StgSessionRefreshQueue{}).Where("id = ?", job.ID).Updates(map[string]interface{}{
		"status":           "succeeded",
		"lease_owner":      "",
		"lease_expires_at": nil,
		"finished_at":      now,
		"last_error":       "",
		"last_error_at":    nil,
		"updated_at":       now,
	}).Error
}

func (a *Aggregator) failSessionRefreshJob(job model.StgSessionRefreshQueue, err error) error {
	if a == nil || a.db == nil {
		return err
	}
	now := time.Now()
	retryCount := job.RetryCount + 1
	backoff := time.Duration(retryCount*10) * time.Second
	nextStatus := "failed"
	if retryCount <= refreshMax(job.MaxRetryCount, 0) {
		nextStatus = "pending"
	}
	return a.db.Model(&model.StgSessionRefreshQueue{}).Where("id = ?", job.ID).Updates(map[string]interface{}{
		"status":           nextStatus,
		"retry_count":      retryCount,
		"next_retry_at":    now.Add(backoff),
		"lease_owner":      "",
		"lease_expires_at": nil,
		"last_error":       trimRefreshError(err),
		"last_error_at":    now,
		"updated_at":       now,
	}).Error
}

func trimRefreshError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if len(msg) <= 1024 {
		return msg
	}
	return msg[:1024]
}

func hostnameOrDefault() string {
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	return "unknown"
}

func refreshMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
