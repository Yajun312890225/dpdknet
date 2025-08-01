package dpdknet

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
)

var (
	pcapWriter    *pcapgo.Writer
	pcapFile      *os.File
	pcapMutex     sync.Mutex
	pcapEnabled   bool
	pcapFilename  string
	packetCounter uint64 // 抓包计数器
)

// InitPcapCapture 初始化 pcap 文件抓包
// 通过环境变量ENABLE_PCAP_CAPTURE控制是否启用，默认不开启
// 自动生成带时间戳的文件名，格式：dpdknet_capture_YYYYMMDD_HHMMSS.pcap
func InitPcapCapture() error {
	// 检查环境变量是否启用pcap抓包
	enablePcap := os.Getenv("ENABLE_PCAP_CAPTURE")
	if enablePcap != "true" && enablePcap != "1" {
		log.Printf("[PCAP] Packet capture disabled (ENABLE_PCAP_CAPTURE=%s)", enablePcap)
		return nil
	}

	pcapMutex.Lock()
	defer pcapMutex.Unlock()

	if pcapEnabled {
		return fmt.Errorf("pcap capture already initialized")
	}

	// 生成带时间戳的文件名
	timestamp := time.Now().Format("20060102_150405")
	pcapFilename = fmt.Sprintf("dpdknet_capture_%s.pcap", timestamp)

	var err error
	pcapFile, err = os.Create(pcapFilename)
	if err != nil {
		return fmt.Errorf("failed to create pcap file %s: %v", pcapFilename, err)
	}

	// 写 pcap 文件头，数据链路类型是 Ethernet
	pcapWriter = pcapgo.NewWriter(pcapFile)
	err = pcapWriter.WriteFileHeader(65536, layers.LinkTypeEthernet)
	if err != nil {
		pcapFile.Close()
		os.Remove(pcapFilename)
		return fmt.Errorf("failed to write pcap file header: %v", err)
	}

	pcapEnabled = true
	packetCounter = 0
	log.Printf("[PCAP] Packet capture initialized, saving to: %s", pcapFilename)
	return nil
}

// WritePacketToPcap 写入一条抓到的包
// 这个函数在性能敏感的数据路径中调用，采用非阻塞设计
func WritePacketToPcap(data []byte) error {
	pcapMutex.Lock()
	defer pcapMutex.Unlock()

	if !pcapEnabled || pcapWriter == nil {
		return nil // 静默忽略，避免影响性能
	}

	captureInfo := gopacket.CaptureInfo{
		Timestamp:     time.Now(),
		CaptureLength: len(data),
		Length:        len(data),
	}

	err := pcapWriter.WritePacket(captureInfo, data)
	if err == nil {
		packetCounter++
	}
	return err
}

// ClosePcapCapture 关闭 pcap 抓包并打印详细的文件信息
// 在程序退出时自动调用，提供抓包统计信息
func ClosePcapCapture() {
	pcapMutex.Lock()
	defer pcapMutex.Unlock()

	if !pcapEnabled {
		return
	}

	if pcapFile != nil {
		pcapFile.Close()
		pcapFile = nil
	}

	pcapWriter = nil
	pcapEnabled = false

	// 获取文件信息并打印详细统计
	if stat, err := os.Stat(pcapFilename); err == nil {
		log.Printf("[PCAP] ========================================")
		log.Printf("[PCAP] Packet capture completed!")
		log.Printf("[PCAP] Capture file: %s", pcapFilename)
		log.Printf("[PCAP] File size: %d bytes", stat.Size())
		log.Printf("[PCAP] Packets captured: %d", packetCounter)
		if packetCounter > 0 {
			log.Printf("[PCAP] Average packet size: %.1f bytes", float64(stat.Size())/float64(packetCounter))
		}
		log.Printf("[PCAP] You can analyze the capture with:")
		log.Printf("[PCAP]   wireshark %s", pcapFilename)
		log.Printf("[PCAP]   tcpdump -r %s", pcapFilename)
		log.Printf("[PCAP] ========================================")
	} else {
		log.Printf("[PCAP] Capture file: %s (unable to get file info: %v)", pcapFilename, err)
	}
}

// IsPcapEnabled 检查pcap是否已启用
func IsPcapEnabled() bool {
	pcapMutex.Lock()
	defer pcapMutex.Unlock()
	return pcapEnabled
}

// GetPcapFilename 获取当前pcap文件名
func GetPcapFilename() string {
	pcapMutex.Lock()
	defer pcapMutex.Unlock()
	return pcapFilename
}

// GetPcapStats 获取pcap抓包统计信息
func GetPcapStats() (enabled bool, filename string, packetCount uint64) {
	pcapMutex.Lock()
	defer pcapMutex.Unlock()
	return pcapEnabled, pcapFilename, packetCounter
}
