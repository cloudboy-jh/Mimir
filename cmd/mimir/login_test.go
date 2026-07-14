package main

import "testing"

func TestParseDeploymentURL(t *testing.T) {
	got, err := parseDeploymentURL(`[{"results":[{"value":"https://mimir.example.workers.dev"}]}]`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://mimir.example.workers.dev" {
		t.Fatalf("URL %q", got)
	}
}

func TestSQLQuote(t *testing.T) {
	if got := sqlQuote("jack's machine"); got != "jack''s machine" {
		t.Fatalf("SQL quote %q", got)
	}
}
