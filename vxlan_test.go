package dpdknet

import (
	"bytes"
	"net"
	"testing"
)

func TestVXLANEncapsulation(t *testing.T) {
	// 创建 VXLAN 配置
	config := &VXLANConfig{
		VNI:       1000,
		LocalIP:   net.IPv4(192, 168, 1, 10),
		RemoteIP:  net.IPv4(192, 168, 1, 20),
		UDPPort:   4789,
		LocalMAC:  net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
		RemoteMAC: net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x02},
	}

	handler := NewVXLANHandler(config)

	// 测试数据
	innerSrcMAC := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	innerDstMAC := net.HardwareAddr{0x00, 0xaa, 0xbb, 0xcc, 0xdd, 0xee}
	testPayload := []byte("Hello VXLAN!")

	// 封装
	encapsulated, err := handler.EncapsulateVXLAN(testPayload, innerSrcMAC, innerDstMAC)
	if err != nil {
		t.Fatalf("Encapsulation failed: %v", err)
	}

	if len(encapsulated) == 0 {
		t.Fatal("Encapsulated packet is empty")
	}

	// 解封装
	innerPayload, recoveredSrcMAC, recoveredDstMAC, recoveredVNI, err := handler.DecapsulateVXLAN(encapsulated)
	if err != nil {
		t.Fatalf("Decapsulation failed: %v", err)
	}

	// 验证 VNI
	if recoveredVNI != config.VNI {
		t.Errorf("VNI mismatch: expected %d, got %d", config.VNI, recoveredVNI)
	}

	// 验证内层 MAC 地址
	if !bytes.Equal(innerSrcMAC, recoveredSrcMAC) {
		t.Errorf("Inner source MAC mismatch: expected %v, got %v", innerSrcMAC, recoveredSrcMAC)
	}

	if !bytes.Equal(innerDstMAC, recoveredDstMAC) {
		t.Errorf("Inner destination MAC mismatch: expected %v, got %v", innerDstMAC, recoveredDstMAC)
	}

	// 验证有效载荷
	if !bytes.Equal(testPayload, innerPayload) {
		t.Errorf("Payload mismatch: expected %s, got %s", string(testPayload), string(innerPayload))
	}

	t.Logf("VXLAN encapsulation/decapsulation test passed!")
	t.Logf("Original payload: %s", string(testPayload))
	t.Logf("Recovered payload: %s", string(innerPayload))
	t.Logf("VNI: %d", recoveredVNI)
}

func TestVXLANAddrParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected *VXLANAddr
		hasError bool
	}{
		{
			input: "192.168.1.100:4789",
			expected: &VXLANAddr{
				IP:   net.IPv4(192, 168, 1, 100),
				Port: 4789,
				VNI:  1000, // 默认值
			},
			hasError: false,
		},
		{
			input:    "invalid:addr",
			expected: nil,
			hasError: true,
		},
		{
			input:    "192.168.1.100", // 缺少端口
			expected: nil,
			hasError: true,
		},
	}

	for _, test := range tests {
		addr, err := ParseVXLANAddr(test.input)

		if test.hasError {
			if err == nil {
				t.Errorf("Expected error for input %s, but got none", test.input)
			}
			continue
		}

		if err != nil {
			t.Errorf("Unexpected error for input %s: %v", test.input, err)
			continue
		}

		if !addr.IP.Equal(test.expected.IP) {
			t.Errorf("IP mismatch for %s: expected %v, got %v", test.input, test.expected.IP, addr.IP)
		}

		if addr.Port != test.expected.Port {
			t.Errorf("Port mismatch for %s: expected %d, got %d", test.input, test.expected.Port, addr.Port)
		}

		if addr.VNI != test.expected.VNI {
			t.Errorf("VNI mismatch for %s: expected %d, got %d", test.input, test.expected.VNI, addr.VNI)
		}
	}
}

func TestNewVXLANAddr(t *testing.T) {
	addr, err := NewVXLANAddr("10.0.0.1", 4789, 2000)
	if err != nil {
		t.Fatalf("Failed to create VXLAN address: %v", err)
	}

	expectedIP := net.IPv4(10, 0, 0, 1)
	if !addr.IP.Equal(expectedIP) {
		t.Errorf("IP mismatch: expected %v, got %v", expectedIP, addr.IP)
	}

	if addr.Port != 4789 {
		t.Errorf("Port mismatch: expected 4789, got %d", addr.Port)
	}

	if addr.VNI != 2000 {
		t.Errorf("VNI mismatch: expected 2000, got %d", addr.VNI)
	}

	// 测试默认值
	addr2, err := NewVXLANAddr("10.0.0.2", 0, 0)
	if err != nil {
		t.Fatalf("Failed to create VXLAN address with defaults: %v", err)
	}

	if addr2.Port != 4789 {
		t.Errorf("Default port mismatch: expected 4789, got %d", addr2.Port)
	}

	if addr2.VNI != 1000 {
		t.Errorf("Default VNI mismatch: expected 1000, got %d", addr2.VNI)
	}
}

func TestIsVXLANPacket(t *testing.T) {
	// 创建一个有效的 VXLAN 包
	config := DefaultVXLANConfig()
	config.LocalIP = net.IPv4(192, 168, 1, 10)
	config.RemoteIP = net.IPv4(192, 168, 1, 20)
	config.LocalMAC = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	config.RemoteMAC = net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x02}

	handler := NewVXLANHandler(config)

	testPayload := []byte("test")
	innerSrcMAC := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}
	innerDstMAC := net.HardwareAddr{0x00, 0xaa, 0xbb, 0xcc, 0xdd, 0xee}

	vxlanPacket, err := handler.EncapsulateVXLAN(testPayload, innerSrcMAC, innerDstMAC)
	if err != nil {
		t.Fatalf("Failed to create VXLAN packet: %v", err)
	}

	// 测试 VXLAN 包检测
	if !IsVXLANPacket(vxlanPacket) {
		t.Error("Valid VXLAN packet not detected")
	}

	// 测试非 VXLAN 包
	nonVXLANPacket := []byte{0x00, 0x01, 0x02, 0x03} // 太短的包
	if IsVXLANPacket(nonVXLANPacket) {
		t.Error("Invalid packet detected as VXLAN")
	}

	// 测试空包
	if IsVXLANPacket([]byte{}) {
		t.Error("Empty packet detected as VXLAN")
	}
}
