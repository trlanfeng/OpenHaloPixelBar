//go:build !windows && !darwin

package appwatch

import "errors"

func Capture() (Snapshot, error) {
	return Snapshot{}, errors.New("应用监控仅支持 Windows 和 macOS")
}
