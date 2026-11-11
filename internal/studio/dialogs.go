package studio

import (
	"github.com/egoist/mygo/ui"
	"openpixelbar/internal/branding"
	"time"
)

// All state transitions run on the UI thread. The probe only performs I/O
// in its worker and dispatches its completion back to that thread.
func (a *App) checkDevice(now time.Time) {
	if a.closed || a.busy || a.checking || a.probe == nil {
		return
	}
	a.checking = true
	a.nextCheck = now.Add(10 * time.Second)
	a.probe(func(err error) { a.deviceResult(err, time.Now()) })
}
func (a *App) pollDevice(now time.Time) {
	if !now.Before(a.nextCheck) {
		a.checkDevice(now)
	}
}
func (a *App) deviceResult(err error, at time.Time) {
	a.checking = false
	a.connected = err == nil
	a.checkedAt = at
	a.deviceError = ""
	if err != nil {
		a.deviceError = err.Error()
	}
}
func (a *App) deviceBar(c *ui.Context) {
	ui.Row(c).Height(52).Shrink(0).Padding(8, 24).Gap(18).Background(ui.RGB(26, 33, 42)).Children(func() {
		ui.Text(c, "Halo PixelBar").Bold()
		status := "等待检测"
		col := muted
		if !a.checkedAt.IsZero() {
			status = "未连接"
			col = ui.RGB(255, 177, 125)
		}
		if a.connected {
			status = "已连接 · 固件 1.1.1"
			col = accent
		}
		if a.checking {
			status = "正在检测…"
			col = muted
		}
		ui.Text(c, "●  "+status).TextColor(col).Tooltip(a.deviceError)
		ui.Text(c, "USB · 256 × 32").FontSize(11).TextColor(muted)
		ui.Spacer(c)
		label := "启动时检测 · 每 10 秒自动刷新"
		if !a.checkedAt.IsZero() {
			label = "每 10 秒自动检测 · 最近 " + a.checkedAt.Format("15:04:05")
		}
		ui.Text(c, label).FontSize(11).TextColor(muted)
		if ui.Button(c, "检测连接").Disabled(a.checking || a.busy).Clicked() {
			a.checkDevice(time.Now())
		}
	})
}

func (a *App) dialogs(c *ui.Context) {
	if ui.DialogBase(c.Key("import-dialog"), &a.importOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(3, 6, 10, .78))
		panel.Width(600).MaxWidthPercent(90).Padding(26).Gap(18).Radius(16).Border(1, border).Background(surface).Label("导入素材")
		ui.Row(c).Children(func() {
			ui.Text(c, "导入素材").Bold().FontSize(23).Grow(1)
			if ui.Button(c, "×").Label("关闭导入弹框").Disabled(a.busy).Clicked() {
				a.importOpen = false
			}
		})
		ui.Text(c, "选择一张图片，或一段 GIF 动画，制作你的屏幕图案。").TextColor(muted)
		ui.Column(c).Gap(12).Padding(16).Background(bg).Radius(10).Children(func() {
			ui.Text(c, "支持的文件格式").Bold()
			ui.Row(c).Gap(7).Children(func() {
				for _, format := range []string{"PNG", "JPG / JPEG", "BMP", "WebP", "GIF"} {
					ui.Text(c, format).FontSize(12).Padding(6, 10).Radius(6).Background(ui.RGB(31, 49, 46)).TextColor(accent)
				}
			})
			ui.Text(c, "单文件 ≤ 32 MB；全部帧合计 ≤ 4000 万像素。\nGIF 最多 8 帧，每帧至少 100 ms；其他格式按静态图片处理。").FontSize(12).TextColor(muted)
		})
		ui.Row(c).Justify(ui.Center).PaddingY(9).Children(func() {
			label := "选择图片或 GIF…"
			if a.busy {
				label = "正在读取素材…"
			}
			if ui.PrimaryButton(c, label).Padding(13, 32).Disabled(a.busy || a.processing).Clicked() {
				a.importError = ""
				a.choose()
			}
		})
		if a.importError != "" {
			ui.Text(c, a.importError).TextColor(ui.RGB(255, 156, 156)).FontSize(12).MaxLines(3)
		}
		ui.Column(c).Gap(7).Children(func() {
			ui.Text(c, "免责声明").Bold().FontSize(12)
			ui.Text(c, branding.Disclaimer).FontSize(12).TextColor(muted)
		})
	}).Dismissed() && a.busy {
		a.importOpen = true
	}
	ui.DialogBase(c.Key("help-dialog"), &a.help, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(3, 6, 10, .78))
		panel.Width(650).MaxWidthPercent(90).Padding(24).Gap(16).Radius(16).Border(1, border).Background(surface).Label("使用说明")
		ui.Row(c).Children(func() {
			ui.Text(c, "使用说明").FontSize(23).Bold().Grow(1)
			if ui.Button(c, "×").Label("关闭使用说明").Clicked() {
				a.help = false
			}
		})
		a.helpView(c)
		ui.Row(c).Justify(ui.End).Children(func() {
			if ui.PrimaryButton(c, "知道了").Padding(10, 24).Clicked() {
				a.help = false
			}
		})
	})
}
