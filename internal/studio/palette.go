package studio

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

// RGB remains the draft sent to the device. Keep HSV separately so selecting a
// hue while the color is gray or black does not lose the chosen hue.
type colorPaletteState struct {
	hue, saturation, value float64
	lastRGB                ui.Color
}

func (s *colorPaletteState) syncRGB(col ui.Color) {
	if col == s.lastRGB {
		return
	}
	r, g, b := float64(col.R)/255, float64(col.G)/255, float64(col.B)/255
	hi, lo := max(r, g, b), min(r, g, b)
	delta := hi - lo
	s.value = hi
	if hi > 0 {
		s.saturation = delta / hi
	}
	if delta > 0 {
		switch hi {
		case r:
			s.hue = 60 * math.Mod((g-b)/delta, 6)
		case g:
			s.hue = 60 * ((b-r)/delta + 2)
		default:
			s.hue = 60 * ((r-g)/delta + 4)
		}
		if s.hue < 0 {
			s.hue += 360
		}
	}
	s.lastRGB = col
}

func paletteRGB(h, s, v float64) ui.Color {
	chroma := v * s
	h = math.Mod(h, 360) / 60
	x := chroma * (1 - math.Abs(math.Mod(h, 2)-1))
	r, g, b := 0., 0., 0.
	switch {
	case h < 1:
		r, g = chroma, x
	case h < 2:
		r, g = x, chroma
	case h < 3:
		g, b = chroma, x
	case h < 4:
		g, b = x, chroma
	case h < 5:
		r, b = x, chroma
	default:
		r, b = chroma, x
	}
	m := v - chroma
	channel := func(n float64) byte { return byte(math.Round(min(1, max(0, n+m)) * 255)) }
	return ui.RGB(channel(r), channel(g), channel(b))
}

func (a *App) applyPaletteColor() {
	col := paletteRGB(a.palette.hue, a.palette.saturation, a.palette.value)
	a.palette.lastRGB = col
	a.red, a.green, a.blue = float64(col.R), float64(col.G), float64(col.B)
	a.colorDirty = true
}

func (a *App) colorPalette(c *ui.Context) {
	a.palette.syncRGB(ui.RGB(byte(a.red), byte(a.green), byte(a.blue)))
	ui.Column(c).Gap(10).Children(func() {
		ui.Text(c, "调色板").Bold()
		var painted ui.Rect
		ui.Box(c).FillWidth().Height(144).Focusable().Label("饱和度与亮度").
			Tooltip("左右调整饱和度，上下调整亮度；支持拖动和方向键").
			Cursor(ui.CursorCrosshair).Disabled(a.busy).
			HandleInput(func(ev ui.InputEvent) bool {
				if a.busy {
					return false
				}
				switch ev.Kind {
				case ui.InputPointerDown, ui.InputPointerMove, ui.InputPointerUp:
					if ev.Button != 0 {
						return false
					}
					if ev.Canceled {
						return true
					}
					if painted.W <= 0 || painted.H <= 0 {
						return false
					}
					a.palette.saturation = min(1, max(0, float64(ev.X/painted.W)))
					a.palette.value = min(1, max(0, 1-float64(ev.Y/painted.H)))
				case ui.InputKeyDown:
					switch ev.Key {
					case ui.KeyLeft:
						a.palette.saturation = max(0, a.palette.saturation-.01)
					case ui.KeyRight:
						a.palette.saturation = min(1, a.palette.saturation+.01)
					case ui.KeyUp:
						a.palette.value = min(1, a.palette.value+.01)
					case ui.KeyDown:
						a.palette.value = max(0, a.palette.value-.01)
					default:
						return false
					}
				default:
					return false
				}
				a.applyPaletteColor()
				return true
			}).Draw(func(p *ui.Painter, r ui.Rect) {
			painted = r
			hue := paletteRGB(a.palette.hue, 1, 1)
			p.FillGradient(r, ui.LinearGradient{From: ui.RGB(255, 255, 255), To: hue, Angle: 90}, 6)
			p.FillGradient(r, ui.LinearGradient{From: ui.RGBA(0, 0, 0, 0), To: ui.RGB(0, 0, 0), Angle: 180}, 6)
			x, y := r.X+r.W*float32(a.palette.saturation), r.Y+r.H*float32(1-a.palette.value)
			p.Stroke(ui.Rect{X: x - 6, Y: y - 6, W: 12, H: 12}, ui.RGB(0, 0, 0), 6, 3)
			p.Stroke(ui.Rect{X: x - 6, Y: y - 6, W: 12, H: 12}, ui.RGB(255, 255, 255), 6, 1.5)
		})
		if ui.SliderBase(c, &a.palette.hue, 0, 360).Height(22).PaddingX(7).Label("色相").Disabled(a.busy).
			Draw(func(p *ui.Painter, r ui.Rect) {
				track := ui.Rect{X: r.X + 7, Y: r.Y + 5, W: r.W - 14, H: 12}
				for i := 0; i < 6; i++ {
					part := ui.Rect{X: track.X + track.W*float32(i)/6, Y: track.Y, W: track.W/6 + .5, H: track.H}
					p.FillGradient(part, ui.LinearGradient{From: paletteRGB(float64(i)*60, 1, 1), To: paletteRGB(float64(i+1)*60, 1, 1), Angle: 90}, 0)
				}
				x := track.X + track.W*float32(a.palette.hue/360)
				p.Fill(ui.Rect{X: x - 5, Y: r.Y + 1, W: 10, H: 20}, ui.RGB(255, 255, 255), 4)
				p.Stroke(ui.Rect{X: x - 5, Y: r.Y + 1, W: 10, H: 20}, border, 4, 1)
			}).Changed() {
			a.applyPaletteColor()
		}
		ui.Row(c).Gap(12).Children(func() {
			col := ui.RGB(byte(a.red), byte(a.green), byte(a.blue))
			ui.Box(c).Size(52, 30).Radius(5).Border(1, border).Background(col)
			ui.Text(c, fmt.Sprintf("待应用颜色  #%02X%02X%02X", col.R, col.G, col.B)).Bold()
		})
	})
}
