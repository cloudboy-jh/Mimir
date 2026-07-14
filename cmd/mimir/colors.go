package main

import (
	"fmt"
	"io"
	"os"
)

const (
	mimirGold       = "197;194;102" // #c5c266
	mimirForest     = "31;50;39"    // #1f3227
	mimirMint       = "126;192;164" // #7ec0a4
	mimirGreen      = "158;192;133" // #9ec085
	mimirTeal       = "30;107;113"  // #1e6b71
	mimirOlive      = "136;127;59"  // #887f3b
	mimirMutedGreen = "106;130;100" // #6a8264
)

func terminalColor(out io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	return ok && isTerminal(file)
}

func cliColor(enabled bool, text, rgb string, bold bool) string {
	if !enabled {
		return text
	}
	weight := ""
	if bold {
		weight = "1;"
	}
	return fmt.Sprintf("\x1b[%s38;2;%sm%s\x1b[0m", weight, rgb, text)
}
