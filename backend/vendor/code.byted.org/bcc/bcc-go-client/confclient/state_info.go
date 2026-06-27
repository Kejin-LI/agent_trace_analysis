package confclient

import "sync"

type clientState int

const (
	clientRunning clientState = 0
	clientClosed  clientState = 1
)

type stateInfo struct {
	state     clientState
	stateLock sync.Mutex
}
