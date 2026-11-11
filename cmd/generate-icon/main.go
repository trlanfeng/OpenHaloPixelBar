// Generate platform icons from the editable SVG. Run from the repository root.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/egoist/mygo/ui"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	dir := "internal/branding"
	source, err := os.ReadFile(filepath.Join(dir, "icon.svg"))
	if err != nil {
		return err
	}
	svg, err := ui.ParseSVG(source)
	if err != nil {
		return err
	}
	pngs := map[int][]byte{}
	for _, size := range []int{16, 24, 32, 48, 64, 128, 256, 512, 1024} {
		var buf bytes.Buffer
		if err = png.Encode(&buf, svg.Image(size, size, color.White)); err != nil {
			return err
		}
		pngs[size] = buf.Bytes()
	}
	if err = os.WriteFile(filepath.Join(dir, "icon.png"), pngs[512], 0644); err != nil {
		return err
	}
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	ico := new(bytes.Buffer)
	put16 := func(v uint16) { _ = binary.Write(ico, binary.LittleEndian, v) }
	put32 := func(v uint32) { _ = binary.Write(ico, binary.LittleEndian, v) }
	put16(0)
	put16(1)
	put16(uint16(len(sizes)))
	offset := 6 + 16*len(sizes)
	for _, size := range sizes {
		ico.Write([]byte{byte(size), byte(size), 0, 0})
		put16(1)
		put16(32)
		put32(uint32(len(pngs[size])))
		put32(uint32(offset))
		offset += len(pngs[size])
	}
	for _, size := range sizes {
		ico.Write(pngs[size])
	}
	if err = os.WriteFile(filepath.Join(dir, "icon.ico"), ico.Bytes(), 0644); err != nil {
		return err
	}
	body := new(bytes.Buffer)
	for _, entry := range []struct {
		tag  string
		size int
	}{{"icp4", 16}, {"icp5", 32}, {"icp6", 64}, {"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024}} {
		body.WriteString(entry.tag)
		_ = binary.Write(body, binary.BigEndian, uint32(len(pngs[entry.size])+8))
		body.Write(pngs[entry.size])
	}
	icns := new(bytes.Buffer)
	icns.WriteString("icns")
	_ = binary.Write(icns, binary.BigEndian, uint32(body.Len()+8))
	icns.Write(body.Bytes())
	return os.WriteFile(filepath.Join(dir, "icon.icns"), icns.Bytes(), 0644)
}
