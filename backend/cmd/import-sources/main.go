// Command import-sources 读取 session 索引 CSV，幂等 upsert 写入 stg_session_sources 表。
//
// 该表是「TOS 实时数据源索引」：仅存 session 元信息与 obj_url（TOS JSONL 地址），
// 不存对话内容。详情页对话由后端实时拉取 obj_url 解析。
//
// 用法（凭证全部来自 TCC / 环境变量，绝不写入代码）：
//
//	go run ./cmd/import-sources -file /path/to/sessions.csv -batch tos-pilot
//
// CSV 需带表头。列名按宽松匹配（大小写/下划线无关），可识别：
//
//	session_id / user_id / user_name / obj_url(objurl/url/tos_url) /
//	create_at(created_at) / update_at(updated_at)
//
// 未识别的列统一收进 extra(JSON)，不丢数据。
// 若无 session_id 列，则从 obj_url 文件名（ses_xxx.jsonl）自动提取。
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"code.byted.org/aidp-playground/agentic_trace_server/internal/db"
	"code.byted.org/aidp-playground/agentic_trace_server/internal/model"
)

func main() {
	var (
		filePath  string
		batchName string
	)
	flag.StringVar(&filePath, "file", "", "session 索引 CSV 路径（必填）")
	flag.StringVar(&batchName, "batch", "tos-import", "导入批次名")
	flag.Parse()

	if filePath == "" {
		log.Fatalf("缺少 -file 参数")
	}

	rows, err := readCSV(filePath)
	if err != nil {
		log.Fatalf("读取 CSV 失败: %v", err)
	}
	log.Printf("已解析 %d 行数据", len(rows))

	gdb, err := db.Open()
	if err != nil {
		log.Fatalf("%v", err)
	}

	// 自动建表（仅新表，不触碰旧 stg_* 表）。
	if err := gdb.AutoMigrate(&model.StgSessionSource{}); err != nil {
		log.Fatalf("建表 stg_session_sources 失败: %v", err)
	}
	log.Printf("✅ 表 stg_session_sources 已就绪")

	var ok, skippedNoArt, skippedFormat int
	err = gdb.Transaction(func(tx *gorm.DB) error {
		for i, r := range rows {
			src, reason := toSource(r, batchName)
			switch reason {
			case "":
				if err := upsertSource(tx, src); err != nil {
					return err
				}
				ok++
			case "no_artifact":
				log.Printf("⚠️ 第 %d 行缺少 artifact_id，跳过", i+2)
				skippedNoArt++
			case "not_jsonl":
				skippedFormat++
			}
		}
		return nil
	})
	if err != nil {
		log.Fatalf("导入失败（已回滚）: %v", err)
	}

	log.Printf("导入完成: 入库 %d 行, 缺 artifact_id 跳过 %d 行, 非 jsonl 跳过 %d 行", ok, skippedNoArt, skippedFormat)
}

// readCSV 读取带表头的 CSV，返回每行的 列名->值 映射（列名已归一化）。
func readCSV(p string) ([]map[string]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rd := csv.NewReader(f)
	rd.FieldsPerRecord = -1 // 容忍每行列数不一致
	rd.LazyQuotes = true

	header, err := rd.Read()
	if err != nil {
		return nil, err
	}
	for i := range header {
		header[i] = normKey(header[i])
	}

	var out []map[string]string
	for {
		rec, err := rd.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		row := make(map[string]string, len(header))
		for i, col := range header {
			if i < len(rec) {
				row[col] = strings.TrimSpace(rec[i])
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// toSource 把一行 CSV 映射为 StgSessionSource。
// 返回值 reason: "" = 入库；"no_artifact" = 缺唯一键；"not_jsonl" = 非 jsonl 格式（按当前阶段约定跳过）。
// 未识别列收进 extra。
func toSource(row map[string]string, batch string) (model.StgSessionSource, string) {
	artifactID := pickCol(row, "artifact_id", "artifactid")
	if artifactID == "" {
		return model.StgSessionSource{}, "no_artifact"
	}
	objURL := pickCol(row, "obj_url", "objurl", "url", "tos_url", "tosurl")
	format := detectFormat(objURL)
	if format != "jsonl" {
		return model.StgSessionSource{}, "not_jsonl"
	}

	sessionID := pickCol(row, "session_id", "sessionid", "session")
	if sessionID == "" {
		sessionID = sessionIDFromURL(objURL)
	}

	known := map[string]struct{}{
		"obj_url": {}, "objurl": {}, "url": {}, "tos_url": {}, "tosurl": {},
		"artifact_id": {}, "artifactid": {},
		"session_id": {}, "sessionid": {}, "session": {},
		"user_id": {}, "userid": {}, "user_name": {}, "username": {},
		"create_at": {}, "created_at": {}, "update_at": {}, "updated_at": {},
	}
	extra := map[string]string{}
	for k, v := range row {
		if _, ok := known[k]; !ok && v != "" {
			extra[k] = v
		}
	}
	extraJSON := "{}"
	if len(extra) > 0 {
		if buf, err := json.Marshal(extra); err == nil {
			extraJSON = string(buf)
		}
	}

	return model.StgSessionSource{
		ArtifactID:      artifactID,
		SessionID:       sessionID,
		UserID:          pickCol(row, "user_id", "userid"),
		UserName:        pickCol(row, "user_name", "username"),
		ObjURL:          objURL,
		ObjFormat:       format,
		SourceCreatedAt: parseTime(pickCol(row, "create_at", "created_at")),
		SourceUpdatedAt: parseTime(pickCol(row, "update_at", "updated_at")),
		Extra:           extraJSON,
		ImportBatch:     batch,
	}, ""
}

func upsertSource(tx *gorm.DB, src model.StgSessionSource) error {
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "artifact_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"session_id", "user_id", "user_name", "obj_url", "obj_format",
			"source_created_at", "source_updated_at",
			"extra", "import_batch", "updated_at",
		}),
	}).Create(&src).Error
}

// detectFormat 通过 obj_url 后缀判断格式：jsonl / json / unknown。
func detectFormat(u string) string {
	if u == "" {
		return ""
	}
	base := strings.ToLower(path.Base(u))
	switch {
	case strings.HasSuffix(base, ".jsonl"):
		return "jsonl"
	case strings.HasSuffix(base, ".json"):
		return "json"
	default:
		return "unknown"
	}
}

// normKey 归一化列名：去空格、转小写、去 BOM。
func normKey(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	return strings.ToLower(strings.TrimSpace(s))
}

func pickCol(row map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := row[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// sessionIDFromURL 从 .../ses_xxx.jsonl 提取 session_id。
func sessionIDFromURL(u string) string {
	if u == "" {
		return ""
	}
	base := path.Base(u)
	base = strings.TrimSuffix(base, ".jsonl")
	if strings.HasPrefix(base, "ses_") {
		return base
	}
	return ""
}

// parseTime 宽松解析常见时间格式，失败返回 nil。
func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		time.RFC3339,
		"2006/01/02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return &t
		}
	}
	return nil
}
