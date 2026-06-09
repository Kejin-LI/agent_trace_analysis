package api

import (
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
	AggregatorEnabled      bool   `json:"aggregator_enabled"`
	AggregatorInitError    string `json:"aggregator_init_error,omitempty"`
	UpstreamEnabled        bool   `json:"upstream_enabled"`
	BundleJSONColumnExists bool   `json:"bundle_json_column_exists"`
}

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
		resp.DBPingOK = h.db.Exec("SELECT 1").Error == nil
		resp.BundleJSONColumnExists = hasColumn(h, "api_session_aggregates", "bundle_json")
	}
	resp.DBOpenError = firstNonEmpty(resp.DBOpenError, os.Getenv("DB_OPEN_ERROR"))
	c.JSON(http.StatusOK, resp)
}

func hasColumn(h *Handler, tableName, columnName string) bool {
	if h == nil || h.db == nil {
		return false
	}
	return h.db.Migrator().HasColumn(tableName, columnName)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
