package dpdknet

import (
	"log"
	"sync"

	"github.com/Yajun312890225/nff-go/flow"
)

var (
	initOnce  sync.Once
	startOnce sync.Once
	initErr   error
	startErr  error
	isStarted bool
)

func Init() error {
	log.Printf("[DEBUG] DPDK Init() called")
	initOnce.Do(func() {
		config := flow.Config{
			TXQueuesNumberPerPort: 1,
			SendCPUCoresPerPort:   1,
			MaxRecv:               1,
			SchedulerInterval:     100,
			DPDKArgs:              []string{},
			// DebugTime:             0,
			// LogType:               common.No,
		}
		log.Printf("[DEBUG] Calling flow.SystemInit with config: %+v", config)
		initErr = flow.SystemInit(&config)
		if initErr != nil {
			log.Printf("[ERROR] flow.SystemInit failed: %v", initErr)
		} else {
			log.Printf("[DEBUG] flow.SystemInit successful")
		}
	})
	return initErr
}

// SystemStart starts the DPDK packet processing system
// This should be called after all flows and handlers are set up
func SystemStart() error {
	log.Printf("[DEBUG] SystemStart() called")
	startOnce.Do(func() {
		log.Printf("[DEBUG] Calling flow.SystemStart...")
		startErr = flow.SystemStart()
		if startErr != nil {
			log.Printf("[ERROR] flow.SystemStart failed: %v", startErr)
		} else {
			log.Printf("[INFO] DPDK packet processing system started successfully")
			isStarted = true
		}
	})
	return startErr
}

// IsStarted returns whether the DPDK system has been started
func IsStarted() bool {
	return isStarted
}
