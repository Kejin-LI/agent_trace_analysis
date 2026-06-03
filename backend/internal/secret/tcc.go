// Package secret 提供从 TCC（配置中心）安全读取敏感凭据的能力。
//
// 设计参考 figma_tracker_server 项目：TCC 坐标（Space/Config/Field）是非敏感的
// "地址元数据"，硬编码进代码以规避申请 TCE env 工单；真正的密钥值只存在 TCC
// 加密配置里，本地与 TCE 环境变量均不可见，满足加密合规要求。
package secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	tccclient "code.byted.org/gopkg/tccclient/v3"
)

var (
	tccCli     *tccclient.ClientV3
	tccInitErr error
	once       sync.Once
)

// TCCReady 报告 TCC 客户端是否已经成功初始化。
func TCCReady() bool {
	return tccCli != nil
}

// defaultDir 是 V3 README 标注的默认监听目录，不填即为该值。
const defaultDir = "/default"

// defaultService 是项目级硬编码的 TCC Space 名（Service Name），
// 对应 TCC 控制台左上角的「空间名」，与本服务 PSM 一致。可由 TCC_SERVICE env 覆盖。
const defaultService = "aidp.playground.agentic_trace_server"

// watchDir 返回初始化时要监听的 TCC 目录。可由 TCC_DIR env 覆盖。
func watchDir() string {
	if d := os.Getenv("TCC_DIR"); d != "" {
		return d
	}
	return defaultDir
}

// resolveServiceName 决定要传给 NewClientV3 的 service name。
// 优先 env（TCC_SERVICE），其次硬编码默认值。
func resolveServiceName() string {
	if s := os.Getenv("TCC_SERVICE"); s != "" {
		return s
	}
	return defaultService
}

// InitTCC 进程启动时调用一次（用 sync.Once 保证单例）。
//
// 服务名（Service Name）= TCC 上的「空间名」（Space）。
// 例：TCC_SERVICE = aidp.turing.config
//
// V3 用 functional option：NewClientV3(serviceName, WithPath(dir))。
func InitTCC() {
	once.Do(func() {
		serviceName := resolveServiceName()
		if serviceName == "" {
			tccInitErr = errors.New("TCC service name is empty, TCC disabled")
			log.Printf("⚠️ [TCC] %v", tccInitErr)
			return
		}

		dir := watchDir()
		cli, err := tccclient.NewClientV3(serviceName, tccclient.WithPath(dir))
		if err != nil {
			tccInitErr = fmt.Errorf("init TCC client failed: %w", err)
			log.Printf("❌ [TCC] %v", tccInitErr)
			return
		}
		tccCli = cli
		log.Printf("✅ [TCC] client initialized: service=%s, dir=%s", serviceName, dir)
	})
}

// splitDirAndName 把上层传进来的单一 key 拆成 V3 要求的 (dir, configName)。
//
//   - 入参不含 "/"：dir 走 watchDir()，configName 原样。
//   - 入参含 "/"：按最后一个 "/" 切分，前半为 dir，后半为 configName。
func splitDirAndName(key string) (string, string) {
	if !strings.Contains(key, "/") {
		return watchDir(), key
	}
	dir, name := path.Split(key)
	dir = strings.TrimRight(dir, "/")
	if dir == "" {
		dir = "/"
	}
	return dir, name
}

// GetEncrypted 从 TCC 拉取加密配置项的原始字符串。
// SDK 内部已实现缓存兜底，启动期 TCC 抖动会自动回退到缓存值。
func GetEncrypted(ctx context.Context, key string) (string, error) {
	if tccCli == nil {
		return "", fmt.Errorf("TCC client not ready: %v", tccInitErr)
	}
	getCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	dir, name := splitDirAndName(key)
	val, err := tccCli.Get(getCtx, dir, name)
	if err != nil {
		return "", fmt.Errorf("TCC Get(dir=%s, name=%s) failed: %w", dir, name, err)
	}
	return strings.TrimSpace(val), nil
}

// GetEncryptedJSON 从 TCC 拉取一个 JSON 类型的加密配置项，反序列化成 map。
//
// configKey: TCC 上的配置项名，例：agentic_trace_server.db.config
//
// TCC 上配置内容形如：
//
//	{
//	  "db_psm":      "toutiao.mysql.agent_trace_staging_write",
//	  "db_name":     "agent_trace_staging",
//	  "db_user":     "age...w-...",
//	  "db_password": "xxxx"
//	}
func GetEncryptedJSON(ctx context.Context, configKey string) (map[string]string, error) {
	raw, err := GetEncrypted(ctx, configKey)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, fmt.Errorf("TCC config %s is empty", configKey)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("TCC config %s is not a JSON object of string→string: %w", configKey, err)
	}
	return m, nil
}
