package appwatch

import (
	"reflect"
	"testing"
)

func TestMatchesPlatform(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		executable string
		process    Process
		want       bool
	}{
		{"空规则", "windows", "  ", Process{Name: "app.exe"}, false},
		{"Windows完整路径忽略大小写", "windows", `C:\Apps\Editor.EXE`, Process{Path: `c:\apps\editor.exe`}, true},
		{"Windows分隔符", "windows", `C:/Apps/Editor.exe`, Process{Path: `C:\Apps\Editor.exe`}, true},
		{"Windows扩展路径", "windows", `\\?\C:\Apps\Editor.exe`, Process{Path: `C:\Apps\Editor.exe`}, true},
		{"Windows扩展UNC路径", "windows", `\\?\UNC\server\share\Editor.exe`, Process{Path: `\\server\share\Editor.exe`}, true},
		{"Windows名称匹配", "windows", "Editor.EXE", Process{Path: `C:\Apps\editor.exe`}, true},
		{"Windows无路径名称匹配", "windows", "Editor.exe", Process{Name: "EDITOR.EXE"}, true},
		{"Windows拒绝访问不能降级", "windows", `C:\Apps\Editor.exe`, Process{Name: "Editor.exe"}, false},
		{"Windows路径不匹配同名", "windows", `C:\Apps\Editor.exe`, Process{Path: `D:\Apps\Editor.exe`, Name: "Editor.exe"}, false},
		{"Windows路径优先于旧名称", "windows", "Editor.exe", Process{Path: `C:\Apps\Other.exe`, Name: "Editor.exe"}, false},
		{"Windows不做子串匹配", "windows", "Editor.exe", Process{Name: "MyEditor.exe"}, false},
		{"Windows不扩展通配符", "windows", "*.exe", Process{Name: "Editor.exe"}, false},
		{"Mac应用包", "darwin", "/Applications/My Editor.app", Process{Path: "/Applications/My Editor.app/Contents/MacOS/My Editor"}, true},
		{"Mac应用包末尾斜杠", "darwin", "/Applications/Editor.app/", Process{Path: "/Applications/Editor.app/Contents/MacOS/Editor"}, true},
		{"Mac精确可执行路径", "darwin", "/usr/bin/editor", Process{Path: "/usr/bin/editor"}, true},
		{"Mac精确进程名", "darwin", "Editor", Process{Path: "/Applications/Editor.app/Contents/MacOS/Editor"}, true},
		{"Mac无路径进程名", "darwin", "Editor", Process{Name: "Editor"}, true},
		{"Mac拒绝访问不匹配包", "darwin", "/Applications/Editor.app", Process{Name: "Editor"}, false},
		{"Mac不匹配另一个目录同名包", "darwin", "/Applications/Editor.app", Process{Path: "/Users/test/Editor.app/Contents/MacOS/Editor"}, false},
		{"Mac不匹配包前缀", "darwin", "/Applications/Editor.app", Process{Path: "/Applications/Editor.app2/Contents/MacOS/Editor"}, false},
		{"Mac不匹配嵌套助手", "darwin", "/Applications/Editor.app", Process{Path: "/Applications/Editor.app/Contents/Frameworks/Helper.app/Contents/MacOS/Helper"}, false},
		{"Mac名称区分大小写", "darwin", "EDITOR", Process{Name: "Editor"}, false},
		{"其他平台进程名匹配", "linux", "editor", Process{Name: "editor"}, true},
		{"其他平台精确路径匹配", "linux", "/usr/bin/editor", Process{Path: "/usr/bin/editor"}, true},
		{"其他平台区分大小写", "linux", "EDITOR", Process{Name: "editor"}, false},
		{"其他平台完整路径不降级", "linux", "/usr/bin/editor", Process{Name: "editor"}, false},
		{"其他平台不展开Mac应用包", "linux", "/Applications/Editor.app", Process{Path: "/Applications/Editor.app/Contents/MacOS/Editor"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesPlatform(tt.executable, tt.process, tt.platform); got != tt.want {
				t.Fatalf("matchesPlatform() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParsePS(t *testing.T) {
	got, err := parsePS("   0 kernel_task\n   1 /sbin/launchd\n  42 /Applications/My Editor.app/Contents/MacOS/My Editor\n\t73\tworker\n  74 worker\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []Process{
		{PID: 1, Path: "/sbin/launchd", Name: "launchd"},
		{PID: 42, Path: "/Applications/My Editor.app/Contents/MacOS/My Editor", Name: "My Editor"},
		{PID: 73, Name: "worker"},
		{PID: 74, Name: "worker"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePS() = %#v, want %#v", got, want)
	}
}

func TestSnapshotValidation(t *testing.T) {
	processes := []Process{{PID: 10, Name: "editor"}, {PID: 20, Name: "editor"}}
	for _, tc := range []struct {
		name     string
		snapshot Snapshot
		valid    bool
	}{
		{"空快照不得当成无应用", Snapshot{}, false},
		{"前台存在但列表为空", Snapshot{ForegroundPID: 10}, false},
		{"允许没有前台窗口", Snapshot{Processes: processes}, true},
		{"识别同名应用第一实例", Snapshot{Processes: processes, ForegroundPID: 10}, true},
		{"识别同名应用第二实例", Snapshot{Processes: processes, ForegroundPID: 20}, true},
		{"前台跨查询启动必须报错", Snapshot{Processes: processes, ForegroundPID: 30}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.snapshot.validate(); (err == nil) != tc.valid {
				t.Fatalf("快照校验结果不符：%v", err)
			}
		})
	}
}

func TestParsePSRejectsInvalidOutput(t *testing.T) {
	for _, input := range []string{"", "\n  ", "1", "invalid /bin/app", "0 /bin/app", "-1 /bin/app", "1  "} {
		if _, err := parsePS(input); err == nil {
			t.Errorf("parsePS(%q) 未拒绝无效输出", input)
		}
	}
}
