package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

var (
	sessionRefreshSchemaMu    sync.Mutex
	sessionRefreshSchemaCache = map[string]bool{}
	sessionRefreshSchemaAt    = map[string]time.Time{}
)

type sessionBundleFreshnessResponse struct {
	Found                      bool   `json:"found"`
	SessionID                  string `json:"session_id,omitempty"`
	ArtifactID                 string `json:"artifact_id,omitempty"`
	ArtifactPublicationStatus  string `json:"artifact_publication_status,omitempty"`
	SourceUpdatedAtMs          int64  `json:"source_updated_at_ms,omitempty"`
	AggregateSourceUpdatedAtMs int64  `json:"aggregate_source_updated_at_ms,omitempty"`
	TraceFingerprint           string `json:"trace_fingerprint,omitempty"`
	AggregateInvalidated       bool   `json:"aggregate_invalidated"`
	NeedsRefresh               bool   `json:"needs_refresh"`
	RefreshStatus              string `json:"refresh_status,omitempty"`
	Reason                     string `json:"reason,omitempty"`
}

func dbSchemaObjectExists(db *gorm.DB, cacheKey string, loader func(*gorm.DB) bool) bool {
	if db == nil {
		return false
	}
	sessionRefreshSchemaMu.Lock()
	defer sessionRefreshSchemaMu.Unlock()
	if ok, exists := sessionRefreshSchemaCache[cacheKey]; exists && time.Since(sessionRefreshSchemaAt[cacheKey]) < time.Minute {
		return ok
	}
	ok := loader(db)
	sessionRefreshSchemaCache[cacheKey] = ok
	sessionRefreshSchemaAt[cacheKey] = time.Now()
	return ok
}

func sessionRefreshQueueTableExists(db *gorm.DB) bool {
	return dbSchemaObjectExists(db, "table:stg_session_refresh_queue", func(db *gorm.DB) bool {
		return db.Migrator().HasTable(&model.StgSessionRefreshQueue{})
	})
}

func apiSessionAggregateHasTraceFingerprintColumn(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	_, ok := apiSessionAggregateColumns(db)["trace_fingerprint"]
	return ok
}

func apiSessionAggregateHasAggregateInvalidatedColumn(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	_, ok := apiSessionAggregateColumns(db)["aggregate_invalidated"]
	return ok
}

func apiSessionAggregateHasAggregateInvalidatedAtColumn(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	_, ok := apiSessionAggregateColumns(db)["aggregate_invalidated_at"]
	return ok
}

func apiSessionAggregateHasLastChangeDetectedAtColumn(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	_, ok := apiSessionAggregateColumns(db)["last_change_detected_at"]
	return ok
}

func (h *Handler) latestSessionAggregateByKey(key string) (model.APISessionAggregate, bool, error) {
	if h == nil || h.db == nil || strings.TrimSpace(key) == "" {
		return model.APISessionAggregate{}, false, nil
	}
	var row model.APISessionAggregate
	err := h.db.Where("session_id = ? OR artifact_id = ?", key, key).Order("updated_at DESC, id DESC").First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.APISessionAggregate{}, false, nil
		}
		return model.APISessionAggregate{}, false, err
	}
	return row, true, nil
}

func (h *Handler) latestIndexedSessionSource(sessionID, artifactID string) (model.StgSessionSource, bool, error) {
	if h == nil || h.db == nil {
		return model.StgSessionSource{}, false, nil
	}
	var row model.StgSessionSource
	q := h.db.Model(&model.StgSessionSource{})
	switch {
	case strings.TrimSpace(sessionID) != "" && strings.TrimSpace(artifactID) != "":
		q = q.Where("(session_id = ? OR artifact_id = ?)", sessionID, artifactID)
	case strings.TrimSpace(sessionID) != "":
		q = q.Where("session_id = ?", sessionID)
	case strings.TrimSpace(artifactID) != "":
		q = q.Where("artifact_id = ?", artifactID)
	default:
		return model.StgSessionSource{}, false, nil
	}
	err := q.Order("source_updated_at DESC, id DESC").First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return model.StgSessionSource{}, false, nil
		}
		return model.StgSessionSource{}, false, err
	}
	return row, true, nil
}

func (h *Handler) latestRefreshQueueStatus(sessionID string) string {
	if h == nil || h.db == nil || strings.TrimSpace(sessionID) == "" || !sessionRefreshQueueTableExists(h.db) {
		return ""
	}
	var row model.StgSessionRefreshQueue
	if err := h.db.Select("status").Where("session_id = ?", sessionID).First(&row).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(row.Status)
}

func (h *Handler) markSessionAggregateInvalidated(row model.APISessionAggregate, changedAt *time.Time) error {
	if h == nil || h.db == nil || row.ID == 0 || !apiSessionAggregateHasAggregateInvalidatedColumn(h.db) {
		return nil
	}
	now := time.Now()
	updates := map[string]interface{}{
		"aggregate_invalidated": true,
		"updated_at":            now,
	}
	if apiSessionAggregateHasAggregateInvalidatedAtColumn(h.db) {
		updates["aggregate_invalidated_at"] = now
	}
	if apiSessionAggregateHasLastChangeDetectedAtColumn(h.db) {
		if changedAt != nil && !changedAt.IsZero() {
			updates["last_change_detected_at"] = *changedAt
		} else {
			updates["last_change_detected_at"] = now
		}
	}
	return h.db.Model(&model.APISessionAggregate{}).Where("id = ?", row.ID).Updates(updates).Error
}

func (h *Handler) getSessionBundleFreshness(c *gin.Context) {
	resp := sessionBundleFreshnessResponse{}
	if h == nil || h.db == nil {
		c.JSON(http.StatusOK, resp)
		return
	}
	key := strings.TrimSpace(c.Param("session_id"))
	if key == "" {
		c.JSON(http.StatusOK, resp)
		return
	}
	row, ok, err := h.latestSessionAggregateByKey(key)
	if err != nil {
		fail(c, err)
		return
	}
	if !ok {
		c.JSON(http.StatusOK, resp)
		return
	}

	resp.Found = true
	resp.SessionID = row.SessionID
	resp.ArtifactID = row.ArtifactID
	resp.ArtifactPublicationStatus = bundlePublicationStatusFromStored(row.ArtifactPublicationStatus)
	resp.SourceUpdatedAtMs = msFromTimePtr(row.SourceUpdateAt)
	resp.AggregateSourceUpdatedAtMs = msFromTimePtr(row.SourceUpdateAt)
	if apiSessionAggregateHasTraceFingerprintColumn(h.db) {
		resp.TraceFingerprint = strings.TrimSpace(row.TraceFingerprint)
	}
	if apiSessionAggregateHasAggregateInvalidatedColumn(h.db) {
		resp.AggregateInvalidated = row.AggregateInvalidated
		resp.NeedsRefresh = row.AggregateInvalidated
		if row.AggregateInvalidated {
			resp.Reason = "aggregate_invalidated"
		}
	}
	resp.RefreshStatus = h.latestRefreshQueueStatus(row.SessionID)

	latestUpdatedAt := row.SourceUpdateAt
	latestStatus := resp.ArtifactPublicationStatus
	var shouldEnqueue bool

	if src, found, srcErr := h.latestIndexedSessionSource(row.SessionID, row.ArtifactID); srcErr == nil && found {
		if src.SourceUpdatedAt != nil {
			latestUpdatedAt = src.SourceUpdatedAt
			if row.SourceUpdateAt == nil || src.SourceUpdatedAt.After(*row.SourceUpdateAt) {
				shouldEnqueue = true
				if resp.Reason == "" {
					resp.Reason = "indexed_source_refresh_queued"
				}
			}
		}
	}

	if !resp.NeedsRefresh && h.upstream != nil {
		cookie := h.effectiveCookie(c)
		if cookie != "" {
			baseBundle := buildDetailBundleFromAggregateRow(row)
			tr := detailLookupTimeRange(c, timeRangeFromQuery(c), baseBundle, true)
			ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
			defer cancel()
			hit, hitStatus, resolveErr := h.resolveSessionPublicationStatus(ctx, key, cookie, tr, normalizeArtifactStatus(c.Query("artifact_status")))
			if resolveErr == nil && hit != nil {
				if hitStatus != "" {
					latestStatus = hitStatus
				}
				updatedAt := parseUpstreamTime(hit.UpdateAt)
				if updatedAt.IsZero() {
					updatedAt = parseUpstreamFileTimestamp(*hit)
				}
				if !updatedAt.IsZero() {
					latestUpdatedAt = &updatedAt
					if row.SourceUpdateAt == nil || updatedAt.After(*row.SourceUpdateAt) {
						shouldEnqueue = true
						if resp.Reason == "" {
							resp.Reason = "upstream_source_refresh_queued"
						}
					}
				}
			}
		}
	}

	resp.SourceUpdatedAtMs = msFromTimePtr(latestUpdatedAt)
	if latestStatus != "" {
		resp.ArtifactPublicationStatus = latestStatus
	}
	if (shouldEnqueue || resp.NeedsRefresh) && h.aggregator != nil && sessionRefreshQueueTableExists(h.db) {
		_ = h.aggregator.EnqueueSessionRefresh(sessionRefreshRequest{
			SessionID:                 row.SessionID,
			ArtifactID:                row.ArtifactID,
			TriggerSource:             sessionRefreshTriggerDetailFreshness,
			Priority:                  sessionRefreshPriorityHigh,
			DiscoveredSourceUpdatedAt: latestUpdatedAt,
		})
		if resp.RefreshStatus == "" || shouldEnqueue {
			resp.RefreshStatus = "pending"
		}
	}
	if resp.NeedsRefresh {
		_ = h.markSessionAggregateInvalidated(row, latestUpdatedAt)
		resp.AggregateInvalidated = true
	}
	c.JSON(http.StatusOK, resp)
}
