package common

import (
	"runtime"
	"time"

	"code.byted.org/bcc/tools"
	"code.byted.org/gopkg/env"

	"code.byted.org/bcc/bcc-go-client"
	"code.byted.org/bcc/bcc-go-client/internal/core/pb"
	"code.byted.org/bcc/bcc-go-client/internal/util"
)

type RegisterInfo struct {
	SdkPath      string
	SdkLang      string
	ClientName   string
	ConnectCount int64
	Tags         map[string]string
	DisableAuth  bool
}

type WatchOption struct {
	Timeout           time.Duration
	EnableEmpty       bool
	DisableListen     bool
	DisableMemory     bool
	FastTerminate     bool
	DisableBackupFile bool
}

func (i RegisterInfo) BuildEnvParam() *pb.EnvParam {
	envParam := &pb.EnvParam{
		SdkPath:        i.SdkPath,
		SdkVersion:     bcc.SDKVersion(),
		SdkLangName:    i.SdkLang,
		SdkLangVersion: runtime.Version(),
		StartTime:      time.Now().Unix(),
		Tags:           i.Tags,
		Envs:           tools.NecessaryEnviron(),
		Os:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		Psm:            env.PSM(),
		Region:         env.Region(),
		Cluster:        util.GetCluster(),
		Idc:            env.IDC(),
		Host:           env.HostIP(),
		PodName:        env.PodName(),
		IsBoe:          env.IsBoe(),
		InTce:          env.InTCE(),
		TceStage:       env.Stage(),
		TceEnv:         env.Env(),
		TceHostEnv:     tools.GetTceHostEnv(),
		HostV4:         env.HostIPV4(),
		HostV6:         env.HostIPV6(),
		AuthToken:      tools.GetZtiToken(i.DisableAuth),
	}

	return envParam
}
