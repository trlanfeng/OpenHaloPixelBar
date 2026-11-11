package studio

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/font/gofont/goregular"
	"openpixelbar/internal/pixelbar"
)

func TestModeNavigationPreservesPreviewAndRejectsStaleText(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := New()
	a.config = ""
	a.playing = false
	path := filepath.Join(t.TempDir(), "test.ttf")
	if err := os.WriteFile(path, goregular.TTF, 0600); err != nil {
		t.Fatal(err)
	}
	a.fonts = []fontChoice{{"Test", path}}
	a.textDraft.Font = "Test"
	a.textDraft.Content = "Hello PixelBar"
	imageFirst := a.frames[0]
	tt := ui.NewTester(a.View, 1080, 720)
	if err := tt.Click(textMode); err != nil {
		t.Fatal(err)
	}
	if a.canSend() || len(a.frames) != 0 {
		t.Fatal("text must start without image-mode frames")
	}
	if err := tt.Click("生成预览"); err != nil {
		t.Fatal(err)
	}
	if !a.canSend() || len(a.frames) != 1 {
		t.Fatal(a.status)
	}
	textFirst := a.frames[0]
	a.textDraft.Content = "Changed"
	if a.canSend() {
		t.Fatal("must not upload stale text")
	}
	a.textDraft.Content = "Hello PixelBar"
	a.textDraft.Motion = "上下移动"
	a.generateText()
	if len(a.frames) != 8 || !a.canSend() {
		t.Fatal(a.status)
	}
	a.playing = false
	a.frame = 3
	if err := tt.Click(colorMode); err != nil {
		t.Fatal(err)
	}
	if a.canSend() {
		t.Fatal("color tab must not upload image")
	}
	if err := tt.Click(imageMode); err != nil {
		t.Fatal(err)
	}
	if a.frames[0] != imageFirst || len(a.frames) != 8 {
		t.Fatal("image preview lost")
	}
	if err := tt.Click(textMode); err != nil {
		t.Fatal(err)
	}
	if a.frame != 3 || len(a.frames) != 8 || a.frames[0] == textFirst {
		t.Fatal("text preview lost")
	}
	a.textDraft.Content = ""
	a.generateText()
	if a.canSend() || !a.failed {
		t.Fatal("failed generation must not allow stale upload")
	}
}

func TestColorSyncKeepsDraftAndFailureMarksUnknown(t *testing.T) {
	a := New()
	a.config = ""
	green := pixelbar.RGB{R: 0, G: 150, B: 4}
	a.applyReadColor(green, nil)
	if !a.colorKnown || a.previewColor != ui.RGB(0, 150, 4) || a.green != 150 {
		t.Fatal("color not synced")
	}
	a.red = 80
	a.colorDirty = true
	a.applyReadColor(pixelbar.RGB{R: 20, G: 30, B: 40}, nil)
	if a.red != 80 || a.previewColor != ui.RGB(20, 30, 40) {
		t.Fatal("poll overwrote draft or failed to update preview")
	}
	a.applyReadColor(pixelbar.RGB{}, errors.New("disconnected"))
	if a.colorKnown || a.colorError == "" {
		t.Fatal("stale color marked current")
	}
	a.busy = true
	calls := 0
	a.probe = func(done func(error)) { calls++; done(nil) }
	a.checkDevice(time.Now())
	if calls != 0 {
		t.Fatal("probe overlaps color write")
	}
}

func TestChineseTextAndModeLayouts(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := New()
	a.config = ""
	a.switchMode(textMode)
	if len(a.fonts) == 0 {
		t.Skip("no installed fonts")
	}
	a.generateText()
	if a.failed {
		t.Skipf("system font does not support Chinese: %s", a.status)
	}
	if len(a.frames) != 1 || !a.canSend() {
		t.Fatal(a.status)
	}
	a.applyReadColor(pixelbar.RGB{R: 0, G: 150, B: 4}, nil)
	tt := ui.NewTester(a.View, 1320, 880)
	for _, mode := range []string{imageMode, textMode, colorMode} {
		a.switchMode(mode)
		for _, size := range [][2]int{{1320, 880}, {1080, 720}} {
			tt.SetSize(size[0], size[1])
			for _, label := range []string{imageMode, textMode, colorMode, "使用说明", "最小化", "最大化或还原", "关闭窗口"} {
				if _, ok := tt.Find(label); !ok {
					t.Fatalf("missing %s", label)
				}
			}
			if dir := os.Getenv("OPENPB_SCREENSHOT"); dir != "" {
				name := map[string]string{imageMode: "image-mode", textMode: "text-mode", colorMode: "color-mode"}[mode]
				if size[0] == 1080 {
					name += "-small"
				}
				saveScreenshot(t, tt, filepath.Join(dir, name+".png"))
			}
		}
	}
}
