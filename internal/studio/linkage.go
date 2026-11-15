package studio

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"openpixelbar/internal/appwatch"
	"openpixelbar/internal/pixelbar"
	"openpixelbar/internal/rotation"
)

// All linkage state is owned by the UI thread. Workers return immutable
// snapshots; generation guards discard results from a stopped monitoring run.
type linkage struct {
	linkEnabled, linkScanning bool
	linkGeneration            uint64
	linkSelected              int
	linkLines                 string
	linkInterval              float64
	linkEngine                *rotation.Engine
	linkSnapshot              appwatch.Snapshot
	linkNextScan              time.Time
	linkMessage, linkLastText string
	linkProcessChoice         string
	linkProcessPaths          map[string]string
	linkCapture               func(func(appwatch.Snapshot, error))
	linkSend                  func(string, func(error))
	linkNow                   func() time.Time
}

func (a *App) initLinkage() {
	a.linkEngine = rotation.New()
	a.linkNow = time.Now
	a.linkMessage = "监控未启用；配置保存在本机，重启后需手动启用。"
	a.loadRuleDraft()
	a.linkCapture = func(done func(appwatch.Snapshot, error)) {
		go func() {
			snapshot, err := appwatch.Capture()
			a.update(func() { done(snapshot, err) })
		}()
	}
	a.linkSend = func(text string, done func(error)) {
		go func() {
			d, err := pixelbar.Connect()
			if err == nil {
				err = d.SetNativeText(text)
				closeErr := d.Close()
				if err == nil {
					err = closeErr
				}
			}
			a.update(func() { done(err) })
		}()
	}
}

func (a *App) loadRuleDraft() {
	if a.linkSelected < 0 || a.linkSelected >= len(a.options.AppRules) {
		a.linkSelected = 0
	}
	a.linkLines, a.linkInterval = "", 5
	if len(a.options.AppRules) > 0 {
		r := a.options.AppRules[a.linkSelected]
		a.linkLines = strings.Join(r.Texts, "\n")
		a.linkInterval = float64(r.IntervalSeconds)
	}
}

func (a *App) validateLinkage(requireEnabled bool) error {
	var enabled []rotation.Rule
	for _, rule := range a.options.AppRules {
		if rule.Enabled {
			enabled = append(enabled, rule)
		}
	}
	if requireEnabled && len(enabled) == 0 {
		return fmt.Errorf("请添加并开启至少一条应用规则")
	}
	return rotation.ValidateRules(enabled)
}

func (a *App) saveLinkage() {
	err := a.validateLinkage(false)
	if err == nil {
		err = a.save()
	}
	if err != nil {
		a.linkMessage = "保存失败：" + err.Error()
		a.failed = true
	} else {
		a.linkMessage = "规则已保存；重启后需手动启用监控。"
		a.failed = false
	}
}

func (a *App) startLinkage(now time.Time) {
	if a.closed || a.exitPending || a.linkEnabled || a.busy || a.processing || a.checking || a.linkScanning {
		return
	}
	if err := a.validateLinkage(true); err != nil {
		a.linkMessage, a.status, a.failed = err.Error(), err.Error(), true
		return
	}
	if err := a.save(); err != nil {
		a.linkMessage = "配置保存失败：" + err.Error()
		a.status, a.failed = a.linkMessage, true
		return
	}
	a.linkGeneration++
	a.linkEnabled = true
	a.linkEngine.Reset()
	a.linkNextScan = time.Time{}
	a.failed = false
	a.linkMessage = "正在检查运行应用；启用即允许替换音箱文字和显示模式。"
	a.status = a.linkMessage
	a.pollLinkage(now)
}

func (a *App) stopLinkage(message string) {
	a.linkEnabled = false
	a.linkGeneration++
	if a.linkEngine != nil {
		a.linkEngine.Reset()
	}
	if message != "" {
		a.linkMessage, a.status = message, message
	}
}

func (a *App) scanLinkage(now time.Time) {
	if a.closed || a.linkScanning || a.linkCapture == nil {
		return
	}
	a.linkScanning = true
	generation := a.linkGeneration
	a.linkCapture(func(snapshot appwatch.Snapshot, err error) {
		a.linkScanning = false
		if a.closed || generation != a.linkGeneration {
			return
		}
		a.linkNextScan = a.linkNow().Add(2 * time.Second)
		if err != nil {
			a.stopLinkage("应用检测失败，监控已暂停：" + err.Error())
			a.failed = true
			return
		}
		a.linkSnapshot = snapshot
		a.linkProcessPaths = map[string]string{}
		for _, p := range snapshot.Processes {
			path := p.Path
			if path == "" {
				path = p.Name
			}
			if path == "" {
				continue
			}
			label := path
			a.linkProcessPaths[label] = path
		}
		if !a.linkEnabled {
			return
		}
		a.linkEngine.Observe(a.options.AppRules, snapshot)
		if a.linkEngine.Active() < 0 {
			a.linkMessage = "监控中：等待目标应用运行；音箱保留最后内容。"
		} else {
			a.linkMessage = "监控中：" + a.options.AppRules[a.linkEngine.Active()].Name + "（前台优先）"
		}
		a.dispatchLinkage(a.linkNow())
	})
}

func (a *App) pollLinkage(now time.Time) {
	if a.closed || !a.linkEnabled {
		return
	}
	if !now.Before(a.linkNextScan) {
		a.scanLinkage(now)
		return
	}
	if !a.linkScanning {
		a.dispatchLinkage(now)
	}
}

func (a *App) dispatchLinkage(now time.Time) {
	if a.closed || !a.linkEnabled || a.linkScanning || a.busy || a.processing || a.checking || a.linkSend == nil {
		return
	}
	req, ok := a.linkEngine.Next(now)
	if !ok {
		return
	}
	generation := a.linkGeneration
	a.busy = true
	a.progress = 0
	a.operation = "正在设置并核对原生文字"
	a.linkSend(req.Text, func(err error) {
		// The device operation owns busy even when the user stops monitoring.
		a.busy = false
		a.operation = ""
		if a.closed {
			return
		}
		if err == nil {
			a.linkLastText = req.Text
		}
		if generation != a.linkGeneration {
			if err != nil {
				a.failed = true
				a.status = "停止前的文字写入未获完整确认：" + err.Error()
				a.linkMessage = a.status
			}
			return
		}
		accepted := a.linkEngine.Complete(req.Token, a.linkNow(), err)
		if err != nil {
			a.stopLinkage("文字设置失败，监控已暂停；不会自动重试：" + err.Error())
			a.failed = true
			return
		}
		if accepted {
			a.failed = false
			a.status = fmt.Sprintf("已确认原生文字：%s · 第 %d 条", req.Text, req.TextIndex+1)
		}
	})
}

func (a *App) linkageEditor(c *ui.Context) {
	locked := a.linkEnabled || a.busy
	ui.Column(c).Width(230).Shrink(0).Background(surface).Padding(16).Gap(12).Children(func() {
		section(c, "应用规则")
		ui.Text(c, "每个应用独立列表与间隔").FontSize(11).TextColor(muted)
		ui.Scroll(c).Grow(1).MinHeight(0).Gap(6).Children(func() {
			for i, r := range a.options.AppRules {
				label := r.Name
				if label == "" {
					label = "未命名规则"
				}
				if !r.Enabled {
					label += "（停用）"
				}
				b := ui.Button(c.Key(fmt.Sprintf("rule-%d", i)), label).FillWidth().Label(fmt.Sprintf("选择规则%d", i+1))
				if a.linkSelected == i {
					b.Background(ui.RGB(37, 58, 58)).TextColor(accent)
				}
				if b.Clicked() {
					a.linkSelected = i
					a.loadRuleDraft()
				}
			}
		})
		if ui.Button(c, "添加应用规则").FillWidth().Disabled(locked).Clicked() {
			a.options.AppRules = append(a.options.AppRules, rotation.Rule{Name: fmt.Sprintf("应用规则 %d", len(a.options.AppRules)+1), IntervalSeconds: 5, Enabled: true, Texts: []string{"欢迎回来"}})
			a.linkSelected = len(a.options.AppRules) - 1
			a.loadRuleDraft()
		}
		if ui.Button(c, "删除当前规则").FillWidth().Disabled(locked || len(a.options.AppRules) == 0).Clicked() {
			a.options.AppRules = append(a.options.AppRules[:a.linkSelected], a.options.AppRules[a.linkSelected+1:]...)
			a.loadRuleDraft()
		}
		if ui.Button(c, "保存规则").FillWidth().Disabled(locked).Clicked() {
			a.saveLinkage()
		}
	})
	ui.Scroll(c).Grow(1).MinWidth(0).Padding(22).Gap(12).Children(func() {
		ui.Text(c, "应用联动 · 原生文字").FontSize(24).Bold()
		ui.Text(c, "前台应用优先；切到未配置应用继续上一条规则。目标全部退出后停止，保留最后文字。").TextColor(muted)
		ui.Column(c).Padding(14).Gap(7).Background(surface).Radius(9).Children(func() {
			ui.Text(c, a.linkMessage).TextColor(accent)
			last := a.linkLastText
			if last == "" {
				last = "尚未发送文字"
			}
			ui.Text(c, "最近确认："+last).FontSize(16)
			ui.Text(c, "这里只展示文字内容，不模拟设备字形，不生成图片。").FontSize(11).TextColor(muted)
		})
		if len(a.options.AppRules) > 0 {
			r := &a.options.AppRules[a.linkSelected]
			ui.Row(c).Gap(12).Children(func() {
				ui.TextInput(c, &r.Name).Label("规则名称").Placeholder("规则名称").Grow(1).Disabled(locked)
				ui.Text(c, "开启此规则")
				ui.Switch(c, &r.Enabled).Label("开启此规则").Disabled(locked)
			})
			ui.Text(c, "应用匹配：可执行文件完整路径或进程名；macOS 也支持 .app 完整路径。").FontSize(12).TextColor(muted)
			ui.TextInput(c, &r.Executable).Label("应用路径或进程名").Placeholder("例如 notepad.exe 或 /Applications/Example.app").FillWidth().Disabled(locked)
			ui.Row(c).Gap(8).Children(func() {
				if ui.Button(c, "刷新运行应用").Disabled(locked || a.linkScanning).Clicked() {
					a.scanLinkage(time.Now())
				}
				choices := make([]string, 0, len(a.linkProcessPaths))
				for name := range a.linkProcessPaths {
					choices = append(choices, name)
				}
				sort.Strings(choices)
				ui.Select(c, &a.linkProcessChoice, choices).Label("运行应用列表").Grow(1).MinWidth(0).Disabled(locked || len(choices) == 0)
				if ui.Button(c, "使用所选应用").Disabled(locked || a.linkProcessPaths[a.linkProcessChoice] == "").Clicked() {
					r.Executable = a.linkProcessPaths[a.linkProcessChoice]
				}
			})
			ui.Row(c).Gap(12).Children(func() {
				ui.Text(c, "更换间隔（秒）")
				if ui.NumberInput(c, &a.linkInterval, 1, 86400, 1).Label("轮播间隔秒数").Width(160).Disabled(locked).Changed() {
					r.IntervalSeconds = int(a.linkInterval)
				}
				ui.Text(c, "每条写入并读回确认后计时。").FontSize(11).TextColor(muted)
			})
			ui.Textf(c, "文字列表 · %d 条 · 每行一条，可粘贴多行，调整行顺序即可排序", len(r.Texts)).Bold()
			if ui.TextArea(c.Key(fmt.Sprintf("rule-text-%d", a.linkSelected)), &a.linkLines).Label("轮播文字列表").FillWidth().Height(180).Disabled(locked).Changed() {
				if a.linkLines == "" {
					r.Texts = nil
				} else {
					r.Texts = strings.Split(strings.ReplaceAll(a.linkLines, "\r\n", "\n"), "\n")
				}
			}
			ui.Text(c, "列表无固定条数上限。每条暂限 16 字且 UTF-8 ≤ 48 字节；不支持空行、换行或制表符。使用设备内置字形与静态文字模式。").FontSize(11).TextColor(muted)
		} else {
			ui.Text(c, "点击左侧“添加应用规则”，设置要监控的应用和文字列表。").Padding(20).TextColor(muted)
		}
		ui.Text(c, "监控每约 2 秒检查一次；最小化到托盘继续监控，明确退出后停止，重启不自动写入。监控期间禁止手动发送图案或修改颜色，需先停止监控。").FontSize(11).TextColor(muted)
		ui.Text(c, "启用将替换音箱保存的文字与当前显示模式。macOS 前台识别尚未实机验证；检测或写入失败会暂停，需手动重新启用。").FontSize(11).TextColor(muted)
	})
}
