package main

import (
	"flag"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

type glyph struct {
	rows  []string
	width int
}

var letters = map[rune]glyph{
	'm': {width: 5, rows: []string{"10001", "11011", "10101", "10101", "10101", "10101", "10101"}},
	'i': {width: 3, rows: []string{"010", "000", "110", "010", "010", "010", "111"}},
	'r': {width: 5, rows: []string{"00000", "11010", "10101", "10000", "10000", "10000", "10000"}},
}

var (
	night    = color.NRGBA{R: 0x11, G: 0x11, B: 0x10, A: 0xff}
	rule     = color.NRGBA{R: 0x36, G: 0x36, B: 0x33, A: 0xff}
	graphite = color.NRGBA{R: 0x62, G: 0x62, B: 0x5d, A: 0xff}
	ink      = color.NRGBA{R: 0xf2, G: 0xf2, B: 0xef, A: 0xff}
	teal     = color.NRGBA{R: 0x0f, G: 0x76, B: 0x6e, A: 0xff}
)

func main() {
	readme := flag.String("readme-out", "assets/images/mimir-readme.png", "README hero output")
	wordmark := flag.String("wordmark-out", "assets/images/mimir-wordmark.png", "dashboard wordmark output")
	flag.Parse()

	must(writeREADME(*readme))
	must(writeWordmark(*wordmark))
}

func writeREADME(path string) error {
	canvas := image.NewNRGBA(image.Rect(0, 0, 1400, 420))
	fill(canvas, canvas.Bounds(), night)
	fill(canvas, image.Rect(24, 24, 1376, 25), rule)
	fill(canvas, image.Rect(24, 395, 1376, 396), rule)
	drawWord(canvas, 175, 63, 42, false)
	return encode(path, canvas)
}

func writeWordmark(path string) error {
	canvas := image.NewNRGBA(image.Rect(0, 0, 1000, 280))
	drawWord(canvas, 100, 28, 32, true)
	return encode(path, canvas)
}

func drawWord(canvas *image.NRGBA, x, y, scale int, outlined bool) {
	cursor := x
	for _, letter := range "mimir" {
		glyph := letters[letter]
		for row, line := range glyph.rows {
			for column, pixel := range line {
				if pixel != '1' {
					continue
				}
				rect := image.Rect(cursor+column*scale, y+row*scale, cursor+(column+1)*scale, y+(row+1)*scale)
				accent := letter == 'i' && row == 0
				if outlined {
					fill(canvas, rect, graphite)
					inset := scale / 8
					fill(canvas, rect.Inset(inset), teal)
				} else if accent {
					fill(canvas, rect, teal)
				} else {
					fill(canvas, rect, ink)
				}
			}
		}
		cursor += (glyph.width + 1) * scale
	}
}

func fill(canvas draw.Image, rect image.Rectangle, value color.Color) {
	draw.Draw(canvas, rect, image.NewUniform(value), image.Point{}, draw.Src)
}

func encode(path string, canvas image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(file, canvas); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
