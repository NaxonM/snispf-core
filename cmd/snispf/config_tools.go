package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"snispf/internal/logx"
	"snispf/internal/tlsclienthello"
	"snispf/internal/utils"
)

const (
	maxSNIBytes       = 219
	maxFakeHelloBytes = 1460
)

func runConfigUI(cfg utils.Config) (utils.Config, error) {
	r := bufio.NewReader(os.Stdin)
	fmt.Println("Interactive config editor")
	fmt.Println("Press Enter to keep current values.")

	cfg.ListenHost = promptString(r, "LISTEN_HOST", cfg.ListenHost)
	cfg.ListenPort = promptInt(r, "LISTEN_PORT", cfg.ListenPort)
	cfg.LogLevel = strings.ToLower(promptString(r, "LOG_LEVEL [error|warn|info|debug]", cfg.LogLevel))
	cfg.ConnectIP = promptString(r, "CONNECT_IP", cfg.ConnectIP)
	cfg.ConnectPort = promptInt(r, "CONNECT_PORT", cfg.ConnectPort)
	cfg.FakeSNI = promptString(r, "FAKE_SNI", cfg.FakeSNI)
	cfg.BypassMethod = strings.ToLower(promptString(r, "BYPASS_METHOD [fragment|fake_sni|combined|wrong_seq]", cfg.BypassMethod))
	cfg.FragmentStrategy = strings.ToLower(promptString(r, "FRAGMENT_STRATEGY [sni_split|half|multi|tls_record_frag]", cfg.FragmentStrategy))
	cfg.FragmentDelay = promptFloat(r, "FRAGMENT_DELAY", cfg.FragmentDelay)
	cfg.UseTTLTrick = promptBool(r, "USE_TTL_TRICK", cfg.UseTTLTrick)
	cfg.FakeSNIMethod = strings.ToLower(promptString(r, "FAKE_SNI_METHOD [prefix_fake|ttl_trick|disorder]", cfg.FakeSNIMethod))
	cfg.WrongSeqConfirmTimeoutMS = promptInt(r, "WRONG_SEQ_CONFIRM_TIMEOUT_MS", cfg.WrongSeqConfirmTimeoutMS)

	return cfg, nil
}

func runConfigDoctor(cfg utils.Config, caps utils.PlatformCapabilities) (issues []string, warnings []string) {
	if !utils.IsValidPort(cfg.ListenPort) {
		issues = append(issues, "LISTEN_PORT must be between 1 and 65535")
	}
	if !utils.IsValidPort(cfg.ConnectPort) {
		issues = append(issues, "CONNECT_PORT must be between 1 and 65535")
	}
	if cfg.ListenHost == "" {
		issues = append(issues, "LISTEN_HOST must not be empty")
	}
	if cfg.ConnectIP == "" {
		issues = append(issues, "CONNECT_IP must not be empty")
	}
	if _, ok := logx.ParseLevel(cfg.LogLevel); !ok {
		issues = append(issues, "LOG_LEVEL must be one of error, warn, info, debug")
	}
	if cfg.FakeSNI == "" {
		issues = append(issues, "FAKE_SNI must not be empty")
	}
	if len([]byte(cfg.FakeSNI)) > maxSNIBytes {
		issues = append(issues, fmt.Sprintf("FAKE_SNI must be <= %d bytes", maxSNIBytes))
	}
	if n := len(tlsclienthello.BuildClientHello(cfg.FakeSNI)); n > maxFakeHelloBytes {
		issues = append(issues, fmt.Sprintf("FAKE_SNI generates fake ClientHello size %d bytes (> %d)", n, maxFakeHelloBytes))
	}

	allowedMethods := map[string]bool{"fragment": true, "fake_sni": true, "combined": true, "wrong_seq": true}
	if !allowedMethods[strings.ToLower(cfg.BypassMethod)] {
		issues = append(issues, "BYPASS_METHOD must be one of fragment, fake_sni, combined, wrong_seq")
	}

	allowedFragments := map[string]bool{"sni_split": true, "half": true, "multi": true, "tls_record_frag": true}
	if !allowedFragments[strings.ToLower(cfg.FragmentStrategy)] {
		issues = append(issues, "FRAGMENT_STRATEGY must be one of sni_split, half, multi, tls_record_frag")
	}

	if cfg.FragmentDelay < 0 {
		issues = append(issues, "FRAGMENT_DELAY must be >= 0")
	}

	allowedLB := map[string]bool{"round_robin": true, "random": true, "failover": true}
	if cfg.LoadBalance != "" && !allowedLB[strings.ToLower(cfg.LoadBalance)] {
		issues = append(issues, "LOAD_BALANCE must be one of round_robin, random, failover")
	}

	if cfg.FailoverRetries < 0 {
		issues = append(issues, "FAILOVER_RETRIES must be >= 0")
	}
	if cfg.ProbeTimeoutMS < 100 {
		warnings = append(warnings, "PROBE_TIMEOUT_MS is very low; endpoint probe may be unreliable")
	}
	if cfg.WrongSeqConfirmTimeoutMS < 100 {
		warnings = append(warnings, "WRONG_SEQ_CONFIRM_TIMEOUT_MS is very low; wrong_seq may fail under jitter")
	}

	enabledEndpoints := utils.EnabledEndpoints(cfg.Endpoints)
	if len(cfg.Endpoints) > 0 && len(enabledEndpoints) == 0 {
		issues = append(issues, "ENDPOINTS present but none are valid+enabled")
	}
	for i, ep := range enabledEndpoints {
		if !utils.IsValidPort(ep.Port) {
			issues = append(issues, fmt.Sprintf("ENDPOINTS[%d] port is invalid", i))
		}
		if strings.TrimSpace(ep.IP) == "" || strings.TrimSpace(ep.SNI) == "" {
			issues = append(issues, fmt.Sprintf("ENDPOINTS[%d] must include IP and SNI", i))
		}
		if len([]byte(ep.SNI)) > maxSNIBytes {
			issues = append(issues, fmt.Sprintf("ENDPOINTS[%d].SNI must be <= %d bytes", i, maxSNIBytes))
		}
		if n := len(tlsclienthello.BuildClientHello(ep.SNI)); n > maxFakeHelloBytes {
			issues = append(issues, fmt.Sprintf("ENDPOINTS[%d].SNI generates fake ClientHello size %d bytes (> %d)", i, n, maxFakeHelloBytes))
		}
	}

	for i, ls := range cfg.Listeners {
		if !utils.IsValidPort(ls.ListenPort) {
			issues = append(issues, fmt.Sprintf("LISTENERS[%d].LISTEN_PORT is invalid", i))
		}
		if !utils.IsValidPort(ls.ConnectPort) {
			issues = append(issues, fmt.Sprintf("LISTENERS[%d].CONNECT_PORT is invalid", i))
		}
		if strings.TrimSpace(ls.ListenHost) == "" || strings.TrimSpace(ls.ConnectIP) == "" || strings.TrimSpace(ls.FakeSNI) == "" {
			issues = append(issues, fmt.Sprintf("LISTENERS[%d] must include LISTEN_HOST, CONNECT_IP, and FAKE_SNI", i))
		}
		if len([]byte(ls.FakeSNI)) > maxSNIBytes {
			issues = append(issues, fmt.Sprintf("LISTENERS[%d].FAKE_SNI must be <= %d bytes", i, maxSNIBytes))
		}
		if n := len(tlsclienthello.BuildClientHello(ls.FakeSNI)); n > maxFakeHelloBytes {
			issues = append(issues, fmt.Sprintf("LISTENERS[%d].FAKE_SNI generates fake ClientHello size %d bytes (> %d)", i, n, maxFakeHelloBytes))
		}
	}

	allowedFakeMethods := map[string]bool{"prefix_fake": true, "ttl_trick": true, "disorder": true, "fragment_fallback": true, "raw_inject": true}
	if !allowedFakeMethods[strings.ToLower(cfg.FakeSNIMethod)] {
		warnings = append(warnings, "FAKE_SNI_METHOD is uncommon; expected prefix_fake, ttl_trick, or disorder")
	}

	if (strings.ToLower(cfg.BypassMethod) == "fake_sni" || strings.ToLower(cfg.BypassMethod) == "combined" || strings.ToLower(cfg.BypassMethod) == "wrong_seq") && !caps.RawInjection {
		warnings = append(warnings, "raw injection unavailable; fake_sni/combined use fallback, wrong_seq cannot operate")
	}

	if strings.ToLower(cfg.BypassMethod) == "wrong_seq" && len(enabledEndpoints) != 1 {
		issues = append(issues, "wrong_seq requires exactly one enabled endpoint")
	}

	if cfg.UseTTLTrick && !caps.IPTTLTrick {
		warnings = append(warnings, "USE_TTL_TRICK enabled but platform capabilities indicate TTL trick may not work")
	}

	return issues, warnings
}

func promptString(r *bufio.Reader, label, current string) string {
	fmt.Printf("%s [%s]: ", label, current)
	text, _ := r.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return current
	}
	return text
}

func promptInt(r *bufio.Reader, label string, current int) int {
	for {
		fmt.Printf("%s [%d]: ", label, current)
		text, _ := r.ReadString('\n')
		text = strings.TrimSpace(text)
		if text == "" {
			return current
		}
		v, err := strconv.Atoi(text)
		if err == nil {
			return v
		}
		fmt.Println("Invalid integer, try again.")
	}
}

func promptFloat(r *bufio.Reader, label string, current float64) float64 {
	for {
		fmt.Printf("%s [%.3f]: ", label, current)
		text, _ := r.ReadString('\n')
		text = strings.TrimSpace(text)
		if text == "" {
			return current
		}
		v, err := strconv.ParseFloat(text, 64)
		if err == nil {
			return v
		}
		fmt.Println("Invalid number, try again.")
	}
}

func promptBool(r *bufio.Reader, label string, current bool) bool {
	for {
		fmt.Printf("%s [%v]: ", label, current)
		text, _ := r.ReadString('\n')
		text = strings.TrimSpace(strings.ToLower(text))
		if text == "" {
			return current
		}
		switch text {
		case "true", "t", "yes", "y", "1":
			return true
		case "false", "f", "no", "n", "0":
			return false
		default:
			fmt.Println("Invalid boolean, use true/false.")
		}
	}
}
