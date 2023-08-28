package etcd

import (
	"math/rand"
	"sync/atomic"
)

type Picker interface {
	Pick() string
}

const (
	randomPicker     = "random"
	roundRobinPicker = "roundrobin"
)

type rPicker struct {
	addrs []string
}

func newRPicker(addrs []string) *rPicker {
	return &rPicker{
		addrs: addrs,
	}
}

func (p *rPicker) Pick() string {
	return p.addrs[rand.Int()%len(p.addrs)]
}

type rrPicker struct {
	addrs []string
	next  uint32
}

func newRRPicker(addrs []string) *rrPicker {
	return &rrPicker{
		addrs: addrs,
	}
}

func (p *rrPicker) Pick() string {
	addrsLen := uint32(len(p.addrs))
	nextIndex := atomic.AddUint32(&p.next, 1)
	sc := p.addrs[nextIndex%addrsLen]
	return sc
}
