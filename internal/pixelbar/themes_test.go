package pixelbar

import (
	"bytes"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinThemesRoundTripAndProtocol(t *testing.T) {
	ids := map[string]bool{}
	art := map[string]string{}
	for _, theme := range BuiltinThemes() {
		t.Run(theme.ID, func(t *testing.T) {
			if theme.ID == "" || ids[theme.ID] {
				t.Fatal("empty or duplicate export name")
			}
			ids[theme.ID] = true
			a := theme.Asset
			if a.Width != Width || a.Height != Height || len(a.Frames) != 8 || len(a.Delays) != 8 {
				t.Fatal("theme must fit the device canvas and eight-frame limit")
			}
			var frames []Frame
			unique := map[Frame]bool{}
			for i, im := range a.Frames {
				if im.Bounds().Dx() != Width || im.Bounds().Dy() != Height {
					t.Fatal("unexpected frame bounds")
				}
				f := Prepare(im, Options{Threshold: 128, Fit: "完整显示"})
				lit := 0
				for y := 0; y < Height; y++ {
					for x := 0; x < Width; x++ {
						r, g, b, alpha := im.At(x, y).RGBA()
						if (r != 0 && r != 65535) || r != g || r != b || alpha != 65535 {
							t.Fatal("theme must be opaque monochrome")
						}
						if f[y*Width+x] {
							lit++
						}
					}
				}
				if lit == 0 || lit == Width*Height || a.Delays[i] < 100 {
					t.Fatal("empty/solid frame or unsupported speed")
				}
				unique[f] = true
				frames = append(frames, f)
			}
			if len(unique) < 2 {
				t.Fatal("animation is static")
			}
			wire, err := Encode(frames, a.Delays)
			if err != nil {
				t.Fatal(err)
			}
			if other := art[string(wire)]; other != "" {
				t.Fatalf("duplicate animation: %s", other)
			}
			art[string(wire)] = theme.ID
			path := filepath.Join(t.TempDir(), theme.ID+".gif")
			if err := Export(path, frames, a.Delays, true); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			g, err := gif.DecodeAll(bytes.NewReader(data))
			if err != nil || g.LoopCount != 0 {
				t.Fatalf("export must loop forever: %v", err)
			}
			decoded, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			var restored []Frame
			for _, im := range decoded.Frames {
				restored = append(restored, Prepare(im, Options{Threshold: 128, Fit: "完整显示"}))
			}
			restoredWire, err := Encode(restored, decoded.Delays)
			if err != nil || !bytes.Equal(wire, restoredWire) {
				t.Fatal("GIF export/import changed pixels or timing")
			}
		})
	}
}
