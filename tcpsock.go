package dpdknet

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type TCPConn struct {
	localAddr  *TCPAddr
	remoteAddr *TCPAddr
	mu         sync.RWMutex // 使用读写锁
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
	gvisorListener, err := CreateGVisorTCPListener(uint16(laddr.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to create gVisor TCP listener: %v", err)
	}

	listener.gvisorListener = gvisorListener
	return listener, nil
}

// ListenTCPWithVXLAN 创建带 VXLAN 封装的 TCP 监听器
func ListenTCPWithVXLAN(network string, laddr *TCPAddr) (*TCPListener, error) {
	listener, err := ListenTCP(network, laddr)
	if err != nil {
		return nil, err
	}

	listener.vxlanConfig = GetGlobalVXLANConfig()
	listener.vxlanHandler = GetGlobalVXLANHandler()

	// 在全局网络栈中注册这个 VXLAN 连接
	if globalGVisorStack != nil {
		globalGVisorStack.RegisterVXLANConnection(
			net.ParseIP("0.0.0.0"), // 监听所有接口
			uint16(laddr.Port),
		)
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
		vxlanConfig:  l.vxlanConfig,
		vxlanHandler: l.vxlanHandler,
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
		return err
	}

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

	// 创建 TCP 连接
	gvisorConn, err := CreateGVisorTCPConnWithTimeout(localAddr, remoteAddr, 5*time.Second)
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
// 如果 vxlanConfig 为 nil，则使用全局VXLAN配置
func DialTCPWithVXLAN(network string, laddr, raddr *TCPAddr) (*TCPConn, error) {
	vxlanConfig := GetGlobalVXLANConfig()

	// VXLAN 场景：raddr 是内层目标地址，实际的外层通信在 VTEP 之间进行
	vxlanLocalAddr := &TCPAddr{
		IP:   vxlanConfig.LocalIP,
		Port: 0,
	}

	// 如果用户指定了本地地址，使用用户指定的端口
	if laddr != nil && laddr.Port != 0 {
		vxlanLocalAddr.Port = laddr.Port
	}

	var gvisorConn net.Conn
	var actualLocalAddr *TCPAddr
	var err error

	// 预先注册 VXLAN 配置，使用端口 0 作为通配符
	if globalGVisorStack != nil {
		if laddr != nil {
			globalGVisorStack.RegisterVXLANConnection(laddr.IP, 0)
		} else {
			globalGVisorStack.RegisterVXLANConnection(vxlanConfig.LocalIP, 0)
		}
	}

	if laddr != nil {
		// 如果指定了内层本地地址，使用它创建 gVisor 连接
		remoteAddr := &net.TCPAddr{
			IP:   raddr.IP,
			Port: raddr.Port,
			Zone: raddr.Zone,
		}
		gvisorConn, err = CreateGVisorTCPConnWithLocalAddr(laddr.IP, uint16(laddr.Port), remoteAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to create gVisor TCP connection with local addr %s:%d: %v", laddr.IP, laddr.Port, err)
		}
		actualLocalAddr = laddr
	} else {
		// 否则使用外层 VTEP 地址
		remoteAddr := &net.TCPAddr{
			IP:   raddr.IP,
			Port: raddr.Port,
			Zone: raddr.Zone,
		}
		gvisorConn, err = CreateGVisorTCPConn(nil, remoteAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to create gVisor TCP connection: %v", err)
		}
		actualLocalAddr = vxlanLocalAddr
	}

	conn := &TCPConn{
		localAddr:  actualLocalAddr,
		remoteAddr: raddr,
		gvisorConn: gvisorConn,
	}

	// 配置 VXLAN
	conn.vxlanConfig = vxlanConfig
	if globalHandler := GetGlobalVXLANHandler(); globalHandler != nil {
		conn.vxlanHandler = globalHandler
	} else {
		conn.vxlanHandler = NewVXLANHandler(vxlanConfig)
	}

	// 注册 VXLAN 连接（更新为实际端口）
	if globalGVisorStack != nil && actualLocalAddr != nil {
		localPort := uint16(actualLocalAddr.Port)
		if localPort != 0 {
			globalGVisorStack.RegisterVXLANConnection(
				actualLocalAddr.IP,
				localPort,
			)
		}
	}

	return conn, nil
}

func (c *TCPConn) Read(buf []byte) (int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return 0, errors.New("connection closed")
	}

	if c.gvisorConn == nil {
		return 0, errors.New("gVisor connection not available")
	}

	n, err := c.gvisorConn.Read(buf)
	return n, err
}

func (c *TCPConn) Write(data []byte) (int, error) {
	c.mu.RLock()
	defer func() {
		c.mu.RUnlock()
	}()

	if c.closed {
		return 0, errors.New("connection closed")
	}

	if c.gvisorConn == nil {
		return 0, errors.New("gVisor connection not available")
	}

	n, err := c.gvisorConn.Write(data)
	return n, err
}

func (c *TCPConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true

	if c.gvisorConn != nil {
		err := c.gvisorConn.Close()
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
