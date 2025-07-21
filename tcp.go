package dpdknet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Yajun312890225/nff-go/flow"
	"github.com/Yajun312890225/nff-go/packet"
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
	localAddr  *net.TCPAddr
	remoteAddr *net.TCPAddr
	state      TCPState
	seqNum     uint32
	ackNum     uint32
	rxFlow     *flow.Flow
	dataCh     chan []byte
	mu         sync.Mutex
	closed     bool
}

type TCPListener struct {
	localAddr *net.TCPAddr
	rxFlow    *flow.Flow
	connCh    chan *TCPConn
	mu        sync.Mutex
	closed    bool
}

// HandleTCPManual 手动处理TCP包
func HandleTCPManual(pkt *packet.Packet, data []byte, ipHeaderStart, headerLength int) {
	tcpStart := ipHeaderStart + headerLength

	if len(data) < tcpStart+20 {
		return
	}

	// 解析TCP头
	srcPort := binary.BigEndian.Uint16(data[tcpStart : tcpStart+2])
	dstPort := binary.BigEndian.Uint16(data[tcpStart+2 : tcpStart+4])
	seqNum := binary.BigEndian.Uint32(data[tcpStart+4 : tcpStart+8])
	ackNum := binary.BigEndian.Uint32(data[tcpStart+8 : tcpStart+12])
	flags := data[tcpStart+13]

	// 解析IP地址
	srcIP := fmt.Sprintf("%d.%d.%d.%d",
		data[ipHeaderStart+12], data[ipHeaderStart+13],
		data[ipHeaderStart+14], data[ipHeaderStart+15])
	dstIP := fmt.Sprintf("%d.%d.%d.%d",
		data[ipHeaderStart+16], data[ipHeaderStart+17],
		data[ipHeaderStart+18], data[ipHeaderStart+19])

	log.Printf("TCP: %s:%d -> %s:%d (seq=%d, ack=%d, flags=0x%02x)",
		srcIP, srcPort, dstIP, dstPort, seqNum, ackNum, flags)

	// 简单的TCP echo服务器实现
	// 只处理目标端口为8080的TCP包
	if dstPort != 8080 {
		return
	}

	// 如果是SYN包，发送SYN+ACK
	if flags&TCPFlagSYN != 0 && flags&TCPFlagACK == 0 {
		sendTCPSynAck(data, ipHeaderStart, headerLength, tcpStart, srcPort, dstPort, seqNum)
		return
	}

	// 如果是ACK包，建立连接
	if flags&TCPFlagACK != 0 && flags&TCPFlagSYN == 0 {
		// 连接已建立，可以处理数据
		tcpHeaderLen := int((data[tcpStart+12] >> 4) * 4)
		dataStart := tcpStart + tcpHeaderLen
		dataLen := len(data) - dataStart

		if dataLen > 0 {
			// 有数据，回显数据
			echoTCPData(data, ipHeaderStart, headerLength, tcpStart, srcPort, dstPort, seqNum, ackNum, dataLen)
		}
	}
}

func sendTCPSynAck(data []byte, ipHeaderStart, headerLength, tcpStart int, srcPort, dstPort uint16, clientSeq uint32) {
	// 交换源和目标IP地址
	for i := 0; i < 4; i++ {
		data[ipHeaderStart+12+i], data[ipHeaderStart+16+i] = data[ipHeaderStart+16+i], data[ipHeaderStart+12+i]
	}

	// 交换源和目标端口
	binary.BigEndian.PutUint16(data[tcpStart:tcpStart+2], dstPort)
	binary.BigEndian.PutUint16(data[tcpStart+2:tcpStart+4], srcPort)

	// 设置序列号和确认号
	serverSeq := uint32(time.Now().Unix())
	binary.BigEndian.PutUint32(data[tcpStart+4:tcpStart+8], serverSeq)    // 服务器序列号
	binary.BigEndian.PutUint32(data[tcpStart+8:tcpStart+12], clientSeq+1) // 确认号

	// 设置标志位：SYN + ACK
	data[tcpStart+13] = TCPFlagSYN | TCPFlagACK

	// 设置窗口大小
	binary.BigEndian.PutUint16(data[tcpStart+14:tcpStart+16], 65535)

	// 重新计算TCP校验和 (简化处理，设为0)
	binary.BigEndian.PutUint16(data[tcpStart+16:tcpStart+18], 0)

	// 重新计算IPv4头校验和
	data[ipHeaderStart+10] = 0
	data[ipHeaderStart+11] = 0
	ipChecksum := CalcChecksum(data[ipHeaderStart : ipHeaderStart+headerLength])
	binary.BigEndian.PutUint16(data[ipHeaderStart+10:ipHeaderStart+12], ipChecksum)

	log.Printf("Sent TCP SYN+ACK: seq=%d, ack=%d", serverSeq, clientSeq+1)
}

func echoTCPData(data []byte, ipHeaderStart, headerLength, tcpStart int, srcPort, dstPort uint16, clientSeq, clientAck uint32, dataLen int) {
	// 交换源和目标IP地址
	for i := 0; i < 4; i++ {
		data[ipHeaderStart+12+i], data[ipHeaderStart+16+i] = data[ipHeaderStart+16+i], data[ipHeaderStart+12+i]
	}

	// 交换源和目标端口
	binary.BigEndian.PutUint16(data[tcpStart:tcpStart+2], dstPort)
	binary.BigEndian.PutUint16(data[tcpStart+2:tcpStart+4], srcPort)

	// 设置序列号和确认号
	binary.BigEndian.PutUint32(data[tcpStart+4:tcpStart+8], clientAck)                  // 使用客户端的ACK作为我们的SEQ
	binary.BigEndian.PutUint32(data[tcpStart+8:tcpStart+12], clientSeq+uint32(dataLen)) // 确认收到的数据

	// 设置标志位：PSH + ACK
	data[tcpStart+13] = TCPFlagPSH | TCPFlagACK

	// 重新计算TCP校验和 (简化处理，设为0)
	binary.BigEndian.PutUint16(data[tcpStart+16:tcpStart+18], 0)

	// 重新计算IPv4头校验和
	data[ipHeaderStart+10] = 0
	data[ipHeaderStart+11] = 0
	ipChecksum := CalcChecksum(data[ipHeaderStart : ipHeaderStart+headerLength])
	binary.BigEndian.PutUint16(data[ipHeaderStart+10:ipHeaderStart+12], ipChecksum)

	log.Printf("Echoed TCP data: %d bytes", dataLen)
}

func ListenTCP(addr *net.TCPAddr) (*TCPListener, error) {
	if err := Init(); err != nil {
		return nil, err
	}

	rxFlow, err := flow.SetReceiver(0)
	if err != nil {
		return nil, err
	}

	listener := &TCPListener{
		localAddr: addr,
		rxFlow:    rxFlow,
		connCh:    make(chan *TCPConn, 1024),
	}

	// 设置TCP包处理器
	flow.SetHandler(rxFlow, listener.handleTCPPacket, nil)

	return listener, nil
}

func (l *TCPListener) handleTCPPacket(pkt *packet.Packet, ctx flow.UserContext) {
	data := pkt.GetRawPacketBytes()
	if len(data) < 14 {
		return
	}

	// 检查是否为IPv4
	etherType := binary.BigEndian.Uint16(data[12:14])
	if etherType != 0x0800 {
		return
	}

	// 检查是否为TCP
	ipHeaderStart := 14
	if len(data) < ipHeaderStart+20 {
		return
	}

	protocol := data[ipHeaderStart+9]
	if protocol != 6 { // TCP协议
		return
	}

	headerLength := int((data[ipHeaderStart] & 0x0F) * 4)
	HandleTCPManual(pkt, data, ipHeaderStart, headerLength)
}

func (l *TCPListener) Accept() (*TCPConn, error) {
	if l.closed {
		return nil, errors.New("listener closed")
	}

	// 在实际实现中，这里应该从连接队列中获取新连接
	// 现在简化为创建一个新连接
	conn := &TCPConn{
		localAddr:  l.localAddr,
		remoteAddr: &net.TCPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 0},
		state:      TCPStateEstablished,
		dataCh:     make(chan []byte, 1024),
	}

	return conn, nil
}

func (l *TCPListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	close(l.connCh)
	return nil
}

func DialTCP(raddr *net.TCPAddr) (*TCPConn, error) {
	if err := Init(); err != nil {
		return nil, err
	}

	conn := &TCPConn{
		localAddr:  &net.TCPAddr{IP: net.IPv4(192, 168, 1, 100), Port: 0}, // 临时本地地址
		remoteAddr: raddr,
		state:      TCPStateSynSent,
		dataCh:     make(chan []byte, 1024),
	}

	// 这里应该发送SYN包并等待SYN+ACK
	// 简化实现，直接设为已建立状态
	conn.state = TCPStateEstablished

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
	if c.closed {
		return 0, errors.New("connection closed")
	}

	// 这里应该构造TCP数据包并发送
	// 简化实现，直接返回成功
	log.Printf("TCP Write: %d bytes to %s", len(data), c.remoteAddr.String())
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
