package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type settings struct{ UUID, Path, Port string }

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var pathRE = regexp.MustCompile(`^/[A-Za-z0-9_-]{12,128}$`)

func load() (settings, error) {
	s := settings{os.Getenv("VLESS_UUID"), os.Getenv("WS_PATH"), os.Getenv("PORT")}
	if s.Port == "" {
		s.Port = "8080"
	}
	if !uuidRE.MatchString(s.UUID) || strings.EqualFold(s.UUID, "00000000-0000-0000-0000-000000000000") {
		return s, errors.New("VLESS_UUID must be a nonzero canonical UUID")
	}
	if !pathRE.MatchString(s.Path) || s.Path == "/healthz" {
		return s, errors.New("WS_PATH must be / followed by 12-128 letters, digits, underscores or hyphens")
	}
	var p int
	if _, err := fmt.Sscanf(s.Port, "%d", &p); err != nil || p < 1024 || p > 65535 || fmt.Sprint(p) != s.Port || p == 10000 {
		return s, errors.New("PORT must be 1024-65535 and not 10000")
	}
	return s, nil
}

func config(s settings) map[string]any {
	return map[string]any{
		"log": map[string]any{"loglevel": "warning", "access": "none"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": 10000, "protocol": "vless", "tag": "vless-ws",
			"settings":       map[string]any{"clients": []any{map[string]any{"id": s.UUID}}, "decryption": "none"},
			"streamSettings": map[string]any{"network": "ws", "security": "none", "wsSettings": map[string]any{"path": s.Path}},
		}},
		"outbounds": []any{
			map[string]any{"protocol": "freedom", "tag": "direct", "settings": map[string]any{"domainStrategy": "UseIP"}},
			map[string]any{"protocol": "blackhole", "tag": "blocked"},
		},
		"routing": map[string]any{"domainStrategy": "IPOnDemand", "rules": []any{
			map[string]any{"type": "field", "network": "udp", "outboundTag": "blocked"},
			map[string]any{"type": "field", "domain": []string{"full:localhost", "domain:local", "domain:internal", "domain:localhost"}, "outboundTag": "blocked"},
			map[string]any{"type": "field", "ip": []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/128", "::1/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001:db8::/32", "fc00::/7", "fe80::/10", "ff00::/8"}, "outboundTag": "blocked"},
		}},
	}
}

func handler(s settings, upstream string) http.Handler {
	target, _ := url.Parse(upstream)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			c, err := net.DialTimeout("tcp", target.Host, time.Second)
			if err != nil {
				http.Error(w, "not ready", 503)
				return
			}
			c.Close()
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprintln(w, "ok")
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == s.Path && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			// Never forward client-supplied routing identity to the loopback service.
			r.Header.Del("Forwarded")
			r.Header.Del("X-Forwarded-For")
			r.Header.Del("X-Forwarded-Host")
			r.Header.Del("X-Forwarded-Proto")
			proxy.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

func run() error {
	s, err := load()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "node-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	b, err := json.Marshal(config(s))
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		return err
	}
	bin := os.Getenv("XRAY_BIN")
	if bin == "" {
		bin = "/usr/local/bin/xray"
	}
	check := exec.Command(bin, "run", "-test", "-config", path)
	if out, err := check.CombinedOutput(); err != nil {
		return fmt.Errorf("Xray config validation failed: %w: %s", err, out)
	}
	if len(os.Args) == 2 && os.Args[1] == "check" {
		fmt.Println("configuration valid")
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	child := exec.Command(bin, "run", "-config", path)
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait(); close(done) }()
	defer func() {
		_ = child.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = child.Process.Kill()
		}
	}()
	ready := false
	for i := 0; i < 100; i++ {
		c, e := net.DialTimeout("tcp", "127.0.0.1:10000", 50*time.Millisecond)
		if e == nil {
			c.Close()
			ready = true
			break
		}
		select {
		case err := <-done:
			return fmt.Errorf("Xray stopped during startup: %v", err)
		case <-ctx.Done():
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !ready {
		return errors.New("Xray startup timeout")
	}
	server := &http.Server{Addr: ":" + s.Port, Handler: handler(s, "http://127.0.0.1:10000"), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	log.Printf("HTTP/WebSocket listener ready on port %s", s.Port)
	select {
	case <-ctx.Done():
	case childErr := <-done:
		err = fmt.Errorf("Xray stopped: %v", childErr)
	case err = <-serveErr:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	return err
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
