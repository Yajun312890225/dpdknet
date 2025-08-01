package dpdknet

import (
	"errors"
	"net"
	"time"
)

// Option 参数设置类型
type Option func(*options)

type options struct {
	useVXLAN  bool     // 是否使用VXLAN（使用全局配置）
	localAddr net.Addr // 本地地址
	// 可扩展更多参数
}

func setDefaultOption() *options {
	return &options{}
}

func WithVXLAN() Option {
	return func(o *options) {
		o.useVXLAN = true
	}
}

// WithLocalAddr 设置本地地址
func WithLocalAddr(localAddr net.Addr) Option {
	return func(o *options) {
		o.localAddr = localAddr
	}
}

// Dial connects to the address on the named network, with Option 风格。
func Dial(network, address string, opts ...Option) (net.Conn, error) {
	o := setDefaultOption()
	for _, opt := range opts {
		opt(o)
	}
	useVXLAN := o.useVXLAN
	localAddr := o.localAddr

	switch network {
	case "tcp", "tcp4", "tcp6":
		addr, err := ResolveTCPAddr(network, address)
		if err != nil {
			return nil, err
		}

		var laddr *TCPAddr
		if localAddr != nil {
			if tcpAddr, ok := localAddr.(*TCPAddr); ok {
				laddr = tcpAddr
			}
		}

		if useVXLAN {
			return DialTCPWithVXLAN(network, laddr, addr)
		}
		return DialTCP(network, laddr, addr)
	case "udp", "udp4", "udp6":
		addr, err := ResolveUDPAddr(network, address)
		if err != nil {
			return nil, err
		}

		var laddr *UDPAddr
		if localAddr != nil {
			if udpAddr, ok := localAddr.(*UDPAddr); ok {
				laddr = udpAddr
			}
		}
		// 检查是否应该使用VXLAN但没有配置
		if useVXLAN {
			return DialUDPWithVXLAN(network, laddr, addr)
		}
		return DialUDP(network, laddr, addr)
	case "icmp", "icmp4", "icmp6":
		addr, err := net.ResolveIPAddr(network, address)
		if err != nil {
			return nil, err
		}
		conn, err := DialICMPWithVXLAN(addr)
		if err != nil {
			return nil, err
		}
		return &ICMPConnAdapter{conn: conn}, nil
	default:
		return nil, errors.New("unsupported network type: " + network)
	}
}

// DialTimeout acts like Dial but takes a timeout.
func DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
		addr, err := ResolveTCPAddr(network, address)
		if err != nil {
			return nil, err
		}
		return DialTCPTimeout(network, nil, addr, timeout)
	case "udp", "udp4", "udp6":
		// UDP是无连接的，超时对建立连接没有意义，直接调用Dial
		return Dial(network, address)
	case "vxlan":
		// VXLAN 基于 UDP，超时处理类似 UDP
		return Dial(network, address)
	default:
		return nil, errors.New("unsupported network type: " + network)
	}
}

// DialTCPTimeout 创建带超时的TCP连接
func DialTCPTimeout(network string, laddr, raddr *TCPAddr, timeout time.Duration) (*TCPConn, error) {
	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, err
	}

	if raddr == nil {
		return nil, errors.New("remote address cannot be nil")
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

	// 通过 gVisor netstack 创建带超时的TCP连接
	gvisorConn, err := CreateGVisorTCPConnWithTimeout(localAddr, remoteAddr, timeout)
	if err != nil {
		return nil, err
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

// DialUDP acts like Dial for UDP networks.
func DialUDP(network string, laddr, raddr *UDPAddr) (*UDPConn, error) {
	conn, err := ListenUDP(network, laddr)
	if err != nil {
		return nil, err
	}
	conn.remoteAddr = raddr
	return conn, nil
}

// DialICMPWithVXLAN creates an ICMP connection with optional VXLAN encapsulation.
func DialICMPWithVXLAN(raddr *net.IPAddr) (*ICMPConn, error) {
	var localIP net.IP
	localIP = getLocalIP()
	return NewICMPConnWithVXLAN(localIP, GetGlobalVXLANConfig())
}
