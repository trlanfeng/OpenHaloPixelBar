package studio

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"openpixelbar/internal/appwatch"
)

type linkageUIHarness struct {
	app      *App
	tester   *ui.Tester
	snapshot appwatch.Snapshot
	captures int
	sent     []string
}

func newLinkageUI(t *testing.T, width, height int) *linkageUIHarness {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	a := New()
	a.config = filepath.Join(dir, "settings.json")
	a.playing = false
	h := &linkageUIHarness{app: a, snapshot: appwatch.Snapshot{
		Processes: []appwatch.Process{
			{PID: 1, Path: "C:/Apps/Editor/editor.exe", Name: "editor.exe"},
			{PID: 2, Name: "music.exe"},
		},
		ForegroundPID: 1,
	}}
	a.linkCapture = func(done func(appwatch.Snapshot, error)) {
		h.captures++
		done(h.snapshot, nil)
	}
	a.linkSend = func(text string, done func(error)) {
		h.sent = append(h.sent, text)
		done(nil)
	}
	a.probe = func(done func(error)) { done(nil) }
	h.tester = ui.NewTester(a.View, width, height)
	a.linkNow = h.tester.Now
	linkageClick(t, h.tester, linkageMode)
	if a.mode != linkageMode {
		t.Fatal("点击应用联动标签未切换页面")
	}
	return h
}

func linkageClick(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("无法点击 %q：%v", label, err)
	}
}

func linkageReplace(t *testing.T, tt *ui.Tester, label, value string) {
	t.Helper()
	linkageClick(t, tt, label)
	tt.Command("selectAll")
	tt.SetClipboard(value)
	tt.Command("paste")
}

func linkageScreenshot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if dir := os.Getenv("OPENPB_SCREENSHOT"); dir != "" {
		saveScreenshot(t, tt, filepath.Join(dir, name))
	}
}

func linkageSavedSettings(t *testing.T, a *App) settings {
	t.Helper()
	b, err := os.ReadFile(a.config)
	if err != nil {
		t.Fatal(err)
	}
	var saved settings
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestLinkageUILayoutAndWindowControls(t *testing.T) {
	for _, size := range [][2]int{{1320, 880}, {1080, 720}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			h := newLinkageUI(t, size[0], size[1])
			a, tt := h.app, h.tester
			linkageClick(t, tt, "添加应用规则")
			linkageReplace(t, tt, "规则名称", "编辑器工作提醒")
			linkageReplace(t, tt, "应用路径或进程名", "editor.exe")
			for _, label := range []string{"最小化", "最大化或还原", "关闭窗口", "保存规则", "启用应用监控"} {
				r, ok := tt.Find(label)
				if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > float32(size[0]) || r.Y+r.H > float32(size[1]) {
					t.Errorf("%q 超出窗口或不可见：%+v", label, r)
				}
			}
			var right float32
			for _, label := range []string{"使用说明", "＋  导入素材", "最小化", "最大化或还原", "关闭窗口"} {
				r, ok := tt.Find(label)
				if !ok || r.X < right || r.Y+r.H > 72 {
					t.Errorf("标题栏控件 %q 重叠或溢出：%+v，前一控件右沿 %v", label, r, right)
				}
				right = r.X + r.W
			}
			for _, label := range []string{"最小化", "最大化或还原", "关闭窗口"} {
				linkageClick(t, tt, label)
			}
			linkageClick(t, tt, "保存规则")
			if a.failed || len(linkageSavedSettings(t, a).AppRules) != 1 {
				t.Fatalf("保存按钮未写入规则：%s", a.linkMessage)
			}
			name := "linkage.png"
			if size[0] == 1080 {
				name = "linkage-small.png"
			}
			linkageScreenshot(t, tt, name)
		})
	}
}

func TestLinkageUILongTextListEditingAndPersistence(t *testing.T) {
	h := newLinkageUI(t, 1320, 880)
	a, tt := h.app, h.tester
	linkageClick(t, tt, "添加应用规则")
	linkageReplace(t, tt, "应用路径或进程名", "editor.exe")
	want := make([]string, 120)
	for i := range want {
		want[i] = fmt.Sprintf("提醒第%03d条", i+1)
	}
	linkageReplace(t, tt, "轮播文字列表", strings.Join(want, "\n"))
	if !slices.Equal(a.options.AppRules[0].Texts, want) {
		t.Fatalf("120 行粘贴被截断或改写，实际 %d 行", len(a.options.AppRules[0].Texts))
	}
	// 在文末实际输入新行，再通过键盘删除；不直接修改应用的草稿或规则。
	tt.Key(ui.Cmd, ui.KeyEnd)
	tt.Type("\n末尾新增提醒")
	want = append(want, "末尾新增提醒")
	if !slices.Equal(a.options.AppRules[0].Texts, want) {
		t.Fatal("在第 120 行后追加文字失败")
	}
	linkageScreenshot(t, tt, "linkage-long-list.png")
	tt.Key(0, ui.KeyHome)
	tt.Key(ui.Shift, ui.KeyEnd)
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyBackspace)
	want = want[:len(want)-1]
	if !slices.Equal(a.options.AppRules[0].Texts, want) {
		t.Fatalf("删除最后一行改变其他行，实际 %d 行", len(a.options.AppRules[0].Texts))
	}
	// 剪切第一行并粘贴到列表末尾，验证真实多行编辑的行序。
	tt.Key(ui.Cmd, ui.KeyHome)
	tt.Key(ui.Shift, ui.KeyEnd)
	tt.Command("cut")
	first := tt.Clipboard()
	if first != want[0] {
		t.Fatalf("剪切首行错误：%q", first)
	}
	tt.Key(0, ui.KeyDelete)
	tt.Key(ui.Cmd, ui.KeyEnd)
	tt.Type("\n")
	tt.Command("paste")
	want = append(append([]string(nil), want[1:]...), first)
	if !slices.Equal(a.options.AppRules[0].Texts, want) {
		t.Fatal("首行移动到末尾后行序或内容错误")
	}
	linkageClick(t, tt, "保存规则")
	if a.failed || !slices.Equal(linkageSavedSettings(t, a).AppRules[0].Texts, want) {
		t.Fatalf("长列表保存未保留完整顺序：%s", a.linkMessage)
	}
	linkageClick(t, tt, "添加应用规则")
	linkageReplace(t, tt, "应用路径或进程名", "music.exe")
	linkageReplace(t, tt, "轮播文字列表", "另一规则")
	linkageClick(t, tt, "选择规则1")
	if a.linkLines != strings.Join(want, "\n") {
		t.Fatal("切换规则后长列表草稿丢失")
	}
	tt.SetSize(1080, 720)
	// 最小窗口允许主编辑区滚动；滚动后仍能操作列表尾部和固定保存按钮。
	tt.Scroll(240, 400, 0, 1000)
	linkageClick(t, tt, "轮播文字列表")
	tt.Key(ui.Cmd, ui.KeyEnd)
	tt.Type("已改")
	want[len(want)-1] += "已改"
	if !slices.Equal(a.options.AppRules[0].Texts, want) {
		t.Fatal("最小窗口滚动后无法编辑长列表末行")
	}
	linkageScreenshot(t, tt, "linkage-long-list-small.png")
	linkageClick(t, tt, "保存规则")
	if a.failed || !slices.Equal(linkageSavedSettings(t, a).AppRules[0].Texts, want) {
		t.Fatalf("最小窗口滚动后保存失败：%s", a.linkMessage)
	}
}

func TestLinkageUIRunningApplicationSelectionAndMonitoring(t *testing.T) {
	h := newLinkageUI(t, 1320, 880)
	a, tt := h.app, h.tester
	linkageClick(t, tt, "添加应用规则")
	linkageClick(t, tt, "刷新运行应用")
	if h.captures != 1 || len(a.linkProcessPaths) != 2 {
		t.Fatal("刷新未使用模拟运行应用快照")
	}
	linkageClick(t, tt, "运行应用列表")
	linkageClick(t, tt, "C:/Apps/Editor/editor.exe")
	linkageClick(t, tt, "使用所选应用")
	if a.options.AppRules[0].Executable != "C:/Apps/Editor/editor.exe" {
		t.Fatal("所选完整路径未应用到规则")
	}
	linkageClick(t, tt, "运行应用列表")
	linkageClick(t, tt, "music.exe")
	linkageClick(t, tt, "使用所选应用")
	if a.options.AppRules[0].Executable != "music.exe" {
		t.Fatal("无完整路径的进程名未应用到规则")
	}
	linkageReplace(t, tt, "应用路径或进程名", "editor.exe")
	linkageClick(t, tt, "启用应用监控")
	if !a.linkEnabled || h.captures != 2 || !slices.Equal(h.sent, []string{"欢迎回来"}) || a.linkLastText != "欢迎回来" {
		t.Fatalf("监控未通过模拟发送确认文字：enabled=%v captures=%d sent=%v", a.linkEnabled, h.captures, h.sent)
	}
	linkageClick(t, tt, "添加应用规则")
	linkageClick(t, tt, "删除当前规则")
	if len(a.options.AppRules) != 1 {
		t.Fatal("监控中仍可增删规则")
	}
	linkageClick(t, tt, "停止应用监控")
	if a.linkEnabled || a.busy || a.linkLastText != "欢迎回来" || len(h.sent) != 1 {
		t.Fatal("停止监控未保留最后文字或触发额外写入")
	}
}

func TestLinkageUIDisableSaveAndDeleteEmptyRules(t *testing.T) {
	h := newLinkageUI(t, 1080, 720)
	a, tt := h.app, h.tester
	linkageClick(t, tt, "启用应用监控")
	if a.linkEnabled || h.captures != 0 || len(h.sent) != 0 {
		t.Fatal("空规则列表不应启用或触发捕获发送")
	}
	linkageClick(t, tt, "添加应用规则")
	linkageReplace(t, tt, "应用路径或进程名", "editor.exe")
	linkageClick(t, tt, "规则名称")
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("开启此规则") {
		t.Fatal("规则名称后无法通过 Tab 聚焦启用开关")
	}
	tt.Key(0, ui.KeySpace)
	if a.options.AppRules[0].Enabled {
		t.Fatal("停用开关未修改规则")
	}
	linkageClick(t, tt, "保存规则")
	saved := linkageSavedSettings(t, a)
	if a.failed || len(saved.AppRules) != 1 || saved.AppRules[0].Enabled {
		t.Fatal("停用状态未保存")
	}
	linkageClick(t, tt, "启用应用监控")
	if a.linkEnabled || h.captures != 0 || len(h.sent) != 0 {
		t.Fatal("全部停用的规则仍触发监控")
	}
	linkageClick(t, tt, "删除当前规则")
	if len(a.options.AppRules) != 0 || a.linkLines != "" {
		t.Fatal("删除最后规则未清空列表和草稿")
	}
	linkageClick(t, tt, "保存规则")
	if a.failed || len(linkageSavedSettings(t, a).AppRules) != 0 {
		t.Fatal("空规则列表未成功持久化")
	}
	linkageClick(t, tt, "删除当前规则")
	linkageClick(t, tt, "添加应用规则")
	if len(a.options.AppRules) != 1 || a.linkSelected != 0 || a.linkLines != "欢迎回来" {
		t.Fatal("空列表删除或重新添加规则后的状态错误")
	}
}
