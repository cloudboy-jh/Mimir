package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemMapIsCurrentAndDeterministic(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	if err := writeSystemMap(first); err != nil {
		t.Fatal(err)
	}
	if err := writeSystemMap(second); err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join("..", "assets", "images", "mimir-system-map.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) || !bytes.Equal(firstBytes, committed) {
		t.Fatal("system map is stale; run go run ./scripts/generate-system-map.go")
	}
	config, err := png.DecodeConfig(bytes.NewReader(firstBytes))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 1400 || config.Height != 520 {
		t.Fatalf("dimensions = %dx%d", config.Width, config.Height)
	}
}
