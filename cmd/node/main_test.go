package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func valid(t *testing.T) settings {
	t.Helper()
	t.Setenv("VLESS_UUID", "12345678-1234-4234-8234-123456789abc")
	t.Setenv("WS_PATH", "/test-only-path-1234")
	t.Setenv("PORT", "8080")
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestLoad(t *testing.T) {
	valid(t)
	for _, tc := range []struct{ k, v string }{
		{"VLESS_UUID", ""}, {"VLESS_UUID", "00000000-0000-0000-0000-000000000000"},
		{"WS_PATH", "/short"}, {"WS_PATH", "/long-but-invalid?query"},
		{"PORT", "80"}, {"PORT", "10000"}, {"PORT", "8080oops"}, {"PORT", "65536"},
	} {
		t.Run(tc.k+tc.v, func(t *testing.T) {
			valid(t)
			t.Setenv(tc.k, tc.v)
			if _, err := load(); err == nil {
				t.Fatal("accepted invalid setting")
			}
		})
	}
}
func TestConfig(t *testing.T) {
	s := valid(t)
	b, err := json.Marshal(config(s))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	in := c["inbounds"].([]any)[0].(map[string]any)
	if in["listen"] != "127.0.0.1" {
		t.Fatal("backend must remain loopback-only")
	}
	if c["log"].(map[string]any)["access"] != "none" {
		t.Fatal("access logs must be off")
	}
}
func TestRoutes(t *testing.T) {
	s := valid(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer upstream.Close()
	h := handler(s, upstream.URL)
	for _, tc := range []struct {
		path, method string
		code         int
	}{
		{"/healthz", "GET", 200}, {"/", "GET", 404}, {s.Path, "GET", 404}, {"/healthz", "POST", 404},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s: got %d", tc.path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", s.Path, nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("WS route not forwarded: %d", w.Code)
	}
	upstream.Close()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 503 {
		t.Fatal("health must fail when Xray is unavailable")
	}
}
