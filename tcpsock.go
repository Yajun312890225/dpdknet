package dpdknet

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type TCPConn struct {
	localAddr  *TCPAddr
	remoteAddr *TCPAddr
	mu         sync.Mutex
	closed     bool

	// gVisor 集成
	gvisorConn net.Conn // gVisor TCP 连接

	// VXLAN 选项
	vxlanConfig  *VXLANConfig // 如果不为 nil，则启用 VXLAN 封装
	vxlanHandler *VXLANHandler
}

type TCPListener struct {
	localAddr *TCPAddr
	mu        sync.Mutex
	closed    bool

	// gVisor 集成
	gvisorListener net.Listener // gVisor TCP 监听器

	// VXLAN 选项
	vxlanConfig  *VXLANConfig // 如果不为 nil，则启用 VXLAN 封装
	vxlanHandler *VXLANHandler
}

func ListenTCP(network string, laddr *TCPAddr) (*TCPListener, error) {
	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, err
	}

	listener := &TCPListener{
		localAddr: laddr,
	}

	// 强制使用 gVisor netstack
	log.Printf("[DEBUG] Creating TCP listener using gVisor netstack")
	gvisorListener, err := CreateGVisorTCPListener(uint16(laddr.Port))
	if err != nil {
		log.Printf("[ERROR] Failed to create gVisor TCP listener: %v", err)
		return nil, fmt.Errorf("failed to create gVisor TCP listener: %v", err)
	}

	listener.gvisorListener = gvisorListener
	log.Printf("[INFO] TCP listener created using gVisor netstack on port %d", laddr.Port)
	return listener, nil
}

// ListenTCPWithVXLAN 创建带 VXLAN 封装的 TCP 监听器
func ListenTCPWithVXLAN(network string, laddr *TCPAddr, vxlanConfig *VXLANConfig) (*TCPListener, error) {
	listener, err := ListenTCP(network, laddr)
	if err != nil {
		return nil, err
	}

	if vxlanConfig != nil {
		listener.vxlanConfig = vxlanConfig
		listener.vxlanHandler = NewVXLANHandler(vxlanConfig)

		// 在全局网络栈中注册这个 VXLAN 连接
		if globalGVisorStack != nil {
			globalGVisorStack.RegisterVXLANConnection(
				net.ParseIP("0.0.0.0"), // 监听所有接口
				uint16(laddr.Port),
				vxlanConfig,
			)
		}

		log.Printf("[INFO] TCP listener enabled VXLAN encapsulation with VNI %d", vxlanConfig.VNI)
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

	if l.gvisorListener == nil {
		return nil, errors.New("gVisor listener not available")
	}

	// 从 gVisor 监听器接受连接
	gvisorConn, err := l.gvisorListener.Accept()
	if err != nil {
		return nil, err
	}

	// 包装为 TCPConn
	tcpConn := &TCPConn{
		localAddr:    l.localAddr,
		gvisorConn:   gvisorConn,
		vxlanConfig:  l.vxlanConfig,  // 传递 VXLAN 配置
		vxlanHandler: l.vxlanHandler, // 传递 VXLAN 处理器
	}

	// 尝试获取远程地址
	if remoteAddr := gvisorConn.RemoteAddr(); remoteAddr != nil {
		if tcpAddr, ok := remoteAddr.(*net.TCPAddr); ok {
			tcpConn.remoteAddr = &TCPAddr{
				IP:   tcpAddr.IP,
				Port: tcpAddr.Port,
				Zone: tcpAddr.Zone,
			}
		}
	}

	log.Printf("[DEBUG] Accepted new TCP connection: %s -> %s",
		tcpConn.RemoteAddr().String(), tcpConn.LocalAddr().String())
	return tcpConn, nil
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

	l.closed = true

	// 强制使用 gVisor，关闭 gVisor 监听器
	if l.gvisorListener != nil {
		err := l.gvisorListener.Close()
		log.Printf("[DEBUG] gVisor TCP listener closed")
		return err
	}

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

	if raddr == nil {
		return nil, fmt.Errorf("remote address cannot be nil")
	}

	// 转换为标准net.TCPAddr
	var localAddr *net.TCPAddr
	if laddr != nil {
		localAddr = &net.TCPAddr{
			IP:   laddr.IP,
			Port: laddr.Port,
			Zone: laddr.Zone,
		}
	}

	remoteAddr := &net.TCPAddr{
		IP:   raddr.IP,
		Port: raddr.Port,
		Zone: raddr.Zone,
	}

	// 通过 gVisor netstack 创建TCP连接
	gvisorConn, err := CreateGVisorTCPConn(localAddr, remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create TCP connection: %v", err)
	}

	// 创建TCPConn包装器
	tcpConn := &TCPConn{
		localAddr:  laddr,
		remoteAddr: raddr,
		gvisorConn: gvisorConn,
	}

	// 如果本地地址为空，尝试从连接中获取
	if tcpConn.localAddr == nil {
		if localAddrFromConn := gvisorConn.LocalAddr(); localAddrFromConn != nil {
			if tcpAddr, ok := localAddrFromConn.(*net.TCPAddr); ok {
				tcpConn.localAddr = &TCPAddr{
					IP:   tcpAddr.IP,
					Port: tcpAddr.Port,
					Zone: tcpAddr.Zone,
				}
			}
		}
	}

	return tcpConn, nil
}

// DialTCPWithVXLAN 创建带 VXLAN 封装的 TCP 连接
func DialTCPWithVXLAN(network string, laddr, raddr *TCPAddr, vxlanConfig *VXLANConfig) (*TCPConn, error) {
	conn, err := DialTCP(network, laddr, raddr)
	if err != nil {
		return nil, err
	}

	if vxlanConfig != nil {
		conn.vxlanConfig = vxlanConfig
		conn.vxlanHandler = NewVXLANHandler(vxlanConfig)
		log.Printf("[INFO] TCP connection enabled VXLAN encapsulation with VNI %d", vxlanConfig.VNI)
	}

	return conn, nil
}

func (c *TCPConn) Read(buf []byte) (int, error) {
	if c.closed {
		return 0, errors.New("connection closed")
	}

	if c.gvisorConn == nil {
		return 0, errors.New("gVisor connection not available")
	}

	return c.gvisorConn.Read(buf)
}

func (c *TCPConn) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return 0, errors.New("connection closed")
	}

	if c.gvisorConn == nil {
		return 0, errors.New("gVisor connection not available")
	}

	return c.gvisorConn.Write(data)
}

func (c *TCPConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	// 强制使用 gVisor，关闭 gVisor 连接
	if c.gvisorConn != nil {
		err := c.gvisorConn.Close()
		log.Printf("[DEBUG] gVisor TCP connection closed")
		return err
	}

	return nil
}

func (c *TCPConn) LocalAddr() net.Addr {
	if c.gvisorConn != nil {
		return c.gvisorConn.LocalAddr()
	}
	return c.localAddr
}

func (c *TCPConn) RemoteAddr() net.Addr {
	if c.gvisorConn != nil {
		return c.gvisorConn.RemoteAddr()
	}
	return c.remoteAddr
}

// SetDeadline sets the read and write deadlines associated with the connection.
func (c *TCPConn) SetDeadline(t time.Time) error {
	if c.gvisorConn != nil {
		return c.gvisorConn.SetDeadline(t)
	}
	return nil
}

// SetReadDeadline sets the deadline for future Read calls.
func (c *TCPConn) SetReadDeadline(t time.Time) error {
	if c.gvisorConn != nil {
		return c.gvisorConn.SetReadDeadline(t)
	}
	return nil
}

// SetWriteDeadline sets the deadline for future Write calls.
func (c *TCPConn) SetWriteDeadline(t time.Time) error {
	if c.gvisorConn != nil {
		return c.gvisorConn.SetWriteDeadline(t)
	}
	return nil
}
