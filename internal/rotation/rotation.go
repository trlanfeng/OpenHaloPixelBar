// Package rotation 根据应用快照选择规则，并在设备确认后推进文字轮播。
// Engine 只应在同一个调用线程中使用；它不执行进程查询或设备写入。
package rotation

import (
	"fmt"
	"strings"
	"time"

	"openpixelbar/internal/appwatch"
	"openpixelbar/internal/pixelbar"
)

// Rule 不限制文字列表的条目数，每条文字统一使用协议层校验。
type Rule struct {
	Name            string   `json:"name"`
	Executable      string   `json:"executable"`
	Texts           []string `json:"texts"`
	IntervalSeconds int      `json:"interval_seconds"`
	Enabled         bool     `json:"enabled"`
}

func ValidateRules(rules []Rule) error {
	for i, rule := range rules {
		prefix := fmt.Sprintf("规则 %d", i+1)
		if !validLine(rule.Name) {
			return fmt.Errorf("%s：名称不能为空或包含换行、NUL", prefix)
		}
		if !validLine(rule.Executable) {
			return fmt.Errorf("%s：可执行文件名不能为空或包含换行、NUL", prefix)
		}
		if rule.IntervalSeconds < 1 || rule.IntervalSeconds > 86400 {
			return fmt.Errorf("%s：间隔必须是 1 到 86400 秒之间的整数", prefix)
		}
		if len(rule.Texts) == 0 {
			return fmt.Errorf("%s：至少需要一条文字", prefix)
		}
		for j, text := range rule.Texts {
			if err := pixelbar.ValidateNativeText(text); err != nil {
				return fmt.Errorf("%s 的文字 %d：%w", prefix, j+1, err)
			}
		}
	}
	return nil
}

func validLine(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "\r\n\x00")
}

// Request 的 Token 仅供对应设备写入完成时传回 Complete，不可复用。
type Request struct {
	Token     uint64
	RuleIndex int
	TextIndex int
	Text      string
}

// Engine 的零值可用。外部必须负责共享设备的写入互斥。
type Engine struct {
	rules       []Rule
	history     []uint64
	observation uint64
	selected    int // 规则下标加一；零表示没有活动规则。
	nextIndex   int
	due         time.Time
	hasText     bool
	pending     *Request
	sequence    uint64
	err         error
}

func New() *Engine { return &Engine{} }

// Reset 停止轮播并清除历史、失败和进度，但不会清屏或复用旧请求的 Token。
func (e *Engine) Reset() {
	sequence := e.sequence
	*e = Engine{sequence: sequence}
}

func (e *Engine) Active() int { return e.selected - 1 }

func (e *Engine) Err() error { return e.err }

// Observe 更新活动规则。规则配置变化会清除选择历史并从首条重新开始。
// 前台不匹配时保留仍运行的当前规则；否则按前台历史、列表顺序回退。
func (e *Engine) Observe(rules []Rule, snapshot appwatch.Snapshot) {
	if !sameRules(e.rules, rules) {
		e.rules = cloneRules(rules)
		e.history = make([]uint64, len(rules))
		e.observation = 0
		e.selectRule(-1)
	}
	running := make([]bool, len(e.rules))
	foreground := -1
	e.observation++
	for i, rule := range e.rules {
		if !rule.Enabled || len(rule.Texts) == 0 || rule.IntervalSeconds < 1 || rule.IntervalSeconds > 86400 {
			continue
		}
		for _, process := range snapshot.Processes {
			if !appwatch.Matches(rule.Executable, process) {
				continue
			}
			running[i] = true
			if snapshot.ForegroundPID > 0 && process.PID == snapshot.ForegroundPID {
				e.history[i] = e.observation
				if foreground == -1 {
					foreground = i
				}
			}
		}
	}
	if foreground != -1 {
		e.selectRule(foreground)
		return
	}
	if active := e.Active(); active >= 0 && running[active] {
		return
	}
	fallback := -1
	for i, isRunning := range running {
		if isRunning && (fallback == -1 || e.history[i] > e.history[fallback]) {
			fallback = i
		}
	}
	e.selectRule(fallback)
}

func (e *Engine) selectRule(index int) {
	if e.selected == index+1 {
		return
	}
	e.selected = index + 1
	e.nextIndex = 0
	e.due = time.Time{}
	e.hasText = false
	e.pending = nil
}

// Next 最多推进一条，延迟调用不会追赶积压。返回 true 后必须调用 Complete。
// 单条列表和相邻重复文字也逐条写入，均在对应确认后计算下一次间隔。
func (e *Engine) Next(now time.Time) (Request, bool) {
	if e.selected == 0 || e.err != nil || e.pending != nil {
		return Request{}, false
	}
	rule := e.rules[e.Active()]
	if e.hasText && now.Before(e.due) {
		return Request{}, false
	}
	text := rule.Texts[e.nextIndex]
	e.sequence++
	request := Request{Token: e.sequence, RuleIndex: e.Active(), TextIndex: e.nextIndex, Text: text}
	e.pending = &request
	return request, true
}

// Complete 接受当前请求的设备确认；过期、重复确认返回 false，且不改变状态。
// 失败会冻结轮播直到 Reset；成功确认的时刻才开始计算下一条的间隔。
func (e *Engine) Complete(token uint64, now time.Time, err error) bool {
	if e.pending == nil || e.pending.Token != token {
		return false
	}
	request := *e.pending
	e.pending = nil
	if err != nil {
		e.err = err
		return true
	}
	rule := e.rules[e.Active()]
	e.hasText = true
	e.nextIndex = (request.TextIndex + 1) % len(rule.Texts)
	e.due = now.Add(time.Duration(rule.IntervalSeconds) * time.Second)
	return true
}

func sameRules(a, b []Rule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Executable != b[i].Executable ||
			a[i].IntervalSeconds != b[i].IntervalSeconds || a[i].Enabled != b[i].Enabled ||
			len(a[i].Texts) != len(b[i].Texts) {
			return false
		}
		for j := range a[i].Texts {
			if a[i].Texts[j] != b[i].Texts[j] {
				return false
			}
		}
	}
	return true
}

func cloneRules(rules []Rule) []Rule {
	result := make([]Rule, len(rules))
	copy(result, rules)
	for i := range result {
		result[i].Texts = append([]string(nil), rules[i].Texts...)
	}
	return result
}
