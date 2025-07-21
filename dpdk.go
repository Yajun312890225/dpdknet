package dpdknet

import (
	"sync"

	"github.com/Yajun312890225/nff-go/common"
	"github.com/Yajun312890225/nff-go/flow"
)

var (
	initOnce sync.Once
	initErr  error
)

func Init() error {
	initOnce.Do(func() {
		config := flow.Config{
			TXQueuesNumberPerPort: 1,
			SendCPUCoresPerPort:   1,
			MaxRecv:               1,
			SchedulerInterval:     100,
			DPDKArgs:              []string{},
			DebugTime:             0,
			LogType:               common.No,
		}
		initErr = flow.SystemInit(&config)
	})
	return initErr
}
