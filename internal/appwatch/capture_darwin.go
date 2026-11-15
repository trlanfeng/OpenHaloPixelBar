//go:build darwin

package appwatch

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// NSWorkspace 是本机只读查询，不通过 System Events 发送 Apple Events，
// 因此不要求自动化或辅助功能授权，也不启动任何被监控应用。
const foregroundScript = `ObjC.import('AppKit');
var app = $.NSWorkspace.sharedWorkspace.frontmostApplication;
app.isNil() ? 0 : Number(app.processIdentifier);`

// Capture 应在后台且串行调用；两个系统命令共用超时，不在失败时重试授权。
// macOS 实现仅完成交叉编译；AppKit/JXA 与 ps 行为仍需实机验证。
func Capture() (Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), captureTimeout)
	defer cancel()

	output, err := runCommand(ctx, "/bin/ps", "-A", "-ww", "-o", "pid=,comm=")
	if err != nil {
		return Snapshot{}, fmt.Errorf("读取运行进程失败: %w", err)
	}
	processes, err := parsePS(string(output))
	if err != nil {
		return Snapshot{}, err
	}
	output, err = runCommand(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", foregroundScript)
	if err != nil {
		return Snapshot{}, fmt.Errorf("读取前台应用失败，请检查系统是否允许本机脚本查询: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || pid < 0 {
		return Snapshot{}, fmt.Errorf("前台应用查询返回了无效 PID")
	}
	result := Snapshot{Processes: processes, ForegroundPID: pid}
	if err := result.validate(); err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

func runCommand(ctx context.Context, executable string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, args...)
	command.WaitDelay = 200 * time.Millisecond
	output, err := command.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		// 不透出 ps 的进程内容或本机路径到错误日志。
		return nil, err
	}
	return output, nil
}
