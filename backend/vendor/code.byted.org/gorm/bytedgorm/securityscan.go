package bytedgorm

import (
	"sync"
)

var (
	ContextSkipSecurityScanTableForRead = "SKIP_SECURITY_SCAN_TABLE_FOR_READ"
	defaultSecurityScanContextKey       = "SECURITY_SCAN"
	defaultSecurityScanTableSuffix      = "_security"     //
	defaultSecurityScanCallbackSuffix   = "security_scan" //
)

// WithSecurityScanSupport security team blackbox request data will inject to _security table
func WithSecurityScanSupport() *stressTest {
	return WithSecurityScanWithOption(SecurityScanOption{})
}

type SecurityScanOption struct {
	SecurityScanTableSuffix                string
	ContextSecurityScanKey                 string
	AllowFallbackToOriginalTable           bool
	CallbackSuffix                         string
	DontSupportTableAliasInDeleteStatement bool
}

// WithSecurityScanWithOption support security scan with custom option
func WithSecurityScanWithOption(config SecurityScanOption) *stressTest {
	if config.ContextSecurityScanKey == "" {
		config.ContextSecurityScanKey = defaultSecurityScanContextKey
	}
	if config.SecurityScanTableSuffix == "" {
		config.SecurityScanTableSuffix = defaultSecurityScanTableSuffix
	}
	if config.CallbackSuffix == "" {
		config.CallbackSuffix = defaultSecurityScanCallbackSuffix
	}
	var stressConfig = StressTestOption{
		StressTestTableSuffix:                  config.SecurityScanTableSuffix,
		ContextStressKey:                       config.ContextSecurityScanKey,
		AllowFallbackToOriginalTable:           config.AllowFallbackToOriginalTable,
		CallbackSuffix:                         config.CallbackSuffix,
		DontSupportTableAliasInDeleteStatement: config.DontSupportTableAliasInDeleteStatement,
	}

	return &stressTest{
		StressTestOption:           stressConfig,
		ContextSkipForRead:         ContextSkipSecurityScanTableForRead,
		tableNameCache:             sync.Map{},
		stressTestTableSuffixBytes: []byte(config.SecurityScanTableSuffix),
	}
}
