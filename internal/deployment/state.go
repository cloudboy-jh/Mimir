package deployment

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

const deploymentStateSchema = 2

type DeploymentState struct {
	Schema       int    `json:"schema"`
	AccountID    string `json:"account_id"`
	WorkerName   string `json:"worker_name"`
	DatabaseName string `json:"database_name"`
	DatabaseID   string `json:"database_id"`
	BucketName   string `json:"bucket_name,omitempty"`
	URL          string `json:"url,omitempty"`
	VerifiedAt   string `json:"verified_at"`
}

func StatePath() (string, error) {
	pointer, err := mimirapi.ConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(pointer), "cloudflare-deployment.json"), nil
}

func LoadState() (DeploymentState, error) {
	path, err := StatePath()
	if err != nil {
		return DeploymentState{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return DeploymentState{}, nil
	}
	if err != nil {
		return DeploymentState{}, err
	}
	if !info.Mode().IsRegular() {
		return DeploymentState{}, fmt.Errorf("refusing to read non-regular deployment state")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DeploymentState{}, err
	}
	var state DeploymentState
	if err := json.Unmarshal(data, &state); err != nil {
		return DeploymentState{}, fmt.Errorf("decoding deployment state: %w", err)
	}
	if state.Schema != deploymentStateSchema {
		return DeploymentState{}, fmt.Errorf("unsupported deployment state schema %d", state.Schema)
	}
	return state, nil
}

func SaveState(state DeploymentState) error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	home := filepath.Dir(path)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace non-regular deployment state")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	state.Schema = deploymentStateSchema
	state.URL = strings.TrimRight(state.URL, "/")
	if state.VerifiedAt == "" {
		state.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(home, ".cloudflare-deployment-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	backup := path + ".previous"
	if _, err := os.Lstat(path); err == nil {
		if err := os.Rename(path, backup); err != nil {
			return err
		}
		if err := os.Rename(tempPath, path); err != nil {
			_ = os.Rename(backup, path)
			return err
		}
		return os.Remove(backup)
	}
	return os.Rename(tempPath, path)
}
