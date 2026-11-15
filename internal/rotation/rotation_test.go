package rotation

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"openpixelbar/internal/appwatch"
)

var start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func testRule(name string, interval int, texts ...string) Rule {
	return Rule{Name: name, Executable: name + ".exe", Texts: texts, IntervalSeconds: interval, Enabled: true}
}

func snapshot(foreground int, names ...string) appwatch.Snapshot {
	result := appwatch.Snapshot{ForegroundPID: foreground}
	for i, name := range names {
		if name != "" {
			result.Processes = append(result.Processes, appwatch.Process{PID: i + 1, Name: name + ".exe"})
		}
	}
	return result
}

func requireNext(t *testing.T, engine *Engine, now time.Time, ruleIndex, textIndex int, text string) Request {
	t.Helper()
	request, ok := engine.Next(now)
	if !ok || request.RuleIndex != ruleIndex || request.TextIndex != textIndex || request.Text != text {
		t.Fatalf("Next = %+v, %v；预期规则 %d 文字 %d %q", request, ok, ruleIndex, textIndex, text)
	}
	return request
}

func requireIdle(t *testing.T, engine *Engine, now time.Time) {
	t.Helper()
	if request, ok := engine.Next(now); ok {
		t.Fatalf("不应发送，实际 %+v", request)
	}
}

func requireComplete(t *testing.T, engine *Engine, request Request, now time.Time) {
	t.Helper()
	if !engine.Complete(request.Token, now, nil) {
		t.Fatal("当前请求确认被拒绝")
	}
}

func TestSelectionForegroundHistoryAndFallback(t *testing.T) {
	rules := []Rule{testRule("a", 2, "a"), testRule("b", 3, "b"), testRule("c", 4, "c")}
	engine := New()
	steps := []struct {
		name string
		snap appwatch.Snapshot
		want int
	}{
		{"无历史按列表顺序", snapshot(0, "a", "b", "c"), 0},
		{"前台优先", snapshot(2, "a", "b", "c"), 1},
		{"不匹配的前台继续当前", snapshot(4, "a", "b", "c", "other"), 1},
		{"另一应用获得前台", snapshot(3, "a", "b", "c"), 2},
		{"前台切回首规则", snapshot(1, "a", "b", "c"), 0},
		{"当前退出回退最近前台", snapshot(0, "", "b", "c"), 2},
		{"再次退出回退历史", snapshot(0, "", "b"), 1},
		{"全部退出停止", snapshot(0), -1},
		{"重新运行立即选择", snapshot(0, "a"), 0},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			engine.Observe(rules, step.snap)
			if engine.Active() != step.want {
				t.Fatalf("Active = %d，预期 %d", engine.Active(), step.want)
			}
		})
	}
}

func TestStartsWhenAlreadyRunningAndWaitsForConfirmation(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 2, "第一条", "第二条")}
	engine.Observe(rules, snapshot(0, "a"))
	request := requireNext(t, engine, start, 0, 0, "第一条")
	requireIdle(t, engine, start.Add(time.Hour))
	confirmed := start.Add(2 * time.Hour)
	requireComplete(t, engine, request, confirmed)
	requireIdle(t, engine, confirmed.Add(2*time.Second-time.Nanosecond))
	second := requireNext(t, engine, confirmed.Add(2*time.Second), 0, 1, "第二条")
	requireComplete(t, engine, second, confirmed.Add(2*time.Second))
	requireNext(t, engine, confirmed.Add(4*time.Second), 0, 0, "第一条")
}

func TestLosingFocusDoesNotResetOrInvalidatePending(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 2, "一", "二")}
	engine.Observe(rules, snapshot(1, "a", "other"))
	first := requireNext(t, engine, start, 0, 0, "一")
	engine.Observe(rules, snapshot(2, "a", "other"))
	requireComplete(t, engine, first, start)
	engine.Observe(rules, snapshot(0, "a", "other"))
	requireIdle(t, engine, start.Add(time.Second))
	second := requireNext(t, engine, start.Add(2*time.Second), 0, 1, "二")
	engine.Observe(rules, snapshot(1, "a", "other"))
	requireComplete(t, engine, second, start.Add(2*time.Second))
	requireNext(t, engine, start.Add(4*time.Second), 0, 0, "一")
}

func TestSwitchRestartsFirstTextAndUsesOwnInterval(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 2, "a1", "a2"), testRule("b", 5, "b1", "b2")}
	engine.Observe(rules, snapshot(1, "a", "b"))
	first := requireNext(t, engine, start, 0, 0, "a1")
	requireComplete(t, engine, first, start)
	second := requireNext(t, engine, start.Add(2*time.Second), 0, 1, "a2")
	requireComplete(t, engine, second, start.Add(2*time.Second))
	engine.Observe(rules, snapshot(2, "a", "b"))
	b := requireNext(t, engine, start.Add(3*time.Second), 1, 0, "b1")
	requireComplete(t, engine, b, start.Add(3*time.Second))
	requireIdle(t, engine, start.Add(7*time.Second))
	requireNext(t, engine, start.Add(8*time.Second), 1, 1, "b2")
	engine.Observe(rules, snapshot(1, "a", "b"))
	requireNext(t, engine, start.Add(8*time.Second), 0, 0, "a1")
}

func TestStaleCompletionCannotAdvanceNewRule(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "a1", "a2"), testRule("b", 1, "b1", "b2")}
	engine.Observe(rules, snapshot(1, "a", "b"))
	old := requireNext(t, engine, start, 0, 0, "a1")
	engine.Observe(rules, snapshot(2, "a", "b"))
	current := requireNext(t, engine, start, 1, 0, "b1")
	if old.Token == current.Token {
		t.Fatal("切换规则后复用了 Token")
	}
	if engine.Complete(old.Token, start.Add(time.Hour), errors.New("过期错误")) || engine.Err() != nil {
		t.Fatal("过期回调污染了当前规则")
	}
	requireIdle(t, engine, start.Add(time.Hour))
	requireComplete(t, engine, current, start)
	if engine.Complete(current.Token, start.Add(time.Hour), nil) {
		t.Fatal("重复回调被接受")
	}
	requireNext(t, engine, start.Add(time.Second), 1, 1, "b2")
}

func TestResetInvalidatesCallbacksAndRestarts(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "一", "二")}
	engine.Observe(rules, snapshot(1, "a"))
	old := requireNext(t, engine, start, 0, 0, "一")
	engine.Reset()
	if engine.Active() != -1 {
		t.Fatal("Reset 后仍有活动规则")
	}
	requireIdle(t, engine, start)
	engine.Observe(rules, snapshot(1, "a"))
	current := requireNext(t, engine, start, 0, 0, "一")
	if current.Token == old.Token || engine.Complete(old.Token, start, nil) {
		t.Fatal("Reset 后旧回调仍有效")
	}
	requireComplete(t, engine, current, start)
}

func TestDisablingAndProcessExitInvalidatePending(t *testing.T) {
	for _, disable := range []bool{false, true} {
		t.Run(fmt.Sprintf("停用=%v", disable), func(t *testing.T) {
			engine := New()
			rules := []Rule{testRule("a", 1, "一", "二")}
			engine.Observe(rules, snapshot(1, "a"))
			old := requireNext(t, engine, start, 0, 0, "一")
			if disable {
				rules[0].Enabled = false
				engine.Observe(rules, snapshot(1, "a"))
			} else {
				engine.Observe(rules, snapshot(0))
			}
			if engine.Active() != -1 || engine.Complete(old.Token, start, nil) {
				t.Fatal("停止后旧回调仍有效")
			}
			requireIdle(t, engine, start.Add(time.Hour))
			rules[0].Enabled = true
			engine.Observe(rules, snapshot(1, "a"))
			requireNext(t, engine, start, 0, 0, "一")
		})
	}
}

func TestFailureFreezesUntilReset(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "a"), testRule("b", 1, "b")}
	engine.Observe(rules, snapshot(1, "a", "b"))
	request := requireNext(t, engine, start, 0, 0, "a")
	failure := errors.New("写入失败")
	if !engine.Complete(request.Token, start, failure) || !errors.Is(engine.Err(), failure) {
		t.Fatal("未保留设备错误")
	}
	requireIdle(t, engine, start.Add(time.Hour))
	engine.Observe(rules, snapshot(2, "a", "b"))
	requireIdle(t, engine, start.Add(time.Hour))
	rules[1].Texts[0] = "修改后"
	engine.Observe(rules, snapshot(2, "a", "b"))
	requireIdle(t, engine, start.Add(time.Hour))
	engine.Reset()
	if engine.Err() != nil {
		t.Fatal("Reset 未清除错误")
	}
	engine.Observe(rules, snapshot(2, "a", "b"))
	requireNext(t, engine, start, 1, 0, "修改后")
}

func TestSlowTicksDoNotCatchUp(t *testing.T) {
	engine := New()
	engine.Observe([]Rule{testRule("a", 1, "一", "二", "三")}, snapshot(1, "a"))
	first := requireNext(t, engine, start, 0, 0, "一")
	requireComplete(t, engine, first, start)
	late := start.Add(24 * time.Hour)
	second := requireNext(t, engine, late, 0, 1, "二")
	requireComplete(t, engine, second, late)
	requireIdle(t, engine, late)
	requireIdle(t, engine, late.Add(time.Second-time.Nanosecond))
	requireNext(t, engine, late.Add(time.Second), 0, 2, "三")
}

func TestSingleTextLoopsAfterEachConfirmation(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "固定文字")}
	engine.Observe(rules, snapshot(1, "a"))
	var previousToken uint64
	confirmed := start.Add(-time.Second)
	for i := 0; i < 10; i++ {
		engine.Observe(rules, snapshot(0, "a"))
		request := requireNext(t, engine, confirmed.Add(time.Second), 0, 0, "固定文字")
		if request.Token == previousToken {
			t.Fatal("单条循环复用了旧请求 Token")
		}
		previousToken = request.Token
		requireIdle(t, engine, confirmed.Add(time.Hour))
		confirmed = confirmed.Add(2 * time.Hour)
		requireComplete(t, engine, request, confirmed)
		requireIdle(t, engine, confirmed.Add(time.Second-time.Nanosecond))
	}
	engine.Observe(rules, snapshot(0))
	requireIdle(t, engine, confirmed.Add(time.Hour))
	engine.Observe(rules, snapshot(0, "a"))
	requireNext(t, engine, confirmed, 0, 0, "固定文字")
}

func TestConsecutiveDuplicatesAreWrittenAndConfirmedIndividually(t *testing.T) {
	engine := New()
	texts := []string{"同", "同", "异", "同"}
	engine.Observe([]Rule{testRule("a", 2, texts...)}, snapshot(1, "a"))
	first := requireNext(t, engine, start, 0, 0, texts[0])
	requireComplete(t, engine, first, start)
	confirmed := start
	for i := 1; i <= 2*len(texts); i++ {
		requireIdle(t, engine, confirmed.Add(2*time.Second-time.Nanosecond))
		index := i % len(texts)
		request := requireNext(t, engine, confirmed.Add(2*time.Second), 0, index, texts[index])
		requireIdle(t, engine, confirmed.Add(time.Hour))
		confirmed = confirmed.Add(2 * time.Hour)
		requireComplete(t, engine, request, confirmed)
	}
}

func TestMultipleInstancesDoNotRestartCurrentRule(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "一", "二")}
	engine.Observe(rules, snapshot(1, "a", "a"))
	first := requireNext(t, engine, start, 0, 0, "一")
	requireComplete(t, engine, first, start)
	engine.Observe(rules, snapshot(2, "", "a"))
	requireNext(t, engine, start.Add(time.Second), 0, 1, "二")
}

func TestSamePIDMatchesMultipleRulesInListOrder(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "一"), testRule("a", 1, "二"), testRule("b", 1, "三")}
	engine.Observe(rules, snapshot(1, "a", "b"))
	if engine.Active() != 0 {
		t.Fatal("同 PID 匹配多规则未选择列表首项")
	}
	engine.Observe(rules, snapshot(2, "a", "b"))
	engine.Observe(rules, snapshot(0, "a"))
	if engine.Active() != 0 {
		t.Fatal("相同前台历史回退未选择列表首项")
	}
}

func TestConfigurationIsCopiedAndEditsRestart(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "原文", "第二条")}
	engine.Observe(rules, snapshot(1, "a"))
	rules[0].Texts[0] = "新文"
	old := requireNext(t, engine, start, 0, 0, "原文")
	engine.Observe(rules, snapshot(1, "a"))
	current := requireNext(t, engine, start, 0, 0, "新文")
	if engine.Complete(old.Token, start, nil) {
		t.Fatal("编辑后旧回调仍有效")
	}
	requireComplete(t, engine, current, start)
}

func TestEmptyAndInvalidRuntimeRulesDoNotPanic(t *testing.T) {
	var engine Engine
	engine.Observe(nil, snapshot(0))
	if engine.Active() != -1 {
		t.Fatal("零值不应有活动规则")
	}
	requireIdle(t, &engine, start)
	invalid := []Rule{testRule("a", 0, "文字"), testRule("a", 86401, "文字"), testRule("a", 1)}
	engine.Observe(invalid, snapshot(1, "a"))
	requireIdle(t, &engine, start)
}

func TestValidateRules(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Rule)
	}{
		{"空名称", func(r *Rule) { r.Name = " " }},
		{"名称换行", func(r *Rule) { r.Name = "名\n称" }},
		{"空程序", func(r *Rule) { r.Executable = "" }},
		{"程序NUL", func(r *Rule) { r.Executable = "a\x00.exe" }},
		{"间隔零", func(r *Rule) { r.IntervalSeconds = 0 }},
		{"间隔负数", func(r *Rule) { r.IntervalSeconds = -1 }},
		{"间隔超限", func(r *Rule) { r.IntervalSeconds = 86401 }},
		{"无文字", func(r *Rule) { r.Texts = nil }},
		{"空文字", func(r *Rule) { r.Texts = []string{""} }},
		{"纯空白文字", func(r *Rule) { r.Texts = []string{" \t "} }},
		{"LF", func(r *Rule) { r.Texts = []string{"前\n后"} }},
		{"CR", func(r *Rule) { r.Texts = []string{"前\r后"} }},
		{"NUL", func(r *Rule) { r.Texts = []string{"前\x00后"} }},
		{"制表符", func(r *Rule) { r.Texts = []string{"前\t后"} }},
		{"控制字符", func(r *Rule) { r.Texts = []string{"前\x1b后"} }},
		{"Unicode行分隔符", func(r *Rule) { r.Texts = []string{"前\u2028后"} }},
		{"Unicode段分隔符", func(r *Rule) { r.Texts = []string{"前\u2029后"} }},
		{"无效UTF8", func(r *Rule) { r.Texts = []string{string([]byte{0xff})} }},
		{"字符数超限", func(r *Rule) { r.Texts = []string{strings.Repeat("a", 17)} }},
		{"UTF8字节超限", func(r *Rule) { r.Texts = []string{strings.Repeat("𠀀", 13)} }},
		{"后续条目无效", func(r *Rule) { r.Texts = []string{"有效", "\t"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := testRule("a", 1, "有效")
			tc.edit(&rule)
			if ValidateRules([]Rule{rule}) == nil {
				t.Fatal("无效规则被接受")
			}
		})
	}
	for _, interval := range []int{1, 86400} {
		rule := testRule("a", interval, " 允许两端空格 ", strings.Repeat("长", 16), strings.Repeat("a", 16), strings.Repeat("𠀀", 12))
		if err := ValidateRules([]Rule{rule}); err != nil {
			t.Fatalf("协议允许的边界文字应可保存：%v", err)
		}
	}
	if err := ValidateRules(nil); err != nil {
		t.Fatalf("空规则列表应可保存：%v", err)
	}
}

func TestResetClearsForegroundHistory(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "a"), testRule("b", 1, "b")}
	engine.Observe(rules, snapshot(2, "a", "b"))
	engine.Reset()
	engine.Observe(rules, snapshot(0, "a", "b"))
	if engine.Active() != 0 {
		t.Fatal("Reset 后仍保留旧前台历史")
	}
}

func TestDuplicateDoesNotCatchUpAfterLateTick(t *testing.T) {
	engine := New()
	engine.Observe([]Rule{testRule("a", 1, "同", "同", "后")}, snapshot(1, "a"))
	request := requireNext(t, engine, start, 0, 0, "同")
	requireComplete(t, engine, request, start)
	late := start.Add(time.Hour)
	duplicate := requireNext(t, engine, late, 0, 1, "同")
	requireIdle(t, engine, late.Add(time.Hour))
	confirmed := late.Add(2 * time.Hour)
	requireComplete(t, engine, duplicate, confirmed)
	requireIdle(t, engine, confirmed.Add(time.Second-time.Nanosecond))
	requireNext(t, engine, confirmed.Add(time.Second), 0, 2, "后")
}

func TestDisabledRuleCannotTakeForeground(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "a"), testRule("b", 1, "b")}
	rules[0].Enabled = false
	engine.Observe(rules, snapshot(1, "a", "b"))
	if engine.Active() != 1 {
		t.Fatal("停用规则不应响应前台")
	}
	requireNext(t, engine, start, 1, 0, "b")
}

func TestLastInstanceExitFallsBackWithoutRestartingRemainingInstance(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 2, "a1", "a2"), testRule("b", 3, "b1", "b2")}
	engine.Observe(rules, snapshot(3, "a", "a", "b"))
	engine.Observe(rules, snapshot(1, "a", "a", "b"))
	first := requireNext(t, engine, start, 0, 0, "a1")
	requireComplete(t, engine, first, start)
	engine.Observe(rules, snapshot(4, "", "a", "b", "other"))
	requireIdle(t, engine, start.Add(time.Second))
	second := requireNext(t, engine, start.Add(2*time.Second), 0, 1, "a2")
	requireComplete(t, engine, second, start.Add(2*time.Second))
	engine.Observe(rules, snapshot(4, "", "", "b", "other"))
	fallback := requireNext(t, engine, start.Add(2*time.Second), 1, 0, "b1")
	requireComplete(t, engine, fallback, start.Add(2*time.Second))
	engine.Observe(rules, snapshot(4, "", "", "", "other"))
	if engine.Active() != -1 {
		t.Fatal("最后实例退出后仍有活动规则")
	}
	requireIdle(t, engine, start.Add(time.Hour))
}

func TestHistoricalRuleDoesNotPreemptStillRunningCurrentRule(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "a"), testRule("b", 1, "b")}
	engine.Observe(rules, snapshot(2, "a", "b"))
	engine.Observe(rules, snapshot(0, "a"))
	first := requireNext(t, engine, start, 0, 0, "a")
	requireComplete(t, engine, first, start)
	engine.Observe(rules, snapshot(3, "a", "b", "other"))
	if engine.Active() != 0 {
		t.Fatal("有前台历史的后台应用重启不应抢占仍运行的当前规则")
	}
	requireIdle(t, engine, start.Add(time.Second-time.Nanosecond))
	requireNext(t, engine, start.Add(time.Second), 0, 0, "a")
}

func TestReturningToSameRuleDoesNotAcceptOldToken(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "a1", "a2"), testRule("b", 1, "b1", "b2")}
	engine.Observe(rules, snapshot(1, "a", "b"))
	old := requireNext(t, engine, start, 0, 0, "a1")
	engine.Observe(rules, snapshot(2, "a", "b"))
	engine.Observe(rules, snapshot(1, "a", "b"))
	current := requireNext(t, engine, start, 0, 0, "a1")
	if old.Token == current.Token || engine.Complete(old.Token, start, nil) || engine.Complete(old.Token, start, errors.New("旧错误")) {
		t.Fatal("切出再切回相同规则后旧请求仍有效")
	}
	if engine.Err() != nil {
		t.Fatal("旧错误冻结了新请求")
	}
	requireIdle(t, engine, start.Add(time.Hour))
	requireComplete(t, engine, current, start.Add(time.Hour))
	requireIdle(t, engine, start.Add(time.Hour+time.Second-time.Nanosecond))
	requireNext(t, engine, start.Add(time.Hour+time.Second), 0, 1, "a2")
}

func TestConfigurationReplacementClearsHistoryAndInvalidatesPending(t *testing.T) {
	engine := New()
	rules := []Rule{testRule("a", 1, "a"), testRule("b", 1, "b")}
	engine.Observe(rules, snapshot(2, "a", "b"))
	old := requireNext(t, engine, start, 1, 0, "b")
	rules = append(rules, testRule("c", 1, "c"))
	engine.Observe(rules, snapshot(0, "a", "b", "c"))
	current := requireNext(t, engine, start, 0, 0, "a")
	if engine.Complete(old.Token, start, nil) {
		t.Fatal("配置替换后旧请求仍有效")
	}
	requireComplete(t, engine, current, start)
}

func TestValidationErrorIdentifiesRuleAndText(t *testing.T) {
	rules := []Rule{testRule("a", 1, "有效"), testRule("b", 1, "有效", "前\t后")}
	err := ValidateRules(rules)
	if err == nil || !strings.Contains(err.Error(), "规则 2 的文字 2") {
		t.Fatalf("校验错误缺少规则与文字位置：%v", err)
	}
}

func TestLargeListsAreNotTruncated(t *testing.T) {
	texts := make([]string, 10001)
	for i := range texts {
		texts[i] = fmt.Sprintf("文字%d", i)
	}
	rules := make([]Rule, 2001)
	for i := range rules {
		rules[i] = testRule(fmt.Sprintf("app%d", i), 1, "占位")
	}
	rules[len(rules)-1].Texts = texts
	if err := ValidateRules(rules); err != nil {
		t.Fatal(err)
	}
	engine := New()
	engine.Observe(rules, appwatch.Snapshot{Processes: []appwatch.Process{{PID: 9, Name: "app2000.exe"}}, ForegroundPID: 9})
	for i := range texts {
		now := start.Add(time.Duration(i) * time.Second)
		request := requireNext(t, engine, now, len(rules)-1, i, texts[i])
		requireComplete(t, engine, request, now)
	}
	requireNext(t, engine, start.Add(time.Duration(len(texts))*time.Second), len(rules)-1, 0, texts[0])
}
