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

func tokenPath() (string, error) {
	path, err := pointerPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "token"), nil
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
		}
	}
	tokenFile, err := tokenPath()
	if err != nil {
		return Pointer{}, err
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return Pointer{}, fmt.Errorf("Mimir machine token is missing; run mimir login")
	}
	p.Token = strings.TrimSpace(string(token))
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
	body := fmt.Sprintf("url = %q\n", strings.TrimRight(p.URL, "/"))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	tokenFile, err := tokenPath()
	if err != nil {
		return err
	}
	return os.WriteFile(tokenFile, []byte(p.Token+"\n"), 0o600)
}
