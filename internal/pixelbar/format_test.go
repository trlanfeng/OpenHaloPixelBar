package pixelbar

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestImportFormatValidation(t *testing.T) {
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewGray(image.Rect(0, 0, 4, 4))); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name  string
		data  []byte
		valid bool
	}{{"image.PNG", b.Bytes(), true}, {"image.jpg", b.Bytes(), false}, {"image.svg", b.Bytes(), false}, {"image.png", []byte("not an image"), false}, {"image", b.Bytes(), false}} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name)
			if e := os.WriteFile(path, tc.data, 0600); e != nil {
				t.Fatal(e)
			}
			_, e := Load(path)
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, e)
			}
		})
	}
}
