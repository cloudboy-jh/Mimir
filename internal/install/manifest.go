package install

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest is $MIMIR_HOME/managed.json: the files Mimir wrote (absolute path ->
// sha256 of the bytes written) plus the installation identity plugins report.
type Manifest struct {
	InstallationID string            `json:"installation_id"`
	BundleVersion  string            `json:"bundle_version"`
	CLI            ManifestCLI       `json:"cli"`
	Harnesses      []string          `json:"harnesses"`
	Files          map[string]string `json:"files"`
}

type ManifestCLI struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	SHA256  string `json:"sha256"`
}

// LoadManifest reads managed.json, seeding it once from a legacy
// install-receipt.json when only the legacy receipt exists.
func LoadManifest() (Manifest, error) {
	paths, err := Paths()
	if err != nil {
		return Manifest{}, err
	}
	return loadManifest(paths)
}

func loadManifest(paths InstallationPaths) (Manifest, error) {
	if err := rejectSymlink(paths.Manifest); err != nil {
		return Manifest{}, err
	}
	data, err := os.ReadFile(paths.Manifest)
	if os.IsNotExist(err) {
		return migrateLegacyReceipt(paths)
	}
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decoding %s: %w", paths.Manifest, err)
	}
	if manifest.Files == nil {
		manifest.Files = map[string]string{}
	}
	return manifest, nil
}

func saveManifest(paths InstallationPaths, manifest Manifest) error {
	if manifest.Files == nil {
		manifest.Files = map[string]string{}
	}
	if manifest.Harnesses == nil {
		manifest.Harnesses = []string{}
	}
	return writeJSONAtomic(paths.Manifest, manifest)
}

func migrateLegacyReceipt(paths InstallationPaths) (Manifest, error) {
	manifest := Manifest{Files: map[string]string{}}
	receiptPath := filepath.Join(paths.MimirHome, "install-receipt.json")
	if err := rejectSymlink(receiptPath); err != nil {
		return Manifest{}, err
	}
	data, err := os.ReadFile(receiptPath)
	if os.IsNotExist(err) {
		return manifest, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var receipt struct {
		InstallationID string      `json:"installation_id"`
		BundleVersion  string      `json:"bundle_version"`
		CLI            ManifestCLI `json:"cli"`
		Harnesses      []string    `json:"harnesses"`
		Artifacts      map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		return Manifest{}, fmt.Errorf("decoding legacy install receipt: %w", err)
	}
	manifest.InstallationID = receipt.InstallationID
	manifest.BundleVersion = receipt.BundleVersion
	manifest.CLI = receipt.CLI
	manifest.Harnesses, _ = NormalizeHarnesses(receipt.Harnesses)
	for target, artifact := range receipt.Artifacts {
		if artifact.SHA256 != "" {
			manifest.Files[target] = artifact.SHA256
		}
	}
	if err := saveManifest(paths, manifest); err != nil {
		return Manifest{}, err
	}
	for _, name := range []string{"install-receipt.json", "install-receipt.json.lock", "install-log.jsonl"} {
		if err := os.Remove(filepath.Join(paths.MimirHome, name)); err != nil && !os.IsNotExist(err) {
			return Manifest{}, err
		}
	}
	return manifest, nil
}

// EnsureInstallationID returns the stable identity of this Mimir installation,
// creating managed.json when nothing has been installed yet.
func EnsureInstallationID() (string, error) {
	paths, err := Paths()
	if err != nil {
		return "", err
	}
	manifest, err := loadManifest(paths)
	if err != nil {
		return "", err
	}
	if manifest.InstallationID != "" {
		return manifest.InstallationID, nil
	}
	if manifest.InstallationID, err = newInstallationID(); err != nil {
		return "", err
	}
	if err := saveManifest(paths, manifest); err != nil {
		return "", err
	}
	return manifest.InstallationID, nil
}

func newInstallationID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("creating installation ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}
