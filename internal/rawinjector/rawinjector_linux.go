//go:build linux

package rawinjector

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"snispf/internal/logx"
)

const (
	ethPIP  = 0x0800
	ethPAll = 0x0003

	ipProtoTCP = 6

	tcpFIN = 0x01
	tcpSYN = 0x02
	tcpRST = 0x04
	tcpPSH = 0x08
	tcpACK = 0x10
)

type portState struct {
	synSeq      uint32
	synSeen     bool
	fakeHello   []byte
	fakeSent    bool
	lastOutSeq  uint32
	lastAckNum  uint32
	lastFlags   uint8
	lastPayload int
	lastEvent   string
	confirmedC  chan struct{}
	failedC     chan struct{}
	confirmOnce sync.Once
	failOnce    sync.Once
	mu          sync.Mutex
}

type injector struct {
	localIP    [4]byte
	remoteIP   [4]byte
	remotePort int

	fd      int
	ifIndex int
	routeMu sync.Mutex

	ports   map[int]*portState
	portsMu sync.RWMutex

	running atomic.Bool
	wg      sync.WaitGroup
}

func New(localIP, remoteIP string, remotePort int, _ func(string) []byte) Interface {
	out := &injector{
		remotePort: remotePort,
		ports:      make(map[int]*portState),
		fd:         -1,
	}
	if lip := net.ParseIP(localIP).To4(); lip != nil {
		copy(out.localIP[:], lip)
	}
	if rip := net.ParseIP(remoteIP).To4(); rip != nil {
		copy(out.remoteIP[:], rip)
	}
	return out
}

func IsRawAvailable() bool {
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(htons(ethPAll)))
	if err != nil {
		return false
	}
	_ = syscall.Close(fd)
	return true
}

func (i *injector) Start() bool {
	if i.fd >= 0 {
		return true
	}
	if i.remoteIP == [4]byte{} {
		setRawDiagnostic("linux raw injector: remote IPv4 is empty/invalid")
		return false
	}

	// Use SOCK_DGRAM so the kernel provides network-layer packets without
	// requiring fixed L2 frame assumptions. This is more robust on PPP-like WANs.
	// Use ETH_P_ALL so capture still sees IPv4 traffic on PPPoE/mixed link-layer
	// paths where ETH_P_IP filtering can miss packets.
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(htons(ethPAll)))
	if err != nil {
		setRawDiagnostic(fmt.Sprintf("linux raw injector socket(AF_PACKET,SOCK_DGRAM) failed: %v", err))
		return false
	}

	idx := i.findRouteInterfaceIndex()
	if err := syscall.Bind(fd, &syscall.SockaddrLinklayer{
		Protocol: htons(ethPAll),
		Ifindex:  idx,
	}); err != nil {
		// Fallback to all interfaces capture to survive route/interface churn.
		if err2 := syscall.Bind(fd, &syscall.SockaddrLinklayer{Protocol: htons(ethPAll), Ifindex: 0}); err2 != nil {
			setRawDiagnostic(fmt.Sprintf("linux raw injector bind failed for ifindex=%d (%v) and any-if (%v)", idx, err, err2))
			_ = syscall.Close(fd)
			return false
		}
		idx = 0
	}

	if i.localIP == [4]byte{} {
		if lip, _, ok := i.routeLocalIPAndIndex(); ok {
			copy(i.localIP[:], lip)
		}
	}

	i.fd = fd
	i.ifIndex = idx
	i.running.Store(true)
	i.wg.Add(1)
	go i.sniffLoop()
	if idx == 0 {
		logx.Infof("raw injector active with wildcard capture (route-aware send ifindex)")
	} else {
		logx.Infof("raw injector active on ifindex=%d", idx)
	}
	setRawDiagnostic("")
	return true
}

func (i *injector) routeLocalIPAndIndex() ([4]byte, int, bool) {
	var out [4]byte
	if i.remoteIP == [4]byte{} {
		return out, 0, false
	}
	remote := net.IP(i.remoteIP[:]).String()
	c, err := net.Dial("udp4", net.JoinHostPort(remote, "53"))
	if err != nil {
		return out, 0, false
	}
	defer c.Close()
	ua, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok || ua.IP == nil {
		return out, 0, false
	}
	lip := ua.IP.To4()
	if lip == nil {
		return out, 0, false
	}
	idx := findInterfaceIndexByIP(lip)
	if idx == 0 {
		return out, 0, false
	}
	copy(out[:], lip)
	return out, idx, true
}

func findInterfaceIndexByIP(target net.IP) int {
	interfaces, err := net.Interfaces()
	if err != nil {
		return 0
	}
	for _, itf := range interfaces {
		addrs, err := itf.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipNet.IP.To4(); ip4 != nil && ip4.Equal(target) {
				return itf.Index
			}
		}
	}
	return 0
}

func (i *injector) findRouteInterfaceIndex() int {
	if _, idx, ok := i.routeLocalIPAndIndex(); ok {
		return idx
	}
	return i.findInterfaceIndex()
}

func (i *injector) findInterfaceIndex() int {
	if i.localIP == [4]byte{} {
		return 0
	}
	target := net.IP(i.localIP[:])
	return findInterfaceIndexByIP(target)
}

func (i *injector) chooseSendIfindex() int {
	i.routeMu.Lock()
	defer i.routeMu.Unlock()
	if lip, idx, ok := i.routeLocalIPAndIndex(); ok {
		if idx != i.ifIndex {
			logx.Warnf("raw injector send route changed old_ifindex=%d new_ifindex=%d", i.ifIndex, idx)
		}
		i.ifIndex = idx
		copy(i.localIP[:], lip)
	}
	return i.ifIndex
}

func (i *injector) Stop() {
	if !i.running.Swap(false) {
		return
	}
	if i.fd >= 0 {
		_ = syscall.Close(i.fd)
		i.fd = -1
	}
	i.wg.Wait()
}

func (i *injector) RegisterPort(localPort int, fakeHello []byte) {
	i.portsMu.Lock()
	defer i.portsMu.Unlock()
	i.ports[localPort] = &portState{
		fakeHello:  append([]byte(nil), fakeHello...),
		confirmedC: make(chan struct{}),
		failedC:    make(chan struct{}),
	}
}

func (i *injector) WaitForConfirmation(localPort int, timeout time.Duration) bool {
	return i.WaitForConfirmationDetailed(localPort, timeout) == ConfirmationStatusConfirmed
}

func (i *injector) WaitForConfirmationDetailed(localPort int, timeout time.Duration) ConfirmationStatus {
	i.portsMu.RLock()
	ps := i.ports[localPort]
	i.portsMu.RUnlock()
	if ps == nil {
		return ConfirmationStatusNotRegistered
	}
	if timeout <= 0 {
		select {
		case <-ps.confirmedC:
			return ConfirmationStatusConfirmed
		case <-ps.failedC:
			return ConfirmationStatusFailed
		default:
			return ConfirmationStatusTimeout
		}
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-ps.confirmedC:
		return ConfirmationStatusConfirmed
	case <-ps.failedC:
		return ConfirmationStatusFailed
	case <-t.C:
		return ConfirmationStatusTimeout
	}
}

func (i *injector) markFailed(ps *portState) {
	ps.mu.Lock()
	ps.lastEvent = "mark_failed"
	ps.mu.Unlock()
	ps.failOnce.Do(func() {
		close(ps.failedC)
	})
}

func (i *injector) DebugPortState(localPort int) string {
	i.portsMu.RLock()
	ps := i.ports[localPort]
	i.portsMu.RUnlock()
	if ps == nil {
		return "port_state=missing"
	}
	ps.mu.Lock()
	synSeen := ps.synSeen
	fakeSent := ps.fakeSent
	synSeq := ps.synSeq
	lastOutSeq := ps.lastOutSeq
	lastAckNum := ps.lastAckNum
	lastFlags := ps.lastFlags
	lastPayload := ps.lastPayload
	lastEvent := ps.lastEvent
	ps.mu.Unlock()

	confirmed := channelClosed(ps.confirmedC)
	failed := channelClosed(ps.failedC)

	return fmt.Sprintf(
		"raw_state={syn_seen=%t fake_sent=%t syn_seq=%d last_out_seq=%d last_ack=%d last_flags=0x%02x last_payload=%d confirmed=%t failed=%t last_event=%q}",
		synSeen, fakeSent, synSeq, lastOutSeq, lastAckNum, lastFlags, lastPayload, confirmed, failed, lastEvent,
	)
}

func (i *injector) CleanupPort(localPort int) {
	i.portsMu.Lock()
	defer i.portsMu.Unlock()
	delete(i.ports, localPort)
}

func (i *injector) sniffLoop() {
	defer i.wg.Done()
	buf := make([]byte, 65536)
	for i.running.Load() {
		n, _, err := syscall.Recvfrom(i.fd, buf, 0)
		if err != nil {
			if i.running.Load() {
				continue
			}
			return
		}
		pkt := append([]byte(nil), buf[:n]...)
		i.handlePacket(pkt)
	}
}

func (i *injector) handlePacket(pkt []byte) {
	ipOff := ipv4Offset(pkt)
	if ipOff < 0 {
		return
	}
	ip := pkt[ipOff:]
	if len(ip) < 20 || (ip[0]>>4) != 4 || ip[9] != ipProtoTCP {
		return
	}
	ihl := ipHeaderLen(ip)
	if len(ip) < ihl+20 {
		return
	}
	tcp := ip[ihl:]
	tcpHdrLen := int((tcp[12] >> 4) * 4)
	if len(tcp) < tcpHdrLen || tcpHdrLen < 20 {
		return
	}

	flags := tcp[13]
	payloadLen := len(tcp) - tcpHdrLen
	srcIP := ip[12:16]
	dstIP := ip[16:20]
	srcPort := int(binary.BigEndian.Uint16(tcp[0:2]))
	dstPort := int(binary.BigEndian.Uint16(tcp[2:4]))

	if !equal4(srcIP, i.remoteIP[:]) && !equal4(dstIP, i.remoteIP[:]) {
		return
	}

	i.portsMu.RLock()
	_, hasSrc := i.ports[srcPort]
	_, hasDst := i.ports[dstPort]
	i.portsMu.RUnlock()

	outbound := equal4(dstIP, i.remoteIP[:]) && dstPort == i.remotePort && hasSrc
	inbound := equal4(srcIP, i.remoteIP[:]) && srcPort == i.remotePort && hasDst

	if outbound {
		seq := binary.BigEndian.Uint32(tcp[4:8])

		if (flags&tcpSYN) != 0 && (flags&tcpACK) == 0 {
			i.portsMu.RLock()
			ps := i.ports[srcPort]
			i.portsMu.RUnlock()
			if ps != nil {
				ps.mu.Lock()
				ps.synSeq = seq
				ps.synSeen = true
				ps.lastOutSeq = seq
				ps.lastFlags = flags
				ps.lastPayload = payloadLen
				ps.lastEvent = "outbound_syn_seen"
				ps.mu.Unlock()
			}
			return
		}

		if (flags&tcpACK) != 0 && (flags&(tcpSYN|tcpFIN|tcpRST)) == 0 && payloadLen == 0 {
			i.portsMu.RLock()
			ps := i.ports[srcPort]
			i.portsMu.RUnlock()
			if ps == nil {
				return
			}
			ps.mu.Lock()
			ps.lastOutSeq = seq
			ps.lastFlags = flags
			ps.lastPayload = payloadLen
			if ps.fakeSent {
				ps.lastEvent = "outbound_ack_seen_after_fake"
				ps.mu.Unlock()
				return
			}
			if !ps.synSeen {
				ps.lastEvent = "outbound_ack_before_syn"
				ps.mu.Unlock()
				return
			}
			if seq != ps.synSeq+1 {
				ps.lastEvent = "outbound_ack_seq_mismatch"
				ps.mu.Unlock()
				return
			}
			ps.fakeSent = true
			isn := ps.synSeq
			fake := append([]byte(nil), ps.fakeHello...)
			ps.lastEvent = "fake_send_scheduled"
			ps.mu.Unlock()

			tpl := append([]byte(nil), ip...)
			go func() {
				time.Sleep(1 * time.Millisecond)
				frame, err := buildFakeFrame(tpl, isn, fake)
				if err != nil {
					ps.mu.Lock()
					ps.lastEvent = "fake_build_failed"
					ps.mu.Unlock()
					i.markFailed(ps)
					return
				}
				if err := i.injectFrame(frame); err != nil {
					ps.mu.Lock()
					ps.lastEvent = "fake_inject_failed"
					ps.mu.Unlock()
					i.markFailed(ps)
					return
				}
				ps.mu.Lock()
				ps.lastEvent = "fake_injected"
				ps.mu.Unlock()
			}()
			return
		}
	}

	if inbound {
		ackNum := binary.BigEndian.Uint32(tcp[8:12])

		// Confirm on inbound ACK packets that are not SYN/FIN/RST.
		// Some servers send ACK+data as the first post-handshake packet; requiring
		// payloadLen==0 causes false timeouts in strict mode.
		if (flags&tcpACK) != 0 && (flags&(tcpSYN|tcpFIN|tcpRST)) == 0 {
			i.portsMu.RLock()
			ps := i.ports[dstPort]
			i.portsMu.RUnlock()
			if ps == nil {
				return
			}
			ps.mu.Lock()
			ps.lastAckNum = ackNum
			ps.lastFlags = flags
			ps.lastPayload = payloadLen
			ps.lastEvent = "inbound_ack_seen"
			confirmed := ps.fakeSent && ackNum == ps.synSeq+1
			if confirmed {
				ps.lastEvent = "confirmed_ack_match"
			}
			ps.mu.Unlock()
			if confirmed {
				ps.confirmOnce.Do(func() {
					close(ps.confirmedC)
				})
			}
			return
		}

		if (flags & tcpRST) != 0 {
			i.portsMu.RLock()
			ps := i.ports[dstPort]
			i.portsMu.RUnlock()
			if ps != nil {
				ps.mu.Lock()
				ps.lastAckNum = ackNum
				ps.lastFlags = flags
				ps.lastPayload = payloadLen
				ps.lastEvent = "inbound_rst_seen"
				ps.mu.Unlock()
				i.markFailed(ps)
			}
		}
	}
}

func channelClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (i *injector) injectFrame(frame []byte) error {
	if len(frame) < 20 || (frame[0]>>4) != 4 {
		return syscall.EINVAL
	}
	ifidx := i.chooseSendIfindex()
	if ifidx <= 0 {
		err := fmt.Errorf("linux raw injector: no route interface available for send")
		setRawDiagnostic(err.Error())
		return err
	}
	return syscall.Sendto(i.fd, frame, 0, &syscall.SockaddrLinklayer{
		Protocol: htons(ethPIP),
		Ifindex:  ifidx,
	})
}

func buildFakeFrame(template []byte, isn uint32, fakePayload []byte) ([]byte, error) {
	if len(template) < 20 || (template[0]>>4) != 4 {
		return nil, syscall.EINVAL
	}
	ihl := ipHeaderLen(template)
	tcpOff := ihl
	if len(template) < tcpOff+20 {
		return nil, syscall.EINVAL
	}
	tcpHdrLen := int((template[tcpOff+12] >> 4) * 4)
	if len(template) < tcpOff+tcpHdrLen {
		return nil, syscall.EINVAL
	}

	headers := append([]byte(nil), template[:tcpOff+tcpHdrLen]...)
	out := append(headers, fakePayload...)

	binary.BigEndian.PutUint16(out[2:4], uint16(len(out)))
	oldID := binary.BigEndian.Uint16(out[4:6])
	binary.BigEndian.PutUint16(out[4:6], oldID+1)

	out[10] = 0
	out[11] = 0
	ipCk := ipChecksum(out[:ihl])
	binary.BigEndian.PutUint16(out[10:12], ipCk)

	out[tcpOff+13] |= tcpPSH
	seq := isn + 1 - uint32(len(fakePayload))
	binary.BigEndian.PutUint32(out[tcpOff+4:tcpOff+8], seq)

	out[tcpOff+16] = 0
	out[tcpOff+17] = 0
	tcpCk := tcpChecksum(out[:ihl], out[tcpOff:])
	binary.BigEndian.PutUint16(out[tcpOff+16:tcpOff+18], tcpCk)

	return out, nil
}

func ipv4Offset(pkt []byte) int {
	if len(pkt) >= 20 && (pkt[0]>>4) == 4 {
		return 0
	}
	if len(pkt) >= 34 && binary.BigEndian.Uint16(pkt[12:14]) == ethPIP && (pkt[14]>>4) == 4 {
		return 14
	}
	return -1
}

func equal4(a, b []byte) bool {
	return len(a) >= 4 && len(b) >= 4 && a[0] == b[0] && a[1] == b[1] && a[2] == b[2] && a[3] == b[3]
}

func htons(v uint16) uint16 {
	return (v<<8)&0xff00 | (v>>8)&0x00ff
}

func ipHeaderLen(ip []byte) int {
	return int(ip[0]&0x0f) * 4
}

func checksumFold(sum uint32) uint16 {
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func sum16(data []byte) uint32 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	return sum
}

func ipChecksum(iph []byte) uint16 {
	return checksumFold(sum16(iph))
}

func tcpChecksum(iph []byte, tcpPayload []byte) uint16 {
	pseudo := make([]byte, 12)
	copy(pseudo[0:4], iph[12:16])
	copy(pseudo[4:8], iph[16:20])
	pseudo[9] = ipProtoTCP
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(tcpPayload)))
	return checksumFold(sum16(pseudo) + sum16(tcpPayload))
}
