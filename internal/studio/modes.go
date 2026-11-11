package studio

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/image/font/opentype"
	"openpixelbar/internal/pixelbar"
)

const imageMode, textMode, colorMode = "图片模式", "文字模式", "颜色设置"

type previewState struct {
	frames  []pixelbar.Frame
	delays  []int
	frame   int
	playing bool
}
type textDraft struct {
	Content, Font, Motion string
	Size, Delay           float64
}
type fontChoice struct{ name, path string }
type modes struct {
	mode                    string
	imageState, textState   previewState
	textDraft, renderedText textDraft
	fonts                   []fontChoice
	colorKnown, colorDirty  bool
	colorError              string
	currentColor            pixelbar.RGB
	red, green, blue        float64
}

func (a *App) initModes() {
	a.mode = imageMode
	a.textDraft = textDraft{Content: "你好，PixelBar!", Size: 24, Delay: 150, Motion: "静态"}
	a.red, a.green, a.blue = 255, 200, 111
}

func (a *App) switchMode(mode string) {
	if a.busy || a.processing || a.mode == mode {
		return
	}
	state := previewState{a.frames, a.delays, a.frame, a.playing}
	switch a.mode {
	case imageMode:
		a.imageState = state
	case textMode:
		a.textState = state
	}
	a.mode = mode
	switch mode {
	case imageMode:
		state = a.imageState
	case textMode:
		state = a.textState
	case colorMode:
		state = previewState{}
	}
	a.frames, a.delays, a.frame, a.playing = state.frames, state.delays, state.frame, state.playing
	a.next = time.Time{}
	if mode == textMode && len(a.fonts) == 0 {
		a.loadFonts()
	}
}

// Offer common installed fonts without copying proprietary font files into the
// application. Discovery and font reads execute in a worker in the live app.
func systemFonts() []fontChoice {
	var candidates []fontChoice
	switch runtime.GOOS {
	case "windows":
		root := filepath.Join(os.Getenv("WINDIR"), "Fonts")
		for _, f := range []fontChoice{{"微软雅黑", "msyh.ttc"}, {"黑体", "simhei.ttf"}, {"宋体", "simsun.ttc"}, {"楷体", "simkai.ttf"}, {"等线", "Deng.ttf"}, {"Arial", "arial.ttf"}, {"Segoe UI", "segoeui.ttf"}} {
			candidates = append(candidates, fontChoice{f.name, filepath.Join(root, f.path)})
		}
	case "darwin":
		candidates = []fontChoice{{"苹方", "/System/Library/Fonts/PingFang.ttc"}, {"苹方", "/System/Library/Fonts/Supplemental/PingFang.ttc"}, {"黑体", "/System/Library/Fonts/STHeiti Light.ttc"}, {"Arial", "/System/Library/Fonts/Supplemental/Arial.ttf"}, {"宋体", "/System/Library/Fonts/Supplemental/Songti.ttc"}}
	default:
		candidates = []fontChoice{{"Noto Sans CJK", "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc"}, {"DejaVu Sans", "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"}}
	}
	var fonts []fontChoice
	seen := map[string]bool{}
	for _, f := range candidates {
		if info, err := os.Stat(f.path); err == nil && !info.IsDir() && !seen[f.name] {
			fonts = append(fonts, f)
			seen[f.name] = true
		}
	}
	return fonts
}

func (a *App) loadFonts() {
	a.processing = true
	apply := func(fonts []fontChoice) {
		a.processing = false
		a.fonts = fonts
		if len(fonts) > 0 {
			a.textDraft.Font = fonts[0].name
		} else {
			a.status = "未找到常用系统字体，请载入 TTF、OTF 或 TTC 字体。"
		}
	}
	if a.window == nil {
		apply(systemFonts())
		return
	}
	go func() { fonts := systemFonts(); a.update(func() { apply(fonts) }) }()
}

func (a *App) textCurrent() bool { return a.textDraft == a.renderedText && len(a.frames) > 0 }
func (a *App) canSend() bool {
	return a.mode != colorMode && len(a.frames) > 0 && (a.mode != textMode || a.textCurrent())
}

func (a *App) generateText() {
	if a.busy || a.processing || a.mode != textMode {
		return
	}
	draft := a.textDraft
	path := ""
	for _, f := range a.fonts {
		if f.name == draft.Font {
			path = f.path
			break
		}
	}
	a.processing = true
	work := func() ([]pixelbar.Frame, []int, error) {
		data, err := readFont(path)
		if err != nil {
			return nil, nil, err
		}
		return pixelbar.RenderText(data, 0, pixelbar.TextOptions{Text: draft.Content, Size: int(draft.Size), Motion: draft.Motion, Delay: int(draft.Delay)})
	}
	apply := func(frames []pixelbar.Frame, delays []int, err error) {
		a.processing = false
		a.failed = err != nil
		if err != nil {
			a.renderedText = textDraft{}
			a.status = err.Error()
			return
		}
		a.frames, a.delays, a.frame, a.playing = frames, delays, 0, true
		a.next = time.Time{}
		a.renderedText = draft
		a.status = "文字点阵已生成；预览、导出和发送使用同一组图像。"
	}
	if a.window == nil {
		f, d, e := work()
		apply(f, d, e)
		return
	}
	go func() { f, d, e := work(); a.update(func() { apply(f, d, e) }) }()
}

func (a *App) applyReadColor(col pixelbar.RGB, err error) {
	a.colorKnown = err == nil
	a.colorError = ""
	if err != nil {
		a.colorError = err.Error()
		return
	}
	a.currentColor = col
	a.previewColor = ui.RGB(col.R, col.G, col.B)
	if !a.colorDirty {
		a.red, a.green, a.blue = float64(col.R), float64(col.G), float64(col.B)
	}
}

func (a *App) colorPreviewLabel() string {
	if a.colorKnown {
		return "已读取音箱颜色 " + a.currentColor.Hex() + "；实际亮度与观感以音箱为准。"
	}
	return "尚未读取当前颜色；暂用模拟色（或上次读取值），连接后自动同步。"
}

func (a *App) writeColor() {
	if a.busy || a.processing || a.checking || a.closed {
		return
	}
	col := pixelbar.RGB{R: byte(a.red), G: byte(a.green), B: byte(a.blue)}
	a.busy = true
	a.failed = false
	a.progress = 0
	a.operation = "正在设置并核对屏幕颜色"
	go func() {
		d, err := pixelbar.Connect()
		if err != nil {
			a.update(func() { a.applyReadColor(pixelbar.RGB{}, err); a.deviceResult(err, time.Now()); a.finish("", err) })
			return
		}
		err = d.SetScreenColor(col)
		// Read even after an unsuccessful confirmation so preview reflects the
		// latest device state, not an unconfirmed optimistic color.
		current, readErr := d.ScreenColor()
		if err == nil && readErr == nil && current != col {
			err = fmt.Errorf("音箱颜色再次发生变化，请重新读取当前颜色")
		}
		_ = d.Close()
		a.update(func() {
			if err == nil {
				a.colorDirty = false
			}
			a.applyReadColor(current, readErr)
			a.deviceResult(readErr, time.Now())
			if err == nil {
				err = readErr
			}
			a.finish("音箱已确认颜色 "+col.Hex()+"，图片与文字预览已同步。", err)
		})
	}()
}

func (a *App) colorEditor(c *ui.Context) {
	ui.Scroll(c).Grow(1).Padding(28).Gap(18).Children(func() {
		ui.Text(c, "像素屏颜色").FontSize(25).Bold()
		ui.Text(c, "设置整块像素屏的统一颜色，图片和文字共用。调整滑块后点击“应用颜色到音箱”。").TextColor(muted)
		ui.Row(c).Gap(24).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Width(340).Gap(16).Padding(22).Background(surface).Radius(12).Children(func() {
				for _, channel := range []struct {
					name  string
					value *float64
				}{{"红 R", &a.red}, {"绿 G", &a.green}, {"蓝 B", &a.blue}} {
					ui.Textf(c, "%s   %.0f", channel.name, *channel.value)
					if ui.StepSlider(c, channel.value, 0, 255, 1).Label(channel.name).Disabled(a.busy).Changed() {
						a.colorDirty = true
					}
				}
				ui.Text(c, "常用颜色").TextColor(muted)
				ui.Row(c).Gap(7).Children(func() {
					for _, col := range []pixelbar.RGB{{R: 255, G: 200, B: 111}, {R: 0, G: 150, B: 4}, {R: 255, G: 255, B: 255}, {R: 50, G: 150, B: 255}, {R: 255, G: 60, B: 100}} {
						if ui.Button(c, " ").Label(col.Hex()).Tooltip(col.Hex()).Size(42, 34).Background(ui.RGB(col.R, col.G, col.B)).Disabled(a.busy).Clicked() {
							a.red, a.green, a.blue = float64(col.R), float64(col.G), float64(col.B)
							a.colorDirty = true
						}
					}
				})
				if ui.Button(c, "读取当前颜色").Disabled(a.checking || a.busy).Clicked() {
					a.checkDevice(time.Now())
				}
				if ui.Button(c, "恢复为已读取颜色").Disabled(!a.colorKnown || a.busy).Clicked() {
					a.colorDirty = false
					a.applyReadColor(a.currentColor, nil)
				}
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(16).Padding(24).Background(surface).Radius(12).Children(func() {
				col := pixelbar.RGB{R: byte(a.red), G: byte(a.green), B: byte(a.blue)}
				ui.Text(c, "待应用颜色  "+col.Hex()).Bold()
				ui.Box(c).FillWidth().Height(120).Radius(10).Background(ui.RGB(col.R, col.G, col.B))
				ui.Text(c, a.colorPreviewLabel()).TextColor(muted)
				if a.colorKnown {
					ui.Box(c).FillWidth().Height(24).Radius(5).Background(a.previewColor)
				}
				if a.colorError != "" {
					ui.Text(c, "读取失败："+a.colorError).TextColor(ui.RGB(255, 145, 145))
				}
				ui.Text(c, "每 10 秒自动读取；未应用的调色不会改变图片和文字预览。黑色会让点阵不可见。").TextColor(muted)
			})
		})
	})
}

func (a *App) textEditor(c *ui.Context) {
	ui.Column(c).Width(300).Shrink(0).Background(surface).Children(func() {
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(20).Gap(10).Children(func() {
			section(c, "文字创作")
			ui.Text(c, "单行文字 · 最多 80 字").TextColor(muted)
			ui.TextInput(c, &a.textDraft.Content).Label("文字内容").MaxLength(80).Disabled(a.processing || a.busy)
			ui.Text(c, "字体").TextColor(muted)
			names := []string{}
			for _, f := range a.fonts {
				names = append(names, f.name)
			}
			ui.Select(c, &a.textDraft.Font, names).Label("文字字体").Disabled(a.processing || a.busy)
			if ui.Button(c, "载入字体文件…").Disabled(a.processing || a.busy).Clicked() {
				a.chooseFont()
			}
			ui.Textf(c, "字号   %.0f px", a.textDraft.Size).TextColor(muted)
			ui.StepSlider(c, &a.textDraft.Size, 8, 32, 1).Label("文字字号").Disabled(a.processing || a.busy)
			ui.Text(c, "动画方式").TextColor(muted)
			ui.Select(c, &a.textDraft.Motion, []string{"静态", "上下移动", "左右移动"}).Label("文字动画").Disabled(a.processing || a.busy)
			ui.Textf(c, "每帧时长   %.0f ms", a.textDraft.Delay).TextColor(muted)
			ui.StepSlider(c, &a.textDraft.Delay, 100, 2000, 50).Label("文字帧时长").Disabled(a.processing || a.busy || a.textDraft.Motion == "静态")
			ui.Text(c, "动画生成 8 帧往返移动；长文字左右移动时步幅较大。上下移动可能从屏幕边缘移入移出。").FontSize(11).TextColor(muted)
		})
		ui.Column(c).Shrink(0).Padding(12, 20).Gap(8).Children(func() {
			if ui.PrimaryButton(c, "生成预览").FillWidth().Disabled(a.processing || a.busy).Clicked() {
				a.generateText()
			}
			if ui.Button(c, "导出文字图案").FillWidth().Disabled(a.processing || a.busy || !a.textCurrent()).Clicked() {
				a.export()
			}
		})
	})
}

func readFont(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("请选择或载入字体")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 64<<20 {
		return nil, fmt.Errorf("字体文件不能超过 64 MB")
	}
	data, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if len(data) > 64<<20 {
		return nil, fmt.Errorf("字体文件不能超过 64 MB")
	}
	return data, err
}

func (a *App) chooseFont() {
	if a.busy || a.processing {
		return
	}
	a.busy = true
	a.operation = "载入字体"
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.window, Title: "选择字体文件", Filters: []mygo.FileFilter{{Name: "字体", Extensions: []string{"ttf", "otf", "ttc"}}}})
		if err != nil || len(paths) == 0 {
			a.update(func() { a.finish("已取消载入字体", err) })
			return
		}
		path := paths[0]
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".ttf" && ext != ".otf" && ext != ".ttc" {
			err = fmt.Errorf("仅支持 TTF、OTF、TTC 字体")
		}
		if err == nil {
			var data []byte
			data, err = readFont(path)
			if err == nil {
				_, err = opentype.ParseCollection(data)
			}
		}
		a.update(func() {
			if err == nil {
				name := filepath.Base(path) + "（载入）"
				found := false
				for i, f := range a.fonts {
					if f.name == name {
						a.fonts[i].path = path
						found = true
					}
				}
				if !found {
					a.fonts = append(a.fonts, fontChoice{name, path})
				}
				a.textDraft.Font = name
				a.renderedText = textDraft{}
			}
			a.finish("字体已载入，请生成预览。", err)
		})
	}()
}
