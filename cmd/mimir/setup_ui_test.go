package main

import (
	"bytes"
	"strings"
	"testing"

	mimirassets "github.com/cloudboy-jh/mimir"
)

func TestWriteITermImage(t *testing.T) {
	var output bytes.Buffer
	writeITermImage(&output, []byte("png"), 64)
	if !strings.Contains(output.String(), "File=inline=1;width=64") {
		t.Fatalf("unexpected iTerm image sequence: %q", output.String())
	}
}

func TestWriteKittyImageChunks(t *testing.T) {
	var output bytes.Buffer
	writeKittyImage(&output, bytes.Repeat([]byte("x"), 5000), 64)
	if !strings.Contains(output.String(), "a=T,f=100,t=d,c=64") || !strings.Contains(output.String(), "m=0;") {
		t.Fatalf("unexpected Kitty image sequence")
	}
}

func TestWriteANSIImage(t *testing.T) {
	var output bytes.Buffer
	if err := writeANSIImage(&output, mimirassets.LogoPNG, 32); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "\x1b[38;2;") {
		t.Fatal("ANSI image has no true-color pixels")
	}
}

func TestWarpUsesITermImageProtocol(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("LC_TERMINAL", "")
	if got := terminalImageProtocol(); got != "iterm" {
		t.Fatalf("terminalImageProtocol() = %q, want iterm", got)
	}
}
