package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"sync/atomic"
	"time"

	"github.com/Yajun312890225/dpdknet"
)

const (
	ENCRYPTYPE_NONE  = 0
	ENCRYPTYPE_MODEL = 1
)

type ProxyConfig struct {
	ListenAddr    string
	TargetAddr    string
	Timeout       time.Duration
	EncryptType   int
	BufferSize    int
	EnableLogging bool
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// 解析命令行参数
	config := &ProxyConfig{}
	flag.StringVar(&config.ListenAddr, "listen", ":8080", "Listen address (e.g., :8080)")
	flag.StringVar(&config.TargetAddr, "target", "101.35.244.39:6888", "Target server address")
	flag.DurationVar(&config.Timeout, "timeout", 30*time.Second, "Connection timeout")
	flag.IntVar(&config.EncryptType, "encrypt", ENCRYPTYPE_NONE, "Encryption type (0=none, 1=model)")
	flag.IntVar(&config.BufferSize, "buffer", 10240, "Buffer size for forwarding")
	flag.BoolVar(&config.EnableLogging, "verbose", true, "Enable detailed logging")
	flag.Parse()

	log.Printf("[INFO] Starting TCP Proxy Server...")
	log.Printf("[INFO] Configuration:")
	log.Printf("[INFO]   Listen Address: %s", config.ListenAddr)
	log.Printf("[INFO]   Target Address: %s", config.TargetAddr)
	log.Printf("[INFO]   Timeout: %v", config.Timeout)
	log.Printf("[INFO]   Encrypt Type: %d", config.EncryptType)
	log.Printf("[INFO]   Buffer Size: %d bytes", config.BufferSize)

	// 解析监听地址
	laddr, err := dpdknet.ResolveTCPAddr("tcp", config.ListenAddr)
	if err != nil {
		log.Fatalf("[ERROR] Failed to resolve listen address: %v", err)
	}

	// 创建监听器
	listener, err := dpdknet.ListenTCP("tcp", laddr)
	if err != nil {
		log.Fatalf("[ERROR] Failed to create listener: %v", err)
	}
	defer listener.Close()

	log.Printf("[INFO] Proxy server listening on %s", listener.Addr().String())

	// 连接计数器
	var connectionCount uint64

	// 接受连接
	for {
		clientConn, err := listener.AcceptTCP()
		if err != nil {
			log.Printf("[ERROR] Failed to accept connection: %v", err)
			continue
		}

		connID := atomic.AddUint64(&connectionCount, 1)
		log.Printf("[INFO] [Conn-%d] Accepted connection from %s", connID, clientConn.RemoteAddr().String())

		// 为每个客户端连接启动一个goroutine处理代理
		go handleProxyWithConfig(clientConn, config, connID)
	}
}

func handleProxyWithConfig(clientConn *dpdknet.TCPConn, config *ProxyConfig, connID uint64) {
	defer clientConn.Close()

	logPrefix := fmt.Sprintf("[Conn-%d]", connID)

	// 解析目标地址
	raddr, err := dpdknet.ResolveTCPAddr("tcp", config.TargetAddr)
	if err != nil {
		log.Printf("[ERROR] %s Failed to resolve target address %s: %v", logPrefix, config.TargetAddr, err)
		return
	}

	// 连接到目标服务器
	if config.EnableLogging {
		log.Printf("[INFO] %s Connecting to target server %s...", logPrefix, config.TargetAddr)
	}

	targetConn, err := dpdknet.DialTCP("tcp", nil, raddr)
	if err != nil {
		log.Printf("[ERROR] %s Failed to connect to target server %s: %v", logPrefix, config.TargetAddr, err)
		return
	}
	defer targetConn.Close()

	if config.EnableLogging {
		log.Printf("[INFO] %s Connected to target server %s", logPrefix, config.TargetAddr)
		log.Printf("[INFO] %s Starting bidirectional forwarding %s <-> %s",
			logPrefix, clientConn.RemoteAddr().String(), config.TargetAddr)
	}

	// 流量统计
	var clientToServerFlow uint64
	var serverToClientFlow uint64
	var clientFlowUser uint64
	var serverFlowUser uint64

	// 启动时间记录
	startTime := time.Now()

	// 启动双向转发
	errChan := make(chan error, 2)

	// 客户端到服务器的转发
	go func() {
		err := Forward(clientConn, targetConn, clientConn, targetConn,
			config.Timeout, &clientToServerFlow, make([]byte, config.BufferSize), &clientFlowUser, config.EncryptType)
		if config.EnableLogging {
			log.Printf("[INFO] %s Client->Server forward ended: %v", logPrefix, err)
		}
		errChan <- err
	}()

	// 服务器到客户端的转发
	go func() {
		err := Forward(targetConn, clientConn, targetConn, clientConn,
			config.Timeout, &serverToClientFlow, make([]byte, config.BufferSize), &serverFlowUser, config.EncryptType)
		if config.EnableLogging {
			log.Printf("[INFO] %s Server->Client forward ended: %v", logPrefix, err)
		}
		errChan <- err
	}()

	// 等待任一方向的转发结束
	<-errChan

	// 计算连接持续时间
	duration := time.Since(startTime)

	// 打印流量统计
	log.Printf("[INFO] %s Connection ended after %v. Traffic stats:", logPrefix, duration)
	log.Printf("[INFO] %s   Client->Server: %d bytes", logPrefix, atomic.LoadUint64(&clientToServerFlow))
	log.Printf("[INFO] %s   Server->Client: %d bytes", logPrefix, atomic.LoadUint64(&serverToClientFlow))
	log.Printf("[INFO] %s   Total: %d bytes", logPrefix,
		atomic.LoadUint64(&clientToServerFlow)+atomic.LoadUint64(&serverToClientFlow))
}

// Forward函数 - 从客户端请求中提取的实现
func Forward(sConn, oConn net.Conn, r io.Reader, w io.Writer, timeout time.Duration, flow *uint64, buf []byte, flowUser *uint64, encryptype int) error {
	if len(buf) == 0 {
		buf = make([]byte, 10*1024)
	}

	for {
		if timeout == 0 {
			sConn.SetDeadline(time.Time{})
			oConn.SetDeadline(time.Time{})
		} else {
			sConn.SetDeadline(time.Now().Add(timeout))
			oConn.SetDeadline(time.Now().Add(timeout))
		}

		wbuf := buf
		if n, err := r.Read(buf); err != nil {
			if err == io.EOF {
				return err
			} else {
				return fmt.Errorf("Read, %v", err)
			}
		} else {
			wbuf = buf[:n]
			// 记录流量
			atomic.AddUint64(flow, uint64(n))
			atomic.AddUint64(flowUser, uint64(n))
		}

		for {
			if len(wbuf) == 0 {
				break
			}
			if encryptype == ENCRYPTYPE_MODEL {
				for i := range wbuf {
					wbuf[i] = byte(uint16(^wbuf[i]-7) % 256)
				}
			}
			if n, err := w.Write(wbuf); err != nil {
				if err == io.EOF {
					return err
				} else {
					return fmt.Errorf("Write, %v", err)
				}
			} else {
				wbuf = wbuf[n:]
			}
		}
	}
}
