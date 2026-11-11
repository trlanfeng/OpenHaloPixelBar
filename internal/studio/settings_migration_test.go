package studio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSettingsNameMigration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		files     map[string]string
		threshold float64
		migrated  bool
	}{
		{"current wins", map[string]string{"OpenHaloPixelBar": "current", "OpenPixelBar": "legacy", "OpenPB": "legacy"}, 180, true},
		{"previous name wins", map[string]string{"OpenPixelBar": "current", "OpenPB": "legacy"}, 180, true},
		{"original name", map[string]string{"OpenPB": "current"}, 180, true},
		{"no settings", nil, 128, false},
		{"invalid current does not resurrect old settings", map[string]string{"OpenHaloPixelBar": "invalid", "OpenPixelBar": "legacy"}, 128, false},
		{"invalid previous does not resurrect oldest settings", map[string]string{"OpenPixelBar": "invalid", "OpenPB": "legacy"}, 128, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("APPDATA", root)
			t.Setenv("XDG_CONFIG_HOME", root)
			t.Setenv("HOME", root)
			dir, err := os.UserConfigDir()
			if err != nil {
				t.Fatal(err)
			}
			want := settings{Recent: []string{"first.png", "second.gif"}, Threshold: 180, Fit: "铺满裁切", Invert: true, Dither: true, Speed: "2×"}
			originals := map[string][]byte{}
			for name, content := range tc.files {
				data := []byte("invalid JSON")
				if content != "invalid" {
					saved := want
					if content == "legacy" {
						saved.Threshold = 70
					}
					data, err = json.Marshal(saved)
					if err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(dir, name, "settings.json")
				if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				if name != "OpenHaloPixelBar" {
					originals[path] = data
				}
			}
			a := New()
			if a.config != filepath.Join(dir, "OpenHaloPixelBar", "settings.json") || a.options.Threshold != tc.threshold {
				t.Fatalf("wrong settings destination or precedence: %s, %+v", a.config, a.options)
			}
			if tc.migrated && !reflect.DeepEqual(a.options, want) {
				t.Fatalf("preferences lost: got %+v, want %+v", a.options, want)
			}
			a.save()
			restored := New()
			if !reflect.DeepEqual(restored.options, a.options) {
				t.Fatalf("settings or recent paths lost after save: %+v", restored.options)
			}
			for path, original := range originals {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != string(original) {
					t.Fatalf("legacy settings changed: %s (%v)", path, err)
				}
			}
		})
	}
}
