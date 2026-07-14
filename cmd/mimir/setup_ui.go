package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"
	"strings"

	mimirassets "github.com/cloudboy-jh/mimir"
)

const setupLogoWidth = 64

func printSetupBanner(out io.Writer) {
	file, ok := out.(*os.File)
	if !ok || !isTerminal(file) {
		return
	}

	switch terminalImageProtocol() {
	case "kitty":
		writeKittyImage(out, mimirassets.LogoPNG, setupLogoWidth)
	case "iterm":
		writeITermImage(out, mimirassets.LogoPNG, setupLogoWidth)
	default:
		if err := writeANSIImage(out, mimirassets.LogoPNG, setupLogoWidth); err != nil {
			fmt.Fprintln(out, "\x1b[1;38;5;116m◆ mimir\x1b[0m")
		}
	}
	fmt.Fprintln(out)
}

func writeANSIImage(out io.Writer, data []byte, width int) error {
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	bounds := source.Bounds()
	// Half-blocks provide two vertical samples per terminal cell. A small
	// correction keeps the artwork from looking compressed in modern IDE
	// terminals whose cells are slightly shorter than the classic 1:2 ratio.
	height := max(2, bounds.Dy()*width*6/(bounds.Dx()*5))
	if height%2 != 0 {
		height++
	}
	for y := 0; y < height; y += 2 {
		for x := 0; x < width; x++ {
			upper := source.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+y*bounds.Dy()/height)
			lower := source.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+(y+1)*bounds.Dy()/height)
			ur, ug, ub, ua := upper.RGBA()
			lr, lg, lb, la := lower.RGBA()
			switch {
			case ua < 0x2000 && la < 0x2000:
				fmt.Fprint(out, "\x1b[0m ")
			case ua >= 0x2000 && la < 0x2000:
				fmt.Fprintf(out, "\x1b[38;2;%d;%d;%dm▀", ur>>8, ug>>8, ub>>8)
			case ua < 0x2000 && la >= 0x2000:
				fmt.Fprintf(out, "\x1b[38;2;%d;%d;%dm▄", lr>>8, lg>>8, lb>>8)
			default:
				fmt.Fprintf(out, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀", ur>>8, ug>>8, ub>>8, lr>>8, lg>>8, lb>>8)
			}
		}
		fmt.Fprintln(out, "\x1b[0m")
	}
	return nil
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func terminalImageProtocol() string {
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	term := strings.ToLower(os.Getenv("TERM"))
	switch {
	case os.Getenv("KITTY_WINDOW_ID") != "", strings.Contains(program, "ghostty"), strings.Contains(program, "wezterm"), strings.Contains(term, "kitty"):
		return "kitty"
	case strings.Contains(program, "iterm"), strings.Contains(program, "warp"), os.Getenv("LC_TERMINAL") == "iTerm2":
		return "iterm"
	default:
		return ""
	}
}

func writeITermImage(out io.Writer, image []byte, width int) {
	encoded := base64.StdEncoding.EncodeToString(image)
	fmt.Fprintf(out, "\x1b]1337;File=inline=1;width=%d;preserveAspectRatio=1:%s\a\n", width, encoded)
}

func writeKittyImage(out io.Writer, image []byte, width int) {
	const chunkSize = 4096
	encoded := base64.StdEncoding.EncodeToString(image)
	for offset := 0; offset < len(encoded); offset += chunkSize {
		end := min(offset+chunkSize, len(encoded))
		more := 0
		if end < len(encoded) {
			more = 1
		}
		if offset == 0 {
			fmt.Fprintf(out, "\x1b_Ga=T,f=100,t=d,c=%d,q=2,m=%d;%s\x1b\\", width, more, encoded[offset:end])
		} else {
			fmt.Fprintf(out, "\x1b_Gm=%d;%s\x1b\\", more, encoded[offset:end])
		}
	}
	fmt.Fprintln(out)
}
