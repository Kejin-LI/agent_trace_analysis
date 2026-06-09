package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

type selfCheckResponse struct {
	DataSource             string `json:"data_source"`
	DBInitialized          bool   `json:"db_initialized"`
	DBPingOK               bool   `json:"db_ping_ok"`
	DBOpenError            string `json:"db_open_error,omitempty"`
	DBProbeError           string `json:"db_probe_error,omitempty"`
	AggregatorEnabled      bool   `json:"aggregator_enabled"`
	AggregatorInitError    string `json:"aggregator_init_error,omitempty"`
	UpstreamEnabled        bool   `json:"upstream_enabled"`
	BundleJSONColumnExists bool   `json:"bundle_json_column_exists"`
}

// selfCheck 必须 panic 安全：它存在的意义就是在依赖（DB/上游）异常时
// 暴露真实状态。任何一步崩了都要降级成可读 JSON，绝不能自己 500 把根因藏起来。
func (h *Handler) selfCheck(c *gin.Context) {
	dbOpenError := ""
	aggregatorInitError := ""
	if h != nil {
		dbOpenError = strings.TrimSpace(h.dbOpenError)
		aggregatorInitError = strings.TrimSpace(h.aggregatorInitError)
	}
	resp := selfCheckResponse{
		DataSource:          dataSourceMode(),
		DBInitialized:       h != nil && h.db != nil,
		DBOpenError:         dbOpenError,
		AggregatorEnabled:   h != nil && h.aggregator != nil,
		AggregatorInitError: aggregatorInitError,
		UpstreamEnabled:     h != nil && h.upstream != nil,
	}
	if h != nil && h.db != nil {
		if perr := safeProbe(func() error {
			return h.db.Exec("SELECT 1").Error
		}); perr != nil {
			resp.DBProbeError = perr.Error()
		} else {
			resp.DBPingOK = true
		}
		_ = safeProbe(func() error {
			resp.BundleJSONColumnExists = h.db.Migrator().HasColumn("api_session_aggregates", "bundle_json")
			return nil
		})
	}
	resp.DBOpenError = firstNonEmpty(resp.DBOpenError, os.Getenv("DB_OPEN_ERROR"))
	c.JSON(http.StatusOK, resp)
}

// safeProbe 执行一个可能 panic 或返回 error 的探针，统一转成 error，
// 保证 selfCheck 自身永不崩溃。
func safeProbe(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
