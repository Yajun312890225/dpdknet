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
	case conn := <-l.connCh:
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
func processTCPPacketForListener(listener *TCPListener, data []byte, tcpStart int, srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, seqNum, ackNum uint32, flags uint8) {
	log.Printf("[DEBUG] Processing TCP packet for listener: flags=0x%02x", flags)

	// 如果是SYN包，创建新连接
	if flags&TCPFlagSYN != 0 && flags&TCPFlagACK == 0 { // SYN 且没有 ACK
		log.Printf("[DEBUG] Received SYN, creating new connection")

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

		// 发送SYN+ACK响应
		sendTCPSynAckForListener(srcIP, srcPort, dstIP, dstPort, seqNum+1, conn.seqNum)

		// 将连接放入accept队列
		select {
		case listener.connCh <- conn:
			log.Printf("[DEBUG] New TCP connection queued for accept")
		default:
			log.Printf("[WARNING] TCP accept queue full")
		}
		return
	}

	// 处理已建立连接的数据包
	// 查找现有连接 (简化实现，实际应该维护连接映射表)
	if flags&TCPFlagACK != 0 {
		// 解析数据部分
		tcpHeaderLen := int((data[tcpStart+12] >> 4) * 4)
		dataStart := tcpStart + tcpHeaderLen
		payloadLen := len(data) - dataStart

		if payloadLen > 0 {
			log.Printf("[DEBUG] Received %d bytes of TCP data", payloadLen)
			// 这里应该将数据传递给对应的连接
			// 简化处理，暂时只记录日志

			// 发送ACK确认
			sendTCPAck(srcIP, srcPort, dstIP, dstPort, ackNum, seqNum+uint32(payloadLen))
		}
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
	log.Printf("[DEBUG] Sending TCP SYN+ACK reply from %s:%d to %s:%d", srcIP.String(), srcPort, dstIP.String(), dstPort)

	// 使用全局网络系统发送SYN+ACK包
	flags := uint8(TCPFlagSYN | TCPFlagACK)
	err := SendTCPPacket(srcIP, dstIP, srcPort, dstPort, seqNum, ackNum, flags, nil)
	if err != nil {
		log.Printf("[ERROR] Failed to send TCP SYN+ACK: %v", err)
	} else {
		log.Printf("[DEBUG] TCP SYN+ACK sent successfully")
	}
}
