package bcc

import (
	"runtime/debug"
)

var sdkVersion = "v0.1.17"

func init() {
	updateVersion()
}

func updateVersion() {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	for _, mod := range buildInfo.Deps {
		if mod.Path != "code.byted.org/bcc/bcc-go-client" {
			continue
		}

		sdkVersion = mod.Version

		if mod.Replace != nil {
			sdkVersion = mod.Replace.Version
		}
	}
}

func SDKVersion() string {
	return sdkVersion
}
