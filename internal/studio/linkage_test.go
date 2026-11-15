package studio

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"openpixelbar/internal/appwatch"
	"openpixelbar/internal/pixelbar"
	"openpixelbar/internal/rotation"
)

type linkageHarness struct {
	a               *App
	now             time.Time
	snapshot        appwatch.Snapshot
	captureErr      error
	scan            func(appwatch.Snapshot, error)
	sends           []string
	done            func(error)
	captures        int
	displayCaptures int
	restores        int
}

func newLinkageHarness(t *testing.T) *linkageHarness {
	t.Helper()
	h := &linkageHarness{now: time.Unix(1000, 0)}
	h.a = &App{config: filepath.Join(t.TempDir(), "settings.json")}
	h.a.options.AppRules = []rotation.Rule{
		{Name: "应用甲", Executable: "a.exe", Texts: []string{"甲一", "甲二"}, IntervalSeconds: 5, Enabled: true},
		{Name: "应用乙", Executable: "b.exe", Texts: []string{"乙一", "乙二"}, IntervalSeconds: 3, Enabled: true},
	}
	h.a.initLinkage()
	h.a.linkNow = func() time.Time { return h.now }
	h.a.linkCapture = func(done func(appwatch.Snapshot, error)) { h.captures++; done(h.snapshot, h.captureErr) }
	h.a.linkSend = func(text string, done func(error)) { h.sends = append(h.sends, text); h.done = done }
	h.a.linkSaveDisplay = func(done func(*pixelbar.DisplaySnapshot, error)) {
		h.displayCaptures++
		done(&pixelbar.DisplaySnapshot{}, nil)
	}
	h.a.linkRestoreDisplay = func(_ *pixelbar.DisplaySnapshot, done func(error)) {
		h.restores++
		done(nil)
	}
	return h
}
func (h *linkageHarness) observe(fg int, names ...string) {
	h.snapshot = appwatch.Snapshot{ForegroundPID: fg}
	for i, name := range names {
		h.snapshot.Processes = append(h.snapshot.Processes, appwatch.Process{PID: i + 1, Name: name, Path: name})
	}
	h.a.scanLinkage(h.now)
}
func (h *linkageHarness) finish(err error)     { done := h.done; h.done = nil; done(err) }
func (h *linkageHarness) tick(d time.Duration) { h.now = h.now.Add(d); h.a.pollLinkage(h.now) }

func TestLinkageIntegrationForegroundBackgroundAndInstances(t *testing.T) {
	h := newLinkageHarness(t)
	h.snapshot = appwatch.Snapshot{ForegroundPID: 1, Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}, {PID: 2, Name: "b.exe"}}}
	h.a.startLinkage(h.now)
	if !h.a.linkEnabled || !h.a.busy || !reflect.DeepEqual(h.sends, []string{"甲一"}) {
		t.Fatal("already-running app must send immediately", h.sends)
	}
	// A slow device confirmation must not start the interval at dispatch time.
	h.tick(20 * time.Second)
	if len(h.sends) != 1 {
		t.Fatal("overlapping write")
	}
	h.finish(nil)
	h.tick(4 * time.Second)
	if len(h.sends) != 1 {
		t.Fatal("interval started before confirmation")
	}
	h.observe(3, "a.exe", "b.exe", "other.exe")
	h.tick(time.Second)
	if h.sends[len(h.sends)-1] != "甲二" {
		t.Fatal("background rule lost its progress", h.sends)
	}
	h.finish(nil)
	h.observe(2, "a.exe", "b.exe")
	if h.sends[len(h.sends)-1] != "乙一" {
		t.Fatal("foreground switch did not start at first text")
	}
	h.finish(nil)
	h.observe(0, "a.exe", "a.exe")
	if h.sends[len(h.sends)-1] != "甲一" {
		t.Fatal("exit did not fall back to live history")
	}
	h.finish(nil)
	h.observe(0, "a.exe")
	if h.a.linkEngine.Active() != 0 {
		t.Fatal("remaining instance lost")
	}
	h.tick(5 * time.Second)
	if h.sends[len(h.sends)-1] != "甲二" {
		t.Fatal("remaining instance reset progress")
	}
	h.finish(nil)
	h.observe(0)
	count := len(h.sends)
	h.tick(time.Hour)
	if len(h.sends) != count || h.a.linkEngine.Active() != -1 || h.a.linkLastText != "甲二" || h.restores != 1 || h.displayCaptures != 1 || h.a.linkPreviousDisplay != nil || !h.a.linkEnabled {
		t.Fatal("all-exit should restore the original mode once and keep monitoring")
	}
}

func TestLinkageInFlightSwitchAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			h := newLinkageHarness(t)
			h.snapshot = appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}}
			h.a.startLinkage(h.now)
			h.observe(2, "a.exe", "b.exe")
			if len(h.sends) != 1 || h.a.linkEngine.Active() != 1 {
				t.Fatal("switch must wait for physical write")
			}
			h.a.status = "新规则等待设备"
			var err error
			if fail {
				err = errors.New("readback mismatch")
			}
			h.finish(err)
			if fail {
				h.tick(time.Hour)
				if h.a.linkEnabled || !h.a.failed || len(h.sends) != 1 {
					t.Fatal("old physical write error must pause globally")
				}
			} else {
				if h.a.status != "新规则等待设备" {
					t.Fatal("stale confirmation overwrote active status")
				}
				h.a.dispatchLinkage(h.now)
				if !reflect.DeepEqual(h.sends, []string{"甲一", "乙一"}) {
					t.Fatal("new rule did not start at first text", h.sends)
				}
				h.finish(nil)
			}
		})
	}
}

func TestLinkageStopKeepsDeviceLockedUntilCompletion(t *testing.T) {
	h := newLinkageHarness(t)
	h.snapshot = appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}}
	h.a.startLinkage(h.now)
	h.a.stopLinkage("已停止")
	if !h.a.busy {
		t.Fatal("stop released physical USB lock")
	}
	probes := 0
	h.a.probe = func(done func(error)) { probes++; done(nil) }
	h.a.checkDevice(h.now)
	h.a.startLinkage(h.now)
	if probes != 0 || h.a.linkEnabled {
		t.Fatal("restarted/probed during in-flight write")
	}
	h.finish(nil)
	h.tick(time.Hour)
	if h.a.busy || h.a.linkEnabled || len(h.sends) != 1 || h.restores != 1 || !strings.Contains(h.a.status, "已确认恢复") {
		t.Fatal("late callback resumed monitoring")
	}
	h.a.startLinkage(h.now)
	if len(h.sends) != 2 || h.sends[1] != "甲一" {
		t.Fatal("manual restart must start from first")
	}
	h.finish(errors.New("timeout"))
	h.tick(time.Hour)
	if len(h.sends) != 2 || h.a.linkEnabled || !h.a.failed {
		t.Fatal("failure retried")
	}
}

func TestLinkageDeviceMutualExclusion(t *testing.T) {
	for _, field := range []string{"busy", "processing", "checking"} {
		t.Run(field, func(t *testing.T) {
			h := newLinkageHarness(t)
			switch field {
			case "busy":
				h.a.busy = true
			case "processing":
				h.a.processing = true
			case "checking":
				h.a.checking = true
			}
			h.a.startLinkage(h.now)
			if h.a.linkEnabled || h.captures != 0 {
				t.Fatal("monitor started during another operation")
			}
			h.a.linkEnabled = true
			h.a.linkEngine.Observe(h.a.options.AppRules, appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}})
			h.a.dispatchLinkage(h.now)
			if len(h.sends) != 0 {
				t.Fatal("write overlapped another operation")
			}
		})
	}
	h := newLinkageHarness(t)
	h.a.linkEnabled = true
	h.a.connect(true)
	h.a.writeColor()
	if h.a.busy {
		t.Fatal("manual USB write allowed during monitoring")
	}
	var done func(error)
	h.a.probe = func(cb func(error)) { done = cb }
	h.a.checkDevice(h.now)
	h.a.linkEngine.Observe(h.a.options.AppRules, appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}})
	h.a.dispatchLinkage(h.now)
	if len(h.sends) != 0 {
		t.Fatal("write overlapped device query")
	}
	done(nil)
	h.a.dispatchLinkage(h.now)
	if len(h.sends) != 1 {
		t.Fatal("write did not resume after query")
	}
	h.finish(nil)
}

func TestLinkageScanGenerationAndClose(t *testing.T) {
	h := newLinkageHarness(t)
	h.a.linkCapture = func(done func(appwatch.Snapshot, error)) { h.captures++; h.scan = done }
	h.a.startLinkage(h.now)
	h.tick(time.Hour)
	if h.captures != 1 {
		t.Fatal("overlapping snapshot capture")
	}
	h.a.stopLinkage("停止检测")
	h.scan(appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}}, nil)
	if len(h.sends) != 0 || h.a.linkScanning || h.a.status != "停止检测" {
		t.Fatal("stale scan changed stopped session")
	}
	h.a.startLinkage(h.now)
	h.scan(appwatch.Snapshot{}, errors.New("foreground unavailable"))
	h.tick(time.Hour)
	if h.a.linkEnabled || !h.a.failed || h.captures != 2 {
		t.Fatal("capture failure must pause without retry")
	}
	h = newLinkageHarness(t)
	h.snapshot = appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}}
	h.a.startLinkage(h.now)
	if h.a.canClose() || h.a.linkEnabled || !h.a.busy {
		t.Fatal("close must stop scheduling and wait for write")
	}
	h.finish(nil)
	if !h.a.canClose() {
		t.Fatal("close should succeed after confirmation")
	}
	h.a.closed = true
	h.tick(time.Hour)
	h.a.scanLinkage(h.now)
	if len(h.sends) != 1 || h.captures != 1 {
		t.Fatal("work scheduled after close")
	}
}

func TestLinkageRestoreWaitsAndRecapturesNextSession(t *testing.T) {
	h := newLinkageHarness(t)
	h.snapshot = appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}}
	h.a.startLinkage(h.now)
	var restored func(error)
	h.a.linkRestoreDisplay = func(_ *pixelbar.DisplaySnapshot, done func(error)) {
		h.restores++
		restored = done
	}
	h.observe(0)
	if h.restores != 0 || !h.a.busy {
		t.Fatal("restore overlapped in-flight text")
	}
	// A rapid restart cannot cancel the pending restoration.
	h.observe(1, "a.exe")
	h.finish(nil)
	if h.restores != 1 || !h.a.busy || !h.a.linkRestoring {
		t.Fatal("restore must run after old text even if target restarts")
	}
	h.tick(time.Hour)
	if len(h.sends) != 1 || h.displayCaptures != 1 {
		t.Fatal("new session overlapped restoration")
	}
	restored(nil)
	h.tick(time.Second)
	if h.displayCaptures != 2 || len(h.sends) != 2 {
		t.Fatal("new takeover must capture the newly restored state")
	}
	h.finish(nil)
}

func TestLinkageCaptureFailureAndStopDuringCapture(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			h := newLinkageHarness(t)
			h.snapshot = appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}}
			var captured func(*pixelbar.DisplaySnapshot, error)
			h.a.linkSaveDisplay = func(done func(*pixelbar.DisplaySnapshot, error)) { captured = done }
			h.a.startLinkage(h.now)
			if len(h.sends) != 0 || !h.a.busy {
				t.Fatal("text sent before saving original mode")
			}
			if fail {
				captured(nil, errors.New("unrestorable state"))
			} else {
				h.a.stopLinkage("停止")
				captured(&pixelbar.DisplaySnapshot{}, nil)
			}
			h.tick(time.Hour)
			if h.a.busy || h.a.linkEnabled || h.restores != 0 || len(h.sends) != 0 || h.a.linkPreviousDisplay != nil || (fail && !h.a.failed) {
				t.Fatal("read-only capture must not trigger writes after stop/failure")
			}
		})
	}
}

func TestLinkageRestoreExitAndFailureNoRetry(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			h := newLinkageHarness(t)
			h.snapshot = appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}}
			h.a.startLinkage(h.now)
			var restored func(error)
			h.a.linkRestoreDisplay = func(_ *pixelbar.DisplaySnapshot, done func(error)) { h.restores++; restored = done }
			quits := 0
			h.a.quitApplication = func() { quits++ }
			h.a.requestExit()
			if quits != 0 || h.restores != 0 {
				t.Fatal("exit did not wait for text")
			}
			h.finish(nil)
			h.a.pollExit()
			if quits != 0 || !h.a.busy || h.restores != 1 {
				t.Fatal("exit did not wait for restoration")
			}
			var err error
			if fail {
				h.a.help, h.a.importOpen, h.a.closePrompt = true, true, true
				err = errors.New("restore timeout")
			}
			restored(err)
			h.a.pollExit()
			if fail {
				if quits != 0 || h.a.exitPending || h.a.help || h.a.importOpen || h.a.closePrompt || !h.a.failed || !strings.Contains(h.a.status, "恢复联动前模式失败") {
					t.Fatal("failed restore should cancel exit and surface error")
				}
				h.tick(time.Hour)
				h.a.stopLinkage("停止")
				h.a.requestExit()
				h.a.pollExit()
			}
			if quits != 1 || h.restores != 1 || !h.a.exitApproved {
				t.Fatal("exit or no-retry restoration contract violated")
			}
		})
	}
}

func TestLinkageWriteFailureCancelsAutomaticRestoreAndExit(t *testing.T) {
	h := newLinkageHarness(t)
	h.snapshot = appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}}
	h.a.startLinkage(h.now)
	h.a.requestExit()
	h.finish(errors.New("text confirmation missing"))
	h.a.pollExit()
	h.tick(time.Hour)
	if h.restores != 0 || h.a.exitPending || h.a.linkEnabled || !h.a.failed || h.a.linkPreviousDisplay == nil {
		t.Fatal("uncertain write should stop automatic writes and preserve original snapshot")
	}
	// A new explicit stop is permission for one restoration, not a text retry.
	h.a.stopLinkage("手动停止")
	if h.restores != 1 || len(h.sends) != 1 {
		t.Fatal("explicit stop did not restore preserved snapshot")
	}
}

func TestLinkageRefreshErrorDoesNotCancelExplicitRestore(t *testing.T) {
	h := newLinkageHarness(t)
	h.snapshot = appwatch.Snapshot{Processes: []appwatch.Process{{PID: 1, Name: "a.exe"}}}
	h.a.startLinkage(h.now)
	h.finish(nil)
	h.a.checking = true
	h.a.stopLinkage("停止")
	h.captureErr = errors.New("process query failed")
	h.a.scanLinkage(h.now)
	if !h.a.linkRestorePending || h.restores != 0 {
		t.Fatal("application refresh must not cancel explicit pending restore")
	}
	h.a.checking = false
	h.tick(time.Second)
	if h.restores != 1 || h.a.linkPreviousDisplay != nil {
		t.Fatal("pending restore did not resume after device query")
	}
}

func TestLinkageStopWithoutTakeoverDoesNotWrite(t *testing.T) {
	h := newLinkageHarness(t)
	h.a.startLinkage(h.now)
	h.a.stopLinkage("停止")
	h.a.requestExit()
	if h.restores != 0 || h.displayCaptures != 0 || len(h.sends) != 0 {
		t.Fatal("idle monitoring must not modify device on stop/exit")
	}
}

func TestLinkageConfigurationSaveAndValidation(t *testing.T) {
	h := newLinkageHarness(t)
	h.a.saveLinkage()
	data, err := os.ReadFile(h.a.config)
	if err != nil || !strings.Contains(string(data), "interval_seconds") {
		t.Fatal("rules not saved", err)
	}
	for _, text := range []string{"", "\tbad", strings.Repeat("字", 17), strings.Repeat("😀", 13), "bad\x00"} {
		h.a.options.AppRules[0].Texts = []string{text}
		if h.a.validateLinkage(true) == nil {
			t.Fatalf("invalid text accepted: %q", text)
		}
	}
	for i := range h.a.options.AppRules {
		h.a.options.AppRules[i].Enabled = false
	}
	h.a.saveLinkage()
	if h.a.failed {
		t.Fatal("disabled drafts must be saveable")
	}
	if h.a.validateLinkage(true) == nil {
		t.Fatal("empty enabled set must not start")
	}
	h.a.options.AppRules = nil
	h.a.saveLinkage()
	if h.a.failed {
		t.Fatal("deleted last rule cannot be saved")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	h.a.config = filepath.Join(blocker, "settings.json")
	h.a.saveLinkage()
	if !h.a.failed || h.a.canClose() {
		t.Fatal("save failure must be surfaced and prevent close")
	}
}
