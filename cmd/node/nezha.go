package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var nezhaUUIDRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// nezhaConfig is deliberately kept to the small, restrictive agent configuration
// used by this service. JSON is also valid YAML, as accepted by the agent.
type nezhaConfig struct {
	Server                string   `json:"server"`
	ClientSecret          string   `json:"client_secret"`
	UUID                  string   `json:"uuid"`
	TLS                   bool     `json:"tls"`
	InsecureTLS           bool     `json:"insecure_tls"`
	Debug                 bool     `json:"debug"`
	DisableCommandExecute bool     `json:"disable_command_execute"`
	DisableAutoUpdate     bool     `json:"disable_auto_update"`
	DisableForceUpdate    bool     `json:"disable_force_update"`
	DisableNAT            bool     `json:"disable_nat"`
	DisableSendQuery      bool     `json:"disable_send_query"`
	GPU                   bool     `json:"gpu"`
	Temperature           bool     `json:"temperature"`
	SkipConnectionCount   bool     `json:"skip_connection_count"`
	SkipProcsCount        bool     `json:"skip_procs_count"`
	ReportDelay           int      `json:"report_delay"`
	IPReportPeriod        int      `json:"ip_report_period"`
	DNS                   []string `json:"dns"`
}

// systemDNSServers parses resolv.conf nameserver entries, retaining only
// literal IP addresses and returning Nezha's host:port representation.
func systemDNSServers(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	seen := make(map[string]struct{})
	servers := make([]string, 0)
	for scanner.Scan() {
		line := scanner.Text()
		if comment := strings.IndexByte(line, '#'); comment >= 0 {
			line = line[:comment]
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "nameserver" {
			continue
		}
		ip := net.ParseIP(fields[1])
		if ip == nil {
			continue
		}
		server := net.JoinHostPort(ip.String(), "53")
		if _, ok := seen[server]; ok {
			continue
		}
		seen[server] = struct{}{}
		servers = append(servers, server)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, errors.New("no valid system DNS servers")
	}
	return servers, nil
}

// startNezha starts one agent, or returns a no-op stop function when Nezha is
// not configured. It intentionally does not use exec.CommandContext: shutdown
// is a controlled SIGTERM, followed by a bounded wait and then SIGKILL.
func startNezha(ctx context.Context, dir string) (func(), error) {
	server, secret, uuid, tlsValue := os.Getenv("NZ_SERVER"), os.Getenv("NZ_CLIENT_SECRET"), os.Getenv("NZ_UUID"), os.Getenv("NZ_TLS")
	if server == "" && secret == "" && uuid == "" && tlsValue == "" {
		return func() {}, nil
	}
	if server == "" || secret == "" || uuid == "" || tlsValue != "true" {
		return func() {}, errors.New("incomplete or invalid Nezha configuration")
	}
	if !nezhaUUIDRE.MatchString(uuid) || strings.EqualFold(uuid, "00000000-0000-0000-0000-000000000000") {
		return func() {}, errors.New("invalid Nezha UUID")
	}
	host, port, err := net.SplitHostPort(server)
	if err != nil || host == "" {
		return func() {}, errors.New("invalid Nezha server address")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return func() {}, errors.New("invalid Nezha server address")
	}
	resolv, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return func() {}, errors.New("could not read system DNS configuration")
	}
	dns, dnsErr := systemDNSServers(resolv)
	closeErr := resolv.Close()
	if dnsErr != nil || closeErr != nil {
		return func() {}, errors.New("could not read system DNS configuration")
	}

	cfg := nezhaConfig{
		Server: server, ClientSecret: secret, UUID: uuid, TLS: true,
		InsecureTLS: false, Debug: false, DisableCommandExecute: true,
		DisableAutoUpdate: true, DisableForceUpdate: true, DisableNAT: true,
		DisableSendQuery: true, GPU: false, Temperature: false,
		SkipConnectionCount: true, SkipProcsCount: true, ReportDelay: 4,
		IPReportPeriod: 1800, DNS: dns,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return func() {}, errors.New("could not encode Nezha configuration")
	}
	configPath := filepath.Join(dir, "nezha-agent.json")
	f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return func() {}, errors.New("could not write Nezha configuration")
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return func() {}, errors.New("could not write Nezha configuration")
	}
	if err = f.Chmod(0600); err != nil {
		_ = f.Close()
		return func() {}, errors.New("could not secure Nezha configuration")
	}
	if err = f.Close(); err != nil {
		return func() {}, errors.New("could not write Nezha configuration")
	}

	bin := os.Getenv("NEZHA_BIN")
	if bin == "" {
		bin = "/usr/local/bin/nezha-agent"
	}
	cmd := exec.Command(bin, "-c", configPath)
	cmd.Env = nezhaAgentEnv()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return func() {}, errors.New("could not prepare Nezha agent")
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return func() {}, errors.New("could not prepare Nezha agent")
	}
	if err = cmd.Start(); err != nil {
		return func() {}, errors.New("could not start Nezha agent")
	}
	log.Printf("nezha: agent started")

	done := make(chan struct{})
	go func() {
		err := cmd.Wait()
		if err != nil {
			nezhaLog("error", "agent stopped")
		}
		close(done)
	}()
	go nezhaLogStream(stdout)
	go nezhaLogStream(stderr)

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			select {
			case <-done:
				return
			default:
			}
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-done:
		}
	}()
	return stop, nil
}

func nezhaAgentEnv() []string {
	env := make([]string, 0, 4)
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "TZ"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func nezhaLogStream(r io.Reader) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.ToLower(s.Text())
		switch {
		case strings.Contains(line, "error"), strings.Contains(line, "fatal"), strings.Contains(line, "failed"), strings.Contains(line, "失败"), strings.Contains(line, "reconnect"), strings.Contains(line, "exit"):
			nezhaLog("error", "agent reported an error")
		case strings.Contains(line, "connection to ") && strings.Contains(line, " established"):
			// A channel being established is not proof of authentication.
			nezhaLog("connection", "agent channel established")
		}
	}
}

func nezhaLog(category, message string) {
	// Keep this writer intentionally fixed-format: never append agent text.
	log.Printf("nezha %s: %s", category, message)
}
