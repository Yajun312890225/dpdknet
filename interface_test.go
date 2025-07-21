package dpdknet

import (
	"net"
	"testing"
	"time"
)

// TestInterfaceCompatibilityOnly 仅测试接口兼容性，不涉及DPDK
func TestInterfaceCompatibilityOnly(t *testing.T) {
	// 测试TCPConn实现了net.Conn接口
	t.Run("TCPConn implements net.Conn", func(t *testing.T) {
		conn := &TCPConn{
			dataCh: make(chan []byte, 1),
		}

		// 测试所有net.Conn接口方法是否存在
		var c net.Conn = conn

		// 这些调用会测试方法签名是否正确
		_ = c.LocalAddr()
		_ = c.RemoteAddr()
		_ = c.SetDeadline(time.Now())
		_ = c.SetReadDeadline(time.Now())
		_ = c.SetWriteDeadline(time.Now())
		_ = c.Close()

		// 测试Read和Write（会立即返回由于没有初始化DPDK）
		go func() {
			c.Write([]byte("test"))
		}()
		_, _ = c.Read(make([]byte, 100))
	})

	// 测试UDPConn实现了net.Conn和net.PacketConn接口
	t.Run("UDPConn implements interfaces", func(t *testing.T) {
		conn := &UDPConn{
			recvCh: make(chan []byte, 1),
		}

		// 测试net.Conn接口
		var c net.Conn = conn
		_ = c.LocalAddr()
		_ = c.RemoteAddr()
		_ = c.SetDeadline(time.Now())
		_ = c.Close()

		// 测试UDP特有方法
		_, _, _ = conn.ReadFromUDP(make([]byte, 100))
		_, _ = conn.WriteToUDP([]byte("test"), &UDPAddr{})
	})

	// 测试TCPListener实现了net.Listener接口
	t.Run("TCPListener implements net.Listener", func(t *testing.T) {
		listener := &TCPListener{
			connCh: make(chan *TCPConn, 1),
		}

		var l net.Listener = listener
		_ = l.Addr()
		_ = l.Close()

		// Accept会阻塞，所以我们在goroutine中测试
		go func() {
			_, _ = l.Accept()
		}()
	})
}
