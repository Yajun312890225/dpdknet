package dpdknet

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

type UDPConn struct {
	localAddr  *UDPAddr
	remoteAddr *UDPAddr
	mu         sync.Mutex
	closed     bool

	// gVisor 集成
	gvisorConn net.PacketConn // gVisor UDP 连接

	// VXLAN 选项
	vxlanConfig  *VXLANConfig // 如果不为 nil，则启用 VXLAN 封装
	vxlanHandler *VXLANHandler
}

func ListenUDP(network string, laddr *UDPAddr) (*UDPConn, error) {
	// 确保全局DPDK系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		log.Printf("[ERROR] Global network init failed: %v", err)
		return nil, err
	}

	// 如果 laddr 为 nil，创建默认地址
	if laddr == nil {
		laddr = &UDPAddr{
			IP:   net.IPv4zero,
			Port: 0, // 让系统自动分配端口
		}
	}

	c := &UDPConn{
		localAddr: laddr,
	}

	// 强制使用 gVisor netstack
	gvisorConn, err := CreateGVisorUDPConn(uint16(laddr.Port))
	if err != nil {
		log.Printf("[ERROR] Failed to create gVisor UDP connection: %v", err)
		return nil, fmt.Errorf("failed to create gVisor UDP connection: %v", err)
	}

	c.gvisorConn = gvisorConn

	return c, nil
}

// ListenUDPWithVXLAN 创建带 VXLAN 封装的 UDP 连接
func ListenUDPWithVXLAN(network string, laddr *UDPAddr, vxlanConfig *VXLANConfig) (*UDPConn, error) {
	conn, err := ListenUDP(network, laddr)
	if err != nil {
		return nil, err
	}

	if vxlanConfig != nil {
		conn.vxlanConfig = vxlanConfig
		conn.vxlanHandler = NewVXLANHandler(vxlanConfig)

		// 在全局网络栈中注册这个 VXLAN 连接
		if globalGVisorStack != nil {
			globalGVisorStack.RegisterVXLANConnection(
				net.ParseIP("0.0.0.0"), // 监听所有接口
				uint16(laddr.Port),
				vxlanConfig,
			)
		}

		log.Printf("[INFO] UDP connection enabled VXLAN encapsulation with VNI %d", vxlanConfig.VNI)
	}

	return conn, nil
}

func (c *UDPConn) ReadFromUDP(buf []byte) (int, *UDPAddr, error) {
	if c.closed {
		return 0, nil, errors.New("connection closed")
	}

	if c.gvisorConn == nil {
		return 0, nil, errors.New("gVisor connection not available")
	}

	n, addr, err := c.gvisorConn.ReadFrom(buf)
	if err != nil {
		return 0, nil, err
	}

	// 转换地址格式
	udpAddr := &UDPAddr{}
	if netAddr, ok := addr.(*net.UDPAddr); ok {
		udpAddr.IP = netAddr.IP
		udpAddr.Port = netAddr.Port
		udpAddr.Zone = netAddr.Zone
	}

	return n, udpAddr, nil
}

// ReadFrom reads a packet from the connection, copying the payload into buf.
// It implements the PacketConn ReadFrom method.
func (c *UDPConn) ReadFrom(buf []byte) (int, net.Addr, error) {
	n, addr, err := c.ReadFromUDP(buf)
	return n, addr, err
}

// Read reads data from the connection.
func (c *UDPConn) Read(buf []byte) (int, error) {
	n, _, err := c.ReadFromUDP(buf)
	return n, err
}

// WriteTo writes a packet with payload buf to addr.
func (c *UDPConn) WriteTo(buf []byte, addr net.Addr) (int, error) {
	udpAddr, ok := addr.(*UDPAddr)
	if !ok {
		return 0, errors.New("invalid address type")
	}
	return c.WriteToUDP(buf, udpAddr)
}

// WriteToUDP writes a UDP packet to addr.
func (c *UDPConn) WriteToUDP(buf []byte, addr *UDPAddr) (int, error) {
	if c.closed {
		return 0, errors.New("connection closed")
	}

	if addr == nil || addr.IP == nil {
		return 0, errors.New("invalid destination address")
	}

	if c.gvisorConn == nil {
		return 0, errors.New("gVisor connection not available")
	}

	netAddr := &net.UDPAddr{
		IP:   addr.IP,
		Port: addr.Port,
		Zone: addr.Zone,
	}
	return c.gvisorConn.WriteTo(buf, netAddr)
}

// Write writes data to the connection.
func (c *UDPConn) Write(buf []byte) (int, error) {
	if c.remoteAddr == nil {
		return 0, errors.New("write to unconnected UDP connection")
	}
	return c.WriteToUDP(buf, c.remoteAddr)
}

// LocalAddr returns the local network address.
func (c *UDPConn) LocalAddr() net.Addr {
	return c.localAddr
}

// RemoteAddr returns the remote network address.
func (c *UDPConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *UDPConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true

	// 强制使用 gVisor，关闭 gVisor 连接
	if c.gvisorConn != nil {
		return c.gvisorConn.Close()
	}

	return nil
}

// SetDeadline sets the read and write deadlines associated with the connection.
func (c *UDPConn) SetDeadline(t time.Time) error {
	if c.gvisorConn != nil {
		// 尝试设置gVisor连接的deadline
		if conn, ok := c.gvisorConn.(interface{ SetDeadline(time.Time) error }); ok {
			return conn.SetDeadline(t)
		}
	}
	return nil
}

// SetReadDeadline sets the deadline for future Read calls.
func (c *UDPConn) SetReadDeadline(t time.Time) error {
	if c.gvisorConn != nil {
		// 尝试设置gVisor连接的read deadline
		if conn, ok := c.gvisorConn.(interface{ SetReadDeadline(time.Time) error }); ok {
			return conn.SetReadDeadline(t)
		}
	}
	return nil
}

// SetWriteDeadline sets the deadline for future Write calls.
func (c *UDPConn) SetWriteDeadline(t time.Time) error {
	if c.gvisorConn != nil {
		// 尝试设置gVisor连接的write deadline
		if conn, ok := c.gvisorConn.(interface{ SetWriteDeadline(time.Time) error }); ok {
			return conn.SetWriteDeadline(t)
		}
	}
	return nil
}

// DialUDPWithVXLAN 创建带 VXLAN 封装的 UDP 连接
func DialUDPWithVXLAN(network string, laddr, raddr *UDPAddr, vxlanConfig *VXLANConfig) (*UDPConn, error) {
	if vxlanConfig == nil {
		// 如果没有 VXLAN 配置，回退到普通 UDP
		return DialUDP(network, laddr, raddr)
	}

	// VXLAN 场景：raddr 是内层目标地址，实际的外层通信在 VTEP 之间进行
	// 1. 创建本地 UDP 监听器，绑定到 VXLAN 本地地址
	vxlanLocalAddr := &UDPAddr{
		IP:   vxlanConfig.LocalIP, // 使用 VXLAN 本地 VTEP IP
		Port: 0,                   // 让系统分配端口
	}

	// 如果用户指定了本地地址，使用用户指定的端口
	if laddr != nil && laddr.Port != 0 {
		vxlanLocalAddr.Port = laddr.Port
	}

	// 2. 创建 UDP 连接
	conn, err := ListenUDP(network, vxlanLocalAddr)
	if err != nil {
		return nil, err
	}

	// 3. 设置内层远程地址（这是 VXLAN 隧道内部的目标）
	conn.remoteAddr = raddr

	// 4. 配置 VXLAN
	conn.vxlanConfig = vxlanConfig
	conn.vxlanHandler = NewVXLANHandler(vxlanConfig)

	// 5. 注册 VXLAN 连接到全局网络栈
	if globalGVisorStack != nil {
		// 注册方式1：按本地 VTEP 地址和端口注册
		globalGVisorStack.RegisterVXLANConnection(
			vxlanConfig.LocalIP,
			uint16(vxlanLocalAddr.Port),
			vxlanConfig,
		)

		// 注册方式2：按内层目标地址和端口注册（这样可以捕获到发往内层目标的数据包）
		globalGVisorStack.RegisterVXLANConnection(
			raddr.IP,
			uint16(raddr.Port),
			vxlanConfig,
		)

		log.Printf("[INFO] VXLAN UDP connection: VNI %d, %s -> %s",
			vxlanConfig.VNI, vxlanConfig.LocalIP, vxlanConfig.RemoteIP)
	}

	return conn, nil
}
