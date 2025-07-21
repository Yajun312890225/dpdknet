package dpdknet

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Yajun312890225/nff-go/packet"
)

const (
	ICMPEchoRequest = 8
	ICMPEchoReply   = 0
)

type ICMPHeader struct {
	Type     uint8
	Code     uint8
	Checksum uint16
	ID       uint16
	Sequence uint16
}

type ICMPConn struct {
	localIP net.IP
	id      uint16
	seq     uint16
	replyCh chan *ICMPReply
	mu      sync.Mutex
	closed  bool
}

type ICMPReply struct {
	Addr     net.IP
	ID       uint16
	Sequence uint16
	RTT      time.Duration
}

func NewICMPConn(localIP net.IP) (*ICMPConn, error) {
	// 确保全局网络系统已初始化
	if err := EnsureGlobalNetworkInit(); err != nil {
		return nil, err
	}

	conn := &ICMPConn{
		localIP: localIP,
		id:      uint16(time.Now().Unix() & 0xFFFF),
		seq:     0,
		replyCh: make(chan *ICMPReply, 1024),
	}

	// ICMP 处理已经通过全局网络系统处理，不需要单独的 flow
	return conn, nil
}

func (c *ICMPConn) Ping(dst net.IP, count int, timeout time.Duration) error {
	for i := 0; i < count; i++ {
		c.mu.Lock()
		c.seq++
		seq := c.seq
		c.mu.Unlock()

		start := time.Now()

		// 发送ICMP Echo Request包 (这里需要实现发送逻辑)
		log.Printf("Ping %s: seq=%d", dst.String(), seq)

		// 等待回复
		select {
		case reply := <-c.replyCh:
			if reply.ID == c.id && reply.Sequence == seq {
				rtt := time.Since(start)
				fmt.Printf("Reply from %s: seq=%d time=%v\n",
					reply.Addr.String(), reply.Sequence, rtt)
			}
		case <-time.After(timeout):
			fmt.Printf("Request timeout for seq=%d\n", seq)
		}

		if i < count-1 {
			time.Sleep(time.Second)
		}
	}
	return nil
}

func (c *ICMPConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.replyCh)
	return nil
}

// HandleICMPPacket 处理ICMP数据包 (由network.go调用)
func HandleICMPPacket(data []byte, ipHeaderStart, headerLength int, srcIP, dstIP net.IP) {
	icmpStart := ipHeaderStart + headerLength
	if len(data) < icmpStart+8 {
		log.Printf("[DEBUG] ICMP packet too short")
		return
	}

	icmpType := data[icmpStart]
	icmpCode := data[icmpStart+1]
	log.Printf("[DEBUG] ICMP: %s -> %s (type=%d, code=%d)",
		srcIP.String(), dstIP.String(), icmpType, icmpCode)

	// 处理不同类型的ICMP包
	switch icmpType {
	case ICMPEchoRequest: // Echo Request (ping)
		if icmpCode == 0 {
			handlePingRequest(data, ipHeaderStart, headerLength, icmpStart, srcIP, dstIP)
		}
	case ICMPEchoReply: // Echo Reply
		log.Printf("[DEBUG] Received ping reply from %s", srcIP.String())
	default:
		log.Printf("[DEBUG] Unsupported ICMP type: %d", icmpType)
	}
}

// handlePingRequest 处理ping请求并发送回复
func handlePingRequest(data []byte, ipHeaderStart, headerLength, icmpStart int, srcIP, dstIP net.IP) {
	log.Printf("[DEBUG] Handling ping request from %s", srcIP.String())

	// 交换源和目标IP
	for i := 0; i < 4; i++ {
		data[ipHeaderStart+12+i], data[ipHeaderStart+16+i] = data[ipHeaderStart+16+i], data[ipHeaderStart+12+i]
	}

	// 将ICMP类型改为Echo Reply (0)
	data[icmpStart] = ICMPEchoReply

	// 重新计算ICMP校验和
	data[icmpStart+2] = 0 // 清零校验和
	data[icmpStart+3] = 0

	icmpLen := len(data) - icmpStart
	sum := uint32(0)
	for i := icmpStart; i < icmpStart+icmpLen-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if icmpLen%2 == 1 {
		sum += uint32(data[icmpStart+icmpLen-1]) << 8
	}

	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	checksum := ^uint16(sum)
	binary.BigEndian.PutUint16(data[icmpStart+2:icmpStart+4], checksum)

	// 重新计算IP头校验和
	data[ipHeaderStart+10] = 0
	data[ipHeaderStart+11] = 0

	sum = uint32(0)
	for i := ipHeaderStart; i < ipHeaderStart+headerLength-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}

	for (sum >> 16) > 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	checksum = ^uint16(sum)
	binary.BigEndian.PutUint16(data[ipHeaderStart+10:ipHeaderStart+12], checksum)

	// 创建新数据包并发送
	pkt, err := packet.NewPacket()
	if err != nil {
		log.Printf("[ERROR] Failed to create ping reply packet: %v", err)
		return
	}

	// 复制修改后的数据
	rawData := pkt.GetRawPacketBytes()
	copy(rawData, data)

	if err := SendPacket(pkt); err != nil {
		log.Printf("[ERROR] Failed to send ping reply: %v", err)
	} else {
		log.Printf("[DEBUG] Ping reply sent to %s", srcIP.String())
	}
}
