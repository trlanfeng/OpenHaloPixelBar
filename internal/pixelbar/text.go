package pixelbar

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type TextOptions struct {
	Text   string
	Size   int
	Motion string
	Delay  int
}

// RenderText produces the exact monochrome frames used by preview, export and
// upload. It runs independently of UI scaling and uses no platform UI renderer.
func RenderText(data []byte, index int, o TextOptions) ([]Frame, []int, error) {
	if strings.TrimSpace(o.Text) == "" || utf8.RuneCountInString(o.Text) > 80 || strings.ContainsAny(o.Text, "\r\n\t") {
		return nil, nil, errors.New("请输入 1～80 字的单行文字")
	}
	if o.Size < 8 || o.Size > 32 || o.Delay < 100 || o.Delay > 2000 {
		return nil, nil, errors.New("字号须为 8～32 像素，帧时长须为 100～2000 ms")
	}
	if o.Motion != "静态" && o.Motion != "左右移动" && o.Motion != "上下移动" {
		return nil, nil, errors.New("不支持的文字动画")
	}
	collection, err := opentype.ParseCollection(data)
	if err != nil {
		return nil, nil, fmt.Errorf("无法解析字体：%w", err)
	}
	f, err := collection.Font(index)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range o.Text {
		glyph, err := f.GlyphIndex(nil, r)
		if err != nil || glyph == 0 {
			return nil, nil, fmt.Errorf("所选字体不支持字符 %q，请换用包含该字符的字体", r)
		}
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(o.Size), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, nil, err
	}
	defer face.Close()
	bounds, _ := font.BoundString(face, o.Text)
	x0, y0 := bounds.Min.X.Floor(), bounds.Min.Y.Floor()
	w, h := bounds.Max.X.Ceil()-x0, bounds.Max.Y.Ceil()-y0
	if w < 1 || h < 1 {
		return nil, nil, errors.New("文字没有可显示的像素")
	}
	if h > Height || w > 2048 || (w > Width && o.Motion != "左右移动") {
		return nil, nil, errors.New("文字超出屏幕，请减小字号、缩短内容或选择左右移动")
	}
	mask := image.NewGray(image.Rect(0, 0, w, h))
	drawer := font.Drawer{Dst: mask, Src: image.White, Face: face, Dot: fixed.P(-x0, -y0)}
	drawer.DrawString(o.Text)
	count := 1
	if o.Motion != "静态" {
		count = 8
	}
	frames, delays := make([]Frame, count), make([]int, count)
	// Eight evenly timed samples of a ping-pong movement, including both ends.
	phase := []int{0, 1, 2, 3, 4, 3, 2, 1}
	for i := range frames {
		x, y := (Width-w)/2, (Height-h)/2
		switch o.Motion {
		case "左右移动":
			if w > Width {
				x = -(w - Width) * phase[i] / 4
			} else {
				travel := min(32, Width-w)
				x += -travel/2 + travel*phase[i]/4
			}
		case "上下移动":
			travel := max(8, Height-h)
			y += -travel/2 + travel*phase[i]/4
		}
		dst := image.NewGray(image.Rect(0, 0, Width, Height))
		draw.Draw(dst, image.Rect(x, y, x+w, y+h), mask, image.Point{}, draw.Src)
		for p, v := range dst.Pix {
			frames[i][p] = v >= 128
		}
		delays[i] = o.Delay
	}
	return frames, delays, nil
}
