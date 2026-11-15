//go:build windows

package appwatch

import (
	"os"
	"testing"
)

func TestCaptureReadOnly(t *testing.T) {
	if os.Getenv("OPENPB_TEST_APPWATCH") != "1" {
		t.Skip("设置 OPENPB_TEST_APPWATCH=1 才执行本机只读进程查询")
	}
	snapshot, err := Capture()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range snapshot.Processes {
		if p.PID == os.Getpid() {
			if p.Path == "" || !Matches(executable, p) {
				t.Fatal("当前测试进程的完整路径查询或匹配失败")
			}
			return
		}
	}
	t.Fatal("运行进程快照中缺少当前测试进程")
}
