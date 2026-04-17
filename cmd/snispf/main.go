package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"runtime"
	"snispf/internal/bypass"
	"snispf/internal/forwarder"
	"snispf/internal/logx"
	"snispf/internal/rawinjector"
	"snispf/internal/tlsclienthello"
	"snispf/internal/utils"
)

const version = "1.1.0-go"
const apiVersion = "v1"

const banner = `
 SNISPF - Cross-Platform DPI Bypass Tool
 SNI Spoofing + TLS Fragmentation
`

var defaultConfig = utils.Config{
	ListenHost:               "0.0.0.0",
	ListenPort:               40443,
	LogLevel:                 "info",
	ConnectIP:                "104.19.229.21",
	ConnectPort:              443,
	FakeSNI:                  "hcaptcha.com",
	BypassMethod:             "wrong_seq",
	FragmentStrategy:         "sni_split",
	FragmentDelay:            0.05,
	UseTTLTrick:              false,
	FakeSNIMethod:            "raw_inject",
	LoadBalance:              "round_robin",
	EndpointProbe:            false,
	AutoFailover:             false,
	FailoverRetries:          0,
	ProbeTimeoutMS:           2500,
	WrongSeqConfirmTimeoutMS: 2000,
}

func main() {
	os.Args = append([]string{os.Args[0]}, normalizeCLIArgs(os.Args[1:])...)

	var (
		configPath       = flag.String("config", "", "Path to JSON config file")
		configPathShort  = flag.String("C", "", "Path to JSON config file")
		generateConfig   = flag.String("generate-config", "", "Generate default config and exit")
		configDoctor     = flag.Bool("config-doctor", false, "Validate config and print recommendations")
		runCore          = flag.Bool("run-core", false, "Internal: run proxy core mode")
		serviceMode      = flag.Bool("service", false, "Run localhost control service API")
		serviceAddr      = flag.String("service-addr", "127.0.0.1:8797", "Control service listen address")
		serviceToken     = flag.String("service-token", "", "Control service auth token (optional)")
		serviceParentPID = flag.Int("service-parent-pid", 0, "Internal: parent process ID to monitor")
		serviceParentTS  = flag.Int64("service-parent-start-unix-ms", 0, "Internal: parent process start time (unix ms) for robust monitoring")
		showBuildInfo    = flag.Bool("build-info", false, "Show core build/runtime metadata")
		listen           = flag.String("listen", "", "Listen address HOST:PORT")
		listenShort      = flag.String("l", "", "Listen address HOST:PORT")
		connect          = flag.String("connect", "", "Target address IP:PORT")
		connectShort     = flag.String("c", "", "Target address IP:PORT")
		sni              = flag.String("sni", "", "Fake SNI hostname")
		sniShort         = flag.String("s", "", "Fake SNI hostname")
		method           = flag.String("method", "", "Bypass method: fragment|fake_sni|combined|wrong_seq")
		methodShort      = flag.String("m", "", "Bypass method: fragment|fake_sni|combined|wrong_seq")
		fragmentStrategy = flag.String("fragment-strategy", "", "Fragment strategy")
		fragmentDelay    = flag.Float64("fragment-delay", -1, "Delay between fragments")
		ttlTrick         = flag.Bool("ttl-trick", false, "Enable TTL trick")
		noRaw            = flag.Bool("no-raw", false, "Disable raw injection")
		verbose          = flag.Bool("verbose", false, "Verbose output")
		verboseShort     = flag.Bool("v", false, "Verbose output")
		quiet            = flag.Bool("quiet", false, "Quiet output")
		quietShort       = flag.Bool("q", false, "Quiet output")
		showInfo         = flag.Bool("info", false, "Show platform capabilities")
		showVersion      = flag.Bool("version", false, "Show version")
		showVersionShort = flag.Bool("V", false, "Show version")
	)
	flag.Parse()

	if *showVersion || *showVersionShort {
		fmt.Println("SNISPF", version)
		return
	}

	if *showBuildInfo {
		fmt.Printf("version=%s\n", version)
		fmt.Printf("api_version=%s\n", apiVersion)
		fmt.Printf("goos=%s\n", runtime.GOOS)
		fmt.Printf("goarch=%s\n", runtime.GOARCH)
		return
	}

	if *runCore {
		// Internal compatibility flag used by service mode process spawning.
	}

	if *showInfo {
		fmt.Print(banner)
		caps := utils.CheckPlatformCapabilities(rawinjector.IsRawAvailable())
		fmt.Printf("platform=%s\n", caps.Platform)
		fmt.Printf("fragment_support=%v\n", caps.Fragment)
		fmt.Printf("tls_record_frag=%v\n", caps.TLSRecordFrag)
		fmt.Printf("fake_sni=%v\n", caps.FakeSNI)
		fmt.Printf("tcp_nodelay=%v\n", caps.TCPNoDelay)
		fmt.Printf("raw_socket=%v\n", caps.RawSocket)
		fmt.Printf("ip_ttl_trick=%v\n", caps.IPTTLTrick)
		fmt.Printf("af_packet=%v\n", caps.AFPacket)
		fmt.Printf("raw_injection=%v\n", caps.RawInjection)
		if diag := strings.TrimSpace(rawinjector.RawDiagnostic()); diag != "" {
			fmt.Printf("raw_injection_diagnostic=%s\n", diag)
		}
		printPrivilegeGuidance(caps)
		return
	}

	cfgPath := firstNonEmpty(*configPath, *configPathShort)
	if cfgPath == "" {
		cfgPath = "config.json"
	}

	if *serviceMode {
		cfgForLogs, _ := loadOrDefaultConfig(cfgPath)
		utils.NormalizeConfig(&cfgForLogs)
		configureLogger(cfgForLogs.LogLevel, *quiet || *quietShort, *verbose || *verboseShort)

		tok := strings.TrimSpace(*serviceToken)
		if tok == "" {
			tok = strings.TrimSpace(os.Getenv("SNISPF_SERVICE_TOKEN"))
		}
		if err := runControlService(cfgPath, *serviceAddr, tok, *serviceParentPID, *serviceParentTS); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *generateConfig != "" {
		if err := writeConfig(*generateConfig, defaultConfig); err != nil {
			log.Fatalf("failed to write config: %v", err)
		}
		fmt.Println("Generated config:", *generateConfig)
		return
	}

	cfg := defaultConfig
	if cfgPath != "" {
		loaded, err := loadConfig(cfgPath)
		if err != nil {
			log.Fatal(err)
		}
		cfg = loaded
	}

	if v := firstNonEmpty(*listen, *listenShort); v != "" {
		host, port, err := utils.ParseHostPort(v, "0.0.0.0", 40443)
		if err != nil {
			log.Fatal(err)
		}
		cfg.ListenHost, cfg.ListenPort = host, port
	}
	if v := firstNonEmpty(*connect, *connectShort); v != "" {
		host, port, err := utils.ParseHostPort(v, "188.114.98.0", 443)
		if err != nil {
			log.Fatal(err)
		}
		cfg.ConnectIP, cfg.ConnectPort = host, port
	}
	if v := firstNonEmpty(*sni, *sniShort); v != "" {
		cfg.FakeSNI = v
	}
	if v := firstNonEmpty(*method, *methodShort); v != "" {
		cfg.BypassMethod = strings.ToLower(v)
	}
	if *fragmentStrategy != "" {
		cfg.FragmentStrategy = *fragmentStrategy
	}
	if *fragmentDelay >= 0 {
		cfg.FragmentDelay = *fragmentDelay
	}
	if *ttlTrick {
		cfg.UseTTLTrick = true
	}

	if !utils.IsValidPort(cfg.ListenPort) || !utils.IsValidPort(cfg.ConnectPort) {
		log.Fatal("invalid listen/connect port")
	}

	utils.NormalizeConfig(&cfg)
	activeEndpoints := utils.EnabledEndpoints(cfg.Endpoints)
	if len(activeEndpoints) == 0 {
		log.Fatal("no valid enabled endpoints in config")
	}
	if cfg.EndpointProbe {
		activeEndpoints = utils.ProbeHealthyEndpoints(
			activeEndpoints,
			time.Duration(cfg.ProbeTimeoutMS)*time.Millisecond,
		)
	}
	cfg.Endpoints = activeEndpoints

	if cfg.BypassMethod != "fragment" && cfg.BypassMethod != "fake_sni" && cfg.BypassMethod != "combined" && cfg.BypassMethod != "wrong_seq" {
		logx.Warnf("unknown bypass method %q, using fragment", cfg.BypassMethod)
		cfg.BypassMethod = "fragment"
	}

	if err := validateSNIGuardrails(cfg); err != nil {
		log.Fatal(err)
	}

	if *configDoctor {
		caps := utils.CheckPlatformCapabilities(rawinjector.IsRawAvailable())
		issues, warnings := runConfigDoctor(cfg, caps)
		if len(issues) == 0 {
			fmt.Println("config-doctor: OK")
		} else {
			fmt.Println("config-doctor: issues found")
			for _, issue := range issues {
				fmt.Printf("- ERROR: %s\n", issue)
			}
		}
		for _, warning := range warnings {
			fmt.Printf("- WARN: %s\n", warning)
		}
		printPrivilegeGuidance(caps)
		if len(issues) > 0 {
			os.Exit(1)
		}
		return
	}

	configureLogger(cfg.LogLevel, *quiet || *quietShort, *verbose || *verboseShort)

	runtimes, err := buildServerRuntimes(cfg, *noRaw)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Print(banner)
	logx.Infof("SNISPF Go | strategy=%s | listeners=%d", cfg.FragmentStrategy, len(runtimes))
	logx.Infof("platform=%s", runtime.GOOS)

	for i := range runtimes {
		rt := runtimes[i]
		printRuntimeModeHint(rt.cfg, rt.injector != nil)
	}

	errCh := make(chan error, len(runtimes))
	for i := range runtimes {
		rt := runtimes[i]
		if rt.injector != nil {
			defer rt.injector.Stop()
		}
		go func(name string, srv *forwarder.Server) {
			if runErr := srv.Run(ctx); runErr != nil && !errors.Is(runErr, context.Canceled) {
				errCh <- fmt.Errorf("listener %s failed: %w", name, runErr)
			}
		}(rt.name, rt.server)
	}

	select {
	case <-ctx.Done():
		return
	case runErr := <-errCh:
		log.Fatal(runErr)
	}
}

func buildStrategy(cfg utils.Config, method string, injector rawinjector.Interface) bypass.Strategy {
	switch strings.ToLower(method) {
	case "fake_sni":
		return bypass.NewFakeSNI(cfg.FakeSNIMethod, cfg.FragmentDelay, time.Duration(cfg.WrongSeqConfirmTimeoutMS)*time.Millisecond, injector)
	case "combined":
		return bypass.NewCombined(cfg.FragmentStrategy, cfg.FragmentDelay, cfg.UseTTLTrick, time.Duration(cfg.WrongSeqConfirmTimeoutMS)*time.Millisecond, injector)
	case "wrong_seq":
		return bypass.NewWrongSeqStrict(injector, time.Duration(cfg.WrongSeqConfirmTimeoutMS)*time.Millisecond)
	default:
		return bypass.NewFragment(cfg.FragmentStrategy, cfg.FragmentDelay)
	}
}

type serverRuntime struct {
	name     string
	cfg      utils.Config
	server   *forwarder.Server
	injector rawinjector.Interface
}

func buildServerRuntimes(cfg utils.Config, noRaw bool) ([]serverRuntime, error) {
	if len(cfg.Listeners) == 0 {
		// Endpoints are already probed at the top level; skip inner probe.
		rt, err := buildSingleRuntime(cfg, noRaw, true, "primary", cfg.ListenHost, cfg.ListenPort, cfg.Endpoints, cfg.BypassMethod)
		if err != nil {
			return nil, err
		}
		return []serverRuntime{rt}, nil
	}

	runtimes := make([]serverRuntime, 0, len(cfg.Listeners))
	for i, ls := range cfg.Listeners {
		name := ls.Name
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("listener-%d", i+1)
		}
		endpoints := []utils.Endpoint{{
			Name:    name + "-upstream",
			IP:      utils.ResolveHost(ls.ConnectIP),
			Port:    ls.ConnectPort,
			SNI:     ls.FakeSNI,
			Enabled: true,
		}}
		method := strings.TrimSpace(ls.BypassMethod)
		if method == "" {
			method = cfg.BypassMethod
		}

		rt, err := buildSingleRuntime(cfg, noRaw, false, name, ls.ListenHost, ls.ListenPort, endpoints, method)
		if err != nil {
			return nil, err
		}
		runtimes = append(runtimes, rt)
	}
	return runtimes, nil
}

func buildSingleRuntime(baseCfg utils.Config, noRaw bool, probeAlreadyDone bool, name, listenHost string, listenPort int, endpoints []utils.Endpoint, method string) (serverRuntime, error) {
	cfg := baseCfg
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		method = "fragment"
	}
	if method != "fragment" && method != "fake_sni" && method != "combined" && method != "wrong_seq" {
		logx.Warnf("unknown bypass method %q for %s, using fragment", method, name)
		method = "fragment"
	}

	if cfg.EndpointProbe && !probeAlreadyDone {
		endpoints = utils.ProbeHealthyEndpoints(endpoints, time.Duration(cfg.ProbeTimeoutMS)*time.Millisecond)
	}
	if len(endpoints) == 0 {
		return serverRuntime{}, fmt.Errorf("%s has no available endpoint", name)
	}

	interfaceIP := utils.GetDefaultInterfaceIPv4(endpoints[0].IP)
	var injector rawinjector.Interface
	if len(endpoints) == 1 && !noRaw && (method == "fake_sni" || method == "combined" || method == "wrong_seq") && rawinjector.IsRawAvailable() {
		injector = rawinjector.New(interfaceIP, endpoints[0].IP, endpoints[0].Port, tlsclienthello.BuildClientHello)
		if !injector.Start() {
			injector = nil
			logx.Warnf("raw injector unavailable at runtime for %s, falling back", name)
		}
	}

	if method == "wrong_seq" {
		if len(endpoints) != 1 {
			return serverRuntime{}, fmt.Errorf("%s: wrong_seq requires exactly one enabled endpoint", name)
		}
		if injector == nil {
			diag := strings.TrimSpace(rawinjector.RawDiagnostic())
			if diag == "" {
				return serverRuntime{}, fmt.Errorf("%s: wrong_seq requires raw injector support; use Linux (CAP_NET_RAW/root) or Windows (Administrator + WinDivert)", name)
			}
			return serverRuntime{}, fmt.Errorf("%s: wrong_seq requires raw injector support; use Linux (CAP_NET_RAW/root) or Windows (Administrator + WinDivert). detail: %s", name, diag)
		}
	}

	cfg.ListenHost = listenHost
	cfg.ListenPort = listenPort
	cfg.ConnectIP = endpoints[0].IP
	cfg.ConnectPort = endpoints[0].Port
	cfg.FakeSNI = endpoints[0].SNI
	cfg.Endpoints = endpoints
	cfg.BypassMethod = method

	strategy := buildStrategy(cfg, method, injector)
	srv := &forwarder.Server{
		ListenHost:      listenHost,
		ListenPort:      listenPort,
		ConnectIP:       endpoints[0].IP,
		ConnectPort:     endpoints[0].Port,
		FakeSNI:         endpoints[0].SNI,
		Endpoints:       endpoints,
		LoadBalance:     cfg.LoadBalance,
		AutoFailover:    cfg.AutoFailover,
		FailoverRetries: cfg.FailoverRetries,
		InterfaceIP:     interfaceIP,
		Strategy:        strategy,
		Injector:        injector,
	}

	return serverRuntime{name: name, cfg: cfg, server: srv, injector: injector}, nil
}

func validateSNIGuardrails(cfg utils.Config) error {
	check := func(scope, sni string) error {
		if len([]byte(sni)) > maxSNIBytes {
			return fmt.Errorf("%s SNI must be <= %d bytes", scope, maxSNIBytes)
		}
		n := len(tlsclienthello.BuildClientHello(sni))
		if n > maxFakeHelloBytes {
			return fmt.Errorf("%s fake ClientHello size is %d bytes (> %d)", scope, n, maxFakeHelloBytes)
		}
		return nil
	}
	if err := check("FAKE_SNI", cfg.FakeSNI); err != nil {
		return err
	}
	for i, ep := range cfg.Endpoints {
		if err := check(fmt.Sprintf("ENDPOINTS[%d].SNI", i), ep.SNI); err != nil {
			return err
		}
	}
	for i, ls := range cfg.Listeners {
		if err := check(fmt.Sprintf("LISTENERS[%d].FAKE_SNI", i), ls.FakeSNI); err != nil {
			return err
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func loadConfig(path string) (utils.Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return utils.Config{}, fmt.Errorf("failed to read config: %w", err)
	}
	cfg := defaultConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return utils.Config{}, fmt.Errorf("invalid config JSON: %w", err)
	}
	return cfg, nil
}

func writeConfig(path string, cfg utils.Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func loadOrDefaultConfig(path string) (utils.Config, error) {
	_, statErr := os.Stat(path)
	if statErr == nil {
		return loadConfig(path)
	}
	if os.IsNotExist(statErr) {
		return defaultConfig, nil
	}
	return utils.Config{}, statErr
}

func printPrivilegeGuidance(caps utils.PlatformCapabilities) {
	if caps.RawInjection {
		fmt.Println("privilege-note: elevated privileges detected for raw injection mode")
		return
	}
	fmt.Println("privilege-note: admin/root is NOT always required; fragment mode works unprivileged")
	if !caps.RawInjection {
		fmt.Println("privilege-note: fake_sni/combined may use fallback behavior without elevated privileges")
	}
}

func printRuntimeModeHint(cfg utils.Config, rawActive bool) {
	if rawActive {
		logx.Infof("runtime: raw injection active")
		return
	}
	if cfg.BypassMethod == "fragment" {
		logx.Infof("runtime: unprivileged fragment mode")
		return
	}
	logx.Infof("runtime: fallback mode (raw injection not active)")
}

func configureLogger(configLevel string, quiet, verbose bool) {
	levelText := strings.ToLower(strings.TrimSpace(configLevel))
	if levelText == "" {
		levelText = "info"
	}

	if verbose {
		levelText = "debug"
	}
	if quiet {
		levelText = "error"
	}

	if err := logx.SetLevelString(levelText); err != nil {
		_ = logx.SetLevelString("info")
		logx.Warnf("invalid LOG_LEVEL %q, using info", configLevel)
	}

	if verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		return
	}
	log.SetFlags(log.LstdFlags)
}

func normalizeCLIArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]

	switch cmd {
	case "run":
		return rest
	case "service":
		return append([]string{"--service"}, rest...)
	case "doctor":
		return append([]string{"--config-doctor"}, rest...)
	case "build-info":
		return append([]string{"--build-info"}, rest...)
	default:
		return args
	}
}
