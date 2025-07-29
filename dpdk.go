package dpdknet

import (
	"log"
	"sync"
	"time"

	"github.com/Yajun312890225/nff-go/common"
	"github.com/Yajun312890225/nff-go/flow"
)

var (
	initOnce    sync.Once
	startOnce   sync.Once
	cleanupOnce sync.Once
	initErr     error
	startErr    error
	isStarted   bool
)

func Init() error {
	initOnce.Do(func() {
		config := flow.Config{
			// CPUList:               "0-7",  // 8核都利用
			MbufNumber:            131072, // 提高 mbuf 数量
			MbufCacheSize:         512,    // 提高 per-core 缓存
			RingSize:              4096,   // 加大环形队列
			TXQueuesNumberPerPort: 2,      // 使用4个 TX 队列
			SendCPUCoresPerPort:   2,      // 4核负责 TX
			MaxRecv:               64,     // 提高批量收包
			SchedulerInterval:     100,
			HWTXChecksum:          true,
			RestrictedCloning:     true,
			LogType:               common.No,
			NoSetSIGINTHandler:    true, // 禁用 SIGINT 处理
		}
		initErr = flow.SystemInit(&config)
		if initErr != nil {
			log.Printf("[ERROR] flow.SystemInit failed: %v", initErr)
		}
	})
	return initErr
}

// SystemStart starts the DPDK packet processing system
// This should be called after all flows and handlers are set up
func SystemStart() error {
	startOnce.Do(func() {
		go func() {
			startErr = flow.SystemStart()
			if startErr != nil {
				log.Printf("[ERROR] flow.SystemStart failed: %v", startErr)
			} else {
				isStarted = true
			}
		}()
		// 给DPDK一些时间启动
		time.Sleep(100 * time.Millisecond)
	})
	return startErr
}

// IsStarted returns whether the DPDK system has been started
func IsStarted() bool {
	return isStarted
}
