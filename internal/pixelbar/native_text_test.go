package pixelbar

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func nativeTextReply(maxLength, packageLength byte, text string) []byte {
	return response(0xe7, append([]byte{0, maxLength, packageLength, byte(len(text))}, []byte(text)...))
}

func nativeTextAck() []byte {
	return append([]byte{0x2f, 0xcc, 0xec, 0xe8, 0, 1, 1, 0xa2}, make([]byte, 56)...)
}

func nativeTextBefore() []byte {
	return []byte{0, 255, 255, 255, 0, 1, 0, 0, 255, 1, 0, 0}
}

func nativeTextAfter() []byte {
	return []byte{0, 255, 255, 255, 0, 2, 0, 1, 255, 1, 0, 0}
}

func nativeTextTransport(text string) *fakeTransport {
	return &fakeTransport{replies: [][]byte{
		response(0xee, nativeTextBefore()),
		nativeTextReply(16, 50, strings.Repeat("字", 15)),
		nativeTextAck(),
		nativeTextReply(16, 50, text),
		response(0xee, nativeTextAfter()),
	}}
}

func countNativeTextWrites(t *testing.T, f *fakeTransport) int {
	t.Helper()
	count := 0
	for _, p := range f.writes {
		switch p[3] {
		case 0xe8:
			count++
		case 0xee, 0xe7:
		default:
			t.Fatalf("unexpected command %02x; must not upload image or switch mode", p[3])
		}
	}
	return count
}

func TestNativeTextRead(t *testing.T) {
	text := strings.Repeat("字", 15)
	for _, tc := range []struct {
		name string
		p    []byte
		ok   bool
	}{
		{"verified 45 bytes", append([]byte{0, 16, 50, 45}, []byte(text)...), true},
		{"empty existing text", []byte{0, 16, 50, 0}, true},
		{"other capability readable", []byte{0, 32, 40, 0}, true},
		{"short", []byte{0, 16, 50}, false},
		{"source", []byte{1, 16, 50, 0}, false},
		{"zero max", []byte{0, 0, 50, 0}, false},
		{"zero package", []byte{0, 16, 0, 0}, false},
		{"truncated text", []byte{0, 16, 50, 2, 'a'}, false},
		{"extra text", []byte{0, 16, 50, 0, 'a'}, false},
		{"invalid utf8", []byte{0, 16, 50, 1, 0xff}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeTransport{replies: [][]byte{response(0xe7, tc.p)}}
			info, err := (&Device{f}).NativeText()
			if (err == nil) != tc.ok {
				t.Fatalf("info=%+v err=%v", info, err)
			}
			if tc.ok && (info.MaxLength != int(tc.p[1]) || info.PackageMaxLength != int(tc.p[2]) || info.Text != string(tc.p[4:])) {
				t.Fatalf("incorrect info: %+v", info)
			}
			want, _ := Packet(0xe7, []byte{0})
			if len(f.writes) != 1 || !bytes.Equal(f.writes[0], want) {
				t.Fatal("query must be E7 source 0")
			}
		})
	}
}

func TestNativeTextAck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func([]byte) []byte
		ok     bool
	}{
		{"verified padded sample", func(p []byte) []byte { return p }, true},
		{"unpadded sample", func(p []byte) []byte { return p[:8] }, true},
		{"truncated", func(p []byte) []byte { return p[:7] }, false},
		{"empty", func(p []byte) []byte { return nil }, false},
		{"too long", func(p []byte) []byte { return append(p, 0) }, false},
		{"frame", func(p []byte) []byte { p[0] = 0x2e; return p }, false},
		{"BB response", func(p []byte) []byte { return response(0xe8, []byte{1}) }, false},
		{"type", func(p []byte) []byte { p[1] = 0xaa; return p }, false},
		{"protocol", func(p []byte) []byte { p[2]++; return p }, false},
		{"command", func(p []byte) []byte { p[3]--; return p }, false},
		{"length high byte", func(p []byte) []byte { p[4] = 1; return p }, false},
		{"length low byte", func(p []byte) []byte { p[5] = 2; return p }, false},
		{"checksum", func(p []byte) []byte { p[7]++; return p }, false},
		{"rejected", func(p []byte) []byte { p[6] = 0; p[7]--; return p }, false},
		{"unknown status", func(p []byte) []byte { p[6] = 2; p[7]++; return p }, false},
		{"padding", func(p []byte) []byte { p[63] = 1; return p }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.mutate(nativeTextAck())
			if err := parseNativeTextAck(p); (err == nil) != tc.ok {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
	if cmd, _, err := Parse(nativeTextAck()); cmd != 0 || err != nil {
		t.Fatal("generic parser must continue ignoring CC acknowledgments")
	}
}

func TestSetNativeTextSinglePacket(t *testing.T) {
	for _, text := range []string{"文字轮播测试", strings.Repeat("字", 16), strings.Repeat("a", 16), strings.Repeat("\U00020000", 12), strings.Repeat("字", 14) + "\U00020000é", " 前后空格 "} {
		t.Run(text, func(t *testing.T) {
			if err := ValidateNativeText(text); err != nil {
				t.Fatal("valid text rejected by offline validation", err)
			}
			f := nativeTextTransport(text)
			if err := (&Device{f}).SetNativeText(text); err != nil {
				t.Fatal(err)
			}
			if countNativeTextWrites(t, f) != 1 || len(f.writes) != 5 {
				t.Fatal("must read state/text, write once, then read text/state")
			}
			wantCommands := []byte{0xee, 0xe7, 0xe8, 0xe7, 0xee}
			for i, p := range f.writes {
				if p[3] != wantCommands[i] {
					t.Fatal("incorrect request order")
				}
			}
			want, _ := Packet(0xe8, append([]byte{0, byte(len(text))}, []byte(text)...))
			if !bytes.Equal(f.writes[2], want) {
				t.Fatalf("wrong packet: %x", f.writes[2])
			}
		})
	}
}

func TestSetNativeTextValidation(t *testing.T) {
	for _, text := range []string{"", " \u3000 ", "a\nb", "a\rb", "a\tb", "a\x00b", "a\x7fb", "a\u0085b", "a\u2028b", "a\u2029b", string([]byte{0xff}), strings.Repeat("a", 17), strings.Repeat("字", 17), strings.Repeat("\U00020000", 13), strings.Repeat("字", 15) + "\U00020000"} {
		t.Run(text, func(t *testing.T) {
			if err := ValidateNativeText(text); err == nil {
				t.Fatal("离线校验不得接受无效文字")
			}
			f := nativeTextTransport(text)
			if err := (&Device{f}).SetNativeText(text); err == nil {
				t.Fatal("invalid text accepted")
			}
			if len(f.writes) != 0 {
				t.Fatal("无效文字必须在任何设备读写前拒绝")
			}
		})
	}
	for _, caps := range [][2]byte{{0, 50}, {16, 0}, {15, 50}, {17, 50}, {16, 49}, {16, 51}, {255, 255}} {
		f := nativeTextTransport("文字")
		f.replies[1] = nativeTextReply(caps[0], caps[1], "")
		if err := (&Device{f}).SetNativeText("文字"); err == nil || countNativeTextWrites(t, f) != 0 {
			t.Fatalf("unsupported capability %v accepted: %v", caps, err)
		}
	}
}

func TestSetNativeTextConfirmationFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fakeTransport)
		writes int
	}{
		{"initial state short", func(f *fakeTransport) { f.replies[0] = response(0xee, nativeTextBefore()[:9]) }, 0},
		{"initial text checksum", func(f *fakeTransport) { f.replies[1][len(f.replies[1])-1]++ }, 0},
		{"initial text truncated frame", func(f *fakeTransport) { f.replies[1] = f.replies[1][:10]; f.replies = f.replies[:2] }, 0},
		{"missing ack", func(f *fakeTransport) { f.replies = f.replies[:2] }, 1},
		{"bad ack checksum", func(f *fakeTransport) { f.replies[2][7]++ }, 1},
		{"bad ack frame", func(f *fakeTransport) { f.replies[2][0]++ }, 1},
		{"bad ack type", func(f *fakeTransport) { f.replies[2][1] = 0xbb }, 1},
		{"bad ack command", func(f *fakeTransport) { f.replies[2][3]-- }, 1},
		{"ack truncated", func(f *fakeTransport) { f.replies[2] = f.replies[2][:7] }, 1},
		{"readback missing", func(f *fakeTransport) { f.replies = f.replies[:3] }, 1},
		{"readback mismatch", func(f *fakeTransport) { f.replies[3] = nativeTextReply(16, 50, "不同文字") }, 1},
		{"readback capability mismatch", func(f *fakeTransport) { f.replies[3] = nativeTextReply(17, 50, "文字") }, 1},
		{"readback invalid utf8", func(f *fakeTransport) { f.replies[3] = nativeTextReply(16, 50, string([]byte{0xff})) }, 1},
		{"readback checksum", func(f *fakeTransport) { f.replies[3][len(f.replies[3])-1]++ }, 1},
		{"state missing", func(f *fakeTransport) { f.replies = f.replies[:4] }, 1},
		{"state short", func(f *fakeTransport) { f.replies[4] = response(0xee, nativeTextAfter()[:9]) }, 1},
		{"state checksum", func(f *fakeTransport) { f.replies[4][len(f.replies[4])-1]++ }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := nativeTextTransport("文字")
			tc.mutate(f)
			if err := (&Device{f}).SetNativeText("文字"); err == nil {
				t.Fatal("confirmation failure accepted")
			}
			if got := countNativeTextWrites(t, f); got != tc.writes {
				t.Fatalf("writes %d, want %d; never retry a failed write", got, tc.writes)
			}
		})
	}
	for _, index := range []int{0, 1, 2, 3, 4, 5, 6, 9, 10, 11} {
		f := nativeTextTransport("文字")
		after := nativeTextAfter()
		after[index]++
		f.replies[4] = response(0xee, after)
		if err := (&Device{f}).SetNativeText("文字"); err == nil || countNativeTextWrites(t, f) != 1 {
			t.Fatalf("unexpected state change at %d accepted: %v", index, err)
		}
	}
}

type nativeTextFailWriteTransport struct{ fakeTransport }

func (f *nativeTextFailWriteTransport) Write(p []byte) error {
	f.fakeTransport.Write(p)
	if p[3] == 0xe8 {
		return errors.New("write failed")
	}
	return nil
}

func TestSetNativeTextWriteFailureDoesNotRetry(t *testing.T) {
	f := &nativeTextFailWriteTransport{*nativeTextTransport("文字")}
	if err := (&Device{f}).SetNativeText("文字"); err == nil {
		t.Fatal("write error ignored")
	}
	if countNativeTextWrites(t, &f.fakeTransport) != 1 || len(f.writes) != 3 {
		t.Fatal("must not retry or read confirmation after failed write")
	}
}

func TestSetNativeTextPreservesColorAndMusic(t *testing.T) {
	f := nativeTextTransport("文字")
	before, after := nativeTextBefore(), nativeTextAfter()
	copy(before[1:4], []byte{12, 34, 56})
	copy(after[1:4], before[1:4])
	copy(before[9:12], []byte{2, 3, 4})
	copy(after[9:12], before[9:12])
	f.replies[0] = response(0xee, before)
	f.replies[4] = response(0xee, after)
	f.replies = append(f.replies[:2], append([][]byte{nil}, f.replies[2:]...)...)
	if err := (&Device{f}).SetNativeText("文字"); err != nil {
		t.Fatal("must preserve arbitrary existing color/music and tolerate empty reads", err)
	}
	if countNativeTextWrites(t, f) != 1 {
		t.Fatal("must write exactly once")
	}
}

func TestNativeTextAckDeadline(t *testing.T) {
	f := &fakeTransport{}
	if err := (&Device{f}).receiveNativeTextAck(time.Now().Add(-time.Second)); !errors.Is(err, errReplyTimeout) {
		t.Fatal("must preserve timeout error", err)
	}
}
