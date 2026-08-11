package demo

import (
	"context"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestServerServesEmbeddedSPAOnLoopback(t *testing.T) {
	server, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	address, ok := server.Address().(*net.TCPAddr)
	if !ok || !address.IP.IsLoopback() || address.Port == 0 {
		t.Fatalf("listener address = %#v", server.Address())
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = server.Close()
	})

	index := getBody(t, server.URL())
	if !strings.Contains(index, `<div id="app"></div>`) {
		t.Fatalf("root did not serve dashboard index:\n%s", index)
	}
	deepLink := getBody(t, server.URL()+"sessions/example")
	if deepLink != index {
		t.Fatal("deep link did not receive SPA fallback")
	}
	if dotted := getBody(t, server.URL()+"sessions/session.1"); dotted != index {
		t.Fatal("dotted session ID did not receive SPA fallback")
	}
	assetMatch := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindStringSubmatch(index)
	if len(assetMatch) != 2 {
		t.Fatalf("index did not reference a built asset:\n%s", index)
	}
	response, err := http.Get(strings.TrimRight(server.URL(), "/") + assetMatch[1])
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d", response.StatusCode)
	}
	if !strings.Contains(response.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset cache control = %q", response.Header.Get("Cache-Control"))
	}
	response, err = http.Get(server.URL() + "mimir-favicon-32.png")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("favicon status=%d cache=%q", response.StatusCode, response.Header.Get("Cache-Control"))
	}

	request, err := http.NewRequest(http.MethodPost, server.URL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", response.StatusCode)
	}
	response, err = http.Get(server.URL() + "dashboard/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("API status = %d", response.StatusCode)
	}
	response, err = http.Get(server.URL() + "assets/")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("asset namespace status = %d", response.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("demo server did not stop after cancellation")
	}
}

func TestCloseReleasesListenerBeforeServe(t *testing.T) {
	server, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	address := server.Address().String()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		t.Fatal("listener accepted a connection after Close")
	}
}

func getBody(t *testing.T, target string) string {
	t.Helper()
	response, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", target, response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
