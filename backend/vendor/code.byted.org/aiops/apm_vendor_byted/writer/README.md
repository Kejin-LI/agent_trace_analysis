# 实现了内场Metrics TCP Sender和HTTP Sender


# 示例
```
import(
	"code.byted.org/aiops/apm_vendor_byted/writer/http"
	"code.byted.org/aiops/apm_vendor_byted/writer/tcp"
)

```
使用TCP Sender
```
	tcpSender, err := tcp.NewTcpWriter()
	if err != nil {
        // handle error
		// panic(fmt.Sprintf("failed to create the tcp writer: %v\n", err))
	}

	client, err = m.NewClient("metrics.sdk.tcp",
		m.SetWriter(tcpSender),
	)

```
使用HTTP Sender
```
	httpSender, err := http.NewHTTPWriter()
	if err != nil {
        // handle error
		// panic(fmt.Sprintf("failed to create the http writer: %v\n", err))
	}

	client, err = m.NewClient("metrics.sdk.http",
		m.SetWriter(httpSender),
	)
```