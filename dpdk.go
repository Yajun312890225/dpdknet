package dpdknet

import (
	"log"
	"sync"
	"time"

	"github.com/Yajun312890225/nff-go/common"
	"github.com/Yajun312890225/nff-go/flow"
)

var (
	initOnce           sync.Once
	startOnce          sync.Once
	cleanupOnce        sync.Once
	initErr            error
	startErr           error
	isStarted          bool
	autoCleanupEnabled bool = true // 默认启用自动清理
)

func Init() error {
	log.Printf("[DEBUG] DPDK Init() called")
	initOnce.Do(func() {
		config := flow.Config{
			CPUList:               "0-7",  // 8核都利用
			MbufNumber:            131072, // 提高 mbuf 数量
			MbufCacheSize:         512,    // 提高 per-core 缓存
			RingSize:              4096,   // 加大环形队列
			TXQueuesNumberPerPort: 4,      // 使用4个 TX 队列
			SendCPUCoresPerPort:   4,      // 4核负责 TX
			MaxRecv:               128,    // 提高批量收包
			SchedulerInterval:     100,
			HWTXChecksum:          true,
			RestrictedCloning:     true,
			LogType:               common.Debug,
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
		log.Printf("[DEBUG] Starting DPDK packet processing in background...")
		go func() {
			log.Printf("[DEBUG] Calling flow.SystemStart in goroutine...")
			startErr = flow.SystemStart()
			if startErr != nil {
				log.Printf("[ERROR] flow.SystemStart failed: %v", startErr)
			} else {
				log.Printf("[INFO] DPDK packet processing system started successfully")
				isStarted = true
			}
		}()
		// 给DPDK一些时间启动
		time.Sleep(100 * time.Millisecond)
		log.Printf("[DEBUG] SystemStart setup completed")
	})
	return startErr
}

// IsStarted returns whether the DPDK system has been started
func IsStarted() bool {
	return isStarted
}
