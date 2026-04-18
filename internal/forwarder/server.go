package forwarder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"snispf/internal/bypass"
	"snispf/internal/logx"
	"snispf/internal/rawinjector"
	"snispf/internal/tlsclienthello"
	"snispf/internal/utils"
)

type Server struct {
	ListenHost      string
	ListenPort      int
	ConnectIP       string
	ConnectPort     int
	FakeSNI         string
	Endpoints       []utils.Endpoint
	LoadBalance     string
	AutoFailover    bool
	FailoverRetries int
	InterfaceIP     string
	Strategy        bypass.Strategy
	Injector        rawinjector.Interface
	lbCounter       atomic.Uint64
}

func (s *Server) Run(ctx context.Context) error {
	laddr, err := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%s:%d", s.ListenHost, s.ListenPort))
	if err != nil {
		return err
	}
	listener, err := net.ListenTCP("tcp4", laddr)
	if err != nil {
		return err
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	logx.Infof("listening on %s:%d", s.ListenHost, s.ListenPort)
	for {
		conn, err := listener.AcceptTCP()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				continue
			}
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, incoming *net.TCPConn) {
	defer incoming.Close()
	_ = incoming.SetReadDeadline(time.Now().Add(30 * time.Second))
	first := make([]byte, 65535)
	n, err := incoming.Read(first)
	if err != nil || n == 0 {
		return
	}
	first = first[:n]
	_ = incoming.SetReadDeadline(time.Time{})

	parsed := tlsclienthello.ParseClientHello(first)
	logx.Debugf("incoming sni=%v bypass=%s", parsed["sni"], s.Strategy.Name())
	requestSNI, _ := parsed["sni"].(string)

	endpoints := s.endpointsOrDefault()
	base := s.pickBaseIndex(len(endpoints))
	retries := 0
	if strings.EqualFold(s.LoadBalance, "failover") {
		// In explicit failover mode, try each endpoint once per incoming connection.
		retries = len(endpoints) - 1
	}
	if s.AutoFailover && s.FailoverRetries > retries {
		retries = s.FailoverRetries
	}
	maxRetries := len(endpoints) - 1
	if retries > maxRetries {
		retries = maxRetries
	}
	totalAttempts := retries + 1

	var outgoing *net.TCPConn
	var selected utils.Endpoint
	var registeredPort int
	registered := false
	var lastConnectErr error
	for attempt := 0; attempt < totalAttempts; attempt++ {
		selected = endpoints[(base+attempt)%len(endpoints)]

		raddr, err := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%s:%d", selected.IP, selected.Port))
		if err != nil {
			lastConnectErr = err
			logx.Warnf("upstream resolve failed endpoint=%s:%d err=%v", selected.IP, selected.Port, err)
			continue
		}

		dynamicIP := utils.GetDefaultInterfaceIPv4(selected.IP)
		bindIP := s.InterfaceIP
		if s.Injector == nil && strings.TrimSpace(dynamicIP) != "" {
			// In non-raw modes, pick source IP per selected upstream endpoint
			// so multi-WAN route changes are reflected without process restart.
			bindIP = dynamicIP
		}
		if s.Injector != nil && s.InterfaceIP != "" && strings.TrimSpace(dynamicIP) != "" && dynamicIP != s.InterfaceIP {
			logx.Warnf("raw injector route-change detected old_local_ip=%s new_local_ip=%s endpoint=%s; restart service to rebind injector", s.InterfaceIP, dynamicIP, selected.IP)
		}

		var laddr *net.TCPAddr
		if bindIP != "" {
			laddr = &net.TCPAddr{IP: net.ParseIP(bindIP)}
		}

		if s.Injector != nil {
			reservedPort, reserveErr := reserveTCPPort(laddr)
			if reserveErr != nil {
				continue
			}
			if laddr == nil {
				laddr = &net.TCPAddr{IP: net.IPv4zero, Port: reservedPort}
			} else {
				laddr = &net.TCPAddr{IP: laddr.IP, Port: reservedPort}
			}
			s.Injector.RegisterPort(reservedPort, tlsclienthello.BuildClientHello(selected.SNI))
			registeredPort = reservedPort
			registered = true
		}

		outgoing, err = net.DialTCP("tcp4", laddr, raddr)
		if err == nil {
			break
		}
		lastConnectErr = err
		logx.Warnf("upstream dial failed endpoint=%s:%d local_ip=%s attempt=%d/%d err=%v", selected.IP, selected.Port, bindIP, attempt+1, totalAttempts, err)
		if registered {
			s.Injector.CleanupPort(registeredPort)
			registered = false
			registeredPort = 0
		}
	}
	if outgoing == nil {
		logx.Warnf("connection dropped before bypass: request_sni=%q reason=upstream_unreachable last_error=%v", requestSNI, lastConnectErr)
		return
	}
	defer outgoing.Close()
	if registered {
		defer s.Injector.CleanupPort(registeredPort)
	}
	_ = outgoing.SetKeepAlive(true)
	_ = outgoing.SetKeepAlivePeriod(60 * time.Second)
	logx.Debugf("selected endpoint ip=%s port=%d sni=%s", selected.IP, selected.Port, selected.SNI)

	if ok := s.Strategy.Apply(ctx, incoming, outgoing, selected.SNI, first); !ok {
		if s.Strategy.Name() == "wrong_seq" {
			diag := strings.TrimSpace(rawinjector.RawDiagnostic())
			if diag != "" {
				logx.Warnf("connection dropped before upstream first-write: strategy=wrong_seq request_sni=%q endpoint=%s:%d reason=strategy_apply_failed detail=%s", requestSNI, selected.IP, selected.Port, diag)
			} else {
				logx.Warnf("connection dropped before upstream first-write: strategy=wrong_seq request_sni=%q endpoint=%s:%d reason=strategy_apply_failed", requestSNI, selected.IP, selected.Port)
			}
			return
		}
		logx.Warnf("strategy apply returned false: strategy=%s request_sni=%q endpoint=%s:%d; falling back to direct first-write", s.Strategy.Name(), requestSNI, selected.IP, selected.Port)
		_, _ = outgoing.Write(first)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, copyErr := io.Copy(outgoing, incoming); copyErr != nil && !errors.Is(copyErr, io.EOF) && !errors.Is(copyErr, net.ErrClosed) {
			logx.Debugf("stream copy incoming->outgoing ended with error: %v", copyErr)
		}
		_ = outgoing.CloseWrite()
	}()
	go func() {
		defer wg.Done()
		if _, copyErr := io.Copy(incoming, outgoing); copyErr != nil && !errors.Is(copyErr, io.EOF) && !errors.Is(copyErr, net.ErrClosed) {
			logx.Debugf("stream copy outgoing->incoming ended with error: %v", copyErr)
		}
		_ = incoming.CloseWrite()
	}()
	wg.Wait()
}

func (s *Server) endpointsOrDefault() []utils.Endpoint {
	if len(s.Endpoints) > 0 {
		return s.Endpoints
	}
	return []utils.Endpoint{{IP: s.ConnectIP, Port: s.ConnectPort, SNI: s.FakeSNI, Enabled: true}}
}

func (s *Server) pickBaseIndex(total int) int {
	if total <= 1 {
		return 0
	}
	switch s.LoadBalance {
	case "random":
		return rand.Intn(total)
	case "failover":
		return 0
	default:
		return int(s.lbCounter.Add(1)-1) % total
	}
}

func reserveTCPPort(laddr *net.TCPAddr) (int, error) {
	bindIP := net.IPv4zero
	if laddr != nil && laddr.IP != nil {
		bindIP = laddr.IP
	}
	ln, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: bindIP, Port: 0})
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}
