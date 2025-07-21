package dpdknet

import (
	"net"
	"testing"
)

func TestCalcChecksum(t *testing.T) {
	// 测试校验和计算
	data := []byte{0x45, 0x00, 0x00, 0x3c, 0x1c, 0x46, 0x40, 0x00, 0x40, 0x06, 0x00, 0x00, 0xac, 0x10, 0x0a, 0x63, 0xac, 0x10, 0x0a, 0x0c}
	checksum := CalcChecksum(data)
	if checksum == 0 {
		t.Error("Checksum should not be 0")
	}
}

func TestParseIPv4Addr(t *testing.T) {
	data := []byte{192, 168, 1, 1}
	ip := ParseIPv4Addr(data, 0)
	expected := net.IPv4(192, 168, 1, 1)
	if !ip.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, ip)
	}
}

func TestFormatIPv4Addr(t *testing.T) {
	data := []byte{192, 168, 1, 1}
	addr := FormatIPv4Addr(data, 0)
	expected := "192.168.1.1"
	if addr != expected {
		t.Errorf("Expected %s, got %s", expected, addr)
	}
}

func TestIsIPv4Packet(t *testing.T) {
	// 构造一个IPv4以太网帧
	data := make([]byte, 14)
	data[12] = 0x08 // IPv4 EtherType high byte
	data[13] = 0x00 // IPv4 EtherType low byte

	if !IsIPv4Packet(data) {
		t.Error("Should recognize IPv4 packet")
	}

	// 测试非IPv4包
	data[12] = 0x08
	data[13] = 0x06 // ARP
	if IsIPv4Packet(data) {
		t.Error("Should not recognize non-IPv4 packet as IPv4")
	}
}

func TestGetIPv4HeaderLength(t *testing.T) {
	// 构造IPv4包头
	data := make([]byte, 15)
	data[12] = 0x08 // IPv4 EtherType high byte
	data[13] = 0x00 // IPv4 EtherType low byte
	data[14] = 0x45 // Version=4, Header Length=5*4=20 bytes

	length := GetIPv4HeaderLength(data)
	if length != 20 {
		t.Errorf("Expected header length 20, got %d", length)
	}
}
