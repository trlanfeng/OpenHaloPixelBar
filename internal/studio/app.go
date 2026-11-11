package studio

import (
	"encoding/json"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"math"
	"openpixelbar/internal/branding"
	"openpixelbar/internal/pixelbar"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type item struct {
	Name, Path, Detail string
	Asset              *pixelbar.Asset
}

const maxRecentItems = 20

type settings struct {
	Recent         []string
	Threshold      float64
	Fit            string
	Invert, Dither bool
	Speed          string
}
type App struct {
	modes
	window                         *mygo.Window
	items                          []*item
	selected                       int
	frames                         []pixelbar.Frame
	delays                         []int
	options                        settings
	frame                          int
	playing, busy, connected, help bool
	processing                     bool
	status, operation              string
	progress                       float64
	failed                         bool
	next                           time.Time
	config                         string
	previewColor                   ui.Color
	importOpen                     bool
	importError                    string
	checking, closed               bool
	checkedAt, nextCheck           time.Time
	deviceError                    string
	probe                          func(func(error))
}

var bg = ui.RGB(15, 18, 24)
var surface = ui.RGB(22, 27, 35)
var border = ui.RGB(43, 51, 63)
var muted = ui.RGB(147, 159, 177)
var accent = ui.RGB(124, 231, 194)
var appIcon, _ = ui.DecodeBitmap(branding.IconPNG)

func New() *App {
	a := &App{playing: true, status: "选择素材，调整预览，然后发送到音箱。", previewColor: ui.RGB(255, 200, 111), options: settings{Threshold: 128, Fit: "完整显示", Speed: "原速"}}
	a.initModes()
	for _, theme := range pixelbar.BuiltinThemes() {
		a.items = append(a.items, &item{Name: theme.Name, Detail: theme.Kind + " · 8 帧", Asset: theme.Asset})
	}
	if dir, e := os.UserConfigDir(); e == nil {
		a.config = filepath.Join(dir, branding.Name, "settings.json")
		b, e := os.ReadFile(a.config)
		// Prefer the current settings, then fall back through previous names.
		for _, legacyName := range []string{"OpenPixelBar", "OpenPB"} {
			if !os.IsNotExist(e) {
				break
			}
			b, e = os.ReadFile(filepath.Join(dir, legacyName, "settings.json"))
		}
		if e == nil {
			var saved settings
			if json.Unmarshal(b, &saved) == nil {
				a.options = saved
			}
		}
	}
	if a.options.Threshold < 1 || a.options.Threshold > 254 {
		a.options.Threshold = 128
	}
	if a.options.Fit != "完整显示" && a.options.Fit != "铺满裁切" && a.options.Fit != "拉伸填满" {
		a.options.Fit = "完整显示"
	}
	if a.options.Speed != "0.5×" && a.options.Speed != "2×" {
		a.options.Speed = "原速"
	}
	for i, path := range a.options.Recent {
		if i >= maxRecentItems {
			break
		}
		a.items = append(a.items, &item{Name: filepath.Base(path), Path: path, Detail: "最近导入 · 点击载入"})
	}
	a.prepare()
	return a
}
func (a *App) prepare() {
	src := a.items[a.selected].Asset
	if src == nil {
		return
	}
	o := pixelbar.Options{Threshold: int(a.options.Threshold), Fit: a.options.Fit, Invert: a.options.Invert, Dither: a.options.Dither}
	speed := a.options.Speed
	work := func() ([]pixelbar.Frame, []int) {
		var frames []pixelbar.Frame
		var delays []int
		for i, frame := range src.Frames {
			frames = append(frames, pixelbar.Prepare(frame, o))
			delay := src.Delays[i]
			if speed == "0.5×" {
				delay *= 2
			}
			if speed == "2×" {
				delay /= 2
			}
			delays = append(delays, min(65535, max(100, delay)))
		}
		return frames, delays
	}
	apply := func(frames []pixelbar.Frame, delays []int) {
		a.frames = frames
		a.delays = delays
		a.frame = 0
		a.next = time.Time{}
		a.processing = false
	}
	if a.window == nil {
		f, d := work()
		apply(f, d)
		return
	}
	a.processing = true
	go func() { f, d := work(); a.update(func() { apply(f, d) }) }()
}
func (a *App) save() {
	if a.config == "" {
		return
	}
	a.options.Recent = nil
	for _, it := range a.items {
		if it.Path != "" {
			a.options.Recent = append(a.options.Recent, it.Path)
		}
	}
	b, e := json.MarshalIndent(a.options, "", "  ")
	if e != nil {
		return
	}
	if e = os.MkdirAll(filepath.Dir(a.config), 0700); e != nil {
		return
	}
	tmp := a.config + ".tmp"
	if os.WriteFile(tmp, b, 0600) == nil {
		_ = os.Rename(tmp, a.config)
	}
}
func (a *App) update(fn func()) {
	a.window.Update(func() {
		if !a.closed {
			fn()
		}
	})
}
func (a *App) finish(message string, e error) {
	a.busy = false
	a.operation = ""
	a.failed = e != nil
	if e != nil {
		a.status = e.Error()
	} else {
		a.status = message
	}
}
func (a *App) choose() {
	if a.busy || a.processing {
		return
	}
	a.busy = true
	a.operation = "选择素材"
	go func() {
		paths, e := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.window, Title: "导入图片或 GIF 动画", Filters: []mygo.FileFilter{{Name: "图片与动画", Extensions: []string{"png", "jpg", "jpeg", "bmp", "webp", "gif"}}}})
		if e != nil || len(paths) == 0 {
			a.update(func() {
				a.finish("已取消导入", e)
				if e != nil {
					a.importError = e.Error()
				}
			})
			return
		}
		a.loadPath(paths[0])
	}()
}
func (a *App) loadPath(path string) {
	asset, e := pixelbar.Load(path)
	a.update(func() {
		if e != nil {
			a.finish("", e)
			a.importError = e.Error()
			return
		}
		for _, it := range a.items {
			if it.Path != "" {
				it.Asset = nil
			}
		}
		index := -1
		for i, it := range a.items {
			if it.Path == path {
				index = i
				break
			}
		}
		if index < 0 {
			a.appendImported(&item{Name: filepath.Base(path), Path: path})
			index = len(a.items) - 1
		}
		it := a.items[index]
		it.Asset = asset
		it.Detail = fmt.Sprintf("%d × %d · %d 帧", asset.Width, asset.Height, len(asset.Frames))
		a.selected = index
		a.mode = imageMode
		a.importOpen = false
		a.importError = ""
		a.help = false
		a.prepare()
		a.save()
		a.finish("素材已载入，可调整图像并预览效果。", nil)
	})
}

// Bound imported history independently of the built-in catalog.
func (a *App) appendImported(it *item) {
	count, last := 0, -1
	for i, existing := range a.items {
		if existing.Path != "" {
			count++
			last = i
		}
	}
	if count >= maxRecentItems {
		a.items = append(a.items[:last], a.items[last+1:]...)
	}
	a.items = append(a.items, it)
}

func (a *App) selectItem(index int) {
	if a.busy || a.processing {
		return
	}
	if a.items[index].Asset != nil {
		a.selected = index
		a.prepare()
		a.help = false
		return
	}
	a.busy = true
	a.operation = "正在读取图片"
	path := a.items[index].Path
	go a.loadPath(path)
}
func (a *App) connect(upload bool) {
	if !upload {
		a.checkDevice(time.Now())
		return
	}
	if a.busy || a.processing || a.checking || !a.canSend() {
		return
	}
	a.busy = true
	a.failed = false
	a.progress = 0
	a.operation = "正在连接音箱"
	a.status = "请保持 USB 连接，并退出 TempoHub。"
	frames := append([]pixelbar.Frame(nil), a.frames...)
	delays := append([]int(nil), a.delays...)
	go func() {
		d, e := pixelbar.Connect()
		if e != nil {
			a.update(func() { a.deviceResult(e, time.Now()); a.finish("", e) })
			return
		}
		if upload {
			a.update(func() { a.connected = true; a.operation = "正在发送图案" })
			e = d.Upload(frames, delays, func(p float64) {
				a.update(func() {
					a.progress = p
					if p >= .95 {
						a.operation = "正在确认屏幕状态"
					}
				})
			})
		}
		col, colorErr := d.ScreenColor()
		_ = d.Close()
		a.update(func() {
			a.applyReadColor(col, colorErr)
			a.deviceResult(e, time.Now())
			message := "已检测到 Halo PixelBar · 固件 1.1.1"
			if upload {
				message = "音箱已确认接收并切换图案，请查看实际屏幕效果。"
			}
			a.finish(message, e)
		})
	}()
}
func (a *App) export() {
	if a.busy || a.processing || !a.canSend() {
		return
	}
	a.busy = true
	a.operation = "导出预览"
	frames := append([]pixelbar.Frame(nil), a.frames...)
	delays := append([]int(nil), a.delays...)
	animated := len(frames) > 1
	ext := "png"
	if animated {
		ext = "gif"
	}
	name := strings.TrimSuffix(a.items[a.selected].Name, filepath.Ext(a.items[a.selected].Name)) + "-pixelbar." + ext
	if a.mode == textMode {
		name = "文字-pixelbar." + ext
	}
	go func() {
		path, e := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.window, Title: "导出点阵图案", DefaultPath: name, Filters: []mygo.FileFilter{{Name: strings.ToUpper(ext), Extensions: []string{ext}}}})
		if e == nil && path != "" {
			if !strings.EqualFold(filepath.Ext(path), "."+ext) {
				path += "." + ext
			}
			e = pixelbar.Export(path, frames, delays, animated)
		}
		a.update(func() {
			message := "已取消导出"
			if path != "" {
				message = "已导出：" + path
			}
			a.finish(message, e)
		})
	}()
}

func (a *App) View(c *ui.Context) {
	t := *ui.DarkTheme()
	t.Background = bg
	t.Surface = surface
	t.Border = border
	t.Accent = accent
	t.AccentHover = ui.RGB(158, 242, 214)
	t.AccentText = bg
	t.InactiveAccent = accent
	t.Text = ui.RGB(236, 240, 246)
	t.TextMuted = muted
	t.Radius = 8
	t.FontSize = 13
	c.SetTheme(&t)
	if a.playing && len(a.frames) > 1 && !a.help {
		now := c.Now()
		if a.next.IsZero() {
			a.next = now.Add(time.Duration(a.delays[a.frame]) * time.Millisecond)
		}
		if !now.Before(a.next) {
			a.frame = (a.frame + 1) % len(a.frames)
			a.next = now.Add(time.Duration(a.delays[a.frame]) * time.Millisecond)
		}
		c.After(a.next.Sub(now))
	}
	if c.Shortcut(ui.Cmd, ui.KeyO) && a.mode == imageMode && !a.busy && !a.processing && !a.help && !a.importOpen {
		a.importOpen = true
		a.importError = ""
	}
	ui.Column(c).Fill().Children(func() {
		a.header(c)
		a.deviceBar(c)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			switch a.mode {
			case textMode:
				a.textEditor(c)
				a.workspace(c)
			case colorMode:
				a.colorEditor(c)
			default:
				a.library(c)
				a.workspace(c)
				a.inspector(c)
			}
		})
		a.footer(c)
	})
	a.dialogs(c)
}
func (a *App) header(c *ui.Context) {
	ui.Row(c).Height(72).Shrink(0).Padding(12, 16).Gap(8).Background(surface).DragWindow().Children(func() {
		ui.Image(c, appIcon).Size(44, 44)
		ui.Column(c).Gap(3).Children(func() {
			ui.Text(c, branding.Name).FontSize(21).Bold()
			ui.Text(c, "PIXELBAR STUDIO · v"+branding.Version).FontSize(10).TextColor(muted)
		})
		ui.Spacer(c)
		for _, mode := range []string{imageMode, textMode, colorMode} {
			b := ui.Button(c, mode).Padding(9, 12).Disabled(a.busy || a.processing)
			if a.mode == mode {
				b.Background(ui.RGB(37, 58, 58)).TextColor(accent)
			}
			if b.Clicked() {
				a.switchMode(mode)
			}
		}
		ui.Spacer(c)
		if ui.Button(c, "使用说明").Clicked() {
			a.help = !a.help
		}
		if ui.PrimaryButton(c, "＋  导入素材").Padding(10, 12).Disabled(a.busy || a.processing || a.mode != imageMode).Clicked() {
			a.importOpen = true
			a.importError = ""
		}
		ui.Box(c).Width(1).Height(24).Background(border).MarginX(6)
		if ui.Button(c, "—").Label("最小化").Tooltip("最小化").Size(32, 30).Padding(0).Justify(ui.Center).Clicked() && a.window != nil {
			a.window.Minimize()
		}
		if ui.Button(c, "□").Label("最大化或还原").Tooltip("最大化 / 还原").Size(32, 30).Padding(0).Justify(ui.Center).MaximizeButton().Clicked() && a.window != nil {
			if a.window.IsMaximized() {
				a.window.Unmaximize()
			} else {
				a.window.Maximize()
			}
		}
		if ui.Button(c, "×").Label("关闭窗口").Tooltip("关闭窗口").Size(32, 30).Padding(0).Justify(ui.Center).Clicked() && a.window != nil {
			a.window.Close()
		}
	})
}
func (a *App) library(c *ui.Context) {
	ui.Column(c).Width(210).Shrink(0).Background(surface).Padding(20, 14).Gap(16).Children(func() {
		ui.Row(c).PaddingX(8).Children(func() { ui.Text(c, "素材库").Bold().Grow(1); ui.Textf(c, "%02d", len(a.items)).TextColor(muted) })
		ui.Scroll(c).Grow(1).MinHeight(0).Gap(7).Children(func() {
			for i, it := range a.items {
				b := ui.ButtonBase(c.Key(fmt.Sprintf("asset-%d", i))).Column().AlignItems(ui.Stretch).Padding(12).Gap(7).Radius(9).Disabled(a.busy || a.processing)
				if i == a.selected {
					b.Background(ui.RGB(37, 58, 58)).Border(1, ui.RGB(65, 113, 98))
				} else {
					b.Background(ui.RGB(28, 34, 43))
				}
				b.Children(func() {
					ui.Text(c, it.Name).Bold().MaxLines(1)
					ui.Text(c, it.Detail).FontSize(11).TextColor(muted).MaxLines(1)
				})
				if b.Clicked() {
					a.selectItem(i)
				}
			}
		})
		if a.items[a.selected].Path != "" {
			if ui.Button(c, "从列表移除").Disabled(a.busy || a.processing).Clicked() {
				a.items = append(a.items[:a.selected], a.items[a.selected+1:]...)
				a.selected = 0
				a.prepare()
				a.save()
			}
		}
		ui.Column(c).Padding(12).Gap(8).Radius(8).Border(1, border).Children(func() {
			ui.Text(c, "本地创作，直接传输").FontSize(12).Bold()
			ui.Text(c, "素材保存在本机，通过 USB 发送到你的音箱。").FontSize(11).TextColor(muted)
		})
	})
}
func section(c *ui.Context, title string) { ui.Text(c, title).Bold().FontSize(13) }
func (a *App) workspace(c *ui.Context) {
	it := a.items[a.selected]
	name := it.Name
	if a.mode == textMode {
		name = "文字点阵预览"
	}
	ui.Scroll(c).Grow(1).MinWidth(0).Padding(22).Gap(14).Children(func() {
		ui.Row(c).Gap(12).Children(func() {
			ui.Column(c).Gap(5).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, name).FontSize(25).Bold().MaxLines(1)
				ui.Text(c, "预览你即将放上音箱的图案").TextColor(muted)
			})
			ui.Text(c, "256 × 32").FontSize(12).TextColor(accent).Padding(7, 10).Background(ui.RGB(29, 47, 46)).Radius(6)
		})
		ui.Column(c).Background(surface).Radius(14).Border(1, border).Padding(18).Gap(16).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, "屏幕预览").Bold().Grow(1)
				ui.Text(c, "单色点阵 · 实时效果").FontSize(11).TextColor(muted)
			})
			var frame pixelbar.Frame
			if len(a.frames) > 0 {
				frame = a.frames[a.frame]
			}
			ui.Box(c).Height(150).FillWidth().Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(r, ui.RGB(10, 13, 18), 10)
				screen := ui.Rect{X: r.X + 22, Y: r.Y + (r.H-(r.W-44)/8)/2, W: r.W - 44, H: (r.W - 44) / 8}
				p.Fill(ui.Rect{X: screen.X - 9, Y: screen.Y - 11, W: screen.W + 18, H: screen.H + 22}, ui.RGB(35, 40, 48), 8)
				p.Fill(screen, ui.RGB(4, 7, 10), 1)
				drawPixels(p, screen, frame, a.previewColor)
			})
			ui.Row(c).Gap(8).Children(func() {
				label := "播放"
				if a.playing {
					label = "暂停"
				}
				if ui.Button(c, label).Disabled(len(a.frames) < 2).Clicked() {
					a.playing = !a.playing
					a.next = time.Time{}
				}
				if ui.Button(c, "上一帧").Disabled(len(a.frames) < 2).Clicked() {
					a.playing = false
					a.frame = (a.frame + len(a.frames) - 1) % len(a.frames)
				}
				if ui.Button(c, "下一帧").Disabled(len(a.frames) < 2).Clicked() {
					a.playing = false
					a.frame = (a.frame + 1) % len(a.frames)
				}
				ui.Spacer(c)
				ui.Textf(c, "%02d / %02d", min(a.frame+1, len(a.frames)), len(a.frames)).TextColor(muted)
			})
		})
		ui.Row(c).Gap(10).Children(func() {
			metric(c, "输出尺寸", "256 × 32")
			kind := "静态图片"
			if len(a.frames) > 1 {
				kind = "循环动画"
			}
			metric(c, "素材类型", kind)
			total := 0
			for _, d := range a.delays {
				total += d
			}
			value := "持续显示"
			if len(a.frames) > 1 {
				value = fmt.Sprintf("%.2f 秒", float64(total)/1000)
			}
			metric(c, "循环时长", value)
		})
		ui.Row(c).Children(func() {
			title := "动画帧"
			if len(a.frames) == 1 {
				title = "图案帧"
			}
			section(c, title)
			ui.Spacer(c)
			ui.Text(c, "点击帧可暂停并查看").FontSize(11).TextColor(muted)
		})
		ui.Column(c).Gap(8).Children(func() {
			for start := 0; start < len(a.frames); start += 4 {
				ui.Row(c).Gap(8).Children(func() {
					for i := start; i < min(start+4, len(a.frames)); i++ {
						f := a.frames[i]
						b := ui.ButtonBase(c.Key(fmt.Sprintf("frame-%d", i))).Column().Grow(1).Basis(0).MinWidth(0).Padding(9).Gap(8).Radius(8).Border(1, border).Background(surface)
						if i == a.frame {
							b.Border(1, accent)
						}
						b.Children(func() {
							ui.Box(c).FillWidth().Height(28).Draw(func(p *ui.Painter, r ui.Rect) {
								p.Fill(r, ui.RGB(5, 8, 11), 2)
								drawPixels(p, r, f, a.previewColor)
							})
							label := fmt.Sprintf("%02d · %d ms", i+1, a.delays[i])
							if len(a.frames) == 1 {
								label = "01 · 静态"
							}
							ui.Text(c, label).FontSize(10).TextColor(muted)
						})
						if b.Clicked() {
							a.frame = i
							a.playing = false
						}
					}
					for i := min(start+4, len(a.frames)); i < start+4; i++ {
						ui.Box(c).Grow(1).Basis(0)
					}
				})
			}
		})
		ui.Text(c, a.colorPreviewLabel()).FontSize(11).TextColor(muted)
		if a.mode == textMode && !a.textCurrent() {
			ui.Text(c, "点击“生成预览”更新图案；生成成功后才能导出或发送。").TextColor(accent)
		}
	})
}
func metric(c *ui.Context, name, value string) {
	ui.Column(c).Grow(1).Basis(0).Padding(12).Gap(8).Background(surface).Radius(8).Children(func() { ui.Text(c, name).TextColor(muted).FontSize(11); ui.Text(c, value).Bold().FontSize(14) })
}
func drawPixels(p *ui.Painter, r ui.Rect, f pixelbar.Frame, col ui.Color) {
	s := min(r.W/256, r.H/32)
	ox, oy := r.X+(r.W-s*256)/2, r.Y+(r.H-s*32)/2
	// Join lit runs and snap their shared edges to physical pixels, avoiding
	// anti-alias seams at fractional DPI/zoom. Unlit pixels share one black plane.
	scale := p.Scale()
	snap := func(v float32) float32 { return float32(math.Round(float64(v*scale))) / scale }
	p.Fill(ui.Rect{X: snap(ox), Y: snap(oy), W: snap(ox+256*s) - snap(ox), H: snap(oy+32*s) - snap(oy)}, ui.RGB(4, 7, 10), 0)
	for y := 0; y < 32; y++ {
		for x := 0; x < 256; {
			if !f[y*256+x] {
				x++
				continue
			}
			start := x
			for x < 256 && f[y*256+x] {
				x++
			}
			left, top := snap(ox+float32(start)*s), snap(oy+float32(y)*s)
			p.Fill(ui.Rect{X: left, Y: top, W: snap(ox+float32(x)*s) - left, H: snap(oy+float32(y+1)*s) - top}, col, 0)
		}
	}
}
func (a *App) inspector(c *ui.Context) {
	ui.Scroll(c).Width(260).Shrink(0).Padding(18).Gap(12).Background(surface).Children(func() {
		section(c, "图像调整")
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, "缩放方式").TextColor(muted).FontSize(12)
			if ui.Select(c, &a.options.Fit, []string{"完整显示", "铺满裁切", "拉伸填满"}).Label("缩放方式").Disabled(a.busy || a.processing).Changed() {
				a.prepare()
				a.save()
			}
		})
		ui.Column(c).Gap(10).Children(func() {
			ui.Row(c).Children(func() {
				ui.Text(c, "亮度阈值").TextColor(muted).Grow(1)
				ui.Textf(c, "%.0f", a.options.Threshold).TextColor(accent)
			})
			if ui.StepSlider(c, &a.options.Threshold, 1, 254, 1).Label("亮度阈值").Disabled(a.busy || a.processing).Changed() {
				a.prepare()
				a.save()
			}
		})
		ui.Row(c).Children(func() {
			ui.Text(c, "反转黑白").Grow(1)
			if ui.Switch(c, &a.options.Invert).Label("反转黑白").Disabled(a.busy || a.processing).Changed() {
				a.prepare()
				a.save()
			}
		})
		ui.Row(c).Children(func() {
			ui.Text(c, "抖动保留层次").Grow(1)
			if ui.Switch(c, &a.options.Dither).Label("抖动保留层次").Disabled(a.busy || a.processing).Changed() {
				a.prepare()
				a.save()
			}
		})
		ui.Text(c, "照片可开启抖动；线条与文字建议关闭。").FontSize(11).TextColor(muted)
		ui.Divider(c)
		section(c, "动画播放")
		if ui.Select(c, &a.options.Speed, []string{"0.5×", "原速", "2×"}).Label("动画速度").Disabled(a.busy || a.processing || len(a.frames) < 2).Changed() {
			a.prepare()
			a.save()
		}
		ui.Text(c, "最多 8 帧，每帧至少 100 ms。预览与发送使用相同时长。").FontSize(11).TextColor(muted)
		ui.Divider(c)
		if ui.Button(c, "重置图像调整").FillWidth().Disabled(a.busy || a.processing).Clicked() {
			a.options.Threshold = 128
			a.options.Fit = "完整显示"
			a.options.Invert = false
			a.options.Dither = false
			a.options.Speed = "原速"
			a.prepare()
			a.save()
		}
		if ui.Button(c, "导出点阵图案").FillWidth().Disabled(a.busy || a.processing).Clicked() {
			a.export()
		}
	})
}
func (a *App) footer(c *ui.Context) {
	ui.Row(c).Height(88).Shrink(0).Padding(16, 24).Gap(24).Background(surface).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(7).Children(func() {
			text := a.status
			if a.busy || a.processing {
				text = a.operation + "…"
				if a.processing {
					text = "正在生成点阵预览…"
				}
			}
			col := muted
			if a.failed {
				col = ui.RGB(255, 145, 145)
			}
			ui.Text(c, text).TextColor(col).MaxLines(1).Tooltip(a.status)
			if a.busy || a.processing {
				p := a.progress
				if p == 0 {
					p = -1
				}
				ui.Progress(c, p).Height(4)
			} else {
				hint := "发送会替换当前屏幕内容；可在 TempoHub 中恢复官方图案。"
				if a.mode == colorMode {
					hint = "应用颜色会修改音箱像素屏 RGB；回读确认后同步预览。"
				}
				ui.Text(c, hint).FontSize(11).TextColor(muted)
			}
		})
		if a.mode == colorMode {
			if ui.PrimaryButton(c, "应用颜色到音箱").Padding(13, 24).Disabled(a.busy || a.checking).Clicked() {
				a.writeColor()
			}
		} else if ui.PrimaryButton(c, "发送到音箱  →").Padding(13, 24).Disabled(a.busy || a.processing || a.checking || !a.canSend()).Clicked() {
			a.connect(true)
		}
	})
}
func (a *App) helpView(c *ui.Context) {
	ui.Scroll(c).Height(390).MinWidth(0).Gap(12).Children(func() {
		ui.Text(c, "开始你的像素创作").FontSize(25).Bold()
		for _, s := range []struct{ title, body string }{
			{"01  连接音箱", "使用 USB 连接 Halo PixelBar，切换到声卡输入，退出官方 TempoHub，然后点击检测连接。"},
			{"02  图片模式", "导入 PNG、JPEG、BMP、WebP 或 GIF。选择完整显示、裁切或拉伸；调整阈值、反色和抖动，让图案在 256 × 32 的点阵上保持清晰。"},
			{"03  文字模式", "输入单行文字，选择字体和字号，设置静态、上下或左右移动，点击生成预览。动画生成 8 帧，预览与发送共用同一组图片；修改参数后需重新生成。"},
			{"04  颜色设置", "连接后自动读取像素屏 RGB，图片与文字共用该颜色。调整滑块后点击应用颜色到音箱，等待回读确认；草稿不影响图案预览，黑色会让点阵不可见。"},
			{"05  预览与发送", "GIF 最多支持 8 帧。可暂停、逐帧查看和调整速度。点击发送到音箱，等待设备确认完成。"},
			{"素材与隐私", "软件不上传图片到网络。素材库记录文件路径，原文件移动或删除后需重新导入。导出会保存处理后的 PNG 或循环 GIF。"},
			{"支持范围", "已验证 Halo PixelBar 固件 1.1.1。Windows 已有设备验证基础；新增颜色写入和文字上传尚未实机验证，macOS 版本仍需实机验证。其他型号和固件不会自动写入。"},
		} {
			ui.Column(c).Padding(18).Gap(10).Radius(10).Background(surface).Children(func() { ui.Text(c, s.title).Bold(); ui.Text(c, s.body).TextColor(muted) })
		}
	})
}

func Run(path string) error {
	a := New()
	mygo.App.SetName(branding.Name)
	mygo.App.WhenReady(func() {
		a.window = mygo.NewWindow(mygo.WindowOptions{Title: branding.Name + " · PixelBar Studio", Width: 1320, Height: 880, MinWidth: 1080, MinHeight: 720, TitleBarStyle: mygo.TitleBarHidden, HideWindowButtons: true, BackgroundColor: "#0f1218", Content: ui.View(a.View)})
		_ = a.window.SetIcon(branding.IconPNG)
		if runtime.GOOS == "darwin" {
			_ = mygo.App.Dock.SetIcon(branding.IconPNG)
		}
		a.probe = func(done func(error)) {
			go func() {
				d, e := pixelbar.Connect()
				var col pixelbar.RGB
				colorErr := e
				if e == nil {
					col, colorErr = d.ScreenColor()
					e = d.Close()
				}
				a.update(func() { a.applyReadColor(col, colorErr); done(e) })
			}()
		}
		a.checkDevice(time.Now())
		stop := make(chan struct{})
		a.window.OnClosed(func() { a.closed = true; close(stop) })
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case now := <-ticker.C:
					a.update(func() { a.pollDevice(now) })
				case <-stop:
					return
				}
			}
		}()
		a.window.OnClose(func(e *mygo.CloseEvent) {
			if a.busy || a.processing {
				e.PreventDefault()
				a.status = "正在执行操作，请完成后再关闭。"
			} else {
				a.save()
			}
		})
		if path != "" {
			a.busy = true
			a.operation = "正在载入素材"
			go a.loadPath(path)
		}
	})
	return mygo.App.Run()
}
