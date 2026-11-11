//go:build !windows && !darwin

package pixelbar

import "errors"

func openTransport() (Transport, error) {
	return nil, errors.New("当前 USB 适配支持 Windows 与 macOS")
}
