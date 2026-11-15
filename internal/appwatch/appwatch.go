// Package appwatch 只读查询运行进程与前台应用，不启动应用或请求提权。
package appwatch

import (
	"errors"
	"path"
	"runtime"
	"strings"
	"time"
)

const captureTimeout = 3 * time.Second

type Process struct {
	PID  int
	Path string
	Name string
}

type Snapshot struct {
	Processes     []Process
	ForegroundPID int
}

// validate 拒绝缺失或跨查询时刻不一致的快照，避免把查询失败视为应用退出。
func (s Snapshot) validate() error {
	if len(s.Processes) == 0 {
		return errors.New("未能读取任何运行进程")
	}
	if s.ForegroundPID == 0 {
		return nil
	}
	for _, process := range s.Processes {
		if process.PID == s.ForegroundPID {
			return nil
		}
	}
	return errors.New("前台应用与运行进程快照不一致，可能在查询期间启动或切换；请手动重新启用监控")
}

// Matches 匹配当前平台的可执行文件路径、进程名或 macOS 应用包路径。
// Path 不可读取时仅允许进程名规则，完整路径规则不会降级匹配。
func Matches(executable string, p Process) bool {
	return matchesPlatform(executable, p, runtime.GOOS)
}

func matchesPlatform(executable string, p Process, platform string) bool {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return false
	}
	if platform == "windows" {
		normalize := func(s string) string {
			s = strings.ReplaceAll(s, `\`, "/")
			// QueryFullProcessImageName 通常不带扩展路径前缀；接受配置中的等价形式。
			if strings.HasPrefix(strings.ToLower(s), "//?/unc/") {
				s = "//" + s[8:]
			} else {
				s = strings.TrimPrefix(s, "//?/")
			}
			return path.Clean(s)
		}
		executable = normalize(executable)
		if strings.ContainsAny(executable, "/:") {
			return p.Path != "" && strings.EqualFold(executable, normalize(p.Path))
		}
		name := p.Name
		if p.Path != "" {
			name = path.Base(normalize(p.Path))
		}
		return name != "" && strings.EqualFold(executable, name)
	}
	// 其他平台仍可使用纯 POSIX 匹配逻辑进行离线规则测试；Capture 不提供支持。
	executable = path.Clean(executable)
	if platform == "darwin" && strings.HasSuffix(executable, ".app") {
		// 只匹配此 bundle 的主可执行目录，不误匹配内部嵌套的 Helper.app。
		if p.Path == "" || !strings.Contains(executable, "/") {
			return false
		}
		return path.Dir(path.Clean(p.Path)) == executable+"/Contents/MacOS"
	}
	if strings.Contains(executable, "/") {
		return p.Path != "" && executable == path.Clean(p.Path)
	}
	name := p.Name
	if p.Path != "" {
		name = path.Base(p.Path)
	}
	return name != "" && executable == name
}
