package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPointerRoundTrip(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	want := Pointer{URL: "https://mimir.example.workers.dev", Token: "secret"}
	if err := savePointer(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadPointer()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRemoteRequestAuthenticates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization %q", got)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/search" {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	data, err := remoteRequest(context.Background(), http.MethodPost, "/search", map[string]string{"query": "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != `{"ok":true}` {
		t.Fatalf("response %s", data)
	}
}
