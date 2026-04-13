package asynqwrap

import "sync"

func syncCond(mu *sync.Mutex) *sync.Cond {
	return sync.NewCond(mu)
}
