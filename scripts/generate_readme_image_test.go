package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedImagesAreDeterministicAndSized(t *testing.T) {
	dir := t.TempDir()
	for _, test := range []struct {
		name, committed string
		width, height   int
		write           func(string) error
	}{
		{name: "readme", committed: "mimir-readme.png", width: 1400, height: 420, write: writeREADME},
		{name: "wordmark", committed: "mimir-wordmark.png", width: 1000, height: 280, write: writeWordmark},
	} {
		t.Run(test.name, func(t *testing.T) {
			first := filepath.Join(dir, test.name+"-first.png")
			second := filepath.Join(dir, test.name+"-second.png")
			if err := test.write(first); err != nil {
				t.Fatal(err)
			}
			if err := test.write(second); err != nil {
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
			if !bytes.Equal(firstBytes, secondBytes) {
				t.Fatal("generated PNG is not deterministic")
			}
			committed, err := os.ReadFile(filepath.Join("..", "assets", "images", test.committed))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(firstBytes, committed) {
				t.Fatalf("%s is stale; run go run ./scripts/generate-readme-image.go", test.committed)
			}
			config, err := png.DecodeConfig(bytes.NewReader(firstBytes))
			if err != nil {
				t.Fatal(err)
			}
			if config.Width != test.width || config.Height != test.height {
				t.Fatalf("dimensions = %dx%d, want %dx%d", config.Width, config.Height, test.width, test.height)
			}
		})
	}
}
