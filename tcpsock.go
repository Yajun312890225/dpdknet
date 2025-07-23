package dpdknet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

const (
	TCPFlagFIN = 0x01
	TCPFlagSYN = 0x02
	TCPFlagRST = 0x04
	TCPFlagPSH = 0x08
	TCPFlagACK = 0x10
	TCPFlagURG = 0x20
)

type TCPState int

const (
	TCPStateClosed TCPState = iota
	TCPStateListen
	TCPStateSynSent
	TCPStateSynReceived
	TCPStateEstablished
	TCPStateFinWait1
	TCPStateFinWait2
	TCPStateCloseWait
	TCPStateClosing
	TCPStateLastAck
	TCPStateTimeWait
)

type TCPHeader struct {
	SrcPort    uint16
	DstPort    uint16
	SeqNum     uint32
	AckNum     uint32
	DataOffset uint8
	Flags      uint8
	Window     uint16
	Checksum   uint16
	UrgentPtr  uint16
}

type TCPConn struct {
	localAddr  *TCPAddr
	remoteAddr *TCPAddr
	state      TCPState
	seqNum     uint32
	ackNum     uint32
	dataCh     chan []byte
	mu         sync.Mutex
	closed     bool
}

type TCPListener struct {
	localAddr *TCPAddr
	connCh    chan *TCPConn
	mu        sync.Mutex
	closed    bool
}

func ListenTCP(network string, laddr *TCPAddr) (*TCPListener, error) {
	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, err
	}

	listener := &TCPListener{
		localAddr: laddr,
		connCh:    make(chan *TCPConn, 1024),
	}

	// 注册TCP监听器到全局网络系统
	key := fmt.Sprintf("tcp:%s:%d", laddr.IP.String(), laddr.Port)
	if err := RegisterTCPListener(key, listener); err != nil {
		return nil, err
	}

	return listener, nil
}

func (l *TCPListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, errors.New("listener closed")
	}
	l.mu.Unlock()

	// 从连接队列中获取新连接
	select {
	case conn, ok := <-l.connCh:
		if !ok {
			return nil, errors.New("listener closed")
		}
		if conn == nil {
			return nil, errors.New("received nil connection")
		}
		log.Printf("[DEBUG] Accepted new TCP connection: %s -> %s",
			conn.remoteAddr.String(), conn.localAddr.String())
		return conn, nil
	case <-time.After(30 * time.Second):
		return nil, errors.New("accept timeout")
	}
}

// AcceptTCP accepts the next incoming call and returns the new connection.
func (l *TCPListener) AcceptTCP() (*TCPConn, error) {
	conn, err := l.Accept()
	if err != nil {
		return nil, err
	}
	return conn.(*TCPConn), nil
}

func (l *TCPListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}

	// 从全局网络系统中注销监听器
	key := fmt.Sprintf("tcp:%s:%d", l.localAddr.IP.String(), l.localAddr.Port)
	UnregisterTCPListener(key)

	l.closed = true
	close(l.connCh)

	log.Printf("[DEBUG] TCP listener closed: %s", l.localAddr.String())
	return nil
}

// Addr returns the listener's network address.
func (l *TCPListener) Addr() net.Addr {
	return l.localAddr
}

func DialTCP(network string, laddr, raddr *TCPAddr) (*TCPConn, error) {
	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, err
	}

	localAddr := laddr
	if localAddr == nil {
		// 使用随机端口
		localAddr = &TCPAddr{
			IP:   net.IPv4(0, 0, 0, 0),                 // 让系统选择本地IP
			Port: int(time.Now().Unix()%10000 + 20000), // 随机端口 20000-29999
		}
	}

	conn := &TCPConn{
		localAddr:  localAddr,
		remoteAddr: raddr,
		state:      TCPStateSynSent,
		dataCh:     make(chan []byte, 1024),
		seqNum:     uint32(time.Now().Unix()), // 初始序列号
	}

	// 发送SYN包开始三次握手
	log.Printf("[DEBUG] Sending TCP SYN to %s", raddr.String())
	flags := uint8(TCPFlagSYN)
	err := SendTCPPacket(localAddr.IP, raddr.IP,
		uint16(localAddr.Port), uint16(raddr.Port),
		conn.seqNum, 0, flags, nil)

	if err != nil {
		log.Printf("[ERROR] Failed to send TCP SYN: %v", err)
		return nil, err
	}

	// 简化实现：直接设为已建立状态
	// 实际应该等待SYN+ACK响应，然后发送ACK完成握手
	time.Sleep(100 * time.Millisecond) // 模拟网络延迟
	conn.state = TCPStateEstablished
	conn.seqNum++

	log.Printf("[DEBUG] TCP connection established: %s -> %s",
		localAddr.String(), raddr.String())

	return conn, nil
}

func (c *TCPConn) Read(buf []byte) (int, error) {
	if c.closed {
		return 0, errors.New("connection closed")
	}

	select {
	case data := <-c.dataCh:
		n := copy(buf, data)
		return n, nil
	case <-time.After(30 * time.Second):
		return 0, errors.New("read timeout")
	}
}

func (c *TCPConn) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return 0, errors.New("connection closed")
	}

	if c.state != TCPStateEstablished {
		return 0, errors.New("connection not established")
	}

	// 使用全局网络系统发送TCP数据包
	flags := uint8(TCPFlagPSH | TCPFlagACK)
	err := SendTCPPacket(c.localAddr.IP, c.remoteAddr.IP,
		uint16(c.localAddr.Port), uint16(c.remoteAddr.Port),
		c.seqNum, c.ackNum, flags, data)

	if err != nil {
		log.Printf("[ERROR] Failed to send TCP data: %v", err)
		return 0, err
	}

	// 更新序列号
	c.seqNum += uint32(len(data))

	log.Printf("[DEBUG] TCP Write: %d bytes to %s", len(data), c.remoteAddr.String())
	return len(data), nil
}

func (c *TCPConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	c.state = TCPStateClosed
	close(c.dataCh)
	return nil
}

func (c *TCPConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *TCPConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// SetDeadline sets the read and write deadlines associated with the connection.
func (c *TCPConn) SetDeadline(t time.Time) error {
	// TODO: 实现超时逻辑
	return nil
}

// SetReadDeadline sets the deadline for future Read calls.
func (c *TCPConn) SetReadDeadline(t time.Time) error {
	// TODO: 实现读超时逻辑
	return nil
}

// SetWriteDeadline sets the deadline for future Write calls.
func (c *TCPConn) SetWriteDeadline(t time.Time) error {
	// TODO: 实现写超时逻辑
	return nil
}

// HandleTCPPacket 处理TCP数据包 (由network.go调用)
func HandleTCPPacket(data []byte, ipHeaderStart, headerLength int, srcIP, dstIP net.IP) {
	tcpStart := ipHeaderStart + headerLength
	if len(data) < tcpStart+20 {
		log.Printf("[DEBUG] TCP packet too short")
		return
	}

	// 解析TCP头
	srcPort := binary.BigEndian.Uint16(data[tcpStart : tcpStart+2])
	dstPort := binary.BigEndian.Uint16(data[tcpStart+2 : tcpStart+4])
	seqNum := binary.BigEndian.Uint32(data[tcpStart+4 : tcpStart+8])
	ackNum := binary.BigEndian.Uint32(data[tcpStart+8 : tcpStart+12])
	flags := data[tcpStart+13]

	log.Printf("[DEBUG] TCP: %s:%d -> %s:%d (seq=%d, ack=%d, flags=0x%02x)",
		srcIP.String(), srcPort, dstIP.String(), dstPort, seqNum, ackNum, flags)

	// 查找对应的TCP监听器
	key := fmt.Sprintf("tcp:%s:%d", dstIP.String(), dstPort)
	listener := findTCPListener(key)

	if listener == nil {
		// 尝试通配符匹配
		wildcardKey := fmt.Sprintf("tcp:0.0.0.0:%d", dstPort)
		listener = findTCPListener(wildcardKey)

		if listener == nil {
			log.Printf("[DEBUG] No TCP listener found for %s or %s", key, wildcardKey)
			return
		}
	}

	// 处理TCP状态机
	processTCPPacketForListener(listener, data, tcpStart, srcIP, srcPort, dstIP, dstPort, seqNum, ackNum, flags)
}

// findTCPListener 查找TCP监听器
func findTCPListener(key string) *TCPListener {
	// 调用network.go中的函数来查找监听器
	return FindTCPListenerByKey(key)
}

// processTCPPacketForListener 为指定的监听器处理TCP数据包
// 简易连接表（真实实现应加锁和超时清理）
var (
	tcpConnTable      = make(map[string]*TCPConn)
	tcpConnTableMutex sync.Mutex
	tcpConnLastActive = make(map[string]time.Time)
)

// 启动后台定时清理协程（只需启动一次）
func init() {
	go func() {
		for {
			time.Sleep(60 * time.Second)
			now := time.Now()
			tcpConnTableMutex.Lock()
			for key, conn := range tcpConnTable {
				last, ok := tcpConnLastActive[key]
				if !ok {
					last = now
				}
				// 10分钟无活跃自动清理
				if now.Sub(last) > 10*time.Minute {
					conn.closed = true
					close(conn.dataCh)
					delete(tcpConnTable, key)
					delete(tcpConnLastActive, key)
					log.Printf("[INFO] TCP connection %s idle timeout, removed", key)
				}
			}
			tcpConnTableMutex.Unlock()
		}
	}()
}

func processTCPPacketForListener(listener *TCPListener, data []byte, tcpStart int, srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, seqNum, ackNum uint32, flags uint8) {
	log.Printf("[DEBUG] Processing TCP packet for listener: flags=0x%02x", flags)

	connKey := fmt.Sprintf("%s:%d-%s:%d", srcIP.String(), srcPort, dstIP.String(), dstPort)
	now := time.Now()

	// 如果是SYN包，创建新连接
	if flags&TCPFlagSYN != 0 && flags&TCPFlagACK == 0 { // SYN 且没有 ACK
		log.Printf("[DEBUG] Received SYN from %s", connKey)

		// 检查连接是否已经存在，避免重复创建
		tcpConnTableMutex.Lock()
		existingConn, exists := tcpConnTable[connKey]
		if exists {
			// 连接已存在，更新活跃时间并重发SYN+ACK
			tcpConnLastActive[connKey] = now
			tcpConnTableMutex.Unlock()
			log.Printf("[DEBUG] Connection already exists, resending SYN+ACK")
			sendTCPSynAckForListener(srcIP, srcPort, dstIP, dstPort, seqNum+1, existingConn.seqNum)
			return
		}

		// 创建新连接
		conn := &TCPConn{
			localAddr: &TCPAddr{
				IP:   dstIP,
				Port: int(dstPort),
			},
			remoteAddr: &TCPAddr{
				IP:   srcIP,
				Port: int(srcPort),
			},
			dataCh: make(chan []byte, 1024),
			seqNum: 1000, // 初始序列号
			ackNum: seqNum + 1,
			state:  TCPStateSynReceived,
		}

		// 保存到连接表，但不放入accept队列
		tcpConnTable[connKey] = conn
		tcpConnLastActive[connKey] = now
		tcpConnTableMutex.Unlock()

		log.Printf("[DEBUG] Created new connection, sending SYN+ACK (our_seq=%d, ack=%d)", conn.seqNum, conn.ackNum)

		// 发送SYN+ACK响应
		sendTCPSynAckForListener(srcIP, srcPort, dstIP, dstPort, conn.ackNum, conn.seqNum)

		// 启动超时清理协程
		go func(key string, c *TCPConn) {
			select {
			case <-time.After(120 * time.Second):
				tcpConnTableMutex.Lock()
				if tcpConnTable[key] == c && c.state != TCPStateEstablished {
					log.Printf("[INFO] TCP connection %s timeout, removing", key)
					delete(tcpConnTable, key)
					delete(tcpConnLastActive, key)
				}
				tcpConnTableMutex.Unlock()
			case <-c.dataCh:
				// 有数据则不清理
			}
		}(connKey, conn)

		return
	}

	// 收到ACK包，完成三次握手，设为Established
	if flags&TCPFlagACK != 0 && flags&TCPFlagSYN == 0 {
		tcpConnTableMutex.Lock()
		conn, ok := tcpConnTable[connKey]
		if ok && conn.state == TCPStateSynReceived {
			// 验证ACK号是否正确 (应该是我们的seq+1)
			expectedAck := conn.seqNum + 1
			if ackNum == expectedAck {
				conn.state = TCPStateEstablished
				conn.seqNum++ // 增加我们的序列号
				log.Printf("[DEBUG] TCP connection established for %s (ack=%d, expected=%d)", connKey, ackNum, expectedAck)

				// 三次握手完成，现在将连接放入accept队列
				select {
				case listener.connCh <- conn:
					log.Printf("[DEBUG] TCP connection queued for accept after handshake")
				default:
					log.Printf("[WARNING] TCP accept queue full after handshake")
				}
			} else {
				log.Printf("[WARNING] Invalid ACK number: got %d, expected %d", ackNum, expectedAck)
			}
		}
		// 活跃时间更新
		if ok {
			tcpConnLastActive[connKey] = now
		}
		tcpConnTableMutex.Unlock()

		// 解析数据部分
		tcpHeaderLen := int((data[tcpStart+12] >> 4) * 4)
		dataStart := tcpStart + tcpHeaderLen
		payloadLen := len(data) - dataStart

		if ok && payloadLen > 0 {
			log.Printf("[DEBUG] Received %d bytes of TCP data", payloadLen)
			// 投递数据到连接
			conn.dataCh <- data[dataStart:]
			// 发送ACK确认
			sendTCPAck(srcIP, srcPort, dstIP, dstPort, ackNum, seqNum+uint32(payloadLen))
			// 活跃时间更新
			tcpConnTableMutex.Lock()
			tcpConnLastActive[connKey] = now
			tcpConnTableMutex.Unlock()
		}
	}

	// FIN包关闭连接
	if flags&TCPFlagFIN != 0 {
		tcpConnTableMutex.Lock()
		conn, ok := tcpConnTable[connKey]
		if ok {
			conn.closed = true
			close(conn.dataCh)
			delete(tcpConnTable, connKey)
			delete(tcpConnLastActive, connKey)
			log.Printf("[INFO] TCP connection %s closed and removed", connKey)
		}
		tcpConnTableMutex.Unlock()
	}
}

// sendTCPAck 发送TCP ACK确认
func sendTCPAck(dstIP net.IP, dstPort uint16, srcIP net.IP, srcPort uint16, seqNum, ackNum uint32) {
	flags := uint8(TCPFlagACK)
	err := SendTCPPacket(srcIP, dstIP, srcPort, dstPort, seqNum, ackNum, flags, nil)
	if err != nil {
		log.Printf("[ERROR] Failed to send TCP ACK: %v", err)
	} else {
		log.Printf("[DEBUG] TCP ACK sent: seq=%d, ack=%d", seqNum, ackNum)
	}
}

// sendTCPSynAckForListener 发送TCP SYN+ACK响应
func sendTCPSynAckForListener(dstIP net.IP, dstPort uint16, srcIP net.IP, srcPort uint16, ackNum, seqNum uint32) {
	log.Printf("[DEBUG] Sending TCP SYN+ACK reply from %s:%d to %s:%d (seq=%d, ack=%d)",
		srcIP.String(), srcPort, dstIP.String(), dstPort, seqNum, ackNum)

	// 使用全局网络系统发送SYN+ACK包
	flags := uint8(TCPFlagSYN | TCPFlagACK)
	err := SendTCPPacket(srcIP, dstIP, srcPort, dstPort, seqNum, ackNum, flags, nil)
	if err != nil {
		log.Printf("[ERROR] Failed to send TCP SYN+ACK: %v", err)
	} else {
		log.Printf("[DEBUG] TCP SYN+ACK sent successfully (seq=%d, ack=%d)", seqNum, ackNum)
	}
}
