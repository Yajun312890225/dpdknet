package dpdknet

import (
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
