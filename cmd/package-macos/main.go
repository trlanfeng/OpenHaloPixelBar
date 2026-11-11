// Package a cross-built binary as a macOS .app ZIP with executable permissions.
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	binary := flag.String("binary", "", "cross-built Mach-O binary")
	out := flag.String("out", "", "output ZIP")
	flag.Parse()
	if *binary == "" || *out == "" {
		return fmt.Errorf("provide -binary and -out")
	}
	f, e := os.Create(*out)
	if e != nil {
		return e
	}
	z := zip.NewWriter(f)
	for _, entry := range []struct {
		source, name string
		mode         os.FileMode
	}{{*binary, "OpenPixelBar.app/Contents/MacOS/OpenPixelBar", 0755}, {"packaging/Info.plist", "OpenPixelBar.app/Contents/Info.plist", 0644}, {"internal/branding/icon.icns", "OpenPixelBar.app/Contents/Resources/OpenPixelBar.icns", 0644}, {"README.md", "OpenPixelBar.app/Contents/Resources/README.md", 0644}, {"THIRD_PARTY_NOTICES.txt", "OpenPixelBar.app/Contents/Resources/THIRD_PARTY_NOTICES.txt", 0644}} {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		h.SetMode(entry.mode)
		w, e := z.CreateHeader(h)
		if e != nil {
			z.Close()
			f.Close()
			return e
		}
		source, e := os.Open(entry.source)
		if e != nil {
			z.Close()
			f.Close()
			return e
		}
		_, e = io.Copy(w, source)
		source.Close()
		if e != nil {
			z.Close()
			f.Close()
			return e
		}
	}
	if e = z.Close(); e != nil {
		f.Close()
		return e
	}
	return f.Close()
}
