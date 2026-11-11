package studio

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"openpixelbar/internal/pixelbar"
)

func TestPaletteRGBSynchronization(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := New()
	a.config = ""
	a.switchMode(colorMode)
	a.applyReadColor(pixelbar.RGB{R: 0, G: 255, B: 0}, nil)
	tt := ui.NewTester(a.View, 1080, 720)
	plane, ok := tt.Find("饱和度与亮度")
	if !ok || plane.W == 0 {
		t.Fatal("missing color plane")
	}
	if a.palette.hue != 120 || a.palette.saturation != 1 || a.palette.value != 1 {
		t.Fatal("device RGB did not move palette selection")
	}
	if err := tt.Click("色相"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyHome)
	if a.red != 255 || a.green != 0 || a.blue != 0 {
		t.Fatal("hue slider did not update RGB")
	}
	a.colorDirty = false
	a.applyReadColor(pixelbar.RGB{R: 0, G: 255, B: 0}, nil)
	tt.Frame()
	preview := a.previewColor
	tt.Press(plane.X+plane.W/2, plane.Y+plane.H/2)
	tt.Move(plane.X+plane.W/4, plane.Y+plane.H/4)
	tt.Release(plane.X+plane.W/4, plane.Y+plane.H/4)
	if math.Abs(a.red-143) > 1 || math.Abs(a.green-191) > 1 || math.Abs(a.blue-143) > 1 || !a.colorDirty {
		t.Fatalf("palette drag did not update RGB: %v %v %v", a.red, a.green, a.blue)
	}
	if a.previewColor != preview {
		t.Fatal("draft changed confirmed preview")
	}
	// Keyboard changes use the same selected point as mouse input.
	before := a.palette.saturation
	tt.Key(0, ui.KeyRight)
	if a.palette.saturation <= before {
		t.Fatal("keyboard cannot adjust selected color")
	}
	// Changing an RGB channel must reposition the palette without a second click.
	if err := tt.Click("红 R"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyHome)
	if a.red != 0 || a.palette.lastRGB != ui.RGB(0, byte(a.green), byte(a.blue)) || a.palette.saturation != 1 {
		t.Fatal("RGB slider did not sync palette")
	}
	if err := tt.Click("#3296FF"); err != nil {
		t.Fatal(err)
	}
	if a.palette.lastRGB != ui.RGB(50, 150, 255) {
		t.Fatal("preset did not sync palette")
	}
	a.applyReadColor(pixelbar.RGB{R: 255, G: 0, B: 0}, nil)
	tt.Frame()
	if a.palette.lastRGB != ui.RGB(50, 150, 255) {
		t.Fatal("poll overwrote draft palette")
	}
	if err := tt.Click("恢复为已读取颜色"); err != nil {
		t.Fatal(err)
	}
	if a.palette.lastRGB != ui.RGB(255, 0, 0) || a.palette.hue != 0 || a.colorDirty {
		t.Fatal("restore did not sync all controls")
	}
	// Pointer capture clamps a drag beyond the plane; it must not wrap channels.
	tt.Press(plane.X+plane.W/2, plane.Y+plane.H/2)
	tt.Release(plane.X+plane.W+30, plane.Y-20)
	if a.red != 255 || a.green != 0 || a.blue != 0 {
		t.Fatal("out-of-bounds drag not clamped")
	}
	a.busy = true
	tt.Frame()
	tt.ClickAt(plane.X+plane.W/2, plane.Y+plane.H/2)
	if a.red != 255 || a.green != 0 || a.blue != 0 {
		t.Fatal("palette changed during device write")
	}
	a.busy = false
	tt.Frame()
	if dir := os.Getenv("OPENPB_SCREENSHOT"); dir != "" {
		saveScreenshot(t, tt, filepath.Join(dir, "palette-small.png"))
		tt.SetSize(1320, 880)
		saveScreenshot(t, tt, filepath.Join(dir, "palette.png"))
	}
}

func TestPaletteKeepsHueThroughBlackAndGray(t *testing.T) {
	a := &App{}
	a.palette.syncRGB(ui.RGB(0, 0, 255))
	a.palette.value = 0
	a.applyPaletteColor()
	a.palette.syncRGB(ui.RGB(byte(a.red), byte(a.green), byte(a.blue)))
	a.palette.hue = 120
	a.applyPaletteColor()
	a.palette.syncRGB(ui.RGB(0, 0, 0))
	a.palette.value = 1
	a.applyPaletteColor()
	if a.red != 0 || a.green != 255 || a.blue != 0 {
		t.Fatal("choosing hue on black lost hue or saturation")
	}
	a.palette.syncRGB(ui.RGB(128, 128, 128))
	a.palette.saturation = 1
	a.applyPaletteColor()
	if a.red != 0 || a.green != 128 || a.blue != 0 {
		t.Fatal("gray lost last hue")
	}
	for _, col := range []ui.Color{ui.RGB(255, 200, 111), ui.RGB(0, 150, 4), ui.RGB(50, 150, 255), ui.RGB(255, 60, 100), ui.RGB(255, 255, 255), ui.RGB(0, 0, 0)} {
		a.palette.syncRGB(col)
		if got := paletteRGB(a.palette.hue, a.palette.saturation, a.palette.value); got != col {
			t.Fatalf("RGB round trip: %v -> %v", col, got)
		}
	}
}
