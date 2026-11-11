// generate-themes exports the same original pixel art used by the application.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"openpixelbar/internal/pixelbar"
	"os"
	"path/filepath"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func main() {
	out := flag.String("out", "dist/themes", "GIF 和主题总览输出目录")
	flag.Parse()
	if err := generate(*out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(*out)
}

func generate(out string) error {
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	themes := pixelbar.BuiltinThemes()
	const scale, rowHeight = 4, 160
	sheet := image.NewRGBA(image.Rect(0, 0, 1024, rowHeight*len(themes)))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{15, 18, 24, 255}), image.Point{}, draw.Src)
	for row, theme := range themes {
		var frames []pixelbar.Frame
		preview := &gif.GIF{LoopCount: 0}
		for i, frame := range theme.Asset.Frames {
			f := pixelbar.Prepare(frame, pixelbar.Options{Threshold: 128, Fit: "完整显示"})
			frames = append(frames, f)
			im := image.NewPaletted(image.Rect(0, 0, 1024, 128), color.Palette{color.RGBA{4, 7, 10, 255}, color.RGBA{255, 200, 111, 255}})
			for y := 0; y < im.Bounds().Dy(); y++ {
				for x := 0; x < im.Bounds().Dx(); x++ {
					if f[(y/scale)*pixelbar.Width+x/scale] {
						im.SetColorIndex(x, y, 1)
					}
				}
			}
			preview.Image = append(preview.Image, im)
			preview.Delay = append(preview.Delay, theme.Asset.Delays[i]/10)
			preview.Disposal = append(preview.Disposal, gif.DisposalNone)
		}
		if err := pixelbar.Export(filepath.Join(out, theme.ID+".gif"), frames, theme.Asset.Delays, true); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(out, theme.ID+"-preview.gif"), func(f *os.File) error { return gif.EncodeAll(f, preview) }); err != nil {
			return err
		}
		d := font.Drawer{Dst: sheet, Src: image.White, Face: basicfont.Face7x13, Dot: fixed.P(12, row*rowHeight+20)}
		d.DrawString(fmt.Sprintf("%02d  %s  /  8 frames  /  %d ms", row+1, theme.ID, theme.Asset.Delays[0]))
		draw.Draw(sheet, image.Rect(0, row*rowHeight+28, 1024, row*rowHeight+156), preview.Image[0], image.Point{}, draw.Src)
	}
	return writeFile(filepath.Join(out, "themes.png"), func(f *os.File) error { return png.Encode(f, sheet) })
}

func writeFile(path string, encode func(*os.File) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = encode(f)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
