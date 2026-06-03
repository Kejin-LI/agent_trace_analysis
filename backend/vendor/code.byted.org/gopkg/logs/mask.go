package logs

import (
	"code.byted.org/gopkg/logs/utils"
	"code.byted.org/security/sensitive_finder_engine"
	"code.byted.org/security/sensitive_finder_engine/mask_pack"
)

func SetMaskMode(flag bool) {
	utils.SetMaskMode(flag)
}

func SetMaskFunc(kind string, udf mask_pack.MaskFunc) {
	mask_pack.SetMaskFunc(kind, udf)
}

func AddMaskEngineProcessor(kind ...string) error {
	return defaultLogger.AddProcessor(sensitive_finder_engine.SecdataEngineSensitiveLogsProcessorWithKind(kind))
}

func AddMaskFinderProcessor(kind ...string) error {
	return defaultLogger.AddProcessor(sensitive_finder_engine.MaskSensitiveLogsProcessorWithKind(kind))
}

func (l *Logger) AddMaskEngineProcessor(kind ...string) error {
	return l.AddProcessor(sensitive_finder_engine.SecdataEngineSensitiveLogsProcessorWithKind(kind))
}

func (l *Logger) AddMaskFinderProcessor(kind ...string) error {
	return l.AddProcessor(sensitive_finder_engine.MaskSensitiveLogsProcessorWithKind(kind))
}
