package consul

import (
	"math/rand"
	"net"
	"strconv"
)

type tags map[string]string

type ConsulEndpoint struct {
	Host string
	Port int
	Tags tags
}

func (e *ConsulEndpoint) parse() Endpoint {
	var ret Endpoint
	ret.Addr = net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
	ret.Tags = e.Tags
	ret.Cluster = e.Tags["cluster"]
	ret.Env = e.Tags["env"]
	if w, err := strconv.Atoi(e.Tags["weight"]); err == nil {
		ret.Weight = w
	} else {
		ret.Weight = 50 // default TCE weight
	}
	return ret
}

type Endpoint struct {
	Addr    string
	Cluster string
	Env     string
	Weight  int
	Tags    tags
}

type Endpoints []Endpoint

func (ee Endpoints) Filter(f func(e Endpoint) bool) Endpoints {
	ret := make([]Endpoint, 0, len(ee))
	for _, e := range ee {
		if f(e) {
			ret = append(ret, e)
		}
	}
	return Endpoints(ret)
}

func (ee Endpoints) FilterCluster(name string) Endpoints {
	ret := make([]Endpoint, 0, len(ee))
	for _, e := range ee {
		if e.Cluster == name {
			ret = append(ret, e)
		}
	}
	return ret
}

func (ee Endpoints) Addrs() []string {
	ret := make([]string, len(ee))
	for i, e := range ee {
		ret[i] = e.Addr
	}
	return ret
}

var NoEndPoint = Endpoint{Addr: "127.4.0.4:404"}

func (ee Endpoints) GetOne() Endpoint {
	if len(ee) == 0 {
		return NoEndPoint
	}
	ww := int64(0)
	for _, e := range ee {
		ww += int64(e.Weight)
	}
	if ww == 0 { // all weight == 0 ?
		return ee[rand.Intn(len(ee))]
	}
	n := rand.Int63n(ww)
	for _, e := range ee {
		n -= int64(e.Weight)
		if n < 0 {
			return e
		}
	}
	panic("should not here")
	return Endpoint{}
}
