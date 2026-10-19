package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clearNezhaEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"NZ_SERVER", "NZ_CLIENT_SECRET", "NZ_UUID", "NZ_TLS", "NEZHA_BIN"} {
		t.Setenv(key, "")
	}
}

func TestStartNezhaNoopAndValidation(t *testing.T) {
	clearNezhaEnv(t)
	stop, err := startNezha(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stop()

	cases := []struct {
		name, server, secret, uuid, tls string
	}{
		{"partial", "monitor.example:443", "", "12345678-1234-4234-8234-123456789abc", "true"},
		{"non-tls", "monitor.example:443", "secret-that-must-not-leak", "12345678-1234-4234-8234-123456789abc", "false"},
		{"uuid", "monitor.example:443", "secret-that-must-not-leak", "not-a-uuid", "true"},
		{"server", "monitor.example", "secret-that-must-not-leak", "12345678-1234-4234-8234-123456789abc", "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearNezhaEnv(t)
			t.Setenv("NZ_SERVER", tc.server)
			t.Setenv("NZ_CLIENT_SECRET", tc.secret)
			t.Setenv("NZ_UUID", tc.uuid)
			t.Setenv("NZ_TLS", tc.tls)
			_, err := startNezha(context.Background(), t.TempDir())
			if err == nil || (tc.secret != "" && strings.Contains(err.Error(), tc.secret)) {
				t.Fatalf("bad validation error: %v", err)
			}
		})
	}
}

func TestStartNezhaConfigAndEnvironment(t *testing.T) {
	clearNezhaEnv(t)
	t.Setenv("NZ_SERVER", "[::1]:443")
	t.Setenv("NZ_CLIENT_SECRET", "unit-test-secret")
	t.Setenv("NZ_UUID", "12345678-1234-4234-8234-123456789abc")
	t.Setenv("NZ_TLS", "true")
	t.Setenv("VLESS_UUID", "main-secret")
	t.Setenv("WS_PATH", "/private-main-path")
	t.Setenv("CLOUD_API_KEY", "cloud-key")

	dir := t.TempDir()
	envFile := filepath.Join(dir, "child-env")
	script := filepath.Join(dir, "fake-agent")
	contents := "#!/bin/sh\nenv > \"$ENV_FILE\"\ntrap 'exit 0' TERM\nsleep 30\n"
	if err := os.WriteFile(script, []byte(contents), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEZHA_BIN", script)
	// ENV_FILE is intentionally not an allowed child variable; the fake reads it
	// only through its inherited shell expansion, so provide it via the path used
	// by the test after start by using a shell-free wrapper argument is impossible.
	// Instead, the fake writes to a fixed file in dir.
	contents = "#!/bin/sh\nenv > " + envFile + "\ntrap 'exit 0' TERM\nsleep 30\n"
	if err := os.WriteFile(script, []byte(contents), 0700); err != nil {
		t.Fatal(err)
	}

	stop, err := startNezha(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "nezha-agent.json")
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("config mode is %o, want 600", info.Mode().Perm())
	}
	var cfg nezhaConfig
	data, _ := os.ReadFile(configPath)
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "[::1]:443" || cfg.ClientSecret != "unit-test-secret" || !cfg.TLS || cfg.InsecureTLS || cfg.Debug || !cfg.DisableCommandExecute || !cfg.DisableAutoUpdate || !cfg.DisableForceUpdate || !cfg.DisableNAT || !cfg.DisableSendQuery || cfg.GPU || cfg.Temperature || !cfg.SkipConnectionCount || !cfg.SkipProcsCount || cfg.ReportDelay != 4 || cfg.IPReportPeriod != 1800 {
		t.Fatalf("unexpected secure config: %+v", cfg)
	}

	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(envFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake agent did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	envData, _ := os.ReadFile(envFile)
	for _, forbidden := range []string{"NZ_SERVER", "NZ_CLIENT_SECRET", "NZ_UUID", "NZ_TLS", "VLESS_UUID", "WS_PATH", "CLOUD_API_KEY"} {
		if strings.Contains(string(envData), forbidden) {
			t.Fatalf("child environment leaked %s: %s", forbidden, envData)
		}
	}
	stop()
	stop() // idempotent
}

func TestStartNezhaContextStopsAgent(t *testing.T) {
	clearNezhaEnv(t)
	t.Setenv("NZ_SERVER", "monitor.example:443")
	t.Setenv("NZ_CLIENT_SECRET", "test-secret")
	t.Setenv("NZ_UUID", "12345678-1234-4234-8234-123456789abc")
	t.Setenv("NZ_TLS", "true")
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap 'exit 0' TERM\nsleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEZHA_BIN", script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := startNezha(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	stop()
	// A second stop must return promptly even if context cancellation raced it.
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not complete")
	}
}

func TestNezhaLogsNeverExposeRawText(t *testing.T) {
	var b bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&b)
	defer log.SetOutput(previous)
	nezhaLogStream(strings.NewReader("Try to reconnect secret-value\n上报系统信息失败: secret-value\nConnection to secret-value established\narbitrary report secret-value\n"))
	text := b.String()
	if strings.Contains(text, "secret-value") || strings.Contains(text, "report received") {
		t.Fatal("raw agent text or unverified success reached logs")
	}
	if strings.Count(text, "agent reported an error") != 2 || strings.Count(text, "agent channel established") != 1 {
		t.Fatal("incorrect classification of connection/error events")
	}
}
