package dpdknet

import (
	"net"
	"testing"
	"time"
)

func TestUDPAddr(t *testing.T) {
	// 测试 UDP 地址解析
	addr, err := ResolveUDPAddr("udp", "127.0.0.1:9090")
	if err != nil {
		t.Fatalf("Failed to resolve UDP addr: %v", err)
	}

	if addr.Port != 9090 {
		t.Errorf("Expected port 9090, got %d", addr.Port)
	}

	if !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("Expected IP 127.0.0.1, got %s", addr.IP.String())
	}

	// 测试地址字符串表示
	expected := "127.0.0.1:9090"
	if addr.String() != expected {
		t.Errorf("Expected %s, got %s", expected, addr.String())
	}
}

func TestUDPAddrNetwork(t *testing.T) {
	addr := &UDPAddr{
		IP:   net.IPv4(192, 168, 1, 1),
		Port: 8080,
	}

	if addr.Network() != "udp" {
		t.Errorf("Expected network 'udp', got '%s'", addr.Network())
	}
}

func TestUDPConnection(t *testing.T) {
	// 创建模拟UDP连接
	localAddr := &UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 12345,
	}

	conn := &UDPConn{
		localAddr: localAddr,
		recvCh:    make(chan []byte, 1024),
	}

	// 测试地址获取
	if conn.LocalAddr().String() != "127.0.0.1:12345" {
		t.Errorf("Unexpected local address: %s", conn.LocalAddr().String())
	}

	// 测试连接关闭
	if err := conn.Close(); err != nil {
		t.Errorf("Failed to close connection: %v", err)
	}

	if !conn.closed {
		t.Error("Connection should be marked as closed")
	}
}

func TestUDPWriteToAndReadFrom(t *testing.T) {
	conn := &UDPConn{
		localAddr: &UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345},
		recvCh:    make(chan []byte, 1024),
		closed:    false,
	}

	// 测试数据写入
	testData := []byte("Hello, UDP!")

	// 模拟收到数据包
	go func() {
		time.Sleep(10 * time.Millisecond)

		// 构造一个简单的UDP数据包 (简化测试)
		mockPacket := make([]byte, 100)
		copy(mockPacket[42:], testData) // 假设UDP数据从偏移42开始

		select {
		case conn.recvCh <- mockPacket:
		default:
		}
	}()

	// 测试从连接读取数据
	buffer := make([]byte, 1024)
	n, addr, err := conn.ReadFromUDP(buffer)

	if err != nil {
		t.Errorf("ReadFromUDP failed: %v", err)
	}

	if n == 0 {
		t.Error("No data read from UDP connection")
	}

	if addr == nil {
		t.Error("No address returned from ReadFromUDP")
	}
}

func TestUDPConnectionTimeout(t *testing.T) {
	conn := &UDPConn{
		recvCh: make(chan []byte),
		closed: false,
	}

	// 测试读取超时
	start := time.Now()
	buffer := make([]byte, 1024)

	go func() {
		time.Sleep(100 * time.Millisecond)
		conn.Close() // 关闭连接
	}()

	_, _, err := conn.ReadFromUDP(buffer)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected ReadFromUDP to fail after connection close")
	}

	if elapsed > 200*time.Millisecond {
		t.Error("ReadFromUDP should fail quickly after connection close")
	}
}

func TestUDPAddrResolution(t *testing.T) {
	testCases := []struct {
		network  string
		address  string
		expected string
		hasError bool
	}{
		{"udp", "localhost:8080", "127.0.0.1:8080", false},
		{"udp4", "192.168.1.1:9000", "192.168.1.1:9000", false},
		{"udp", ":8080", "0.0.0.0:8080", false},
		{"udp", "invalid:address", "", true},
	}

	for _, tc := range testCases {
		addr, err := ResolveUDPAddr(tc.network, tc.address)

		if tc.hasError {
			if err == nil {
				t.Errorf("Expected error for address %s, but got none", tc.address)
			}
			continue
		}

		if err != nil {
			t.Errorf("Unexpected error for address %s: %v", tc.address, err)
			continue
		}

		if tc.expected != "" && addr.String() != tc.expected {
			t.Errorf("Expected %s, got %s for address %s", tc.expected, addr.String(), tc.address)
		}
	}
}

func TestUDPZeroValue(t *testing.T) {
	var addr UDPAddr

	// 零值UDP地址的网络类型应该是"udp"
	if addr.Network() != "udp" {
		t.Errorf("Expected network 'udp', got '%s'", addr.Network())
	}

	// 零值UDP地址的字符串表示
	expected := "<nil>:0"
	if addr.String() != expected {
		t.Errorf("Expected %s, got %s", expected, addr.String())
	}
}

func TestUDPConnectionStates(t *testing.T) {
	conn := &UDPConn{
		recvCh: make(chan []byte, 1024),
		closed: false,
	}

	// 连接应该最初是打开的
	if conn.closed {
		t.Error("New connection should not be closed")
	}

	// 关闭连接
	if err := conn.Close(); err != nil {
		t.Errorf("Failed to close connection: %v", err)
	}

	// 连接现在应该是关闭的
	if !conn.closed {
		t.Error("Connection should be closed after Close()")
	}

	// 再次关闭应该不会出错
	if err := conn.Close(); err != nil {
		t.Errorf("Second close should not error: %v", err)
	}
}

// 基准测试
func BenchmarkUDPAddrString(b *testing.B) {
	addr := &UDPAddr{
		IP:   net.IPv4(192, 168, 1, 100),
		Port: 9090,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = addr.String()
	}
}

func BenchmarkUDPConnCreation(b *testing.B) {
	localAddr := &UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn := &UDPConn{
			localAddr: localAddr,
			recvCh:    make(chan []byte, 1024),
		}
		_ = conn
	}
}

func BenchmarkUDPWrite(b *testing.B) {
	conn := &UDPConn{
		localAddr: &UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345},
		closed:    false,
	}

	data := []byte("benchmark test data")
	remoteAddr := &UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = conn.WriteToUDP(data, remoteAddr)
	}
}
