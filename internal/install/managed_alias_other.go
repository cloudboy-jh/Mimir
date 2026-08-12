//go:build !windows

package install

import "os"

func managedAliasIsLink(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}
