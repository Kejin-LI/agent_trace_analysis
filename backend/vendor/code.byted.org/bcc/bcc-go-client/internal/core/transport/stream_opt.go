package transport

import "time"

type optionInfo struct {
	addr    string
	psm     string
	cluster string
	idc     string
	timeout time.Duration
}

type StreamClientOption func(*StreamClient)

func StreamWithPSM(psm string) StreamClientOption {
	return func(s *StreamClient) {
		s.opInfo.psm = psm
	}
}

func StreamWithCluster(cluster string) StreamClientOption {
	return func(s *StreamClient) {
		s.opInfo.cluster = cluster
	}
}

func StreamWithIDC(idc string) StreamClientOption {
	return func(s *StreamClient) {
		s.opInfo.idc = idc
	}
}

func StreamWithTimeout(timeout time.Duration) StreamClientOption {
	return func(s *StreamClient) {
		s.opInfo.timeout = timeout
	}
}

func StreamWithAddr(addr string) StreamClientOption {
	return func(s *StreamClient) {
		s.opInfo.addr = addr
	}
}
