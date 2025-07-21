package dpdknet

import (
	"net"
	"testing"
	"time"
)

func TestTCPAddr(t *testing.T) {
	// 测试 TCP 地址解析
	addr, err := ResolveTCPAddr("tcp", "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Failed to resolve TCP addr: %v", err)
	}

	if addr.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", addr.Port)
	}

	if !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("Expected IP 127.0.0.1, got %s", addr.IP.String())
	}

	// 测试地址字符串表示
	expected := "127.0.0.1:8080"
	if addr.String() != expected {
		t.Errorf("Expected %s, got %s", expected, addr.String())
	}
}

func TestTCPConnection(t *testing.T) {
	// 模拟测试（不需要真实的DPDK环境）
	localAddr := &TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 12345,
	}

	remoteAddr := &TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 8080,
	}

	conn := &TCPConn{
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
		state:      TCPStateEstablished,
		dataCh:     make(chan []byte, 1024),
		seqNum:     1000,
		ackNum:     2000,
	}

	// 测试地址获取
	if conn.LocalAddr().String() != "127.0.0.1:12345" {
		t.Errorf("Unexpected local address: %s", conn.LocalAddr().String())
	}

	if conn.RemoteAddr().String() != "127.0.0.1:8080" {
		t.Errorf("Unexpected remote address: %s", conn.RemoteAddr().String())
	}

	// 测试连接关闭
	if err := conn.Close(); err != nil {
		t.Errorf("Failed to close connection: %v", err)
	}

	if conn.state != TCPStateClosed {
		t.Errorf("Expected state TCPStateClosed, got %v", conn.state)
	}
}

func TestTCPListener(t *testing.T) {
	addr := &TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 8080,
	}

	listener := &TCPListener{
		localAddr: addr,
		connCh:    make(chan *TCPConn, 1024),
	}

	// 测试监听器地址
	if listener.Addr().String() != "127.0.0.1:8080" {
		t.Errorf("Unexpected listener address: %s", listener.Addr().String())
	}

	// 测试关闭监听器
	if err := listener.Close(); err != nil {
		t.Errorf("Failed to close listener: %v", err)
	}

	if !listener.closed {
		t.Error("Listener should be marked as closed")
	}
}

func TestTCPStates(t *testing.T) {
	states := []TCPState{
		TCPStateClosed,
		TCPStateListen,
		TCPStateSynSent,
		TCPStateSynReceived,
		TCPStateEstablished,
		TCPStateFinWait1,
		TCPStateFinWait2,
		TCPStateCloseWait,
		TCPStateClosing,
		TCPStateLastAck,
		TCPStateTimeWait,
	}

	// 验证所有状态都是唯一的
	stateMap := make(map[TCPState]bool)
	for _, state := range states {
		if stateMap[state] {
			t.Errorf("Duplicate state found: %v", state)
		}
		stateMap[state] = true
	}
}

func TestTCPFlags(t *testing.T) {
	// 测试TCP标志位
	testCases := []struct {
		flag     uint8
		expected uint8
	}{
		{TCPFlagFIN, 0x01},
		{TCPFlagSYN, 0x02},
		{TCPFlagRST, 0x04},
		{TCPFlagPSH, 0x08},
		{TCPFlagACK, 0x10},
		{TCPFlagURG, 0x20},
	}

	for _, tc := range testCases {
		if tc.flag != tc.expected {
			t.Errorf("Expected flag value 0x%02x, got 0x%02x", tc.expected, tc.flag)
		}
	}

	// 测试标志位组合
	synAck := uint8(TCPFlagSYN | TCPFlagACK)
	expected := uint8(0x02 | 0x10)
	if synAck != expected {
		t.Errorf("Expected SYN+ACK flag 0x%02x, got 0x%02x", expected, synAck)
	}
}

func TestTCPTimeout(t *testing.T) {
	conn := &TCPConn{
		dataCh: make(chan []byte),
		closed: false,
	}

	// 测试读取超时
	start := time.Now()
	buf := make([]byte, 1024)

	go func() {
		time.Sleep(100 * time.Millisecond)
		conn.Close() // 关闭连接触发错误
	}()

	_, err := conn.Read(buf)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected read to fail after connection close")
	}

	if elapsed > 200*time.Millisecond {
		t.Error("Read should fail quickly after connection close")
	}
}

// 基准测试
func BenchmarkTCPAddrString(b *testing.B) {
	addr := &TCPAddr{
		IP:   net.IPv4(192, 168, 1, 100),
		Port: 8080,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = addr.String()
	}
}

func BenchmarkTCPConnCreation(b *testing.B) {
	localAddr := &TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
	remoteAddr := &TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn := &TCPConn{
			localAddr:  localAddr,
			remoteAddr: remoteAddr,
			state:      TCPStateEstablished,
			dataCh:     make(chan []byte, 1024),
			seqNum:     uint32(i),
		}
		_ = conn
	}
}
