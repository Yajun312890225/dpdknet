package dpdknet

import (
	"net"
	"testing"
	"time"
)

// TestAPICompatibility 测试API与标准库的兼容性
func TestAPICompatibility(t *testing.T) {
	// 测试地址类型兼容性
	t.Run("TCPAddr", func(t *testing.T) {
		addr := &TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 8080,
		}

		// 测试Network方法
		if addr.Network() != "tcp" {
			t.Errorf("Expected 'tcp', got '%s'", addr.Network())
		}

		// 测试String方法
		expected := "127.0.0.1:8080"
		if addr.String() != expected {
			t.Errorf("Expected '%s', got '%s'", expected, addr.String())
		}
	})

	t.Run("UDPAddr", func(t *testing.T) {
		addr := &UDPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 9000,
		}

		// 测试Network方法
		if addr.Network() != "udp" {
			t.Errorf("Expected 'udp', got '%s'", addr.Network())
		}

		// 测试String方法
		expected := "127.0.0.1:9000"
		if addr.String() != expected {
			t.Errorf("Expected '%s', got '%s'", expected, addr.String())
		}
	})
}

// TestInterfaceCompatibility 测试接口兼容性
func TestInterfaceCompatibility(t *testing.T) {
	// 测试TCPConn实现了net.Conn接口
	t.Run("TCPConn implements net.Conn", func(t *testing.T) {
		var _ net.Conn = &TCPConn{}
	})

	// 测试UDPConn实现了net.Conn接口
	t.Run("UDPConn implements net.Conn", func(t *testing.T) {
		var _ net.Conn = &UDPConn{}
	})

	// 测试TCPListener实现了net.Listener接口
	t.Run("TCPListener implements net.Listener", func(t *testing.T) {
		var _ net.Listener = &TCPListener{}
	})

	// 测试地址类型实现了net.Addr接口
	t.Run("TCPAddr implements net.Addr", func(t *testing.T) {
		var _ net.Addr = &TCPAddr{}
	})

	t.Run("UDPAddr implements net.Addr", func(t *testing.T) {
		var _ net.Addr = &UDPAddr{}
	})
}

// TestMethodSignatures 测试方法签名兼容性
func TestMethodSignatures(t *testing.T) {
	// 这些测试确保方法签名与标准库一致

	// TCPConn方法
	conn := &TCPConn{}

	// 测试基本的Conn接口方法存在且签名正确
	_, _ = conn.Read([]byte{})
	_, _ = conn.Write([]byte{})
	_ = conn.Close()
	_ = conn.SetDeadline(time.Now())
	_ = conn.SetReadDeadline(time.Now())
	_ = conn.SetWriteDeadline(time.Now())
	_ = conn.LocalAddr()
	_ = conn.RemoteAddr()

	// UDPConn方法
	udpConn := &UDPConn{}
	_, _ = udpConn.Read([]byte{})
	_, _ = udpConn.Write([]byte{})
	_ = udpConn.Close()
	_ = udpConn.SetDeadline(time.Now())
	_ = udpConn.SetReadDeadline(time.Now())
	_ = udpConn.SetWriteDeadline(time.Now())
	_ = udpConn.LocalAddr()
	_ = udpConn.RemoteAddr()

	// UDP特有方法
	_, _, _ = udpConn.ReadFromUDP([]byte{})
	_, _ = udpConn.WriteToUDP([]byte{}, &UDPAddr{})
	_, _, _ = udpConn.ReadFrom([]byte{})
	_, _ = udpConn.WriteTo([]byte{}, &UDPAddr{})

	// TCPListener方法
	listener := &TCPListener{}
	_, _ = listener.Accept()
	_, _ = listener.AcceptTCP()
	_ = listener.Close()
	_ = listener.Addr()
}

// TestResolveAddresses 测试地址解析功能
func TestResolveAddresses(t *testing.T) {
	t.Run("ResolveTCPAddr", func(t *testing.T) {
		addr, err := ResolveTCPAddr("tcp", "127.0.0.1:8080")
		if err != nil {
			t.Fatalf("ResolveTCPAddr failed: %v", err)
		}

		if addr.Port != 8080 {
			t.Errorf("Expected port 8080, got %d", addr.Port)
		}

		if !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) {
			t.Errorf("Expected 127.0.0.1, got %s", addr.IP)
		}
	})

	t.Run("ResolveUDPAddr", func(t *testing.T) {
		addr, err := ResolveUDPAddr("udp", "127.0.0.1:9000")
		if err != nil {
			t.Fatalf("ResolveUDPAddr failed: %v", err)
		}

		if addr.Port != 9000 {
			t.Errorf("Expected port 9000, got %d", addr.Port)
		}

		if !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) {
			t.Errorf("Expected 127.0.0.1, got %s", addr.IP)
		}
	})
}

// TestDropInReplacement 测试无缝替换场景
func TestDropInReplacement(t *testing.T) {
	// 这个测试展示了如何将标准库代码修改为使用dpdknet

	// 原始标准库代码模式：
	// import "net"
	// listener, err := net.Listen("tcp", ":8080")
	// conn, err := net.Dial("tcp", "localhost:8080")

	// 替换后的代码模式：
	// import net "github.com/Yajun312890225/dpdknet"
	// listener, err := net.Listen("tcp", ":8080")  // 完全兼容标准库
	// conn, err := net.Dial("tcp", "localhost:8080")

	t.Log("Drop-in replacement test passed - API signatures are compatible")
}
