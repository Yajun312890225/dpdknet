package dpdknet

import (
	"encoding/binary"
	"fmt"
	"net"
)

func CalcChecksum(data []byte) uint16 {
	sum := uint32(0)
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}

// ParseIPv4Addr 解析IPv4地址
func ParseIPv4Addr(data []byte, offset int) net.IP {
	if len(data) < offset+4 {
		return nil
	}
	return net.IPv4(data[offset], data[offset+1], data[offset+2], data[offset+3])
}

// FormatIPv4Addr 格式化IPv4地址
func FormatIPv4Addr(data []byte, offset int) string {
	if len(data) < offset+4 {
		return "0.0.0.0"
	}
	return fmt.Sprintf("%d.%d.%d.%d", data[offset], data[offset+1], data[offset+2], data[offset+3])
}

// SwapIPv4Addrs 交换源和目标IPv4地址
func SwapIPv4Addrs(data []byte, ipHeaderStart int) {
	for i := 0; i < 4; i++ {
		data[ipHeaderStart+12+i], data[ipHeaderStart+16+i] = data[ipHeaderStart+16+i], data[ipHeaderStart+12+i]
	}
}

// CalcIPv4Checksum 计算IPv4头校验和
func CalcIPv4Checksum(data []byte, ipHeaderStart, headerLength int) uint16 {
	// 清零校验和字段
	data[ipHeaderStart+10] = 0
	data[ipHeaderStart+11] = 0

	// 计算校验和
	checksum := CalcChecksum(data[ipHeaderStart : ipHeaderStart+headerLength])
	return checksum
}

// IsIPv4Packet 检查是否为IPv4数据包
func IsIPv4Packet(data []byte) bool {
	if len(data) < 14 {
		return false
	}
	// 检查EtherType是否为IPv4 (0x0800)
	etherType := binary.BigEndian.Uint16(data[12:14])
	return etherType == 0x0800
}

// GetIPv4Protocol 获取IPv4协议类型
func GetIPv4Protocol(data []byte) uint8 {
	if !IsIPv4Packet(data) || len(data) < 24 {
		return 0
	}
	return data[23] // IPv4头中的协议字段
}

// GetIPv4HeaderLength 获取IPv4头长度
func GetIPv4HeaderLength(data []byte) int {
	if !IsIPv4Packet(data) || len(data) < 15 {
		return 0
	}
	return int((data[14] & 0x0F) * 4)
}
