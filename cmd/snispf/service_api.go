package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"snispf/internal/logx"
	"snispf/internal/utils"
)

type controlService struct {
	mu        sync.Mutex
	cfgPath   string
	token     string
	exePath   string
	child     *exec.Cmd
	childLog  *os.File
	startedAt time.Time
	lastError string
	logPath   string
}

type serviceStatus struct {
	APIVersion   string    `json:"api_version"`
	Running      bool      `json:"running"`
	PID          int       `json:"pid,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	LogPath      string    `json:"log_path"`
	ConfigPath   string    `json:"config_path"`
	Platform     string    `json:"platform"`
	Architecture string    `json:"architecture"`
}

type healthEndpoint struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	SNI       string `json:"sni"`
	Healthy   bool   `json:"healthy"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type wrongSeqHealthStats struct {
	Confirmed      int `json:"confirmed"`
	Timeout        int `json:"timeout"`
	Failed         int `json:"failed"`
	NotRegistered  int `json:"not_registered"`
	FirstWriteFail int `json:"first_write_fail"`
	SourceLines    int `json:"source_lines"`
}

func runControlService(cfgPath, addr, token string, parentPID int, parentStartUnixMS int64) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	logPath, err := serviceLogPath()
	if err != nil {
		return err
	}
	if err := ensureLogSink(logPath); err != nil {
		return err
	}

	svc := &controlService{
		cfgPath: cfgPath,
		token:   token,
		exePath: exePath,
		logPath: logPath,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", svc.withAuth(svc.handleStatus))
	mux.HandleFunc("/v1/start", svc.withAuth(svc.handleStart))
	mux.HandleFunc("/v1/stop", svc.withAuth(svc.handleStop))
	mux.HandleFunc("/v1/health", svc.withAuth(svc.handleHealth))
	mux.HandleFunc("/v1/validate", svc.withAuth(svc.handleValidate))
	mux.HandleFunc("/v1/logs", svc.withAuth(svc.handleLogs))

	srv := &http.Server{Addr: addr, Handler: mux}

	ctx, stop := signalContext()
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		_ = svc.stopCore()
	}()

	if parentPID > 0 {
		go func() {
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for range t.C {
				if !parentProcessAlive(parentPID, parentStartUnixMS) {
					logx.Warnf("control-service parent %d exited or changed; shutting down", parentPID)
					_ = svc.stopCore()
					shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					_ = srv.Shutdown(shutdownCtx)
					cancel()
					return
				}
			}
		}()
	}

	logx.Infof("control-service listening on %s", addr)
	if token != "" {
		logx.Infof("control-service auth enabled")
	}

	err = srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func serviceLogPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	dir := filepath.Join(base, "snispf", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "service.log"), nil
}

func ensureLogSink(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	log.SetOutput(io.MultiWriter(os.Stdout, f))
	return nil
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

func (s *controlService) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			provided := strings.TrimSpace(r.Header.Get("X-SNISPF-Token"))
			if provided == "" {
				provided = strings.TrimSpace(r.URL.Query().Get("token"))
			}
			if provided != s.token {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next(w, r)
	}
}

func (s *controlService) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, s.status())
}

func (s *controlService) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := s.startCore(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.status())
}

func (s *controlService) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := s.stopCore(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.status())
}

func (s *controlService) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	cfg, err := loadOrDefaultConfig(s.cfgPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	utils.NormalizeConfig(&cfg)
	active := utils.EnabledEndpoints(cfg.Endpoints)
	if len(cfg.Listeners) > 0 {
		active = make([]utils.Endpoint, 0, len(cfg.Listeners))
		for _, ls := range cfg.Listeners {
			resolvedIP := utils.ResolveHost(ls.ConnectIP)
			active = append(active, utils.Endpoint{
				Name:    ls.Name,
				IP:      resolvedIP,
				Port:    ls.ConnectPort,
				SNI:     ls.FakeSNI,
				Enabled: true,
			})
		}
	}
	timeout := time.Duration(cfg.ProbeTimeoutMS) * time.Millisecond
	out := make([]healthEndpoint, 0, len(active))
	for _, ep := range active {
		healthy, latency, probeErr := probeEndpoint(ep, timeout)
		errText := ""
		if probeErr != nil {
			errText = probeErr.Error()
		}
		out = append(out, healthEndpoint{
			Name:      ep.Name,
			IP:        ep.IP,
			Port:      ep.Port,
			SNI:       ep.SNI,
			Healthy:   healthy,
			LatencyMS: latency.Milliseconds(),
			Error:     errText,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"api_version": apiVersion,
		"checked_at":  time.Now().UTC(),
		"endpoints":   out,
		"wrong_seq":   s.collectWrongSeqHealthStats(5000),
	})
}

func (s *controlService) collectWrongSeqHealthStats(maxLines int) wrongSeqHealthStats {
	stats := wrongSeqHealthStats{}
	b, err := os.ReadFile(s.logPath)
	if err != nil {
		return stats
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	stats.SourceLines = len(lines)

	for _, ln := range lines {
		if !strings.Contains(ln, "wrong_seq:") {
			continue
		}
		switch {
		case strings.Contains(ln, "confirmation succeeded"):
			stats.Confirmed++
		case strings.Contains(ln, "status=timeout") || strings.Contains(ln, "confirmation timeout"):
			stats.Timeout++
		case strings.Contains(ln, "status=not_registered"):
			stats.NotRegistered++
		case strings.Contains(ln, "status=failed"):
			stats.Failed++
		case strings.Contains(ln, "first payload write failed"):
			stats.FirstWriteFail++
		case strings.Contains(ln, "confirmation failed"):
			stats.Failed++
		}
	}

	return stats
}

func (s *controlService) handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	cfg, err := loadOrDefaultConfig(s.cfgPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	utils.NormalizeConfig(&cfg)
	caps := utils.CheckPlatformCapabilities(false)
	issues, warnings := runConfigDoctor(cfg, caps)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"api_version": apiVersion,
		"issues":      issues,
		"warnings":    warnings,
	})
}

func (s *controlService) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 2000 {
				n = 2000
			}
			limit = n
		}
	}
	level := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("level")))
	if level == "ALL" {
		level = ""
	}

	b, err := os.ReadFile(s.logPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"logs": ""})
		return
	}
	allLines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(allLines))
	for _, ln := range allLines {
		if level == "" {
			lines = append(lines, ln)
			continue
		}
		up := strings.ToUpper(ln)
		if strings.Contains(up, " "+level+" ") || strings.Contains(up, level+":") {
			lines = append(lines, ln)
		}
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"api_version":    apiVersion,
		"logs":           strings.Join(lines, "\n"),
		"returned_lines": len(lines),
		"limit":          limit,
		"level":          level,
	})
}

func (s *controlService) startCore() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.child != nil && s.child.Process != nil {
		if s.child.ProcessState == nil || !s.child.ProcessState.Exited() {
			return fmt.Errorf("core already running")
		}
	}

	cfg, err := loadOrDefaultConfig(s.cfgPath)
	if err != nil {
		return err
	}
	utils.NormalizeConfig(&cfg)
	caps := utils.CheckPlatformCapabilities(false)
	issues, _ := runConfigDoctor(cfg, caps)
	if len(issues) > 0 {
		return fmt.Errorf("config has %d issue(s); call /v1/validate", len(issues))
	}

	cmd := exec.Command(s.exePath, "--run-core", "--config", s.cfgPath)
	setHiddenProcessAttrs(cmd)

	lf, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		cmd.Stdout = lf
		cmd.Stderr = lf
		s.childLog = lf
	}

	if err := cmd.Start(); err != nil {
		if s.childLog != nil {
			_ = s.childLog.Close()
			s.childLog = nil
		}
		s.lastError = err.Error()
		return err
	}

	s.child = cmd
	s.startedAt = time.Now().UTC()
	s.lastError = ""

	go func(c *exec.Cmd) {
		err := c.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.child == c {
			s.child = nil
			if s.childLog != nil {
				_ = s.childLog.Close()
				s.childLog = nil
			}
			if err != nil {
				s.lastError = err.Error()
			}
		}
	}(cmd)

	return nil
}

func (s *controlService) stopCore() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.child == nil || s.child.Process == nil {
		s.child = nil
		return nil
	}
	err := s.child.Process.Kill()
	if s.childLog != nil {
		_ = s.childLog.Close()
		s.childLog = nil
	}
	s.child = nil
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "process already finished") {
		s.lastError = err.Error()
		return err
	}
	return nil
}

func (s *controlService) status() serviceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := serviceStatus{
		APIVersion:   apiVersion,
		Running:      false,
		ConfigPath:   s.cfgPath,
		LogPath:      s.logPath,
		LastError:    s.lastError,
		Platform:     runtime.GOOS,
		Architecture: runtime.GOARCH,
	}
	if s.child != nil && s.child.Process != nil {
		if s.child.ProcessState == nil || !s.child.ProcessState.Exited() {
			st.Running = true
			st.PID = s.child.Process.Pid
			st.StartedAt = s.startedAt
		}
	}
	return st
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func probeEndpoint(ep utils.Endpoint, timeout time.Duration) (bool, time.Duration, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ep.IP, ep.Port), timeout)
	if err != nil {
		return false, time.Since(start), err
	}
	_ = conn.Close()
	return true, time.Since(start), nil
}
