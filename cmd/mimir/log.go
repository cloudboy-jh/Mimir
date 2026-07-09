package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// appendLog writes one durable audit line. Failures are silent so product planes
// never hard-depend on log I/O.
func appendLog(cfg ControlConfig, verb, detail, status string) error {
	path, err := logAbsPath(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	// pad verb column for skimmability
	line := fmt.Sprintf("%s  %-16s %s", ts, verb, strings.TrimSpace(detail))
	if status != "" {
		line += " " + status
	}
	line += "\n"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

func mustLoadCfgOrDefault() ControlConfig {
	cfg, err := loadControlConfig()
	if err != nil {
		return defaultControlConfig(deriveMachine())
	}
	return cfg
}
