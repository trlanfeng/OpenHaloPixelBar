package studio

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"openpixelbar/internal/branding"
)

const closeAsk, closeTray, closeExit = "", "tray", "exit"

// All lifecycle state and menu callbacks are owned by the UI thread.
type lifecycle struct {
	tray                                    *mygo.Tray
	trayReady, hiddenToTray                 bool
	closePrompt, rememberClose              bool
	closeError                              string
	exitPending, exitApproved               bool
	hideWindow, showWindow, quitApplication func()
}

func (a *App) installLifecycle() {
	a.initTray()
	mygo.App.OnActivate(func(_ bool) { a.update(a.restoreWindow) })
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !a.exitApproved {
			e.PreventDefault()
			a.update(a.requestExit)
		}
	})
	a.window.OnClose(func(e *mygo.CloseEvent) {
		if !a.exitApproved {
			e.PreventDefault()
			a.update(a.requestWindowClose)
		}
	})
}

func (a *App) initTray() {
	a.hideWindow = a.window.Hide
	a.showWindow = func() {
		a.window.Show()
		if a.window.IsMinimized() {
			a.window.Restore()
		}
		a.window.Focus()
	}
	a.quitApplication = mygo.App.Quit
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "openhalopixelbar-main", Icon: branding.IconPNG, ToolTip: branding.Name, Menu: mygo.NewMenu(a.trayMenuItems())})
	if err != nil {
		a.closeError = "托盘创建失败，窗口不会隐藏：" + err.Error()
		a.status, a.failed = a.closeError, true
		return
	}
	a.tray, a.trayReady = tray, true
	tray.OnClick(func() { a.update(a.restoreWindow) })
}

func (a *App) trayMenuItems() []*mygo.MenuItem {
	action := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(_ *mygo.MenuItem, _ *mygo.Window) { a.update(fn) }
	}
	return []*mygo.MenuItem{
		{Label: "显示主窗口", Click: action(a.restoreWindow)},
		mygo.Separator(),
		{Label: "启用应用监控", Click: action(func() {
			if a.exitPending {
				return
			}
			a.restoreWindow()
			a.startLinkage(time.Now())
		})},
		{Label: "停止应用监控", Click: action(func() {
			a.stopLinkage("已从托盘停止监控，将恢复联动前的音箱模式。")
		})},
		mygo.Separator(),
		{Label: "恢复关闭时询问", Click: action(func() { a.restoreWindow(); a.resetClosePreference() })},
		{Label: "退出程序", Click: action(a.requestExit)},
	}
}

func (a *App) restoreWindow() {
	if a.closed {
		return
	}
	a.hiddenToTray = false
	if a.showWindow != nil {
		a.showWindow()
	}
}

func (a *App) requestWindowClose() {
	if a.closed || a.exitApproved {
		return
	}
	if a.exitPending {
		a.restoreWindow()
		return
	}
	switch a.options.CloseAction {
	case closeExit:
		a.requestExit()
	case closeTray:
		a.chooseClose(closeTray, false)
	default:
		a.restoreWindow()
		a.help, a.importOpen = false, false
		if !a.closePrompt {
			a.rememberClose = false
		}
		a.closePrompt = true
	}
}

func (a *App) chooseClose(action string, remember bool) {
	if a.closed || a.exitPending || (action != closeTray && action != closeExit) {
		return
	}
	if action == closeTray && (!a.trayReady || a.hideWindow == nil) {
		a.restoreWindow()
		a.closePrompt = true
		a.closeError = "托盘不可用，无法隐藏窗口；可以取消或选择退出程序。"
		return
	}
	previous := a.options.CloseAction
	if remember {
		a.options.CloseAction = action
	}
	// Persist before hiding or stopping. On failure keep the window and prompt.
	if err := a.save(); err != nil {
		a.options.CloseAction = previous
		a.restoreWindow()
		a.closePrompt = true
		a.closeError = "保存关闭偏好失败：" + err.Error()
		a.failed = true
		return
	}
	a.closePrompt, a.rememberClose = false, false
	a.closeError = ""
	if action == closeExit {
		a.requestExit()
		return
	}
	a.hiddenToTray = true
	a.hideWindow()
}

func (a *App) resetClosePreference() {
	previous := a.options.CloseAction
	a.options.CloseAction = closeAsk
	if err := a.save(); err != nil {
		a.options.CloseAction = previous
		a.status, a.failed = "恢复关闭询问失败："+err.Error(), true
		return
	}
	a.status, a.failed = "已恢复关闭时询问，下次关闭可重新选择。", false
}

func (a *App) requestExit() {
	if a.closed || a.exitApproved {
		return
	}
	a.closePrompt = false
	a.exitPending = true
	a.stopLinkage("正在退出：已停止监控，等待在途操作完成。")
	a.restoreWindow()
	a.pollExit()
}

func (a *App) pollExit() {
	if !a.exitPending || a.closed || a.exitApproved {
		return
	}
	a.restoreLinkageDisplay()
	// A synchronous test completion or a failed restore can cancel exit here.
	if !a.exitPending || a.busy || a.processing || a.checking || a.linkScanning || a.linkRestorePending {
		return
	}
	if !a.canClose() {
		a.exitPending = false
		a.restoreWindow()
		return
	}
	a.exitApproved = true
	if a.quitApplication != nil {
		a.quitApplication()
	}
}

func (a *App) closeDialog(c *ui.Context) {
	if ui.DialogBase(c.Key("close-dialog"), &a.closePrompt, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(3, 6, 10, .78))
		panel.Width(600).MaxWidthPercent(90).Padding(24).Gap(18).Radius(16).Border(1, border).Background(surface).Label("关闭程序")
		ui.Text(c, "关闭程序").FontSize(23).Bold()
		ui.Text(c, "选择退出程序，还是最小化到托盘？").FontSize(16)
		ui.Text(c, "最小化到托盘：隐藏窗口，应用监控与后台任务继续运行。可从 Windows 托盘或 macOS 菜单栏图标恢复窗口。").TextColor(muted)
		ui.Text(c, "退出程序：停止监控，等待当前操作完成并恢复联动前模式后退出；恢复失败会保留窗口提示。").TextColor(muted)
		ui.Checkbox(c, &a.rememberClose, "不再询问，记住本次选择").Label("不再询问")
		ui.Text(c, "可通过托盘菜单或“使用说明”恢复关闭时询问；不影响重启后需手动启用监控。").FontSize(11).TextColor(muted)
		if a.closeError != "" {
			ui.Text(c, a.closeError).TextColor(ui.RGB(255, 145, 145))
		}
		ui.Row(c).Gap(12).Justify(ui.End).Children(func() {
			if ui.Button(c, "取消").Clicked() {
				a.closePrompt = false
				a.rememberClose = false
			}
			if ui.Button(c, "退出程序").Clicked() {
				a.chooseClose(closeExit, a.rememberClose)
			}
			if ui.PrimaryButton(c, "最小化到托盘").Disabled(!a.trayReady).Clicked() {
				a.chooseClose(closeTray, a.rememberClose)
			}
		})
	}).Dismissed() {
		a.rememberClose = false
	}
}
