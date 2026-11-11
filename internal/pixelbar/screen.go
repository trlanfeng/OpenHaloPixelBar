package pixelbar

import (
	"bytes"
	"errors"
	"fmt"
)

type RGB struct{ R, G, B byte }

func (c RGB) Hex() string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

// ScreenState reads the complete 1.1.1 state, including both scene and music
// settings. RGB occupies bytes 1..3; the remaining fields are not color data.
func (d *Device) ScreenState() ([]byte, error) {
	p, err := d.request(0xee, nil)
	if err != nil {
		return nil, err
	}
	if len(p) != 12 || p[0] != 0 {
		return nil, errors.New("音箱屏幕状态不完整或格式不支持")
	}
	return append([]byte(nil), p...), nil
}

func (d *Device) ScreenColor() (RGB, error) {
	p, err := d.ScreenState()
	if err != nil {
		return RGB{}, err
	}
	return RGB{p[1], p[2], p[3]}, nil
}

// SetScreenColor uses EF action 3 (RGB only), not a scene-selection command.
// Never retry a write. A reply alone is insufficient: read back RGB and verify
// that every unrelated screen setting is unchanged before reporting success.
func (d *Device) SetScreenColor(col RGB) error {
	before, err := d.ScreenState()
	if err != nil {
		return err
	}
	if _, err = d.request(0xef, []byte{3, col.R, col.G, col.B}); err != nil {
		return err
	}
	after, err := d.ScreenState()
	if err != nil {
		return err
	}
	if !bytes.Equal(after[1:4], []byte{col.R, col.G, col.B}) {
		return errors.New("颜色已发送，但音箱回读颜色不一致；请重新读取当前颜色")
	}
	if !bytes.Equal(before[4:], after[4:]) {
		return errors.New("颜色已发送，但音箱的其他屏幕设置发生变化；请检查设备状态")
	}
	return nil
}
