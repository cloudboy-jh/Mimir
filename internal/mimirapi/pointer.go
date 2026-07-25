package mimirapi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const EnvHome = "MIMIR_HOME"

type Pointer struct {
	URL   string
	Token string
}

func ConfigPath() (string, error) {
	if home := strings.TrimSpace(os.Getenv(EnvHome)); home != "" {
		return filepath.Join(home, "config"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mimir", "config"), nil
}

func TokenPath() (string, error) {
	path, err := ConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(path), "token"), nil
}

func LoadPointer() (Pointer, error) {
	path, err := ConfigPath()
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
	var pointer Pointer
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"")
		if strings.TrimSpace(key) == "url" {
			pointer.URL = strings.TrimRight(value, "/")
		}
	}
	tokenFile, err := TokenPath()
	if err != nil {
		return Pointer{}, err
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return Pointer{}, fmt.Errorf("Mimir machine token is missing; run mimir login")
	}
	pointer.Token = strings.TrimSpace(string(token))
	if pointer.URL == "" || pointer.Token == "" {
		return Pointer{}, fmt.Errorf("invalid Mimir pointer config: url and token are required")
	}
	return pointer, nil
}

func SavePointer(pointer Pointer) error {
	if strings.TrimSpace(pointer.URL) == "" || strings.TrimSpace(pointer.Token) == "" {
		return fmt.Errorf("url and token are required")
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	home := filepath.Dir(path)
	if err := securePointerHome(home); err != nil {
		return err
	}
	body := fmt.Sprintf("url = %q\n", strings.TrimRight(pointer.URL, "/"))
	tokenFile, err := TokenPath()
	if err != nil {
		return err
	}
	targets := []pointerWrite{{path: path, data: []byte(body)}, {path: tokenFile, data: []byte(pointer.Token + "\n")}}
	for _, target := range targets {
		if err := validatePointerTarget(home, target.path); err != nil {
			return err
		}
	}
	for i := range targets {
		staged, err := stagePointerWrite(home, targets[i].data)
		if err != nil {
			cleanupPointerWrites(targets)
			return err
		}
		targets[i].staged = staged
	}
	defer cleanupPointerWrites(targets)
	for _, target := range targets {
		if err := validatePointerTarget(home, target.path); err != nil {
			return err
		}
	}
	for i := range targets {
		if err := replacePointerWrite(home, &targets[i]); err != nil {
			return rollbackPointerWrites(targets, i, err)
		}
	}
	for i := range targets {
		if targets[i].backup != "" {
			if err := os.Remove(targets[i].backup); err != nil {
				return err
			}
			targets[i].backup = ""
		}
	}
	return nil
}

type pointerWrite struct {
	path, staged, backup string
	data                 []byte
	replaced             bool
}

var commitPointerWrite = os.Rename

func stagePointerWrite(home string, data []byte) (string, error) {
	temp, err := os.CreateTemp(home, ".mimir-pointer-*")
	if err != nil {
		return "", err
	}
	path := temp.Name()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		_ = os.Remove(path)
		return "", err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func replacePointerWrite(home string, write *pointerWrite) error {
	if err := validatePointerTarget(home, write.path); err != nil {
		return err
	}
	if _, err := os.Lstat(write.path); err == nil {
		backup, err := os.CreateTemp(home, ".mimir-pointer-backup-*")
		if err != nil {
			return err
		}
		write.backup = backup.Name()
		if err := backup.Close(); err != nil {
			return err
		}
		if err := os.Remove(write.backup); err != nil {
			return err
		}
		if err := os.Rename(write.path, write.backup); err != nil {
			return err
		}
	}
	if err := commitPointerWrite(write.staged, write.path); err != nil {
		if write.backup != "" {
			if restoreErr := os.Rename(write.backup, write.path); restoreErr != nil {
				return errors.Join(err, fmt.Errorf("restoring pointer backup %s: %w", write.backup, restoreErr))
			}
			write.backup = ""
		}
		return err
	}
	write.staged = ""
	write.replaced = true
	return nil
}

func rollbackPointerWrites(writes []pointerWrite, failed int, cause error) error {
	var rollbackErr error
	for i := failed - 1; i >= 0; i-- {
		if !writes[i].replaced {
			continue
		}
		if err := os.Remove(writes[i].path); err != nil && !os.IsNotExist(err) {
			rollbackErr = errors.Join(rollbackErr, err)
		}
		if writes[i].backup != "" {
			if err := os.Rename(writes[i].backup, writes[i].path); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			} else {
				writes[i].backup = ""
			}
		}
	}
	return errors.Join(cause, rollbackErr)
}

func cleanupPointerWrites(writes []pointerWrite) {
	for _, write := range writes {
		if write.staged != "" {
			_ = os.Remove(write.staged)
		}
	}
}

func securePointerHome(home string) error {
	info, err := os.Lstat(home)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(home, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(home)
	}
	if err != nil {
		return err
	}
	for component := home; ; component = filepath.Dir(component) {
		info, err = os.Lstat(component)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("refusing to use symlinked or non-directory MIMIR_HOME component %s", component)
		}
		if parent := filepath.Dir(component); parent == component {
			break
		}
	}
	return os.Chmod(home, 0o700)
}

func validatePointerTarget(home, path string) error {
	if filepath.Dir(path) != home {
		return fmt.Errorf("refusing to write pointer outside MIMIR_HOME")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace symlinked or non-regular pointer file %s", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
