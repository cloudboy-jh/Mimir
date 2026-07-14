package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const envMimirHome = "MIMIR_HOME"

type Pointer struct {
	URL   string
	Token string
}

func pointerPath() (string, error) {
	if home := strings.TrimSpace(os.Getenv(envMimirHome)); home != "" {
		return filepath.Join(home, "config"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mimir", "config"), nil
}

func loadPointer() (Pointer, error) {
	path, err := pointerPath()
	if err != nil {
		return Pointer{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Pointer{}, fmt.Errorf("Mimir is not connected; run mimir setup")
		}
		return Pointer{}, err
	}
	var p Pointer
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"")
		switch strings.TrimSpace(key) {
		case "url":
			p.URL = strings.TrimRight(value, "/")
		case "token":
			p.Token = value
		}
	}
	if p.URL == "" || p.Token == "" {
		return Pointer{}, fmt.Errorf("invalid Mimir pointer config: url and token are required")
	}
	return p, nil
}

func savePointer(p Pointer) error {
	if strings.TrimSpace(p.URL) == "" || strings.TrimSpace(p.Token) == "" {
		return fmt.Errorf("url and token are required")
	}
	path, err := pointerPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body := fmt.Sprintf("url = %q\ntoken = %q\n", strings.TrimRight(p.URL, "/"), p.Token)
	return os.WriteFile(path, []byte(body), 0o600)
}
