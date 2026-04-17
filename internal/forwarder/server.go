package forwarder

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
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
	if s.AutoFailover {
		retries = s.FailoverRetries
	}

	var outgoing *net.TCPConn
	var selected utils.Endpoint
	var registeredPort int
	registered := false
	var lastConnectErr error
	for attempt := 0; attempt <= retries; attempt++ {
		selected = endpoints[(base+attempt)%len(endpoints)]

		raddr, err := net.ResolveTCPAddr("tcp4", fmt.Sprintf("%s:%d", selected.IP, selected.Port))
		if err != nil {
			lastConnectErr = err
			logx.Warnf("upstream resolve failed endpoint=%s:%d err=%v", selected.IP, selected.Port, err)
			continue
		}

		var laddr *net.TCPAddr
		if s.InterfaceIP != "" {
			laddr = &net.TCPAddr{IP: net.ParseIP(s.InterfaceIP)}
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
		logx.Warnf("upstream dial failed endpoint=%s:%d local_ip=%s attempt=%d/%d err=%v", selected.IP, selected.Port, s.InterfaceIP, attempt+1, retries+1, err)
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
			return
		}
		_, _ = outgoing.Write(first)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(outgoing, incoming)
		_ = outgoing.CloseWrite()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(incoming, outgoing)
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
