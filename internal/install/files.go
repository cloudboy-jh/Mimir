package install

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func filesystemRoot(path string) string {
	if volume := filepath.VolumeName(filepath.Clean(path)); volume != "" {
		return volume + string(filepath.Separator)
	}
	return string(filepath.Separator)
}

func isLink(info os.FileInfo) bool {
	// Windows junctions report as irregular files since Go 1.23.
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

// pathContainsSymlink reports whether any existing component of target is a
// link. target must stay inside root.
func pathContainsSymlink(root, target string) (bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return false, err
	}
	if rel, err := filepath.Rel(root, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("path %s escapes %s", target, root)
	}
	current := filesystemRoot(target)
	rel, err := filepath.Rel(current, target)
	if err != nil {
		return false, err
	}
	parts := []string{""}
	if rel != "." {
		parts = append(parts, strings.Split(rel, string(filepath.Separator))...)
	}
	for _, part := range parts {
		if part != "" {
			current = filepath.Join(current, part)
		}
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if isLink(info) && !allowedDarwinFilesystemAlias(current) {
			return true, nil
		}
	}
	return false, nil
}

func rejectSymlink(path string) error {
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing symlinked path %s", path)
	}
	return nil
}

func allowedDarwinFilesystemAlias(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	want, ok := map[string]string{
		string(filepath.Separator) + "etc": "private/etc",
		string(filepath.Separator) + "tmp": "private/tmp",
		string(filepath.Separator) + "var": "private/var",
	}[filepath.Clean(path)]
	if !ok {
		return false
	}
	target, err := os.Readlink(path)
	return err == nil && filepath.Clean(target) == want
}

func writeFileAtomic(root, path string, data []byte) error {
	if symlink, err := pathContainsSymlink(root, path); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to write symlinked path %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".mimir-artifact-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(root, path); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to replace symlinked path %s", path)
	}
	return os.Rename(tempPath, path)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Dir(path), path, append(data, '\n'))
}
