package utils

import (
	"fmt"
	"log"
)

func LogsErrorf(format string, v ...interface{}) {
	log.Printf(fmt.Sprintf("Error %s %s", "sensitive_finder_engine", format), v...)
}

func LogsInfo(format string, v ...interface{}) {
	log.Printf(fmt.Sprintf("Info %s %s", "sensitive_finder_engine", format), v...)
}

func LogsWarnf(format string, v ...interface{}) {
	log.Printf(fmt.Sprintf("Warn %s %s", "sensitive_finder_engine", format), v...)
}
