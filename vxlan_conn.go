package dpdknet

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// VXLANConn VXLAN 连接，基于 DPDK 数据路径
type VXLANConn struct {
	localAddr  *VXLANAddr
	remoteAddr *VXLANAddr
	handler    *VXLANHandler
	readChan   chan []byte // 接收数据通道
	writeChan  chan []byte // 发送数据通道
	closed     bool
	mu         sync.RWMutex
}

// VXLANAddr VXLAN 地址 - 表示内层虚拟网络地址
type VXLANAddr struct {
	IP   net.IP // 内层虚拟 IP（应用层地址）
	Port int    // 内层端口（应用层端口）
	VNI  uint32 // VXLAN Network Identifier
}

func (va *VXLANAddr) Network() string {
	return "vxlan"
}

func (va *VXLANAddr) String() string {
	return fmt.Sprintf("%s:%d/vni:%d", va.IP.String(), va.Port, va.VNI)
}

// ParseVXLANAddr 解析 VXLAN 地址
// 格式: "ip:port/vni:1000" 或 "ip:port" (使用默认 VNI 1000)
func ParseVXLANAddr(addr string) (*VXLANAddr, error) {
	// 简化解析实现
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid VXLAN address format: %v", err)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", host)
	}

	// 解析端口
	portNum := 4789 // 默认 VXLAN 端口
	if port != "" {
		if p, err := net.LookupPort("udp", port); err == nil {
			portNum = p
		}
	}

	return &VXLANAddr{
		IP:   ip,
		Port: portNum,
		VNI:  1000, // 默认 VNI
	}, nil
}

// NewVXLANAddr 创建新的 VXLAN 地址
func NewVXLANAddr(ip string, port int, vni uint32) (*VXLANAddr, error) {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ip)
	}

	if port <= 0 {
		port = 4789 // 默认 VXLAN 端口
	}

	if vni == 0 {
		vni = 1000 // 默认 VNI
	}

	return &VXLANAddr{
		IP:   parsedIP,
		Port: port,
		VNI:  vni,
	}, nil
}

// VXLAN 连接管理
var (
	vxlanConnections = make(map[string]*VXLANConn)
	vxlanMutex       sync.RWMutex
)

// DialVXLAN 创建 VXLAN 连接
func DialVXLAN(network string, localAddr, remoteAddr *VXLANAddr) (*VXLANConn, error) {
	if remoteAddr == nil {
		return nil, fmt.Errorf("remote address cannot be nil")
	}

	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, fmt.Errorf("failed to initialize DPDK network: %v", err)
	}

	// 从环境变量获取外层 VTEP 地址
	localVTEP := getLocalIPFromEnv()
	remoteVTEP := getRemoteVTEPFromEnv() // 需要新增这个函数

	// 创建 VXLAN 配置 - 外层是 VTEP 地址，内层是 VXLANAddr
	config := &VXLANConfig{
		VNI:       remoteAddr.VNI,
		LocalIP:   localVTEP,  // 外层本地 VTEP IP
		RemoteIP:  remoteVTEP, // 外层远程 VTEP IP
		UDPPort:   4789,       // 外层 VXLAN UDP 端口固定 4789
		LocalMAC:  getLocalMAC(),
		RemoteMAC: getRemoteMACFromARP(remoteVTEP), // 基于 VTEP IP 查询 MAC
	}

	vxlanConn := &VXLANConn{
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
		handler:    NewVXLANHandler(config),
		readChan:   make(chan []byte, 100),
		writeChan:  make(chan []byte, 100),
		closed:     false,
	}

	// 注册 VXLAN 连接到全局处理器
	connKey := fmt.Sprintf("%s:%d", remoteAddr.IP.String(), remoteAddr.VNI)
	vxlanMutex.Lock()
	vxlanConnections[connKey] = vxlanConn
	vxlanMutex.Unlock()

	// 启动 VXLAN 包处理 goroutine
	go vxlanConn.startPacketProcessor()

	log.Printf("[INFO] VXLAN connection established: local_vtep=%s, remote_vtep=%s, inner_local=%s, inner_remote=%s, VNI=%d",
		localVTEP, remoteVTEP, localAddr, remoteAddr, config.VNI)

	return vxlanConn, nil
}

// startPacketProcessor 启动包处理器
func (vc *VXLANConn) startPacketProcessor() {
	for {
		select {
		case data := <-vc.writeChan:
			vc.mu.RLock()
			if vc.closed {
				vc.mu.RUnlock()
				return
			}
			vc.mu.RUnlock()

			// 封装 VXLAN 包并通过 DPDK 发送
			if err := vc.sendVXLANPacket(data); err != nil {
				log.Printf("[ERROR] Failed to send VXLAN packet: %v", err)
			}
		}
	}
}

// sendVXLANPacket 发送 VXLAN 包
func (vc *VXLANConn) sendVXLANPacket(data []byte) error {
	// 生成内层以太网地址
	innerSrcMAC := vc.handler.config.LocalMAC
	innerDstMAC := vc.handler.config.RemoteMAC

	// 封装 VXLAN 包
	vxlanPacket, err := vc.handler.EncapsulateVXLAN(data, innerSrcMAC, innerDstMAC)
	if err != nil {
		return fmt.Errorf("VXLAN encapsulation failed: %v", err)
	}

	// 通过 DPDK 发送原始字节
	return SendRawBytes(vxlanPacket)
}

// Read 实现 net.Conn 接口
func (vc *VXLANConn) Read(b []byte) (n int, err error) {
	vc.mu.RLock()
	if vc.closed {
		vc.mu.RUnlock()
		return 0, fmt.Errorf("connection closed")
	}
	vc.mu.RUnlock()

	// 从读取通道获取数据
	select {
	case data := <-vc.readChan:
		if len(data) > len(b) {
			return 0, fmt.Errorf("buffer too small: need %d, got %d", len(data), len(b))
		}
		copy(b, data)
		return len(data), nil
	case <-time.After(30 * time.Second): // 30秒超时
		return 0, fmt.Errorf("read timeout")
	}
}

// Write 实现 net.Conn 接口
func (vc *VXLANConn) Write(b []byte) (n int, err error) {
	vc.mu.RLock()
	if vc.closed {
		vc.mu.RUnlock()
		return 0, fmt.Errorf("connection closed")
	}
	vc.mu.RUnlock()

	// 将数据发送到写入通道
	select {
	case vc.writeChan <- b:
		return len(b), nil
	case <-time.After(5 * time.Second): // 5秒超时
		return 0, fmt.Errorf("write timeout")
	}
}

// Close 实现 net.Conn 接口
func (vc *VXLANConn) Close() error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	if vc.closed {
		return nil
	}

	vc.closed = true
	close(vc.readChan)
	close(vc.writeChan)

	// 从全局连接表中移除
	connKey := fmt.Sprintf("%s:%d", vc.remoteAddr.IP.String(), vc.remoteAddr.VNI)
	vxlanMutex.Lock()
	delete(vxlanConnections, connKey)
	vxlanMutex.Unlock()

	log.Printf("[INFO] VXLAN connection closed: %s", connKey)
	return nil
}

// LocalAddr 实现 net.Conn 接口
func (vc *VXLANConn) LocalAddr() net.Addr {
	return vc.localAddr
}

// RemoteAddr 实现 net.Conn 接口
func (vc *VXLANConn) RemoteAddr() net.Addr {
	return vc.remoteAddr
}

// SetDeadline 实现 net.Conn 接口
func (vc *VXLANConn) SetDeadline(t time.Time) error {
	// 简化实现，不支持超时
	return nil
}

// SetReadDeadline 实现 net.Conn 接口
func (vc *VXLANConn) SetReadDeadline(t time.Time) error {
	// 简化实现，不支持超时
	return nil
}

// SetWriteDeadline 实现 net.Conn 接口
func (vc *VXLANConn) SetWriteDeadline(t time.Time) error {
	// 简化实现，不支持超时
	return nil
}

// VXLANListener VXLAN 监听器，基于 DPDK 数据路径
type VXLANListener struct {
	addr     *VXLANAddr
	handler  *VXLANHandler
	acceptCh chan *VXLANConn
	closed   bool
	mu       sync.RWMutex
}

// ListenVXLAN 创建 VXLAN 监听器
func ListenVXLAN(network string, addr *VXLANAddr) (*VXLANListener, error) {
	if addr == nil {
		return nil, fmt.Errorf("address cannot be nil")
	}

	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, fmt.Errorf("failed to initialize DPDK network: %v", err)
	}

	// 从环境变量获取本地 VTEP 地址（外层）
	localVTEP := getLocalIPFromEnv()

	// 创建 VXLAN 配置 - addr 是内层地址，VTEP 是外层地址
	config := &VXLANConfig{
		VNI:       addr.VNI,
		LocalIP:   localVTEP, // 外层本地 VTEP IP
		UDPPort:   4789,      // 外层固定端口 4789
		LocalMAC:  getLocalMAC(),
		RemoteMAC: nil, // 监听器不需要远程 MAC
	}

	listener := &VXLANListener{
		addr:     addr,
		handler:  NewVXLANHandler(config),
		acceptCh: make(chan *VXLANConn, 100),
		closed:   false,
	}

	// 注册 VXLAN 处理器到全局包处理器
	registerVXLANHandler(addr.VNI, listener)

	log.Printf("[INFO] VXLAN listener started: vtep=%s, inner_addr=%s, VNI=%d", localVTEP, addr, addr.VNI)
	return listener, nil
}

// Accept 实现 net.Listener 接口
func (vl *VXLANListener) Accept() (net.Conn, error) {
	vl.mu.RLock()
	if vl.closed {
		vl.mu.RUnlock()
		return nil, fmt.Errorf("listener closed")
	}
	vl.mu.RUnlock()

	select {
	case conn := <-vl.acceptCh:
		return conn, nil
	}
}

// Close 实现 net.Listener 接口
func (vl *VXLANListener) Close() error {
	vl.mu.Lock()
	defer vl.mu.Unlock()

	if vl.closed {
		return nil
	}

	vl.closed = true
	close(vl.acceptCh)

	// 注销 VXLAN 处理器
	unregisterVXLANHandler(vl.addr.VNI)

	log.Printf("[INFO] VXLAN listener closed: VNI=%d", vl.addr.VNI)
	return nil
}

// Addr 实现 net.Listener 接口
func (vl *VXLANListener) Addr() net.Addr {
	return vl.addr
}

// VXLAN 处理器注册
var (
	vxlanHandlers     = make(map[uint32]*VXLANListener)
	vxlanHandlerMutex sync.RWMutex
)

// registerVXLANHandler 注册 VXLAN 处理器
func registerVXLANHandler(vni uint32, listener *VXLANListener) {
	vxlanHandlerMutex.Lock()
	vxlanHandlers[vni] = listener
	vxlanHandlerMutex.Unlock()
}

// unregisterVXLANHandler 注销 VXLAN 处理器
func unregisterVXLANHandler(vni uint32) {
	vxlanHandlerMutex.Lock()
	delete(vxlanHandlers, vni)
	vxlanHandlerMutex.Unlock()
}

// handleIncomingVXLANPacket 处理传入的 VXLAN 包
func handleIncomingVXLANPacket(data []byte) {
	// 检查是否为 VXLAN 包
	if !IsVXLANPacket(data) {
		return
	}

	// 创建临时处理器进行解封装
	tmpHandler := NewVXLANHandler(DefaultVXLANConfig())
	innerPayload, _, _, vni, err := tmpHandler.DecapsulateVXLAN(data)
	if err != nil {
		log.Printf("[DEBUG] Failed to decapsulate VXLAN packet: %v", err)
		return
	}

	// 查找对应的处理器
	vxlanHandlerMutex.RLock()
	listener, exists := vxlanHandlers[vni]
	vxlanHandlerMutex.RUnlock()

	if exists && !listener.closed {
		// 创建新的连接或将数据发送到现有连接
		// 这里简化实现，直接创建一个连接
		remoteAddr := &VXLANAddr{
			IP:   net.IPv4(192, 168, 1, 100), // 临时远程地址
			Port: 4789,
			VNI:  vni,
		}

		conn := &VXLANConn{
			localAddr:  listener.addr,
			remoteAddr: remoteAddr,
			handler:    listener.handler,
			readChan:   make(chan []byte, 100),
			writeChan:  make(chan []byte, 100),
			closed:     false,
		}

		// 将解封装的数据发送到连接的读取通道
		select {
		case conn.readChan <- innerPayload:
		default:
			// 通道满，丢弃数据
		}

		// 尝试将连接发送到监听器
		select {
		case listener.acceptCh <- conn:
		default:
			// 接受队列满，丢弃连接
		}
	}
}
