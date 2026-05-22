package transport

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pickFreePort opens a listener on :0 to discover a port the OS guarantees
// is free, then closes it and hands the port back. There's a small TOCTOU
// window before the test-under-test re-binds, but in practice it's reliable
// enough for unit tests and avoids hard-coding ports.
func pickFreePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	defer l.Close()
	_, p, _ := net.SplitHostPort(l.Addr().String())
	port, _ := strconv.Atoi(p)
	return ":" + strconv.Itoa(port)
}

// waitFor polls fn at 25ms intervals until it returns true or the deadline
// elapses. Cheaper than a fixed sleep; required by the test-discipline rule
// against sleep > 100ms in unit tests.
func waitFor(t *testing.T, deadline time.Duration, fn func() bool) {
	t.Helper()
	stop := time.Now().Add(deadline)
	for time.Now().Before(stop) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition not met within %v", deadline)
}

func TestRunHTTP_HealthzAndShutdown(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	addr := pickFreePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunHTTP(ctx, nil, server, HTTPOptions{Addr: addr, AllowAnyOrigin: true})
	}()

	// Wait until the listener is accepting.
	url := "http://127.0.0.1" + addr + "/healthz"
	waitFor(t, 2*time.Second, func() bool {
		resp, err := http.Get(url)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	// Verify the body shape.
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if got["ok"] != true {
		t.Errorf("/healthz body = %v, want {ok:true}", got)
	}

	// Cancel ctx → graceful shutdown should return nil.
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("RunHTTP returned %v on graceful shutdown, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunHTTP did not return after ctx cancel")
	}
}

func TestRunHTTP_CallerProvidedMux(t *testing.T) {
	// Pre-register a /custom endpoint to confirm callers can compose the
	// mux for adapter-specific routes (e.g. SDV /status).
	mux := http.NewServeMux()
	mux.HandleFunc("/custom", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`hi`))
	})

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.0"}, nil)
	addr := pickFreePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go RunHTTP(ctx, nil, server, HTTPOptions{Addr: addr, Mux: mux, AllowAnyOrigin: true})

	custom := "http://127.0.0.1" + addr + "/custom"
	waitFor(t, 2*time.Second, func() bool {
		resp, err := http.Get(custom)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	// /healthz must still work (RunHTTP registers it on the same mux).
	healthz := "http://127.0.0.1" + addr + "/healthz"
	resp, err := http.Get(healthz)
	if err != nil {
		t.Fatalf("GET /healthz on caller mux: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, want 200", resp.StatusCode)
	}
}
