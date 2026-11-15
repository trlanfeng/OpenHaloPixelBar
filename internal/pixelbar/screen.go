package pixelbar

import (
	"bytes"
	"errors"
	"fmt"
)

// DisplaySnapshot 保存联动接管前的显示设置；字段不对外开放，避免构造任意设备命令。
// 它不备份图案槽位，调用方必须在快照有效期间禁止图案上传及其他设备写入。
type DisplaySnapshot struct {
	state    [12]byte
	text     string
	captured bool
}

// CaptureDisplay 仅查询，不写入。未知模式或不能安全重发的旧文字会阻止接管。
func (d *Device) CaptureDisplay() (*DisplaySnapshot, error) {
	state, err := d.ScreenState()
	if err != nil {
		return nil, err
	}
	// EE 的音乐选项独立保存在 9..11；不能把十二字节查询结果当作 EF 参数。
	// 已有场景和自创文字模型仅解析组合 0、模式 1/2；组合 0 也可独立开启音乐可视化。
	if state[4] != 0 || (state[5] != 1 && state[5] != 2) {
		return nil, fmt.Errorf("当前显示模式尚不能安全恢复（组合 %d、模式 %d）；未接管音箱", state[4], state[5])
	}
	snapshot := &DisplaySnapshot{}
	copy(snapshot.state[:], state)
	if state[5] == 2 {
		info, err := d.NativeText()
		if err != nil {
			return nil, fmt.Errorf("无法保存原生旧文字；未接管音箱：%w", err)
		}
		if info.MaxLength != nativeTextMaxLength || info.PackageMaxLength != nativeTextPackageMaxLength {
			return nil, fmt.Errorf("原生旧文字能力 %d/%d 尚不能安全恢复；未接管音箱", info.MaxLength, info.PackageMaxLength)
		}
		if err := ValidateNativeText(info.Text); err != nil {
			return nil, fmt.Errorf("原生旧文字不能安全恢复；未接管音箱：%w", err)
		}
		snapshot.text = info.Text
		current, err := d.ScreenState()
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(current, state) {
			return nil, errors.New("保存旧文字时屏幕设置发生变化；未接管音箱")
		}
	}
	snapshot.captured = true
	return snapshot, nil
}

// RestoreDisplay 恢复捕获的文字及模式；每条写入均等待确认，最终完整回读，不重试。
func (d *Device) RestoreDisplay(snapshot *DisplaySnapshot) error {
	if snapshot == nil || !snapshot.captured {
		return errors.New("缺少有效的显示快照，不能恢复音箱")
	}
	before, err := d.ScreenState()
	if err != nil {
		return err
	}
	// 联动文字不会修改音乐选项；若被外部程序改变，不能假装九字节 EF 能恢复它们。
	if !bytes.Equal(before[9:12], snapshot.state[9:12]) {
		return errors.New("音箱音乐可视化选项已变化，无法完整恢复旧显示；未执行恢复写入")
	}
	if snapshot.state[5] == 2 {
		if err := d.SetNativeText(snapshot.text); err != nil {
			return fmt.Errorf("恢复原生旧文字失败：%w", err)
		}
	}
	// EF action 1 的有效负载为 action、RGB、组合、模式及三个非音乐选项。
	selection := append([]byte{1}, snapshot.state[1:9]...)
	if _, err := d.request(0xef, selection); err != nil {
		return fmt.Errorf("恢复显示模式未获有效确认（未自动重试）：%w", err)
	}
	after, err := d.ScreenState()
	if err != nil {
		return fmt.Errorf("恢复显示已发送，但完整状态回读失败：%w", err)
	}
	if !bytes.Equal(after, snapshot.state[:]) {
		return errors.New("恢复显示已发送，但音箱完整状态回读不一致；未自动重试，请检查设备状态")
	}
	if snapshot.state[5] == 2 {
		info, err := d.NativeText()
		if err != nil {
			return fmt.Errorf("恢复模式后旧文字回读失败：%w", err)
		}
		if info != (NativeTextInfo{MaxLength: nativeTextMaxLength, PackageMaxLength: nativeTextPackageMaxLength, Text: snapshot.text}) {
			return errors.New("恢复模式后旧文字或文字能力回读不一致；未自动重试，请检查设备状态")
		}
	}
	return nil
}

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
