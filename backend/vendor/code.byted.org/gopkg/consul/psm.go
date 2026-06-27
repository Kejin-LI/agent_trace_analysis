package consul

import (
	"os"
	"sync"
)

const psmUnknown = "-"

var (
	psm     string
	psmOnce sync.Once
)

func init() {
	psmOnce.Do(loadPSM)
}

func loadPSM() {
	psm = os.Getenv("LOAD_SERVICE_PSM")
	if psm == "" {
		psm = os.Getenv("PSM")
	}
	if psm == "" {
		psm = os.Getenv("TCE_PSM")
	}
	if psm == "" {
		psm = psmUnknown
	}
}

func PSM() string {
	psmOnce.Do(loadPSM)
	return psm
}
