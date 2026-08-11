package demoassets

import "embed"

// Files contains the generated fixture dashboard. It is separate from the
// production Worker bundle and is served only by the local demo command.
//
//go:embed static
var Files embed.FS
