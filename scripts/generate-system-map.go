package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

const renderScale = 2

var (
	canvasColor = color.NRGBA{R: 0x11, G: 0x11, B: 0x10, A: 0xff}
	surface     = color.NRGBA{R: 0x19, G: 0x19, B: 0x18, A: 0xff}
	rule        = color.NRGBA{R: 0x36, G: 0x36, B: 0x33, A: 0xff}
	muted       = color.NRGBA{R: 0x62, G: 0x62, B: 0x5d, A: 0xff}
	ink         = color.NRGBA{R: 0xf2, G: 0xf2, B: 0xef, A: 0xff}
	teal        = color.NRGBA{R: 0x2d, G: 0xd4, B: 0xbf, A: 0xff}
)

func main() {
	out := "assets/images/mimir-system-map.png"
	if len(os.Args) == 2 {
		out = os.Args[1]
	}
	if err := writeSystemMap(out); err != nil {
		panic(err)
	}
}

func writeSystemMap(path string) error {
	hi := image.NewNRGBA(image.Rect(0, 0, 1400*renderScale, 520*renderScale))
	paint(hi, hi.Bounds(), canvasColor)
	paint(hi, scaled(image.Rect(24, 24, 1376, 25)), rule)
	paint(hi, scaled(image.Rect(24, 495, 1376, 496)), rule)

	inputNode(hi, 60, 90, 0)
	inputNode(hi, 60, 220, 1)
	inputNode(hi, 60, 350, 2)
	for _, y := range []int{130, 260, 390} {
		arrow(hi, 330, y, 480, y, teal)
	}

	box(hi, 480, 60, 380, 400)
	pixelM(hi, 595, 150, 30)
	for _, y := range []int{160, 360} {
		arrow(hi, 860, y, 980, y, teal)
	}

	cylinder(hi, 980, 100, "R2")
	cylinder(hi, 980, 300, "D1")
	arrow(hi, 1120, 160, 1210, 220, teal)
	arrow(hi, 1120, 360, 1210, 300, teal)
	outputNode(hi, 1210, 180)

	return encodePNG(path, downsample(hi, 1400, 520))
}

func inputNode(img *image.NRGBA, x, y, kind int) {
	box(img, x, y, 270, 80)
	switch kind {
	case 0:
		for i, width := range []int{170, 205, 145} {
			paint(img, scaled(image.Rect(x+32, y+22+i*14, x+32+width, y+27+i*14)), teal)
		}
	case 1:
		for i, width := range []int{150, 190, 115} {
			paint(img, scaled(image.Rect(x+32+i*14, y+20+i*16, x+32+i*14+width, y+26+i*16)), ink)
		}
	case 2:
		for i := 0; i < 6; i++ {
			circle(img, x+42+i*36, y+40, 7, colorFor(i%2 == 0))
		}
	}
}

func outputNode(img *image.NRGBA, x, y int) {
	box(img, x, y, 140, 160)
	paint(img, scaled(image.Rect(x+25, y+30, x+115, y+91)), rule)
	paint(img, scaled(image.Rect(x+32, y+37, x+108, y+84)), surface)
	paint(img, scaled(image.Rect(x+42, y+48, x+84, y+53)), teal)
	paint(img, scaled(image.Rect(x+42, y+62, x+96, y+67)), muted)
	paint(img, scaled(image.Rect(x+52, y+107, x+88, y+113)), ink)
	paint(img, scaled(image.Rect(x+42, y+126, x+98, y+132)), teal)
}

func cylinder(img *image.NRGBA, x, y int, label string) {
	paint(img, scaled(image.Rect(x, y+20, x+140, y+100)), surface)
	ellipse(img, x+70, y+20, 70, 20, rule)
	ellipse(img, x+70, y+18, 66, 16, surface)
	ellipse(img, x+70, y+100, 70, 20, rule)
	ellipse(img, x+70, y+98, 66, 16, surface)
	drawLabel(img, x+48, y+44, label, 7)
}

func pixelM(img *image.NRGBA, x, y, size int) {
	rows := []string{"10001", "11011", "10101", "10101", "10101", "10101", "10101"}
	for row, line := range rows {
		for col, value := range line {
			if value == '1' {
				paint(img, scaled(image.Rect(x+col*size, y+row*size, x+(col+1)*size, y+(row+1)*size)), teal)
			}
		}
	}
}

func drawLabel(img *image.NRGBA, x, y int, value string, size int) {
	glyphs := map[rune][]string{
		'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
		'D': {"11110", "10001", "10001", "10001", "10001", "10001", "11110"},
		'1': {"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
		'2': {"11110", "00001", "00001", "11110", "10000", "10000", "11111"},
	}
	for _, char := range value {
		for row, line := range glyphs[char] {
			for col, on := range line {
				if on == '1' {
					paint(img, scaled(image.Rect(x+col*size, y+row*size, x+(col+1)*size, y+(row+1)*size)), ink)
				}
			}
		}
		x += size * 6
	}
}

func box(img *image.NRGBA, x, y, width, height int) {
	paint(img, scaled(image.Rect(x, y, x+width, y+height)), rule)
	paint(img, scaled(image.Rect(x+2, y+2, x+width-2, y+height-2)), surface)
}

func arrow(img *image.NRGBA, x1, y1, x2, y2 int, value color.Color) {
	dx, dy := x2-x1, y2-y1
	steps := max(abs(dx), abs(dy))
	for i := 0; i <= steps-12; i++ {
		x := x1 + dx*i/steps
		y := y1 + dy*i/steps
		circle(img, x, y, 2, value)
	}
	for i := 0; i < 13; i++ {
		centerX := x2 - dx*i/steps
		centerY := y2 - dy*i/steps
		radius := i * 5 / 12
		circle(img, centerX, centerY, radius, value)
	}
}

func circle(img *image.NRGBA, cx, cy, radius int, value color.Color) {
	for y := -radius * renderScale; y <= radius*renderScale; y++ {
		for x := -radius * renderScale; x <= radius*renderScale; x++ {
			if x*x+y*y <= radius*radius*renderScale*renderScale {
				img.Set(cx*renderScale+x, cy*renderScale+y, value)
			}
		}
	}
}

func ellipse(img *image.NRGBA, cx, cy, rx, ry int, value color.Color) {
	for y := -ry * renderScale; y <= ry*renderScale; y++ {
		for x := -rx * renderScale; x <= rx*renderScale; x++ {
			if x*x*ry*ry+y*y*rx*rx <= rx*rx*ry*ry*renderScale*renderScale {
				img.Set(cx*renderScale+x, cy*renderScale+y, value)
			}
		}
	}
}

func downsample(source *image.NRGBA, width, height int) *image.NRGBA {
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var r, g, b, a uint32
			for sy := 0; sy < renderScale; sy++ {
				for sx := 0; sx < renderScale; sx++ {
					pixel := source.NRGBAAt(x*renderScale+sx, y*renderScale+sy)
					r += uint32(pixel.R)
					g += uint32(pixel.G)
					b += uint32(pixel.B)
					a += uint32(pixel.A)
				}
			}
			count := uint32(renderScale * renderScale)
			target.SetNRGBA(x, y, color.NRGBA{R: uint8(r / count), G: uint8(g / count), B: uint8(b / count), A: uint8(a / count)})
		}
	}
	return target
}

func encodePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var data bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&data, img); err != nil {
		return err
	}
	return os.WriteFile(path, data.Bytes(), 0o644)
}

func paint(img draw.Image, rect image.Rectangle, value color.Color) {
	draw.Draw(img, rect, image.NewUniform(value), image.Point{}, draw.Src)
}

func scaled(rect image.Rectangle) image.Rectangle {
	return image.Rect(rect.Min.X*renderScale, rect.Min.Y*renderScale, rect.Max.X*renderScale, rect.Max.Y*renderScale)
}

func colorFor(active bool) color.Color {
	if active {
		return teal
	}
	return ink
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
