package config_service

import (
	"os"

	"code.byted.org/gopkg/env"
)

type RegionType int32

const (
	REGION_NORMAL RegionType = iota
	REGION_TTP
	REGION_GCP
)

func DetermineRegion() RegionType {
	if env.Region() == env.R_USTTP || env.Region() == env.R_USTTP2 ||
		os.Getenv("METRICS_IN_TTP") == "true" {
		return REGION_TTP
	} else if env.IDC() == "useast2a" ||
		os.Getenv("METRICS_IN_GCP") == "true" {
		return REGION_GCP
	}
	return REGION_NORMAL
}
