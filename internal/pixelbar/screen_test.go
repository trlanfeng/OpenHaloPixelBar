package pixelbar

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCaptureDisplayReadOnly(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			state := nativeTextBefore()
			text := " 原来的文字 "
			if native {
				state = nativeTextAfter()
			}
			f := &fakeTransport{replies: [][]byte{response(0xee, state)}}
			if native {
				f.replies = append(f.replies, nativeTextReply(16, 50, text), response(0xee, state))
			}
			snapshot, err := (&Device{f}).CaptureDisplay()
			if err != nil || snapshot == nil || !snapshot.captured || !bytes.Equal(snapshot.state[:], state) {
				t.Fatalf("快照不完整：%+v %v", snapshot, err)
			}
			if native && snapshot.text != text {
				t.Fatal("旧文字必须逐字保留，不裁剪空格")
			}
			assertDisplayWrites(t, f, 0, 0)
			state[1]++
			if snapshot.state[1] == state[1] {
				t.Fatal("快照必须独立保存状态")
			}
		})
	}
}

func TestCaptureDisplayRejectsUnsafeState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fakeTransport)
	}{
		{"查询失败", func(f *fakeTransport) { f.replies = nil }},
		{"状态不完整", func(f *fakeTransport) { f.replies[0] = response(0xee, nativeTextAfter()[:9]) }},
		{"状态校验失败", func(f *fakeTransport) { f.replies[0][len(f.replies[0])-1]++ }},
		{"未知组合", func(f *fakeTransport) { p := nativeTextAfter(); p[4] = 1; f.replies[0] = response(0xee, p) }},
		{"未知模式", func(f *fakeTransport) { p := nativeTextAfter(); p[5] = 16; f.replies[0] = response(0xee, p) }},
		{"旧文字查询失败", func(f *fakeTransport) { f.replies = f.replies[:1] }},
		{"旧文字长度不完整", func(f *fakeTransport) { f.replies[1] = response(0xe7, []byte{0, 16, 50, 2, 'a'}) }},
		{"旧文字无效编码", func(f *fakeTransport) { f.replies[1] = nativeTextReply(16, 50, string([]byte{0xff})) }},
		{"旧文字为空", func(f *fakeTransport) { f.replies[1] = nativeTextReply(16, 50, "") }},
		{"旧文字空白", func(f *fakeTransport) { f.replies[1] = nativeTextReply(16, 50, "　 ") }},
		{"旧文字含控制符", func(f *fakeTransport) { f.replies[1] = nativeTextReply(16, 50, "旧\n文字") }},
		{"旧文字字符超限", func(f *fakeTransport) { f.replies[1] = nativeTextReply(16, 50, strings.Repeat("a", 17)) }},
		{"旧文字字节超限", func(f *fakeTransport) { f.replies[1] = nativeTextReply(16, 50, strings.Repeat("\U00020000", 13)) }},
		{"能力不支持", func(f *fakeTransport) { f.replies[1] = nativeTextReply(32, 50, "旧文字") }},
		{"分包能力不支持", func(f *fakeTransport) { f.replies[1] = nativeTextReply(16, 40, "旧文字") }},
		{"二次状态失败", func(f *fakeTransport) { f.replies = f.replies[:2] }},
		{"捕获中模式变化", func(f *fakeTransport) { f.replies[2] = response(0xee, nativeTextBefore()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeTransport{replies: [][]byte{response(0xee, nativeTextAfter()), nativeTextReply(16, 50, "旧文字"), response(0xee, nativeTextAfter())}}
			tc.mutate(f)
			snapshot, err := (&Device{f}).CaptureDisplay()
			if err == nil || snapshot != nil {
				t.Fatalf("不安全的快照不能允许接管：%+v %v", snapshot, err)
			}
			assertDisplayWrites(t, f, 0, 0)
		})
	}
}

func capturedDisplay(t *testing.T, state []byte, text string) *DisplaySnapshot {
	t.Helper()
	f := &fakeTransport{replies: [][]byte{response(0xee, state)}}
	if state[5] == 2 {
		f.replies = append(f.replies, nativeTextReply(16, 50, text), response(0xee, state))
	}
	snapshot, err := (&Device{f}).CaptureDisplay()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertDisplayWrites(t *testing.T, f *fakeTransport, nativeWrites, sceneWrites int) {
	t.Helper()
	var gotNative, gotScene int
	for _, p := range f.writes {
		switch p[3] {
		case 0xe8:
			gotNative++
		case 0xef:
			gotScene++
			if p[5] != 9 || p[6] != 1 {
				t.Fatalf("只能使用九字节 EF action 1：%x", p)
			}
		case 0xee, 0xe7:
		default:
			t.Fatalf("恢复不能上传图案或发送其他命令：%x", p)
		}
	}
	if gotNative != nativeWrites || gotScene != sceneWrites {
		t.Fatalf("写入次数不符，不能自动重试：E8=%d EF=%d，需要 %d/%d", gotNative, gotScene, nativeWrites, sceneWrites)
	}
}

func TestRestoreDisplayScene(t *testing.T) {
	for _, scene := range [][3]byte{{3, 6, 255}, {6, 52, 255}} {
		state := nativeTextBefore()
		copy(state[6:9], scene[:])
		snapshot := capturedDisplay(t, state, "")
		f := &fakeTransport{replies: [][]byte{response(0xee, nativeTextAfter()), response(0xef, nil), response(0xee, state)}}
		if err := (&Device{f}).RestoreDisplay(snapshot); err != nil {
			t.Fatal(err)
		}
		assertDisplayWrites(t, f, 0, 1)
		want, _ := Packet(0xef, append([]byte{1}, state[1:9]...))
		if len(f.writes) != 3 || !bytes.Equal(f.writes[1], want) {
			t.Fatal("必须保留原场景和颜色，仅选择旧槽位，不能重传图案")
		}
		if !bytes.Equal(snapshot.state[:], state) {
			t.Fatal("恢复不能修改原快照")
		}
	}
}

func nativeDisplayRestoreTransport(state []byte, text string) *fakeTransport {
	f := &fakeTransport{replies: [][]byte{response(0xee, nativeTextAfter())}}
	f.replies = append(f.replies, nativeTextTransport(text).replies...)
	f.replies = append(f.replies, response(0xef, nil), response(0xee, state), nativeTextReply(16, 50, text))
	return f
}

func TestRestoreDisplayNativeText(t *testing.T) {
	for _, effect := range []byte{0, 1} {
		state := nativeTextAfter()
		state[6] = effect
		snapshot := capturedDisplay(t, state, "之前的文字")
		f := nativeDisplayRestoreTransport(state, snapshot.text)
		if err := (&Device{f}).RestoreDisplay(snapshot); err != nil {
			t.Fatal(err)
		}
		assertDisplayWrites(t, f, 1, 1)
		wantText, _ := Packet(0xe8, append([]byte{0, byte(len(snapshot.text))}, []byte(snapshot.text)...))
		wantScene, _ := Packet(0xef, append([]byte{1}, state[1:9]...))
		if len(f.writes) != 9 || !bytes.Equal(f.writes[3], wantText) || !bytes.Equal(f.writes[6], wantScene) {
			t.Fatal("必须先恢复旧文字并确认，再恢复原效果并完整回读")
		}
	}
}

func TestRestoreDisplayPreservesMusicVisualization(t *testing.T) {
	for _, music := range [][3]byte{{0, 0, 0}, {1, 0, 0}, {1, 1, 2}} {
		for _, native := range []bool{false, true} {
			t.Run(fmt.Sprintf("音乐选项%v/原生文字%t", music, native), func(t *testing.T) {
				state := nativeTextBefore()
				if native {
					state = nativeTextAfter()
				}
				copy(state[9:12], music[:])
				snapshot := capturedDisplay(t, state, "旧文字")
				current := nativeTextAfter()
				copy(current[9:12], music[:])
				f := &fakeTransport{replies: [][]byte{response(0xee, current), response(0xef, nil), response(0xee, state)}}
				nativeWrites := 0
				if native {
					f = nativeDisplayRestoreTransport(state, snapshot.text)
					f.replies[0] = response(0xee, current)
					f.replies[1] = response(0xee, current)
					f.replies[5] = response(0xee, current)
					nativeWrites = 1
				}
				if err := (&Device{f}).RestoreDisplay(snapshot); err != nil {
					t.Fatal(err)
				}
				assertDisplayWrites(t, f, nativeWrites, 1)
				for _, p := range f.writes {
					if p[3] == 0xef && p[11] != state[5] {
						t.Fatal("只能恢复非音乐模式；不能把 EE 音乐字段错误作为场景参数或额外切换音乐")
					}
				}
			})
		}
	}
}

func TestRestoreDisplayRejectsInvalidSnapshot(t *testing.T) {
	for _, snapshot := range []*DisplaySnapshot{nil, {}} {
		f := &fakeTransport{}
		if err := (&Device{f}).RestoreDisplay(snapshot); err == nil || len(f.writes) != 0 {
			t.Fatal("空快照必须在任何通信前拒绝")
		}
	}
}

func TestRestoreDisplaySceneFailureDoesNotRetry(t *testing.T) {
	state := nativeTextBefore()
	snapshot := capturedDisplay(t, state, "")
	for _, tc := range []struct {
		name   string
		mutate func(*fakeTransport)
		writes int
	}{
		{"初始状态失败", func(f *fakeTransport) { f.replies = nil }, 0},
		{"音乐选项外部改变", func(f *fakeTransport) { p := nativeTextAfter(); p[9]++; f.replies[0] = response(0xee, p) }, 0},
		{"确认缺失", func(f *fakeTransport) { f.replies = f.replies[:1] }, 1},
		{"确认校验错误", func(f *fakeTransport) { f.replies[1][len(f.replies[1])-1]++ }, 1},
		{"确认命令错误", func(f *fakeTransport) { f.replies[1] = response(0xe8, nil); f.replies = f.replies[:2] }, 1},
		{"回读缺失", func(f *fakeTransport) { f.replies = f.replies[:2] }, 1},
		{"回读不完整", func(f *fakeTransport) { f.replies[2] = response(0xee, state[:9]) }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeTransport{replies: [][]byte{response(0xee, nativeTextAfter()), response(0xef, nil), response(0xee, state)}}
			tc.mutate(f)
			if err := (&Device{f}).RestoreDisplay(snapshot); err == nil {
				t.Fatal("恢复失败不能报告成功")
			}
			assertDisplayWrites(t, f, 0, tc.writes)
		})
	}
	for index := range state {
		t.Run(fmt.Sprintf("完整核对字段%d", index), func(t *testing.T) {
			after := append([]byte(nil), state...)
			after[index]++
			f := &fakeTransport{replies: [][]byte{response(0xee, nativeTextAfter()), response(0xef, nil), response(0xee, after)}}
			if err := (&Device{f}).RestoreDisplay(snapshot); err == nil {
				t.Fatal("恢复必须核对全部十二字节，不能只核对模式")
			}
			assertDisplayWrites(t, f, 0, 1)
		})
	}
}

func TestRestoreDisplayNativeTextFailureDoesNotRetry(t *testing.T) {
	state := nativeTextAfter()
	snapshot := capturedDisplay(t, state, "旧文字")
	for _, tc := range []struct {
		name   string
		mutate func(*fakeTransport)
		native int
		scene  int
	}{
		{"恢复前文字能力不支持", func(f *fakeTransport) { f.replies[2] = nativeTextReply(32, 50, "当前文字") }, 0, 0},
		{"旧文字确认损坏", func(f *fakeTransport) { f.replies[3][7]++ }, 1, 0},
		{"旧文字未恢复", func(f *fakeTransport) { f.replies[4] = nativeTextReply(16, 50, "错误文字") }, 1, 0},
		{"旧文字未进入静态模式", func(f *fakeTransport) { f.replies[5] = response(0xee, nativeTextBefore()) }, 1, 0},
		{"旧文字恢复后场景确认缺失", func(f *fakeTransport) { f.replies = f.replies[:6] }, 1, 1},
		{"最终文字回读失败", func(f *fakeTransport) { f.replies = f.replies[:8] }, 1, 1},
		{"切模式后文字变化", func(f *fakeTransport) { f.replies[8] = nativeTextReply(16, 50, "不同文字") }, 1, 1},
		{"切模式后文字能力变化", func(f *fakeTransport) { f.replies[8] = nativeTextReply(32, 50, "旧文字") }, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := nativeDisplayRestoreTransport(state, snapshot.text)
			tc.mutate(f)
			if err := (&Device{f}).RestoreDisplay(snapshot); err == nil {
				t.Fatal("旧文字恢复失败不能报告成功")
			}
			assertDisplayWrites(t, f, tc.native, tc.scene)
		})
	}
}

type displayFailWriteTransport struct {
	fakeTransport
	command byte
}

func (f *displayFailWriteTransport) Write(p []byte) error {
	f.fakeTransport.Write(p)
	if p[3] == f.command {
		return errors.New("模拟写入失败")
	}
	return nil
}

func TestRestoreDisplayWriteFailureDoesNotRetry(t *testing.T) {
	for _, command := range []byte{0xe8, 0xef} {
		state := nativeTextAfter()
		snapshot := capturedDisplay(t, state, "旧文字")
		f := &displayFailWriteTransport{*nativeDisplayRestoreTransport(state, snapshot.text), command}
		if err := (&Device{f}).RestoreDisplay(snapshot); err == nil {
			t.Fatal("写入失败不能报告成功")
		}
		sceneWrites := 0
		if command == 0xef {
			sceneWrites = 1
		}
		assertDisplayWrites(t, &f.fakeTransport, 1, sceneWrites)
	}
}

func TestScreenColorReadAndWrite(t *testing.T) {
	before := []byte{0, 0, 150, 4, 0, 1, 6, 52, 255, 1, 0, 0}
	after := append([]byte(nil), before...)
	copy(after[1:4], []byte{20, 40, 60})
	for _, tc := range []string{"success", "mismatch", "scene changed", "short", "bad checksum", "missing ack"} {
		t.Run(tc, func(t *testing.T) {
			result := append([]byte(nil), after...)
			switch tc {
			case "mismatch":
				result[1]++
			case "scene changed":
				result[7]++
			case "short":
				result = result[:9]
			}
			reply := response(0xee, result)
			if tc == "bad checksum" {
				reply[len(reply)-1]++
			}
			f := &fakeTransport{replies: [][]byte{response(0xee, before), response(0xef, nil), reply}}
			if tc == "missing ack" {
				f.replies = f.replies[:1]
			}
			err := (&Device{f}).SetScreenColor(RGB{20, 40, 60})
			if (err == nil) != (tc == "success") {
				t.Fatalf("unexpected result: %v", err)
			}
			if len(f.writes) < 2 || !bytes.Equal(f.writes[1][6:10], []byte{3, 20, 40, 60}) || f.writes[1][5] != 4 {
				t.Fatal("must use RGB-only action")
			}
			writes := 0
			for _, p := range f.writes {
				if p[3] == 0xef {
					writes++
				}
			}
			if writes != 1 {
				t.Fatal("color writes must not retry")
			}
		})
	}
	f := &fakeTransport{replies: [][]byte{response(0xee, before)}}
	col, err := (&Device{f}).ScreenColor()
	if err != nil || col != (RGB{0, 150, 4}) {
		t.Fatal(col, err)
	}
	f = &fakeTransport{replies: [][]byte{response(0xee, before[:8])}}
	if err := (&Device{f}).SetScreenColor(RGB{}); err == nil || len(f.writes) != 1 {
		t.Fatal("must not write after incomplete state")
	}
}
