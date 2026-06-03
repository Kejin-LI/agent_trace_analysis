package common

import (
	"runtime/debug"

	"code.byted.org/middleware/fic_client"
)

var ClientVersion = "v3.0.2"

func init() {
	collectVersion()
}

func collectVersion() {
	fic_client.Collect("tccclient-go", ClientVersion, nil)
	buildInfo, ok := debug.ReadBuildInfo()
	if ok {
		for _, mod := range buildInfo.Deps {
			if mod.Path == "code.byted.org/gopkg/tccclient/v3" {
				fic_client.Collect("tccclient-go-mod", mod.Version, nil)
				ClientVersion = mod.Version
				if mod.Replace != nil {
					fic_client.Collect("tccclient-go-mod-replace", mod.Replace.Version, nil)
					ClientVersion = mod.Replace.Version
				}
			}
		}
	}
}
