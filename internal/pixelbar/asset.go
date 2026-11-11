package pixelbar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type Asset struct {
	Frames        []image.Image
	Delays        []int
	Width, Height int
}
type Options struct {
	Threshold int
	Invert    bool
	Fit       string
	Dither    bool
}

func Load(path string) (*Asset, error) {
	expected := map[string]string{".png": "png", ".jpg": "jpeg", ".jpeg": "jpeg", ".bmp": "bmp", ".webp": "webp", ".gif": "gif"}[strings.ToLower(filepath.Ext(path))]
	if expected == "" {
		return nil, errors.New("仅支持 PNG、JPG/JPEG、BMP、WebP 和 GIF 文件")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, 32<<20+1))
	if e != nil {
		return nil, e
	}
	if len(data) > 32<<20 {
		return nil, errors.New("文件不能超过 32 MB")
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("文件不是有效的图片：%w", err)
	}
	if format != expected {
		return nil, errors.New("文件扩展名与实际图片格式不一致，请使用正确格式的原始文件")
	}
	return Decode(data)
}
func Decode(data []byte) (*Asset, error) {
	cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil {
		return nil, fmt.Errorf("无法读取图片：%w", e)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return nil, errors.New("图片需小于 4000 万像素")
	}
	a := &Asset{Width: cfg.Width, Height: cfg.Height}
	if format != "gif" {
		im, _, e := image.Decode(bytes.NewReader(data))
		if e != nil {
			return nil, e
		}
		if format == "jpeg" {
			im = orient(im, jpegOrientation(data))
		}
		a.Width, a.Height = im.Bounds().Dx(), im.Bounds().Dy()
		a.Frames = []image.Image{im}
		a.Delays = []int{100}
		return a, nil
	}
	n, e := gifCount(data)
	if e != nil {
		return nil, e
	}
	if n < 1 || n > 8 {
		return nil, errors.New("GIF 支持 1～8 帧，请先精简帧数")
	}
	if int64(cfg.Width)*int64(cfg.Height)*int64(n) > 40_000_000 {
		return nil, errors.New("动画所有帧合计需小于 4000 万像素")
	}
	g, e := gif.DecodeAll(bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	background := func(f *image.Paletted) *image.Uniform {
		for _, col := range f.Palette {
			_, _, _, alpha := col.RGBA()
			if alpha == 0 {
				return image.Transparent
			}
		}
		if palette, ok := g.Config.ColorModel.(color.Palette); ok && int(g.BackgroundIndex) < len(palette) {
			return image.NewUniform(palette[g.BackgroundIndex])
		}
		return image.Transparent
	}
	draw.Draw(canvas, canvas.Bounds(), background(g.Image[0]), image.Point{}, draw.Src)
	for i, f := range g.Image {
		before := image.NewNRGBA(canvas.Bounds())
		copy(before.Pix, canvas.Pix)
		draw.Draw(canvas, f.Bounds(), f, f.Bounds().Min, draw.Over)
		frame := image.NewNRGBA(canvas.Bounds())
		copy(frame.Pix, canvas.Pix)
		a.Frames = append(a.Frames, frame)
		a.Delays = append(a.Delays, min(65535, max(100, g.Delay[i]*10)))
		if i < len(g.Disposal) {
			switch g.Disposal[i] {
			case gif.DisposalBackground:
				draw.Draw(canvas, f.Bounds(), background(f), image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				canvas = before
			}
		}
	}
	return a, nil
}

// Inspect the GIF container before DecodeAll can allocate many full frames.
func gifCount(b []byte) (int, error) {
	bad := errors.New("GIF 文件不完整")
	if len(b) < 13 {
		return 0, bad
	}
	p := 13
	if b[10]&128 != 0 {
		p += 3 * (1 << ((b[10] & 7) + 1))
	}
	count := 0
	skip := func() bool {
		for p < len(b) {
			n := int(b[p])
			p++
			if n == 0 {
				return true
			}
			p += n
		}
		return false
	}
	for p < len(b) {
		tag := b[p]
		p++
		switch tag {
		case 0x3b:
			return count, nil
		case 0x21:
			if p >= len(b) {
				return 0, bad
			}
			p++
			if !skip() {
				return 0, bad
			}
		case 0x2c:
			if p+9 > len(b) {
				return 0, bad
			}
			packed := b[p+8]
			p += 9
			if packed&128 != 0 {
				p += 3 * (1 << ((packed & 7) + 1))
			}
			p++
			if !skip() {
				return 0, bad
			}
			count++
			if count > 8 {
				return count, nil
			}
		default:
			return 0, bad
		}
	}
	return 0, bad
}

func Prepare(source image.Image, o Options) Frame {
	b := source.Bounds()
	w, h := b.Dx(), b.Dy()
	sw, sh := Width, Height
	if o.Fit != "拉伸填满" {
		scale := math.Min(float64(Width)/float64(w), float64(Height)/float64(h))
		if o.Fit == "铺满裁切" {
			scale = math.Max(float64(Width)/float64(w), float64(Height)/float64(h))
		}
		sw = max(1, int(math.Round(float64(w)*scale)))
		sh = max(1, int(math.Round(float64(h)*scale)))
	}
	// Scale directly into a bounded destination, including cover's clipped area.
	dst := image.NewRGBA(image.Rect(0, 0, Width, Height))
	rect := image.Rect((Width-sw)/2, (Height-sh)/2, (Width-sw)/2+sw, (Height-sh)/2+sh)
	xdraw.CatmullRom.Scale(dst, rect, source, b, draw.Src, nil)
	values := make([]float64, Width*Height)
	for y := 0; y < Height; y++ {
		for x := 0; x < Width; x++ {
			r, g, b, _ := dst.At(x, y).RGBA()
			v := float64((299*r+587*g+114*b)/1000) / 257
			if o.Invert {
				v = 255 - v
			}
			values[y*Width+x] = v
		}
	}
	var result Frame
	for y := 0; y < Height; y++ {
		for x := 0; x < Width; x++ {
			i := y*Width + x
			result[i] = values[i] >= float64(o.Threshold)
			if o.Dither {
				target := 0.
				if result[i] {
					target = 255
				}
				err := values[i] - target
				if x+1 < Width {
					values[i+1] += err * 7 / 16
				}
				if y+1 < Height {
					if x > 0 {
						values[i+Width-1] += err * 3 / 16
					}
					values[i+Width] += err * 5 / 16
					if x+1 < Width {
						values[i+Width+1] += err / 16
					}
				}
			}
		}
	}
	return result
}
func (f Frame) Image() *image.Gray {
	im := image.NewGray(image.Rect(0, 0, Width, Height))
	for i, on := range f {
		if on {
			im.Pix[i] = 255
		}
	}
	return im
}
func Export(path string, frames []Frame, delays []int, animated bool) error {
	if len(frames) == 0 || len(frames) != len(delays) {
		return errors.New("没有可导出的图案")
	}
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	if !animated {
		e = png.Encode(f, frames[0].Image())
	} else {
		g := &gif.GIF{LoopCount: 0}
		palette := color.Palette{color.Black, color.White}
		for i, frame := range frames {
			im := image.NewPaletted(image.Rect(0, 0, Width, Height), palette)
			for p, on := range frame {
				if on {
					im.Pix[p] = 1
				}
			}
			g.Image = append(g.Image, im)
			g.Delay = append(g.Delay, max(10, (delays[i]+5)/10))
			g.Disposal = append(g.Disposal, gif.DisposalNone)
		}
		e = gif.EncodeAll(f, g)
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}

func jpegOrientation(b []byte) int {
	for p := 2; p+4 < len(b) && b[p] == 255; {
		tag := b[p+1]
		if tag == 0xda || tag == 0xd9 {
			break
		}
		n := int(binary.BigEndian.Uint16(b[p+2 : p+4]))
		if n < 2 || p+2+n > len(b) {
			break
		}
		seg := b[p+4 : p+2+n]
		p += 2 + n
		if tag != 0xe1 || len(seg) < 14 || !bytes.Equal(seg[:6], []byte("Exif\x00\x00")) {
			continue
		}
		t := seg[6:]
		var order binary.ByteOrder = binary.LittleEndian
		if string(t[:2]) == "MM" {
			order = binary.BigEndian
		} else if string(t[:2]) != "II" {
			continue
		}
		off := int(order.Uint32(t[4:8]))
		if off < 0 || off+2 > len(t) {
			continue
		}
		count := int(order.Uint16(t[off : off+2]))
		for i := 0; i < count; i++ {
			at := off + 2 + i*12
			if at+12 > len(t) {
				break
			}
			if order.Uint16(t[at:]) == 0x112 && order.Uint16(t[at+2:]) == 3 && order.Uint32(t[at+4:]) == 1 {
				return int(order.Uint16(t[at+8:]))
			}
		}
	}
	return 1
}
func orient(im image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return im
	}
	b := im.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, y
			switch o {
			case 2:
				dx = w - 1 - x
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dy = h - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, im.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
