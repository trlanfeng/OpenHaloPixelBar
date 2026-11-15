//go:build windows

package appwatch

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                   = windows.NewLazySystemDLL("user32.dll")
	getForegroundWindow      = user32.NewProc("GetForegroundWindow")
	getWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
)

// Capture 应在后台且串行调用。系统查询不发送窗口消息，不访问设备；
// 每个进程查询前检查截止时间。系统调用本身由 Windows 同步执行。
func Capture() (Snapshot, error) {
	deadline := time.Now().Add(captureTimeout)
	if err := getForegroundWindow.Find(); err != nil {
		return Snapshot{}, fmt.Errorf("加载前台窗口查询: %w", err)
	}
	if err := getWindowThreadProcessID.Find(); err != nil {
		return Snapshot{}, fmt.Errorf("加载前台进程查询: %w", err)
	}
	handle, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return Snapshot{}, fmt.Errorf("查询运行进程: %w", err)
	}
	defer windows.CloseHandle(handle)

	var result Snapshot
	buffer := make([]uint16, 32768)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	err = windows.Process32First(handle, &entry)
	for err == nil {
		if time.Now().After(deadline) {
			return Snapshot{}, fmt.Errorf("查询运行进程超时: %w", context.DeadlineExceeded)
		}
		if entry.ProcessID != 0 {
			p := Process{PID: int(entry.ProcessID), Name: windows.UTF16ToString(entry.ExeFile[:])}
			// 受保护或已退出的进程仍保留快照中的名称，不伪造完整路径。
			if process, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID); openErr == nil {
				size := uint32(len(buffer))
				if queryErr := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); queryErr == nil {
					p.Path = windows.UTF16ToString(buffer[:size])
				}
				windows.CloseHandle(process)
			}
			result.Processes = append(result.Processes, p)
		}
		err = windows.Process32Next(handle, &entry)
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return Snapshot{}, fmt.Errorf("枚举运行进程: %w", err)
	}
	window, _, _ := getForegroundWindow.Call()
	if window != 0 {
		var pid uint32
		thread, _, callErr := getWindowThreadProcessID.Call(window, uintptr(unsafe.Pointer(&pid)))
		if thread == 0 {
			return Snapshot{}, fmt.Errorf("读取前台进程失败: %v", callErr)
		}
		result.ForegroundPID = int(pid)
	}
	if err := result.validate(); err != nil {
		return Snapshot{}, err
	}
	return result, nil
}
