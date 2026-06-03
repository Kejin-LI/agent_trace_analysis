为了更新msg的数据结构同时保持gosdk的兼容性，新建此repo作为logs库中agent_provider的发送sdk

##日志规范定义

https://bytedance.feishu.cn/space/doc/doccnYepiDINJoiCAyup1avyq8d#

## gosdk使用方法
### 打印业务日志
    // 创建一个sender，创建时就要定义好TaskName等固定头信息
    // 对于一般业务日志而言name就是PSM
    sender := NewSenderByName("p.s.m")
    sender.Start()
    	
    // 创建一个MsgV3对象
    header := &MsgV3Header{
            Level:    "Info",
            Location: "server.go:10",
            LogID:    "20190627195938010026066219617688E",
            Ts:       1561703943000,
            SpanID:   12345,
    }
    msg := NewMsgV3([]byte("this is my first log"),header,"myCustomTag","1")
    // 发送日志对象
    err = sender.Send(msg)
    if err != nil {
        t.Error("send fail %s", err.Error())
    }
    // 优雅关闭sender
    sender.Exit()

### 打印rpc日志
    // 创建一个sender，对于Rpc日志而言，TaskName是固定值"_rpc"
    sender := NewTraceLogSender()
    sender.Start()

    // 创建一个RpcMsg对象
    msg := &RpcMsg{
        Level:         "Info",
        Location:      "call.go:10",
        LogID:         "20190627195938010026066219617688E",
        Ts:            1561703943000,
        Type:          TypeThriftAccess,
        SpanID:        rand.Uint64(),
        ParentSpanID:  rand.Uint64(),
        CostInUs:      100000,
        LocalMethod:   "localM",
        RemoteMethod:  "remoteM",
        RemoteCluster: "online",
        RemoteService: "bytedance.remote.service",
        RemoteIp:      1234567,
        Tags: []*KeyValueV3{
            {"myCustomTag", "1"},
        },
    }
    // 发送日志对象
    err = sender.Send(msg)
    if err != nil {
        t.Error("send fail", err.Error())
    }
    // 优雅关闭sender
    sender.Exit()    