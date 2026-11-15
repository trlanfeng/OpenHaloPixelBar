package appwatch

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// parsePS 保留可执行路径内部的空格，不读取可能含敏感数据的进程参数。
func parsePS(output string) ([]Process, error) {
	var processes []Process
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		separator := strings.IndexAny(line, " \t")
		if separator < 0 {
			return nil, fmt.Errorf("运行进程列表格式无效")
		}
		pid, err := strconv.Atoi(line[:separator])
		if err != nil || pid < 0 {
			return nil, fmt.Errorf("运行进程列表包含无效 PID")
		}
		command := strings.TrimSpace(line[separator:])
		if command == "" {
			return nil, fmt.Errorf("运行进程列表缺少程序名称")
		}
		// PID 0 是系统内核进程，不作为可联动应用，也不使整个列表失效。
		if pid == 0 {
			continue
		}
		p := Process{PID: pid, Name: path.Base(command)}
		// ps 的退出进程或不可读路径可能仅显示名称；不可当成绝对路径。
		if path.IsAbs(command) {
			p.Path = command
		}
		processes = append(processes, p)
	}
	if len(processes) == 0 {
		return nil, fmt.Errorf("未能读取任何运行进程")
	}
	return processes, nil
}
