package pixelbar

import (
	_ "embed"
	"image"
	"image/color"
	"math"
)

//go:embed assets/car-on-road.gif
var carGIF []byte

// Theme is an original, deliberately simplified monochrome pixel illustration.
// Train names identify the inspiration, not an exact scale model or endorsement.
type Theme struct {
	ID, Name, Kind string
	Asset          *Asset
}

// BuiltinThemes contains the verified road GIF and three original pixel scenes.
// Generated scenes use integer geometry for the device's 256 x 32 raster.
func BuiltinThemes() []Theme {
	specs := []struct {
		id, name, kind string
		delay          int
		draw           func(*pixelCanvas, int)
	}{
		{"df4d", "东风 · DF4D", "内燃机车", 120, drawDF4D},
		{"ocean", "海底漫游", "海洋", 160, drawOcean},
		{"landscape", "山水行舟", "山水", 180, drawLandscape},
	}
	car, err := Decode(carGIF)
	if err != nil {
		panic("invalid embedded road animation: " + err.Error())
	}
	result := []Theme{{"car-on-road", "公路旅行", "内置动画", car}}
	for _, spec := range specs {
		a := &Asset{Width: Width, Height: Height}
		for phase := 0; phase < 8; phase++ {
			c := &pixelCanvas{image.NewGray(image.Rect(0, 0, Width, Height))}
			spec.draw(c, phase)
			a.Frames = append(a.Frames, c.Gray)
			a.Delays = append(a.Delays, spec.delay)
		}
		result = append(result, Theme{spec.id, spec.name, spec.kind, a})
	}
	return result
}

type pixelCanvas struct{ *image.Gray }

func (c *pixelCanvas) dot(x, y int, on bool) {
	v := uint8(0)
	if on {
		v = 255
	}
	c.SetGray(x, y, color.Gray{Y: v})
}

func (c *pixelCanvas) line(x0, y0, x1, y1 int, on bool) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		c.dot(x0, y0, on)
		if x0 == x1 && y0 == y1 {
			return
		}
		e := 2 * err
		if e >= dy {
			err += dy
			x0 += sx
		}
		if e <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (c *pixelCanvas) rect(x, y, w, h int, on bool) {
	for row := y; row < y+h; row++ {
		c.line(x, row, x+w-1, row, on)
	}
}

func (c *pixelCanvas) path(on bool, xy ...int) {
	for i := 2; i < len(xy); i += 2 {
		c.line(xy[i-2], xy[i-1], xy[i], xy[i+1], on)
	}
}

func (c *pixelCanvas) circle(x, y, r int, on bool) {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			d := dx*dx + dy*dy
			if d <= r*r && d >= (r-1)*(r-1) {
				c.dot(x+dx, y+dy, on)
			}
		}
	}
}

func railScene(c *pixelCanvas, phase int) {
	c.path(true, 0, 14, 16, 9, 27, 12, 45, 3, 68, 14)
	c.path(true, 190, 13, 212, 4, 230, 11, 244, 7, 255, 12)
	c.line(0, 27, 255, 27, true)
	for x := -32; x < 288; x += 32 {
		p := x - phase*4
		for dx := 0; dx < 32; dx += 8 {
			c.line(p+dx, 29, p+dx+3, 29, true)
		}
	}
}

func trainWindows(c *pixelCanvas, x, end, y int) {
	for ; x < end; x += 10 {
		c.rect(x, y, 6, 3, true)
	}
}

func bogie(c *pixelCanvas, x, y, wheels, phase int) {
	c.line(x-2, y-2, x+(wheels-1)*6+2, y-2, true)
	for i := 0; i < wheels; i++ {
		wx := x + i*6
		c.circle(wx, y, 2, true)
		c.dot(wx+(phase%2), y, true)
	}
}

func exhaust(c *pixelCanvas, x, phase int) {
	for i := 0; i < 3; i++ {
		p := (phase + i*2) % 8
		c.line(x-p*3, 7-p/2, x-p*3+2, 7-p/2, true)
	}
}

func coach(c *pixelCanvas, x, w, phase int) {
	c.rect(x, 13, w, 11, false)
	c.path(true, x, 23, x, 13, x+w-1, 13, x+w-1, 23, x, 23)
	trainWindows(c, x+5, x+w-6, 15)
	c.line(x+1, 21, x+w-2, 21, true)
	bogie(c, x+8, 25, 2, phase)
	bogie(c, x+w-17, 25, 2, phase)
}

func drawDF4D(c *pixelCanvas, phase int) {
	railScene(c, phase)
	coach(c, 7, 61, phase)
	coach(c, 72, 61, phase)
	c.line(68, 22, 72, 22, true)
	c.line(133, 22, 141, 22, true)
	c.rect(141, 10, 98, 17, false)
	c.path(true, 141, 23, 141, 13, 146, 10, 224, 10, 232, 14, 236, 23, 141, 23)
	// Double cab, long engine room, louvres and six axles.
	c.rect(147, 12, 7, 4, true)
	c.path(true, 221, 12, 225, 12, 229, 16, 221, 16, 221, 12)
	for x := 164; x < 211; x += 4 {
		c.line(x, 13, x, 17, true)
	}
	c.line(143, 19, 232, 19, true)
	c.line(143, 21, 234, 21, true)
	c.line(157, 12, 157, 22, true)
	bogie(c, 153, 25, 3, phase)
	bogie(c, 214, 25, 3, phase)
	c.rect(232, 17, 2, 2, true)
	exhaust(c, 186, phase)
}

func fish(c *pixelCanvas, x, y, phase int) {
	c.path(true, x, y, x+5, y-4, x+15, y-4, x+20, y, x+15, y+4, x+5, y+4, x, y)
	tail := 4 + phase%2
	c.path(true, x+19, y, x+25, y-tail, x+25, y+tail, x+19, y)
	c.dot(x+5, y-1, true)
	c.line(x+11, y-2, x+13, y, true)
}

func drawOcean(c *pixelCanvas, phase int) {
	for x := 0; x < 256; x++ {
		y := 29 + int(math.Round(math.Sin(float64(x)*math.Pi/32)))
		c.dot(x, y, true)
	}
	for _, x := range []int{12, 38, 211, 241} {
		sway := []int{0, 1, 2, 1, 0, -1, -2, -1}[phase]
		c.path(true, x, 29, x-2, 24, x+2, 19, x+sway, 13)
		c.path(true, x, 26, x+5, 22, x+4+sway, 18)
	}
	bob := []int{0, 0, 1, 1, 0, 0, -1, -1}[phase]
	fish(c, 77, 15+bob, phase)
	fish(c, 149, 20-bob, phase+1)
	for i := 0; i < 4; i++ {
		y := 3 + ((i*8 - phase*4 + 32) % 32)
		c.circle(59+i*43, y, 1+i%2, true)
	}
}

func drawLandscape(c *pixelCanvas, phase int) {
	c.path(true, 0, 22, 16, 17, 33, 5, 47, 13, 59, 7, 82, 23)
	c.path(true, 0, 23, 24, 20, 39, 13, 56, 24)
	c.path(true, 172, 22, 191, 9, 205, 15, 222, 4, 242, 17, 255, 12)
	c.circle(160, 6, 4, true)
	// Small pavilion on the bank.
	c.path(true, 207, 21, 217, 17, 227, 21, 207, 21)
	c.line(211, 22, 211, 25, true)
	c.line(223, 22, 223, 25, true)
	c.line(204, 26, 232, 26, true)
	for i := 0; i <= 8; i++ {
		x := i*32 - phase*4
		c.line(x, 29, x+10, 29, true)
		c.line(x+17, 31, x+23, 31, true)
	}
	c.path(true, 94, 24, 100, 28, 136, 28, 143, 24, 94, 24)
	c.path(true, 105, 23, 105, 18, 109, 15, 121, 15, 125, 18, 125, 23)
	c.line(111, 16, 111, 22, true)
	c.circle(132, 17, 1, true)
	c.path(true, 132, 19, 132, 23, 129, 24)
	intensity := []int{0, 1, 2, 1, 0, -1, -2, -1}[phase]
	c.line(133, 20, 145+intensity, 29, true)
	// Pair of birds.
	c.path(true, 89, 6, 92, 8, 95, 6)
	c.path(true, 101, 4, 104, 6, 107, 4)
}
