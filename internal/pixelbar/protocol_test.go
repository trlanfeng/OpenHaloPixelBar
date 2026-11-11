package pixelbar

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"testing"
	"time"
)

func TestWireAndPixelOrder(t *testing.T) {
	p, _ := Packet(0xc6, nil)
	if !bytes.Equal(p[:7], []byte{0x2e, 0xaa, 0xec, 0xc6, 0, 0, 0x5c}) {
		t.Fatalf("report %x", p)
	}
	if _, e := Packet(1, make([]byte, 58)); e == nil {
		t.Fatal("oversized report accepted")
	}
	var frame Frame
	frame[0] = true
	frame[7*Width] = true
	frame[8*Width+255] = true
	b, e := Encode([]Frame{frame}, []int{100})
	if e != nil || len(b) != 1064 {
		t.Fatal(e, len(b))
	}
	if b[5] != 0x81 || b[3] != 255 || b[4] != 255 {
		t.Fatalf("wrong first block %x", b[:6])
	}
	if b[3*133+5+127] != 1 {
		t.Fatal("wrong page / column order")
	}
	a, _ := Encode([]Frame{frame, frame}, []int{100, 250})
	if a[1064] != 6 || a[1066] != 8 || a[1068] != 250 {
		t.Fatal("animation record numbering or delay")
	}
}
func response(cmd byte, p []byte) []byte {
	b := append([]byte{0x2f, 0xbb, 0xec, cmd, 0, byte(len(p))}, p...)
	var sum byte
	for _, v := range b[1:] {
		sum += v
	}
	return append(b, sum)
}
func TestParse(t *testing.T) {
	b := response(0xc6, []byte{1, 1, 1})
	c, p, e := Parse(b)
	if e != nil || c != 0xc6 || !bytes.Equal(p, []byte{1, 1, 1}) {
		t.Fatal(c, p, e)
	}
	b[len(b)-1]++
	if _, _, e = Parse(b); e == nil {
		t.Fatal("corruption accepted")
	}
	if c, _, _ := Parse(b[:5]); c != 0 {
		t.Fatal("partial accepted")
	}
}

type fakeTransport struct {
	replies [][]byte
	writes  [][]byte
}

func (f *fakeTransport) Write(p []byte) error {
	f.writes = append(f.writes, append([]byte(nil), p...))
	return nil
}
func (f *fakeTransport) Read(time.Duration) ([]byte, error) {
	if len(f.replies) == 0 {
		return nil, errors.New("no more replies")
	}
	p := f.replies[0]
	f.replies = f.replies[1:]
	return p, nil
}
func (f *fakeTransport) Close() error { return nil }
func TestUploadConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		final   byte
		scene   byte
		wantErr bool
	}{{"success", 5, 6, false}, {"rejected", 7, 6, true}, {"wrong scene", 5, 2, true}} {
		t.Run(tc.name, func(t *testing.T) {
			state := []byte{0, 0, 150, 4, 0, 1, 3, 6, 255}
			f := &fakeTransport{replies: [][]byte{response(0xee, state), response(0x17, []byte{0, 1, 0, 50})}}
			for i := 0; i < 21; i++ {
				f.replies = append(f.replies, response(0x17, []byte{0, 3}))
			}
			f.replies = append(f.replies, response(0x17, []byte{0, tc.final}), response(0xef, nil))
			current := append([]byte(nil), state...)
			current[7] = tc.scene
			f.replies = append(f.replies, response(0xee, current))
			progress := 0.
			e := (&Device{f}).Upload([]Frame{{}}, []int{100}, func(p float64) { progress = p })
			if (e != nil) != tc.wantErr {
				t.Fatalf("error %v", e)
			}
			if !tc.wantErr && progress != 1 {
				t.Fatal("missing final progress")
			}
			if tc.wantErr && progress == 1 {
				t.Fatal("premature success")
			}
		})
	}
}
func TestPrepareTransparencyAndFit(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < len(im.Pix); i += 4 {
		copy(im.Pix[i:], []byte{255, 255, 255, 255})
	}
	f := Prepare(im, Options{Threshold: 128})
	count := 0
	for _, on := range f {
		if on {
			count++
		}
	}
	if count != 1024 || !f[112] || f[111] {
		t.Fatal("contain geometry", count)
	}
	for i := 3; i < len(im.Pix); i += 4 {
		im.Pix[i] = 0
	}
	f = Prepare(im, Options{Threshold: 128})
	for _, on := range f {
		if on {
			t.Fatal("transparent must be black")
		}
	}
}
func TestGIFCompositionAndLimit(t *testing.T) {
	palette := color.Palette{color.Transparent, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 3, 1), palette)
	first.Pix[0] = 1
	second := image.NewPaletted(image.Rect(1, 0, 2, 1), palette)
	second.Pix[0] = 1
	third := image.NewPaletted(image.Rect(2, 0, 3, 1), palette)
	third.Pix[0] = 1
	g := &gif.GIF{Image: []*image.Paletted{first, second, third}, Delay: []int{1, 20, 30}, Disposal: []byte{gif.DisposalNone, gif.DisposalPrevious, gif.DisposalNone}, Config: image.Config{Width: 3, Height: 1, ColorModel: palette}}
	var b bytes.Buffer
	if e := gif.EncodeAll(&b, g); e != nil {
		t.Fatal(e)
	}
	a, e := Decode(b.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	_, _, _, alpha := a.Frames[2].At(1, 0).RGBA()
	if alpha != 0 {
		t.Fatal("restore previous not applied")
	}
	if a.Delays[0] != 100 || a.Delays[1] != 200 {
		t.Fatal("delay clamp")
	}
	g.Image = nil
	g.Delay = nil
	g.Disposal = nil
	for i := 0; i < 9; i++ {
		g.Image = append(g.Image, first)
		g.Delay = append(g.Delay, 10)
	}
	b.Reset()
	gif.EncodeAll(&b, g)
	if _, e := Decode(b.Bytes()); e == nil {
		t.Fatal("9 frame GIF accepted")
	}
}
