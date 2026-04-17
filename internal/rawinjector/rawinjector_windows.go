//go:build windows

package rawinjector

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	winDivertLayerNetwork = 0

	winDivertFlagSniff    = 1
	winDivertFlagRecvOnly = 4
	winDivertFlagSendOnly = 8

	ipProtoTCP = 6
	tcpFIN     = 0x01
	tcpSYN     = 0x02
	tcpRST     = 0x04
	tcpPSH     = 0x08
	tcpACK     = 0x10
)

type winDivertAddress [64]byte

type winPortState struct {
	synSeq      uint32
	synSeen     bool
	fakeHello   []byte
	fakeSent    bool
	confirmedC  chan struct{}
	failedC     chan struct{}
	confirmOnce sync.Once
	failOnce    sync.Once
	lastAddr    winDivertAddress
	mu          sync.Mutex
}

type winInjector struct {
	localIP    [4]byte
	remoteIP   [4]byte
	remotePort int

	sniffHandle syscall.Handle
	sendHandle  syscall.Handle

	ports   map[int]*winPortState
	portsMu sync.RWMutex

	running atomic.Bool
	wg      sync.WaitGroup
}

var (
	winDivertDLL        = syscall.NewLazyDLL("WinDivert.dll")
	procWinDivertOpen   = winDivertDLL.NewProc("WinDivertOpen")
	procWinDivertRecv   = winDivertDLL.NewProc("WinDivertRecv")
	procWinDivertSend   = winDivertDLL.NewProc("WinDivertSend")
	procWinDivertClose  = winDivertDLL.NewProc("WinDivertClose")
	procHelperChecksums = winDivertDLL.NewProc("WinDivertHelperCalcChecksums")
)

func New(localIP, remoteIP string, remotePort int, _ func(string) []byte) Interface {
	out := &winInjector{
		remotePort: remotePort,
		ports:      make(map[int]*winPortState),
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
	if err := winDivertDLL.Load(); err != nil {
		setRawDiagnostic(fmt.Sprintf("WinDivert load failed: %v", err))
		return false
	}
	h, err := winDivertOpenWithFallback(
		[]string{"false"},
		[]uint64{winDivertFlagSendOnly},
	)
	if err != nil {
		setRawDiagnostic(fmt.Sprintf("WinDivert open failed (admin/driver/version mismatch?): %v", err))
		return false
	}
	_ = winDivertClose(h)
	setRawDiagnostic("")
	return true
}

func (i *winInjector) Start() bool {
	if i.sniffHandle != 0 && i.sendHandle != 0 {
		setRawDiagnostic("")
		return true
	}
	if i.localIP == [4]byte{} || i.remoteIP == [4]byte{} {
		setRawDiagnostic("invalid local/remote IPv4 for WinDivert injector")
		return false
	}
	if err := winDivertDLL.Load(); err != nil {
		setRawDiagnostic(fmt.Sprintf("WinDivert load failed: %v", err))
		return false
	}

	local := net.IP(i.localIP[:]).String()
	remote := net.IP(i.remoteIP[:]).String()
	sniffFilter := fmt.Sprintf(
		"tcp and ((ip.SrcAddr == %s and ip.DstAddr == %s and tcp.DstPort == %d) or (ip.SrcAddr == %s and ip.DstAddr == %s and tcp.SrcPort == %d))",
		local, remote, i.remotePort, remote, local, i.remotePort,
	)

	sniff, err := winDivertOpenWithFallback(
		[]string{sniffFilter, "ip and tcp"},
		[]uint64{winDivertFlagSniff | winDivertFlagRecvOnly, winDivertFlagSniff},
	)
	if err != nil {
		setRawDiagnostic(fmt.Sprintf("WinDivert sniff open failed (admin/driver/version mismatch?): %v", err))
		return false
	}
	send, err := winDivertOpenWithFallback(
		[]string{"false"},
		[]uint64{winDivertFlagSendOnly},
	)
	if err != nil {
		_ = winDivertClose(sniff)
		setRawDiagnostic(fmt.Sprintf("WinDivert send open failed (admin/driver/version mismatch?): %v", err))
		return false
	}
	i.sniffHandle = sniff
	i.sendHandle = send
	i.running.Store(true)
	setRawDiagnostic("")
	i.wg.Add(1)
	go i.sniffLoop()
	return true
}

func (i *winInjector) Stop() {
	if !i.running.Swap(false) {
		return
	}
	if i.sniffHandle != 0 {
		_ = winDivertClose(i.sniffHandle)
		i.sniffHandle = 0
	}
	if i.sendHandle != 0 {
		_ = winDivertClose(i.sendHandle)
		i.sendHandle = 0
	}
	i.wg.Wait()
}

func (i *winInjector) RegisterPort(localPort int, fakeHello []byte) {
	i.portsMu.Lock()
	defer i.portsMu.Unlock()
	i.ports[localPort] = &winPortState{
		fakeHello:  append([]byte(nil), fakeHello...),
		confirmedC: make(chan struct{}),
		failedC:    make(chan struct{}),
	}
}

func (i *winInjector) WaitForConfirmation(localPort int, timeout time.Duration) bool {
	return i.WaitForConfirmationDetailed(localPort, timeout) == ConfirmationStatusConfirmed
}

func (i *winInjector) WaitForConfirmationDetailed(localPort int, timeout time.Duration) ConfirmationStatus {
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

func (i *winInjector) CleanupPort(localPort int) {
	i.portsMu.Lock()
	defer i.portsMu.Unlock()
	delete(i.ports, localPort)
}

func (i *winInjector) markFailed(ps *winPortState) {
	ps.failOnce.Do(func() {
		close(ps.failedC)
	})
}

func (i *winInjector) sniffLoop() {
	defer i.wg.Done()
	buf := make([]byte, 65535)
	for i.running.Load() {
		var readLen uint32
		var addr winDivertAddress
		r1, _, _ := procWinDivertRecv.Call(
			uintptr(i.sniffHandle),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&readLen)),
			uintptr(unsafe.Pointer(&addr)),
		)
		if r1 == 0 || readLen == 0 {
			if !i.running.Load() {
				return
			}
			continue
		}
		if int(readLen) > len(buf) {
			continue
		}
		pkt := append([]byte(nil), buf[:readLen]...)
		i.handlePacket(pkt, addr)
	}
}

func (i *winInjector) handlePacket(pkt []byte, addr winDivertAddress) {
	if len(pkt) < 40 || (pkt[0]>>4) != 4 {
		return
	}
	if pkt[9] != ipProtoTCP {
		return
	}
	ihl := ipHeaderLen(pkt)
	if len(pkt) < ihl+20 {
		return
	}
	tcp := pkt[ihl:]
	tcpHdrLen := int((tcp[12] >> 4) * 4)
	if len(tcp) < tcpHdrLen || tcpHdrLen < 20 {
		return
	}

	flags := tcp[13]
	payloadLen := len(tcp) - tcpHdrLen
	srcIP := pkt[12:16]
	dstIP := pkt[16:20]
	srcPort := int(binary.BigEndian.Uint16(tcp[0:2]))
	dstPort := int(binary.BigEndian.Uint16(tcp[2:4]))

	outbound := equal4(srcIP, i.localIP[:]) && equal4(dstIP, i.remoteIP[:]) && dstPort == i.remotePort
	inbound := equal4(srcIP, i.remoteIP[:]) && equal4(dstIP, i.localIP[:]) && srcPort == i.remotePort

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
			if ps.fakeSent {
				ps.mu.Unlock()
				return
			}
			if !ps.synSeen || seq != ps.synSeq+1 {
				ps.mu.Unlock()
				return
			}
			ps.fakeSent = true
			ps.lastAddr = addr
			isn := ps.synSeq
			fake := append([]byte(nil), ps.fakeHello...)
			sendAddr := ps.lastAddr
			ps.mu.Unlock()

			tpl := append([]byte(nil), pkt...)
			go func() {
				time.Sleep(1 * time.Millisecond)
				frame, err := buildFakePacket(tpl, isn, fake)
				if err != nil {
					i.markFailed(ps)
					return
				}
				if err := winDivertCalcChecksums(frame); err != nil {
					i.markFailed(ps)
					return
				}
				if err := i.injectPacket(frame, sendAddr); err != nil {
					i.markFailed(ps)
				}
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
			confirmed := ps.fakeSent && ackNum == ps.synSeq+1
			ps.mu.Unlock()
			if confirmed {
				ps.confirmOnce.Do(func() { close(ps.confirmedC) })
			}
			return
		}
		if (flags & tcpRST) != 0 {
			i.portsMu.RLock()
			ps := i.ports[dstPort]
			i.portsMu.RUnlock()
			if ps != nil {
				i.markFailed(ps)
			}
		}
	}
}

func (i *winInjector) injectPacket(packet []byte, addr winDivertAddress) error {
	var writeLen uint32
	r1, _, e := procWinDivertSend.Call(
		uintptr(i.sendHandle),
		uintptr(unsafe.Pointer(&packet[0])),
		uintptr(len(packet)),
		uintptr(unsafe.Pointer(&writeLen)),
		uintptr(unsafe.Pointer(&addr)),
	)
	if r1 == 0 {
		if e != nil {
			return e
		}
		return syscall.EINVAL
	}
	if int(writeLen) != len(packet) {
		return syscall.EIO
	}
	return nil
}

func winDivertCalcChecksums(packet []byte) error {
	r1, _, e := procHelperChecksums.Call(
		uintptr(unsafe.Pointer(&packet[0])),
		uintptr(len(packet)),
		0,
		0,
	)
	if r1 == 0 {
		if e != nil {
			return e
		}
		return syscall.EINVAL
	}
	return nil
}

func winDivertOpen(filter string, layer uint32, priority int16, flags uint64) (syscall.Handle, error) {
	p, err := syscall.BytePtrFromString(filter)
	if err != nil {
		return 0, err
	}
	r1, _, e := procWinDivertOpen.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(layer),
		uintptr(priority),
		uintptr(flags),
	)
	h := syscall.Handle(r1)
	if h == 0 || h == syscall.InvalidHandle {
		if e != nil {
			return 0, e
		}
		return 0, syscall.EINVAL
	}
	return h, nil
}

func winDivertOpenWithFallback(filters []string, flagsList []uint64) (syscall.Handle, error) {
	attempts := make([]string, 0, len(filters)*len(flagsList))
	for _, f := range filters {
		for _, fl := range flagsList {
			h, err := winDivertOpen(f, winDivertLayerNetwork, 0, fl)
			if err == nil {
				return h, nil
			}
			attempts = append(attempts, fmt.Sprintf("filter=%q flags=%d err=%v", f, fl, err))
		}
	}
	if len(attempts) == 0 {
		return 0, syscall.EINVAL
	}
	return 0, fmt.Errorf("all WinDivertOpen attempts failed: %s", strings.Join(attempts, "; "))
}

func winDivertClose(h syscall.Handle) error {
	r1, _, e := procWinDivertClose.Call(uintptr(h))
	if r1 == 0 {
		if e != nil {
			return e
		}
		return syscall.EINVAL
	}
	return nil
}

func buildFakePacket(template []byte, isn uint32, fakePayload []byte) ([]byte, error) {
	if len(template) < 40 {
		return nil, syscall.EINVAL
	}
	ipOff := 0
	ihl := ipHeaderLen(template)
	tcpOff := ipOff + ihl
	if len(template) < tcpOff+20 {
		return nil, syscall.EINVAL
	}
	tcpHdrLen := int((template[tcpOff+12] >> 4) * 4)
	if len(template) < tcpOff+tcpHdrLen {
		return nil, syscall.EINVAL
	}

	headers := append([]byte(nil), template[:tcpOff+tcpHdrLen]...)
	out := append(headers, fakePayload...)

	binary.BigEndian.PutUint16(out[ipOff+2:ipOff+4], uint16(len(out)-ipOff))
	oldID := binary.BigEndian.Uint16(out[ipOff+4 : ipOff+6])
	binary.BigEndian.PutUint16(out[ipOff+4:ipOff+6], oldID+1)

	out[ipOff+10] = 0
	out[ipOff+11] = 0
	binary.BigEndian.PutUint16(out[ipOff+10:ipOff+12], ipChecksum(out[ipOff:ipOff+ihl]))

	out[tcpOff+13] |= tcpPSH
	seq := isn + 1 - uint32(len(fakePayload))
	binary.BigEndian.PutUint32(out[tcpOff+4:tcpOff+8], seq)

	out[tcpOff+16] = 0
	out[tcpOff+17] = 0
	binary.BigEndian.PutUint16(out[tcpOff+16:tcpOff+18], tcpChecksum(out[ipOff:ipOff+ihl], out[tcpOff:]))

	return out, nil
}

func equal4(a, b []byte) bool {
	return len(a) >= 4 && len(b) >= 4 && a[0] == b[0] && a[1] == b[1] && a[2] == b[2] && a[3] == b[3]
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
	for j := 0; j+1 < len(data); j += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[j : j+2]))
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
