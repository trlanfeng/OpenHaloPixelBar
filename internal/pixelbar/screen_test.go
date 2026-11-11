package pixelbar

import (
	"bytes"
	"testing"
)

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
