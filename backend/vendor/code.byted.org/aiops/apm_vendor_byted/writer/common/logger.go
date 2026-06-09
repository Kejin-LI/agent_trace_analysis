package common

import (
	"fmt"
	"os"
	"time"

	"github.com/go-kit/log"
)

const tsFormat = time.RFC3339

var (
	Logger = log.With(
		log.NewLogfmtLogger(os.Stdout),
		"ts",
		log.TimestampFormat(
			func() time.Time { return time.Now().Local() },
			tsFormat,
		),
		"caller",
		log.Caller(4),
	)

	LogFunc = func(format string, args ...interface{}) {
		_ = Logger.Log("msg", fmt.Sprintf(format, args...))
	}
)
