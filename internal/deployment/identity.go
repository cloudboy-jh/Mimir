package deployment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

func IdentityPath() (string, error) {
	pointer, err := mimirapi.ConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(pointer), "cloudflare-user.json"), nil
}

func LoadIdentity() (Identity, error) {
	path, err := IdentityPath()
	if err != nil {
		return Identity{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Identity{}, err
	}
	var identity Identity
	if err := json.Unmarshal(data, &identity); err != nil {
		return Identity{}, err
	}
	if !identity.LoggedIn {
		return Identity{}, fmt.Errorf("cached Cloudflare user is not logged in")
	}
	return identity, nil
}

func SaveIdentity(identity Identity) error {
	path, err := IdentityPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
