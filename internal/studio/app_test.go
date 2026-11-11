package studio

import (
	"crypto/sha256"
	"fmt"
	"github.com/egoist/mygo/ui"
	"image"
	"image/png"
	"openpixelbar/internal/pixelbar"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestBundledAnimationMatchesVerifiedWireData(t *testing.T) {
	a := pixelbar.BuiltinThemes()[0].Asset
	var frames []pixelbar.Frame
	for _, f := range a.Frames {
		frames = append(frames, pixelbar.Prepare(f, pixelbar.Options{Threshold: 128, Fit: "完整显示"}))
	}
	b, e := pixelbar.Encode(frames, a.Delays)
	if e != nil {
		t.Fatal(e)
	}
	// SHA-256 of the original Python-generated car-on-road.bin uploaded to the device.
	if got := fmt.Sprintf("%x", sha256.Sum256(b)); got != "3da523714b6914e1d2a46d6eebb0aaf89570673708de4a8fa460991838e1a806" {
		t.Fatalf("wire data changed: %s", got)
	}
}

func TestStudioNavigationAndPlayback(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := New()
	a.config = ""
	tt := ui.NewTester(a.View, 1320, 810)
	if !tt.HasText("公路旅行") {
		t.Fatal("missing asset")
	}
	if e := tt.Click("暂停"); e != nil {
		t.Fatal(e)
	}
	if a.playing {
		t.Fatal("pause failed")
	}
	if e := tt.Click("下一帧"); e != nil {
		t.Fatal(e)
	}
	if a.frame != 1 {
		t.Fatal("next frame")
	}
	if e := tt.Click("播放"); e != nil {
		t.Fatal(e)
	}
	tt.Advance(110 * time.Millisecond)
	if a.frame == 1 {
		t.Fatal("animation did not advance")
	}
	a.appendImported(&item{Name: "静态测试素材", Path: "static.png", Asset: staticTestAsset()})
	tt.Frame()
	if e := tt.Click("静态测试素材"); e != nil {
		t.Fatal(e)
	}
	if len(a.frames) != 1 {
		t.Fatal("static selection")
	}
	if e := tt.Click("从列表移除"); e != nil {
		t.Fatal(e)
	}
	if e := tt.Click("使用说明"); e != nil {
		t.Fatal(e)
	}
	if !tt.HasText("开始你的像素创作") {
		t.Fatal("help missing")
	}
	if e := tt.Click("知道了"); e != nil {
		t.Fatal(e)
	}
	if e := tt.Click("公路旅行"); e != nil {
		t.Fatal(e)
	}
	for _, size := range [][2]int{{1320, 810}, {1080, 680}} {
		tt.SetSize(size[0], size[1])
		if _, ok := tt.Find("发送到音箱  →"); !ok {
			t.Fatal("upload action missing")
		}
	}
	if dir := os.Getenv("OPENPB_SCREENSHOT"); dir != "" {
		for _, size := range [][2]int{{1320, 810}, {1080, 680}} {
			tt.SetSize(size[0], size[1])
			name := "studio.png"
			if size[0] == 1080 {
				name = "studio-small.png"
			}
			f, e := os.Create(filepath.Join(dir, name))
			if e != nil {
				t.Fatal(e)
			}
			e = png.Encode(f, tt.Image())
			f.Close()
			if e != nil {
				t.Fatal(e)
			}
		}
	}
}

func TestImportAndHelpDialogs(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := New()
	a.config = ""
	tt := ui.NewTester(a.View, 1080, 720)
	if e := tt.Click("＋  导入素材"); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"免责声明", "支持的文件格式", "选择图片或 GIF…"} {
		if !tt.HasText(s) {
			t.Fatalf("missing %s", s)
		}
	}
	if !a.importOpen || a.busy {
		t.Fatal("import should show instructions before opening picker")
	}
	if e := tt.Click("东风 · DF4D"); e == nil {
		t.Fatal("modal must block background interaction")
	}
	if dir := os.Getenv("OPENPB_SCREENSHOT"); dir != "" {
		saveScreenshot(t, tt, filepath.Join(dir, "import-dialog.png"))
	}
	tt.Key(0, ui.KeyEscape)
	if a.importOpen {
		t.Fatal("Escape did not dismiss import")
	}
	if e := tt.Click("使用说明"); e != nil {
		t.Fatal(e)
	}
	if dir := os.Getenv("OPENPB_SCREENSHOT"); dir != "" {
		saveScreenshot(t, tt, filepath.Join(dir, "help-dialog.png"))
	}
	if e := tt.Click("知道了"); e != nil {
		t.Fatal(e)
	}
	if a.help {
		t.Fatal("help still open")
	}
}
func saveScreenshot(t *testing.T, tt *ui.Tester, path string) {
	t.Helper()
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = png.Encode(f, tt.Image()); e != nil {
		t.Fatal(e)
	}
}

func TestDevicePollingDoesNotOverlapOrReplaceWorkStatus(t *testing.T) {
	a := &App{status: "保留上传结果"}
	var completion func(error)
	calls := 0
	a.probe = func(done func(error)) { calls++; completion = done }
	now := time.Unix(1000, 0)
	a.pollDevice(now)
	if calls != 1 || !a.checking {
		t.Fatal("startup probe missing")
	}
	a.pollDevice(now.Add(10 * time.Second))
	if calls != 1 {
		t.Fatal("overlapping probe")
	}
	completion(nil)
	a.pollDevice(now.Add(9 * time.Second))
	if calls != 1 {
		t.Fatal("early probe")
	}
	a.busy = true
	a.pollDevice(now.Add(10 * time.Second))
	if calls != 1 {
		t.Fatal("probe during upload")
	}
	a.busy = false
	a.pollDevice(now.Add(20 * time.Second))
	if calls != 2 {
		t.Fatal("probe did not resume")
	}
	completion(fmt.Errorf("device unplugged"))
	if a.connected || a.deviceError == "" || a.status != "保留上传结果" {
		t.Fatal("device status / footer isolation failed")
	}
	a.closed = true
	a.pollDevice(now.Add(time.Minute))
	if calls != 2 {
		t.Fatal("probe after close")
	}
}

func TestPreviewHasNoPixelGaps(t *testing.T) {
	var f pixelbar.Frame
	for i := range f {
		f[i] = true
	}
	col := ui.RGB(255, 200, 111)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(791, 101).Draw(func(p *ui.Painter, r ui.Rect) { drawPixels(p, r, f, col) })
	}, 791, 101)
	for _, scale := range []float32{1, 1.5, 2} {
		tt.SetScale(scale)
		im := tt.Image()
		for y := int(5 * scale); y < int(95*scale); y++ {
			for x := int(5 * scale); x < int(785*scale); x++ {
				r, g, b, _ := im.At(x, y).RGBA()
				if r != 65535 || g != 200*257 || b != 111*257 {
					t.Fatalf("seam at %d,%d scale %v", x, y, scale)
				}
			}
		}
	}
}

func TestThemeSelectionAndLibraryScroll(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	a := New()
	a.config = ""
	a.playing = false
	want := []string{"公路旅行", "东风 · DF4D", "海底漫游", "山水行舟"}
	var names []string
	for _, it := range a.items {
		if it.Path == "" {
			names = append(names, it.Name)
		}
	}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("unexpected built-in catalog: %v", names)
	}
	tt := ui.NewTester(a.View, 1320, 810)
	if err := tt.Click("东风 · DF4D"); err != nil {
		t.Fatal(err)
	}
	if a.items[a.selected].Name != "东风 · DF4D" || len(a.frames) != 8 {
		t.Fatal("train selection did not prepare animation")
	}
	if tt.HasText("从列表移除") {
		t.Fatal("built-in theme must not be removable")
	}
	for _, size := range [][2]int{{1320, 810}, {1080, 680}} {
		tt.SetSize(size[0], size[1])
		if dir := os.Getenv("OPENPB_SCREENSHOT"); dir != "" {
			saveScreenshot(t, tt, filepath.Join(dir, fmt.Sprintf("train-%d.png", size[0])))
		}
	}
	tt.Scroll(100, 300, 0, 2000)
	if err := tt.Click("山水行舟"); err != nil {
		t.Fatalf("last theme must be reachable by scrolling: %v", err)
	}
	if a.items[a.selected].Name != "山水行舟" {
		t.Fatal("wrong scrolled selection")
	}
	if err := tt.Click("下一帧"); err != nil {
		t.Fatal(err)
	}
	if a.frame != 1 {
		t.Fatal("new theme frame navigation failed")
	}
	if dir := os.Getenv("OPENPB_SCREENSHOT"); dir != "" {
		saveScreenshot(t, tt, filepath.Join(dir, "themes-scrolled.png"))
	}
	a.appendImported(&item{Name: "用户素材", Path: "user.png", Asset: staticTestAsset()})
	a.selectItem(len(a.items) - 1)
	tt.Frame()
	if err := tt.Click("从列表移除"); err != nil {
		t.Fatal(err)
	}
	if a.selected != 0 || a.items[len(a.items)-1].Name != "山水行舟" {
		t.Fatal("removing imported item changed built-in catalog")
	}
}

func staticTestAsset() *pixelbar.Asset {
	return &pixelbar.Asset{
		Frames: []image.Image{image.NewGray(image.Rect(0, 0, pixelbar.Width, pixelbar.Height))},
		Delays: []int{100}, Width: pixelbar.Width, Height: pixelbar.Height,
	}
}

func TestBuiltinCatalogDoesNotConsumeRecentCapacity(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("settings round-trip uses an isolated Windows APPDATA directory")
	}
	t.Setenv("APPDATA", t.TempDir())
	a := New()
	builtinCount := len(a.items)
	for i := 0; i < maxRecentItems+1; i++ {
		path := fmt.Sprintf("import-%d.png", i)
		a.appendImported(&item{Name: path, Path: path})
	}
	if len(a.items) != builtinCount+maxRecentItems {
		t.Fatal("built-in catalog reduced imported history capacity")
	}
	for _, it := range a.items[:builtinCount] {
		if it.Path != "" || it.Asset == nil {
			t.Fatal("import evicted a built-in theme")
		}
	}
	if a.items[len(a.items)-1].Path != "import-20.png" {
		t.Fatal("new import missing from full library")
	}
	a.save()
	restored := New()
	if len(restored.items) != len(a.items) {
		t.Fatal("recent history truncated on restart")
	}
	for i := builtinCount; i < len(a.items); i++ {
		if restored.items[i].Path != a.items[i].Path {
			t.Fatal("recent paths changed on restart")
		}
	}
}
