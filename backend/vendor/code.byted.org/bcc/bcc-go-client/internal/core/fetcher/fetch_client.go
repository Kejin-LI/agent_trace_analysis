package fetcher

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"runtime"
	"time"

	"code.byted.org/bcc/bcc-go-client"
	"code.byted.org/bcc/bcc-go-client/internal/core/common"
	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
	pullmodel "code.byted.org/bcc/pull_json_model/bcc/pull/service"
	cmodel "code.byted.org/bcc/pull_json_model/bccgrpc"
	"code.byted.org/bcc/tools"
	"code.byted.org/gopkg/consul"
	"code.byted.org/gopkg/env"
)

type fetchRequest struct {
	pathList []pathStat
	keyList  []keyStat
}

type client struct {
	opt     fetchOptions
	httpCli *http.Client
}

func newFetchClient(opt fetchOptions) *client {
	c := &client{
		opt: opt,
	}
	c.httpCli = &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				addr, err := c.getEndpoint()
				if err != nil {
					return nil, err
				}
				dialer := net.Dialer{}
				return dialer.DialContext(ctx, network, addr)
			},
			MaxIdleConns: 5,
		},
		Timeout: opt.timeout,
	}
	return c
}

// 向pull_server获取新数据
func (c *client) fetch(msg fetchRequest) ([]*common.UpdatePathMsg, []*common.UpdateKeyMsg, []*common.FinishPathMsg, *common.UpdateIntervalMsg, error) {
	ctx := util.CreateCtx()
	req, err := http.NewRequest(http.MethodPost, c.getUri(), c.getRequestBody(msg))
	if err != nil {
		logger.CtxWarn(ctx, "new request uri:%v err:%v", err)
		return nil, nil, nil, nil, err
	}
	//todo logid

	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	util.AddLogIDToHttpReq(ctx, req)

	t0 := time.Now()
	rsp, err := c.httpCli.Do(req)
	if err != nil {
		logger.CtxWarn(ctx, "get from pull server err:%v", err)
		return nil, nil, nil, nil, err
	}
	cost := time.Since(t0)
	defer rsp.Body.Close()

	if rsp.StatusCode != 200 {
		//pull渠道是可以重试的。因此这里用warning
		if b, iErr := ioutil.ReadAll(rsp.Body); iErr != nil {
			logger.CtxWarn(ctx, "get from pull server read body err:%v", iErr)
		} else {
			logger.CtxWarn(ctx, "get from pull server status[%v] != 200 msg: %v", rsp.StatusCode, string(b))
		}
		return nil, nil, nil, nil, err
	}

	var fetchResp *pullmodel.GetConfRes
	if err = json.NewDecoder(rsp.Body).Decode(&fetchResp); err != nil {
		return nil, nil, nil, nil, err
	}
	logger.Debug("body res:%v", tools.ToJsonStringer(fetchResp))

	firstPath := make(map[string]struct{}) // 首次穿透拉取的path数据列表，成功拉取需要触发一个finish path回调
	for _, path := range msg.pathList {
		if path.lastFetchTime.IsZero() {
			firstPath[path.path] = struct{}{}
		}
	}

	pathRes, keyRes, finishPathMsg, updateIntervalMsg := c.getResult(fetchResp, firstPath)
	logger.CtxDebug(ctx, "get keys:%+v path:%+v cost:%v size:%v bytes", msg.keyList, msg.pathList, cost, rsp.ContentLength)

	return pathRes, keyRes, finishPathMsg, updateIntervalMsg, nil

}

func (c *client) getEndpoint() (string, error) {
	if c.opt.addr != "" {
		return c.opt.addr, nil
	}
	var opts []consul.LookupOptions
	if c.opt.cluster != "" {
		opts = append(opts, consul.WithCluster(c.opt.cluster))
	}
	ep, err := util.GetOneEndpoint(c.opt.psm, opts...)

	return ep.Addr, err

}
func (c *client) getResult(fetchResp *pullmodel.GetConfRes, firstPath map[string]struct{}) (pathMsgList []*common.UpdatePathMsg,
	keyMsgList []*common.UpdateKeyMsg, finishPathMsg []*common.FinishPathMsg, updateIntervalMsg *common.UpdateIntervalMsg) {
	for _, pathItems := range fetchResp.PathUpdate {
		for _, item := range pathItems.UpdateItems {
			pathMsgList = append(pathMsgList, &common.UpdatePathMsg{
				Path:    pathItems.Path,
				KeyItem: ToServerItem(item),
			})
		}
		// 首次通过pull拉取path的配置且成功，触发一个finish path回调
		if _, exist := firstPath[pathItems.Path]; exist {
			finishPathMsg = append(finishPathMsg, &common.FinishPathMsg{
				Path:  pathItems.Path,
				Total: int64(len(pathItems.UpdateItems)),
			})
		}
	}

	for _, k := range fetchResp.KeyUpdate {
		keyMsgList = append(keyMsgList, &common.UpdateKeyMsg{
			KeyItem: ToServerItem(k),
		})
	}
	updateIntervalMsg = &common.UpdateIntervalMsg{
		Interval: fetchResp.QueryInterval,
	}
	return pathMsgList, keyMsgList, finishPathMsg, updateIntervalMsg
}
func (c *client) getUri() string {
	return "http://bytedance.bcc.pull_server/conf/get"
}

func (c *client) getRequestBody(msg fetchRequest) io.Reader {
	fetReq := &pullmodel.GetConfReq{
		Paths:    make([]*pullmodel.PathRequest, 0, len(msg.pathList)),
		Keys:     make([]*pullmodel.KeyRequest, 0, len(msg.keyList)),
		EnvParam: c.getEnvParam(),
	}
	for _, p := range msg.pathList {
		fetReq.Paths = append(fetReq.Paths, &pullmodel.PathRequest{
			Path:          p.path,
			LastFetchTime: p.getLastFetchTs(),
		})
	}
	for _, k := range msg.keyList {
		fetReq.Keys = append(fetReq.Keys, &pullmodel.KeyRequest{
			Key:      k.key,
			UpdateId: k.updateId,
		})
	}

	//todo 高性能序列化
	msgBS, err := json.Marshal(fetReq)
	if err != nil {
		logger.Error("marshal request err:%v", err)
		return nil
	}
	return bytes.NewReader(msgBS)
}

func (c *client) getEnvParam() *cmodel.EnvParam {
	return &cmodel.EnvParam{
		SdkPath:        c.opt.SdkPath,
		SdkVersion:     bcc.SDKVersion(),
		SdkLangName:    c.opt.SdkLang,
		SdkLangVersion: runtime.Version(),
		StartTime:      time.Now().Unix(),
		Tags:           c.opt.Tags,
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
		AuthToken:      tools.GetZtiToken(c.opt.DisableAuth),
	}
}
