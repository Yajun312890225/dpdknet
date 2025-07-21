package dpdknet

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/Yajun312890225/nff-go/flow"
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
	rxFlow  *flow.Flow
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

// HandleICMPManual 手动处理ICMP包
func HandleICMPManual(pkt *packet.Packet, data []byte, ipHeaderStart, headerLength int) {
	icmpStart := ipHeaderStart + headerLength

	if len(data) < icmpStart+8 {
		return
	}

	icmpType := data[icmpStart]
	icmpCode := data[icmpStart+1]

	// 只处理ICMP Echo Request (type=8, code=0)
	if icmpType != ICMPEchoRequest || icmpCode != 0 {
		return
	}

	// 解析IP地址
	srcIP := fmt.Sprintf("%d.%d.%d.%d",
		data[ipHeaderStart+12], data[ipHeaderStart+13],
		data[ipHeaderStart+14], data[ipHeaderStart+15])
	dstIP := fmt.Sprintf("%d.%d.%d.%d",
		data[ipHeaderStart+16], data[ipHeaderStart+17],
		data[ipHeaderStart+18], data[ipHeaderStart+19])

	log.Printf("ICMP ping: %s -> %s", srcIP, dstIP)

	// 交换源和目标IP地址
	for i := 0; i < 4; i++ {
		data[ipHeaderStart+12+i], data[ipHeaderStart+16+i] = data[ipHeaderStart+16+i], data[ipHeaderStart+12+i]
	}

	// 将ICMP Echo Request改为Echo Reply
	data[icmpStart] = ICMPEchoReply
	data[icmpStart+1] = 0

	// 重新计算ICMP校验和
	data[icmpStart+2] = 0 // 清零校验和
	data[icmpStart+3] = 0

	// 计算新的ICMP校验和
	icmpLen := len(data) - icmpStart
	checksum := CalcChecksum(data[icmpStart : icmpStart+icmpLen])
	binary.BigEndian.PutUint16(data[icmpStart+2:icmpStart+4], checksum)

	// 重新计算IPv4头校验和
	data[ipHeaderStart+10] = 0 // 清零校验和
	data[ipHeaderStart+11] = 0

	ipChecksum := CalcChecksum(data[ipHeaderStart : ipHeaderStart+headerLength])
	binary.BigEndian.PutUint16(data[ipHeaderStart+10:ipHeaderStart+12], ipChecksum)
}

func NewICMPConn(localIP net.IP) (*ICMPConn, error) {
	if err := Init(); err != nil {
		return nil, err
	}

	rxFlow, err := flow.SetReceiver(0)
	if err != nil {
		return nil, err
	}

	conn := &ICMPConn{
		localIP: localIP,
		id:      uint16(time.Now().Unix() & 0xFFFF),
		seq:     0,
		rxFlow:  rxFlow,
		replyCh: make(chan *ICMPReply, 1024),
	}

	// 设置ICMP包处理器
	flow.SetHandler(rxFlow, conn.handleICMPPacket, nil)

	return conn, nil
}

func (c *ICMPConn) handleICMPPacket(pkt *packet.Packet, ctx flow.UserContext) {
	data := pkt.GetRawPacketBytes()
	if len(data) < 14 {
		return
	}

	// 检查是否为IPv4
	etherType := binary.BigEndian.Uint16(data[12:14])
	if etherType != 0x0800 {
		return
	}

	// 检查是否为ICMP
	ipHeaderStart := 14
	if len(data) < ipHeaderStart+20 {
		return
	}

	protocol := data[ipHeaderStart+9]
	if protocol != 1 { // ICMP协议
		return
	}

	headerLength := int((data[ipHeaderStart] & 0x0F) * 4)
	icmpStart := ipHeaderStart + headerLength

	if len(data) < icmpStart+8 {
		return
	}

	icmpType := data[icmpStart]
	if icmpType == ICMPEchoReply {
		// 解析ICMP Echo Reply
		id := binary.BigEndian.Uint16(data[icmpStart+4 : icmpStart+6])
		seq := binary.BigEndian.Uint16(data[icmpStart+6 : icmpStart+8])

		srcIP := net.IPv4(data[ipHeaderStart+12], data[ipHeaderStart+13],
			data[ipHeaderStart+14], data[ipHeaderStart+15])

		reply := &ICMPReply{
			Addr:     srcIP,
			ID:       id,
			Sequence: seq,
			RTT:      time.Now().Sub(time.Now()), // 这里应该计算实际的RTT
		}

		select {
		case c.replyCh <- reply:
		default:
			// 如果通道满了，丢弃这个回复
		}
	}
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
