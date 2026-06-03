package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"time"

	"code.byted.org/bcc/bcc-go-client/internal/util"
	"code.byted.org/bcc/bcc-go-client/logger"
	pullmodel "code.byted.org/bcc/pull_json_model/bcc/pull/service"
	"code.byted.org/bcc/tools"
	"code.byted.org/gopkg/consul"
	"code.byted.org/gopkg/env"
)

var httpCli *http.Client = newHttpClient()

type ServerSelector struct {
}

func (s ServerSelector) FindBestServer(opts *optionInfo) (string, error) {
	//s.queryBestServer(opts)             //即使在设置了环境变量BCC_SVR_ENV，都能测试queryBestServer
	if os.Getenv("BCC_SVR_ENV") == "" { //环境变量具有最高优先级。如果设置了，就不走pull_svr路子
		if ip, err := s.queryBestServer(opts); err != nil {
			logger.CtxTrace(context.Background(), "fail to query best server. err:%v") //这里不能用warning，一些业务方不需要有warning
			util.EmitFindBestPushSvrCounter("fail")
		} else {
			logger.CtxTrace(context.Background(), "get best server: %v", ip)
			util.EmitFindBestPushSvrCounter("success")
			return ip, nil
		}
	}

	servicePsm := opts.psm
	var consulOpts []consul.LookupOptions
	if opts.idc != "" {
		consulOpts = append(consulOpts, consul.WithIDC(consul.IDC(opts.idc)))

	}
	if opts.cluster != "" {
		consulOpts = append(consulOpts, consul.WithCluster(opts.cluster))
	}
	logger.CtxTrace(context.Background(), "findBestServer opts: %v", tools.ToJson(opts))

	ep, err := util.GetOneEndpoint(servicePsm, consulOpts...)
	if err != nil {
		return "", err
	}
	return util.GetEndpointAddr(ep, util.OtherPORT2), nil
}

// 通过向pull_svr询问得到最优的push_svr地址
func (s ServerSelector) queryBestServer(opts *optionInfo) (string, error) {
	ips, err := s.doQueryBestServer(opts)
	if err != nil {
		return "", err
	}

	if len(ips) == 0 {
		return "", fmt.Errorf("empty query")
	}
	//考虑特殊的场景：对于一个刚启动的push svr，由于连接数最少，本接口会下发这个刚启动的push_svr给所有的SDK，于是所有新的SDK都会连过来这个push_svr
	//由于push svr并不像cds svr内存有了所有的数据。所以这个push svr会了启动的时候发起大量bytekv请求，可能会处理不过来。
	//因此这里返回一个数组，其代表最优的N个push_svr。SDK随机选择一个连接即可。
	index := rand.Intn(len(ips))
	return ips[index], nil
}

func (s ServerSelector) doQueryBestServer(opts *optionInfo) ([]string, error) {
	uri := s.genQueryBestServerURI(opts)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		logger.CtxTrace(context.Background(), "fail to new request, uri:%v, err:%v", uri, err)
		return nil, err
	}

	rsp, err := httpCli.Do(req)
	if err != nil {
		logger.CtxTrace(context.Background(), "fail to get from pull server. err:%v", err)
		return nil, err
	}
	defer rsp.Body.Close()

	if rsp.StatusCode != http.StatusOK {
		logger.CtxTrace(context.Background(), "pull_svr response code: %v", rsp.StatusCode)
		return nil, fmt.Errorf(rsp.Status)
	}

	var queryResp pullmodel.LookupPushServerRes
	if err = json.NewDecoder(rsp.Body).Decode(&queryResp); err != nil {
		return nil, err
	}

	return queryResp.Address, nil
}

func newHttpClient() *http.Client {
	cli := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				ep, err := util.GetOneEndpoint("bytedance.bcc.pull_server")
				if err != nil {
					return nil, err
				}

				newAddr := ep.Addr
				dialer := net.Dialer{}
				return dialer.DialContext(ctx, network, newAddr)
			},
			MaxIdleConns: 5,
		},
		Timeout: 1 * time.Second,
	}

	return cli
}

func (s ServerSelector) genQueryBestServerURI(opts *optionInfo) string {
	//这三个参数是业务方通过option设置的。表示期望目标值。
	var psm, cluster, idc string

	if opts.psm != "" {
		psm = opts.psm
	}
	if opts.cluster != "" {
		cluster = opts.cluster
	}
	if opts.idc != "" {
		idc = opts.idc
	}

	srcPSM, srcCluster, srcIDC := env.PSM(), env.Cluster(), env.IDC() //当业务方没有设置目标时，基本是根据SDK所处的环境返回最优的PushSvr
	return fmt.Sprintf("http://bytedance.bcc.pull_server/lookup_push_server?psm=%s&cluster=%s&idc=%s&src_psm=%s&src_cluster=%s&src_idc=%s", psm, cluster, idc, srcPSM, srcCluster, srcIDC)
}
