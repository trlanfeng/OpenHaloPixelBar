package studio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"openpixelbar/internal/appwatch"
)

func fakeLifecycle(a *App) (hidden, shown, quit *int) {
	hidden, shown, quit = new(int), new(int), new(int)
	a.trayReady = true
	a.hideWindow = func() { *hidden++ }
	a.showWindow = func() { *shown++ }
	a.quitApplication = func() { *quit++ }
	return
}

func TestClosePromptCancelTrayAndRemember(t *testing.T) {
	for _, size := range [][2]int{{1320, 880}, {1080, 720}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			h := newLinkageUI(t, size[0], size[1])
			a, tt := h.app, h.tester
			hidden, _, quit := fakeLifecycle(a)
			a.linkEnabled = true
			linkageClick(t, tt, "关闭窗口")
			if !a.closePrompt || !a.linkEnabled {
				t.Fatal("asking must not stop monitoring")
			}
			if err := tt.Click("添加应用规则"); err == nil {
				t.Fatal("close modal did not block background")
			}
			linkageClick(t, tt, "不再询问")
			tt.Key(0, ui.KeyEscape)
			if a.closePrompt || a.rememberClose || a.options.CloseAction != "" {
				t.Fatal("escape saved preference")
			}
			linkageClick(t, tt, "关闭窗口")
			linkageClick(t, tt, "取消")
			if !a.linkEnabled || *hidden != 0 || *quit != 0 {
				t.Fatal("cancel changed lifecycle")
			}
			linkageClick(t, tt, "关闭窗口")
			linkageClick(t, tt, "不再询问")
			if dir := os.Getenv("OPENPB_SCREENSHOT"); dir != "" {
				name := "close-dialog.png"
				if size[0] == 1080 {
					name = "close-dialog-small.png"
				}
				saveScreenshot(t, tt, filepath.Join(dir, name))
			}
			linkageClick(t, tt, "最小化到托盘")
			if !a.hiddenToTray || !a.linkEnabled || *hidden != 1 || *quit != 0 || a.closePrompt {
				t.Fatal("tray choice must preserve monitoring")
			}
			if linkageSavedSettings(t, a).CloseAction != closeTray {
				t.Fatal("remembered choice not saved")
			}
			a.restoreWindow()
			a.requestWindowClose()
			if *hidden != 2 || a.closePrompt {
				t.Fatal("remembered tray choice prompted")
			}
			a.restoreWindow()
			a.resetClosePreference()
			a.requestWindowClose()
			if !a.closePrompt || linkageSavedSettings(t, a).CloseAction != closeAsk {
				t.Fatal("reset did not restore asking")
			}
		})
	}
}

func TestCloseWithoutRememberAndTrayFailure(t *testing.T) {
	h := newLinkageHarness(t)
	a := h.a
	hidden, _, quit := fakeLifecycle(a)
	a.linkEnabled = true
	a.requestWindowClose()
	a.chooseClose(closeTray, false)
	if a.options.CloseAction != closeAsk || *hidden != 1 {
		t.Fatal("unremembered choice persisted")
	}
	a.restoreWindow()
	a.requestWindowClose()
	if !a.closePrompt {
		t.Fatal("next close should ask")
	}
	a.trayReady = false
	a.options.CloseAction = closeTray
	a.chooseClose(closeTray, true)
	if *hidden != 1 || a.hiddenToTray || !a.closePrompt || a.closeError == "" || !a.linkEnabled {
		t.Fatal("unavailable tray trapped user or stopped monitor")
	}
	a.chooseClose(closeExit, false)
	if *quit != 1 || !a.exitApproved || a.linkEnabled {
		t.Fatal("tray unavailable must still allow explicit exit")
	}
}

func TestExplicitExitWaitsAndNeverHides(t *testing.T) {
	for _, state := range []string{"busy", "processing", "checking", "scanning"} {
		t.Run(state, func(t *testing.T) {
			h := newLinkageHarness(t)
			a := h.a
			hidden, _, quit := fakeLifecycle(a)
			a.options.CloseAction = closeTray
			a.linkEnabled = true
			switch state {
			case "busy":
				a.busy = true
			case "processing":
				a.processing = true
			case "checking":
				a.checking = true
			case "scanning":
				a.linkScanning = true
			}
			a.requestExit()
			if !a.exitPending || a.linkEnabled || *hidden != 0 || *quit != 0 {
				t.Fatal("explicit exit must stop without hiding or aborting in-flight work")
			}
			captures := 0
			a.probe = func(func(error)) { captures++ }
			a.checkDevice(h.now)
			a.startLinkage(h.now)
			a.choose()
			a.generateText()
			a.writeColor()
			if captures != 0 || a.linkEnabled {
				t.Fatal("new work scheduled while exiting")
			}
			a.busy, a.processing, a.checking, a.linkScanning = false, false, false, false
			a.pollExit()
			a.pollExit()
			if *quit != 1 || !a.exitApproved || *hidden != 0 {
				t.Fatal("exit did not finish exactly once")
			}
		})
	}
}

func TestRememberExitAndPreferenceMigration(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	a := New()
	_, _, quit := fakeLifecycle(a)
	a.chooseClose(closeExit, true)
	if *quit != 1 || a.options.CloseAction != closeExit {
		t.Fatal("exit choice failed")
	}
	restored := New()
	if restored.options.CloseAction != closeExit || restored.linkEnabled {
		t.Fatal("restart lost preference or enabled monitor")
	}
	_, _, again := fakeLifecycle(restored)
	restored.requestWindowClose()
	if *again != 1 || restored.closePrompt {
		t.Fatal("remembered exit should not ask")
	}
	restored.options.CloseAction = "invalid-from-future"
	if err := restored.save(); err != nil {
		t.Fatal(err)
	}
	if New().options.CloseAction != closeAsk {
		t.Fatal("invalid preference should default to asking")
	}
}

func TestClosePreferenceSaveFailureDoesNotHideOrStop(t *testing.T) {
	h := newLinkageHarness(t)
	a := h.a
	hidden, _, quit := fakeLifecycle(a)
	a.linkEnabled = true
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	a.config = filepath.Join(blocker, "settings.json")
	a.chooseClose(closeTray, true)
	if *hidden != 0 || *quit != 0 || !a.linkEnabled || a.options.CloseAction != "" || !a.closePrompt || a.closeError == "" {
		t.Fatal("save failure changed lifecycle or preference")
	}
	a.options.CloseAction = closeTray
	a.resetClosePreference()
	if a.options.CloseAction != closeTray || !a.failed {
		t.Fatal("reset failure did not preserve preference")
	}
	a.requestExit()
	if *quit != 0 || a.exitPending || !a.failed {
		t.Fatal("failed save must cancel exit")
	}
}

func TestTrayMenuAndHiddenMonitoring(t *testing.T) {
	h := newLinkageHarness(t)
	a := h.a
	_, _, quit := fakeLifecycle(a)
	expected := []string{"显示主窗口", "启用应用监控", "停止应用监控", "恢复关闭时询问", "退出程序"}
	var labels []string
	for _, item := range a.trayMenuItems() {
		if item.Label != "" {
			labels = append(labels, item.Label)
			if item.Click == nil {
				t.Fatal("menu has no action")
			}
		}
	}
	if strings.Join(labels, "|") != strings.Join(expected, "|") {
		t.Fatal(labels)
	}
	h.snapshot = appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}}
	a.startLinkage(h.now)
	a.chooseClose(closeTray, false)
	// Hide while a write is in flight, then continue the next entry on schedule.
	h.finish(nil)
	h.tick(5 * time.Second)
	if h.captures != 2 || !a.linkEnabled || a.closed || *quit != 0 || len(h.sends) != 2 || h.sends[1] != "甲二" {
		t.Fatal("hidden monitor stopped", h.sends)
	}
	a.requestExit()
	if *quit != 0 || a.linkEnabled {
		t.Fatal("exit aborted in-flight write")
	}
	h.finish(nil)
	a.pollExit()
	if *quit != 1 {
		t.Fatal("confirmed final write did not unblock exit")
	}
}
