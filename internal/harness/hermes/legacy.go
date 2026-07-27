package hermes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var legacySkillHashes = map[string]map[string]bool{
	"SKILL.md": {
		"0edbb1e68958a3f4ecb471af8012f2afc3ec5ce54ed80b4f4a8e6be7551bfa9b": true,
		"3942d092433833b37e950a8a2ae628c6b44c34d239cb57ba766c6f7341e5f8a7": true,
		"616baa2a24398384c8778bace6e61798f67d99a44e66a60161dce08c457fe0f5": true,
		"0272eaf947e6cfd90b2406c2f4ea49db425c6469ab5b9a5ce9ac29a8edf83dd8": true,
		"c51a4d55766a22fe1981c040f8d1b6f3757c07255dbf832ca991ae01e52dbb15": true,
	},
	"references/formats.md": {
		"43c0b5823e7a2caef4c771f984079e69b7a7b87f0238707555b4a7141a2433eb": true,
		"83266a28551ce481e456973342cd7cacc567333585b35ff257e67cdac91372ea": true,
	},
}

func cleanupLegacyMimir(home string) error {
	if err := cleanupLegacySkill(filepath.Join(home, "skills", "mimir")); err != nil {
		return err
	}
	return cleanupLegacyMCPConfig(filepath.Join(home, "config.yaml"))
}

func cleanupLegacySkill(root string) error {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil
	}
	files := 0
	safe := true
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			safe = false
			return fs.SkipDir
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		allowed := legacySkillHashes[filepath.ToSlash(relative)]
		if allowed == nil {
			safe = false
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		if !allowed[hex.EncodeToString(digest[:])] {
			safe = false
		}
		files++
		return nil
	})
	if err != nil {
		return err
	}
	if !safe || (files != 0 && files != len(legacySkillHashes)) {
		return nil
	}
	return os.RemoveAll(root)
}

func cleanupLegacyMCPConfig(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, changed := removeLegacyMCPBlock(current)
	if !changed {
		return nil
	}
	return writeFileAtomic(path, updated, info.Mode().Perm())
}

func removeLegacyMCPBlock(current []byte) ([]byte, bool) {
	newline := []byte("\n")
	if bytes.Contains(current, []byte("\r\n")) {
		newline = []byte("\r\n")
	}
	normalized := strings.ReplaceAll(string(current), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	parent, child := -1, -1
	for index, line := range lines {
		if line == "mcp_servers:" {
			if parent != -1 {
				return current, false
			}
			parent = index
			continue
		}
		if parent != -1 && line == "  mimir:" {
			if child != -1 {
				return current, false
			}
			child = index
		}
	}
	if parent == -1 || child == -1 || child < parent {
		return current, false
	}
	parentEnd := len(lines)
	for index := parent + 1; index < len(lines); index++ {
		if lines[index] != "" && lines[index][0] != ' ' && lines[index][0] != '\t' && !strings.HasPrefix(lines[index], "#") {
			parentEnd = index
			break
		}
	}
	if child >= parentEnd {
		return current, false
	}
	childEnd := parentEnd
	for index := child + 1; index < parentEnd; index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent <= 2 || (strings.HasPrefix(trimmed, "#") && indent < 4) {
			childEnd = index
			break
		}
	}
	if !isLegacyMCPBlock(lines[child:childEnd]) {
		return current, false
	}
	remainingChild := false
	for index := parent + 1; index < parentEnd; index++ {
		if index >= child && index < childEnd {
			continue
		}
		trimmed := strings.TrimSpace(lines[index])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			remainingChild = true
			break
		}
	}
	start, end := child, childEnd
	if !remainingChild {
		start, end = parent, childEnd
	}
	lines = append(lines[:start], lines[end:]...)
	return bytes.Join(stringSliceToBytes(lines), newline), true
}

func isLegacyMCPBlock(lines []string) bool {
	if len(lines) < 2 || lines[0] != "  mimir:" {
		return false
	}
	command, serve := false, false
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "command:"):
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "command:"))
			value = strings.Trim(value, `"'`)
			value = strings.ReplaceAll(value, `\`, "/")
			base := strings.ToLower(filepath.Base(value))
			if base != "mimir" && base != "mimir.exe" {
				return false
			}
			command = true
		case trimmed == "args:":
		case trimmed == `args: ["serve"]` || trimmed == "args: ['serve']":
			serve = true
		case trimmed == "- serve":
			serve = true
		case trimmed == "enabled: true":
		default:
			return false
		}
	}
	return command && serve
}

func stringSliceToBytes(lines []string) [][]byte {
	result := make([][]byte, len(lines))
	for index, line := range lines {
		result[index] = []byte(line)
	}
	return result
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".mimir-hermes-config-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace Hermes config: %w", err)
	}
	return nil
}
