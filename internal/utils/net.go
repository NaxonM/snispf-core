package utils

import (
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type Config struct {
	ListenHost               string     `json:"LISTEN_HOST"`
	ListenPort               int        `json:"LISTEN_PORT"`
	LogLevel                 string     `json:"LOG_LEVEL,omitempty"`
	ConnectIP                string     `json:"CONNECT_IP"`
	ConnectPort              int        `json:"CONNECT_PORT"`
	FakeSNI                  string     `json:"FAKE_SNI"`
	Listeners                []Listener `json:"LISTENERS,omitempty"`
	BypassMethod             string     `json:"BYPASS_METHOD"`
	FragmentStrategy         string     `json:"FRAGMENT_STRATEGY"`
	FragmentDelay            float64    `json:"FRAGMENT_DELAY"`
	UseTTLTrick              bool       `json:"USE_TTL_TRICK"`
	FakeSNIMethod            string     `json:"FAKE_SNI_METHOD"`
	Endpoints                []Endpoint `json:"ENDPOINTS,omitempty"`
	LoadBalance              string     `json:"LOAD_BALANCE,omitempty"`
	EndpointProbe            bool       `json:"ENDPOINT_PROBE,omitempty"`
	AutoFailover             bool       `json:"AUTO_FAILOVER,omitempty"`
	FailoverRetries          int        `json:"FAILOVER_RETRIES,omitempty"`
	ProbeTimeoutMS           int        `json:"PROBE_TIMEOUT_MS,omitempty"`
	WrongSeqConfirmTimeoutMS int        `json:"WRONG_SEQ_CONFIRM_TIMEOUT_MS,omitempty"`
}

type Listener struct {
	Name         string `json:"NAME,omitempty"`
	ListenHost   string `json:"LISTEN_HOST"`
	ListenPort   int    `json:"LISTEN_PORT"`
	ConnectIP    string `json:"CONNECT_IP"`
	ConnectPort  int    `json:"CONNECT_PORT"`
	FakeSNI      string `json:"FAKE_SNI"`
	BypassMethod string `json:"BYPASS_METHOD,omitempty"`
}

type Endpoint struct {
	Name    string `json:"NAME,omitempty"`
	IP      string `json:"IP"`
	Port    int    `json:"PORT"`
	SNI     string `json:"SNI"`
	Enabled bool   `json:"ENABLED,omitempty"`
}

type PlatformCapabilities struct {
	Platform      string
	Fragment      bool
	TLSRecordFrag bool
	FakeSNI       bool
	TCPNoDelay    bool
	RawSocket     bool
	IPTTLTrick    bool
	AFPacket      bool
	RawInjection  bool
}

func GetDefaultInterfaceIPv4(dest string) string {
	c, err := net.Dial("udp4", net.JoinHostPort(dest, "53"))
	if err != nil {
		return ""
	}
	defer c.Close()
	if addr, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

func ResolveHost(host string) string {
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return host
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ips[0].String()
}

func IsValidPort(port int) bool {
	return port >= 1 && port <= 65535
}

func ParseHostPort(addr, defaultHost string, defaultPort int) (string, int, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return defaultHost, defaultPort, nil
	}
	if strings.HasPrefix(addr, ":") {
		p, err := strconv.Atoi(addr[1:])
		if err != nil {
			return "", 0, fmt.Errorf("invalid port: %q", addr)
		}
		return defaultHost, p, nil
	}
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, defaultPort, nil
	}
	host := addr[:i]
	if host == "" {
		host = defaultHost
	}
	p, err := strconv.Atoi(addr[i+1:])
	if err != nil {
		return "", 0, fmt.Errorf("invalid address: %q", addr)
	}
	return host, p, nil
}

func CheckPlatformCapabilities(rawInjectionAvailable bool) PlatformCapabilities {
	c := PlatformCapabilities{
		Platform:      runtime.GOOS,
		Fragment:      true,
		TLSRecordFrag: true,
		FakeSNI:       true,
		TCPNoDelay:    true,
		RawSocket:     false,
		IPTTLTrick:    false,
		AFPacket:      false,
		RawInjection:  rawInjectionAvailable,
	}

	if runtime.GOOS != "windows" {
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_TCP)
		if err == nil {
			c.RawSocket = true
			c.IPTTLTrick = true
			_ = syscall.Close(fd)
		}
	}

	if runtime.GOOS == "linux" {
		c.AFPacket = hasAFPacketSupport()
	}

	return c
}
