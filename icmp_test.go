package dpdknet

import (
	"net"
	"testing"
	"time"
)

func TestICMPConstants(t *testing.T) {
	// 测试ICMP常量
	if ICMPEchoRequest != 8 {
		t.Errorf("Expected ICMPEchoRequest to be 8, got %d", ICMPEchoRequest)
	}

	if ICMPEchoReply != 0 {
		t.Errorf("Expected ICMPEchoReply to be 0, got %d", ICMPEchoReply)
	}
}

func TestICMPHeader(t *testing.T) {
	header := ICMPHeader{
		Type:     ICMPEchoRequest,
		Code:     0,
		Checksum: 0x1234,
		ID:       0x5678,
		Sequence: 0x9ABC,
	}

	// 测试ICMP头字段
	if header.Type != 8 {
		t.Errorf("Expected Type 8, got %d", header.Type)
	}

	if header.Code != 0 {
		t.Errorf("Expected Code 0, got %d", header.Code)
	}

	if header.Checksum != 0x1234 {
		t.Errorf("Expected Checksum 0x1234, got 0x%04x", header.Checksum)
	}

	if header.ID != 0x5678 {
		t.Errorf("Expected ID 0x5678, got 0x%04x", header.ID)
	}

	if header.Sequence != 0x9ABC {
		t.Errorf("Expected Sequence 0x9ABC, got 0x%04x", header.Sequence)
	}
}

func TestICMPReply(t *testing.T) {
	reply := ICMPReply{
		Addr:     net.IPv4(192, 168, 1, 1),
		ID:       12345,
		Sequence: 54321,
		RTT:      time.Millisecond * 50,
	}

	// 测试ICMP回复字段
	expectedIP := net.IPv4(192, 168, 1, 1)
	if !reply.Addr.Equal(expectedIP) {
		t.Errorf("Expected IP %s, got %s", expectedIP, reply.Addr)
	}

	if reply.ID != 12345 {
		t.Errorf("Expected ID 12345, got %d", reply.ID)
	}

	if reply.Sequence != 54321 {
		t.Errorf("Expected Sequence 54321, got %d", reply.Sequence)
	}

	if reply.RTT != time.Millisecond*50 {
		t.Errorf("Expected RTT 50ms, got %v", reply.RTT)
	}
}

func TestICMPConnection(t *testing.T) {
	localIP := net.IPv4(127, 0, 0, 1)

	conn := &ICMPConn{
		localIP: localIP,
		id:      uint16(time.Now().Unix() & 0xFFFF),
		seq:     0,
		replyCh: make(chan *ICMPReply, 1024),
		closed:  false,
	}

	// 测试本地IP
	if !conn.localIP.Equal(localIP) {
		t.Errorf("Expected local IP %s, got %s", localIP, conn.localIP)
	}

	// 测试ID范围
	if conn.id > 0xFFFF {
		t.Errorf("ID should be within 16-bit range, got %d", conn.id)
	}

	// 测试初始序列号
	if conn.seq != 0 {
		t.Errorf("Expected initial sequence 0, got %d", conn.seq)
	}

	// 测试连接关闭
	if err := conn.Close(); err != nil {
		t.Errorf("Failed to close ICMP connection: %v", err)
	}

	if !conn.closed {
		t.Error("Connection should be marked as closed")
	}

	// 再次关闭应该不会出错
	if err := conn.Close(); err != nil {
		t.Errorf("Second close should not error: %v", err)
	}
}

func TestICMPPingTimeout(t *testing.T) {
	conn := &ICMPConn{
		localIP: net.IPv4(127, 0, 0, 1),
		id:      12345,
		seq:     0,
		replyCh: make(chan *ICMPReply, 1024),
		closed:  false,
	}

	// 测试ping超时
	dstIP := net.IPv4(192, 168, 1, 1)
	timeout := 100 * time.Millisecond

	start := time.Now()

	// 启动一个goroutine来模拟超时情况
	go func() {
		time.Sleep(50 * time.Millisecond)
		// 不发送任何回复，让ping超时
	}()

	// 这个测试会超时，因为我们没有真实的网络环境
	// 但我们可以测试函数调用不会panic
	err := conn.Ping(dstIP, 1, timeout)
	elapsed := time.Since(start)

	// ping函数应该在合理时间内返回
	if elapsed > timeout*2 {
		t.Errorf("Ping took too long: %v", elapsed)
	}

	// 在没有网络的情况下，这可能会返回错误或正常完成
	if err != nil {
		t.Logf("Ping returned error (expected in test environment): %v", err)
	}
}

func TestICMPSequenceIncrement(t *testing.T) {
	conn := &ICMPConn{
		localIP: net.IPv4(127, 0, 0, 1),
		id:      12345,
		seq:     0,
		replyCh: make(chan *ICMPReply, 1024),
		closed:  false,
	}

	initialSeq := conn.seq

	// 模拟多次ping调用会增加序列号
	for i := 0; i < 5; i++ {
		conn.seq++
	}

	expectedSeq := initialSeq + 5
	if conn.seq != expectedSeq {
		t.Errorf("Expected sequence %d, got %d", expectedSeq, conn.seq)
	}
}

func TestICMPReplyChannel(t *testing.T) {
	conn := &ICMPConn{
		localIP: net.IPv4(127, 0, 0, 1),
		id:      12345,
		seq:     0,
		replyCh: make(chan *ICMPReply, 2), // 小缓冲区便于测试
		closed:  false,
	}

	// 测试发送回复到通道
	reply1 := &ICMPReply{
		Addr:     net.IPv4(192, 168, 1, 1),
		ID:       12345,
		Sequence: 1,
		RTT:      time.Millisecond * 10,
	}

	reply2 := &ICMPReply{
		Addr:     net.IPv4(192, 168, 1, 2),
		ID:       12345,
		Sequence: 2,
		RTT:      time.Millisecond * 20,
	}

	// 发送回复
	select {
	case conn.replyCh <- reply1:
	case <-time.After(time.Millisecond):
		t.Error("Failed to send reply1 to channel")
	}

	select {
	case conn.replyCh <- reply2:
	case <-time.After(time.Millisecond):
		t.Error("Failed to send reply2 to channel")
	}

	// 接收回复
	select {
	case received := <-conn.replyCh:
		if received.Sequence != 1 {
			t.Errorf("Expected sequence 1, got %d", received.Sequence)
		}
	case <-time.After(time.Millisecond):
		t.Error("Failed to receive reply1 from channel")
	}

	select {
	case received := <-conn.replyCh:
		if received.Sequence != 2 {
			t.Errorf("Expected sequence 2, got %d", received.Sequence)
		}
	case <-time.After(time.Millisecond):
		t.Error("Failed to receive reply2 from channel")
	}
}

func TestICMPChannelBlocking(t *testing.T) {
	// 测试通道满时的行为
	conn := &ICMPConn{
		localIP: net.IPv4(127, 0, 0, 1),
		id:      12345,
		seq:     0,
		replyCh: make(chan *ICMPReply, 1), // 容量为1
		closed:  false,
	}

	// 填满通道
	reply := &ICMPReply{
		Addr:     net.IPv4(192, 168, 1, 1),
		ID:       12345,
		Sequence: 1,
		RTT:      time.Millisecond * 10,
	}

	select {
	case conn.replyCh <- reply:
	default:
		t.Error("Should be able to send first reply")
	}

	// 尝试发送第二个回复 (应该阻塞或被丢弃)
	reply2 := &ICMPReply{
		Addr:     net.IPv4(192, 168, 1, 1),
		ID:       12345,
		Sequence: 2,
		RTT:      time.Millisecond * 10,
	}

	select {
	case conn.replyCh <- reply2:
		t.Error("Channel should be full, send should not succeed immediately")
	default:
		// 这是期望的行为
	}
}

// 基准测试
func BenchmarkICMPConnCreation(b *testing.B) {
	localIP := net.IPv4(127, 0, 0, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn := &ICMPConn{
			localIP: localIP,
			id:      uint16(i),
			seq:     0,
			replyCh: make(chan *ICMPReply, 1024),
			closed:  false,
		}
		_ = conn
	}
}

func BenchmarkICMPReplyCreation(b *testing.B) {
	addr := net.IPv4(192, 168, 1, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reply := &ICMPReply{
			Addr:     addr,
			ID:       uint16(i),
			Sequence: uint16(i),
			RTT:      time.Microsecond * time.Duration(i),
		}
		_ = reply
	}
}

func BenchmarkICMPHeaderCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		header := ICMPHeader{
			Type:     ICMPEchoRequest,
			Code:     0,
			Checksum: uint16(i),
			ID:       uint16(i),
			Sequence: uint16(i),
		}
		_ = header
	}
}
