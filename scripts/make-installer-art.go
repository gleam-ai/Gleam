package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

const (
	headerW, headerH   = 150, 57
	sidebarW, sidebarH = 164, 314
)

var bgColor = color.RGBA{R: 0xF5, G: 0xF5, B: 0xF2, A: 0xFF}

func main() {
	logo := loadPNG("assets/icons/gleam-256.png")
	if err := writeBMP("assets/installer-header.bmp", compose(headerW, headerH, logo, 40)); err != nil {
		fail(err)
	}
	if err := writeBMP("assets/installer-sidebar.bmp", compose(sidebarW, sidebarH, logo, 96)); err != nil {
		fail(err)
	}
	fmt.Println("已生成 assets/installer-header.bmp（150×57）与 assets/installer-sidebar.bmp（164×314）")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "生成安装图失败：", err)
	os.Exit(1)
}

func loadPNG(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		fail(err)
	}
	return img
}

func compose(w, h int, logo image.Image, want int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, y, bgColor)
		}
	}
	b := logo.Bounds()
	scale := float64(want) / float64(b.Dy())
	sw, sh := int(float64(b.Dx())*scale), want
	ox, oy := (w-sw)/2, (h-sh)/2
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			sx := b.Min.X + x*b.Dx()/sw
			sy := b.Min.Y + y*b.Dy()/sh
			src := color.NRGBAModel.Convert(logo.At(sx, sy)).(color.NRGBA)
			if src.A == 0 {
				continue
			}
			base := dst.RGBAAt(ox+x, oy+y)
			a := float64(src.A) / 255
			dst.SetRGBA(ox+x, oy+y, color.RGBA{
				R: uint8(float64(src.R)*a + float64(base.R)*(1-a)),
				G: uint8(float64(src.G)*a + float64(base.G)*(1-a)),
				B: uint8(float64(src.B)*a + float64(base.B)*(1-a)),
				A: 0xFF,
			})
		}
	}
	return dst
}

func writeBMP(path string, img *image.RGBA) error {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rowPad := (4 - (w*3)%4) % 4
	pixelBytes := (w*3 + rowPad) * h
	fileSize := 14 + 40 + pixelBytes

	buf := make([]byte, 0, fileSize)
	buf = append(buf, 'B', 'M')
	buf = appendLE(buf, uint32(fileSize))
	buf = appendLE(buf, uint32(0))
	buf = appendLE(buf, uint32(14+40))
	buf = appendLE(buf, uint32(40))
	buf = appendLE(buf, int32(w))
	buf = appendLE(buf, int32(h))
	buf = appendLE(buf, uint16(1))
	buf = appendLE(buf, uint16(24))
	buf = appendLE(buf, uint32(0))
	buf = appendLE(buf, uint32(pixelBytes))
	buf = appendLE(buf, int32(2835))
	buf = appendLE(buf, int32(2835))
	buf = appendLE(buf, uint32(0))
	buf = appendLE(buf, uint32(0))
	pad := make([]byte, rowPad)
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			c := img.RGBAAt(b.Min.X+x, b.Min.Y+y)
			buf = append(buf, c.B, c.G, c.R)
		}
		buf = append(buf, pad...)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}

func appendLE(buf []byte, v any) []byte {
	switch t := v.(type) {
	case uint16:
		return binary.LittleEndian.AppendUint16(buf, t)
	case uint32:
		return binary.LittleEndian.AppendUint32(buf, t)
	case int32:
		return binary.LittleEndian.AppendUint32(buf, uint32(t))
	default:
		panic("unsupported")
	}
}
