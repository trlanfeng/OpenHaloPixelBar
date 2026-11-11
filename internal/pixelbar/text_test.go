package pixelbar

import (
	"golang.org/x/image/font/gofont/goregular"
	"strings"
	"testing"
)

func TestTextFramesAndMotion(t *testing.T) {
	for _, motion := range []string{"静态", "上下移动", "左右移动"} {
		frames, delays, err := RenderText(goregular.TTF, 0, TextOptions{"Hello PixelBar", 24, motion, 150})
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if motion != "静态" {
			want = 8
		}
		if len(frames) != want || len(delays) != want {
			t.Fatal("frame count")
		}
		lit := 0
		for _, p := range frames[0] {
			if p {
				lit++
			}
		}
		if lit == 0 {
			t.Fatal("blank text")
		}
		if want == 8 && frames[0] == frames[4] {
			t.Fatal("animation did not move")
		}
		if _, err := Encode(frames, delays); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTextRejectsInvalidOrMissingGlyphs(t *testing.T) {
	for _, o := range []TextOptions{{"", 24, "静态", 150}, {"你好", 24, "静态", 150}, {strings.Repeat("W", 40), 24, "静态", 150}, {"Hello", 33, "静态", 150}, {"Hello", 24, "静态", 99}, {"Hello\nWorld", 24, "静态", 150}, {"Hello", 24, "unknown", 150}} {
		if _, _, err := RenderText(goregular.TTF, 0, o); err == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
	if _, _, err := RenderText([]byte("bad font"), 0, TextOptions{"Hello", 24, "静态", 150}); err == nil {
		t.Fatal("invalid font accepted")
	}
	frames, _, err := RenderText(goregular.TTF, 0, TextOptions{strings.Repeat("W", 40), 24, "左右移动", 150})
	if err != nil || len(frames) != 8 {
		t.Fatal("long horizontal text", err)
	}
}
