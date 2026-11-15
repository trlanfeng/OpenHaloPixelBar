package pixelbar

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	nativeTextMaxLength        = 16
	nativeTextPackageMaxLength = 50
	nativeTextMaxBytes         = min(nativeTextMaxLength*3, 255, nativeTextPackageMaxLength, 55)
)

type NativeTextInfo struct {
	MaxLength        int
	PackageMaxLength int
	Text             string
}

// NativeText 读取设备原生文字及能力，不将截断或无效 UTF-8 当作有效内容。
func (d *Device) NativeText() (NativeTextInfo, error) {
	p, err := d.request(0xe7, []byte{0})
	if err != nil {
		return NativeTextInfo{}, err
	}
	if len(p) < 4 || p[0] != 0 || len(p) != 4+int(p[3]) {
		return NativeTextInfo{}, errors.New("音箱原生文字回复不完整或格式不支持")
	}
	if p[1] == 0 || p[2] == 0 {
		return NativeTextInfo{}, errors.New("音箱原生文字能力不支持：长度或分包长度为零")
	}
	if !utf8.Valid(p[4:]) {
		return NativeTextInfo{}, errors.New("音箱原生文字不是有效 UTF-8")
	}
	return NativeTextInfo{MaxLength: int(p[1]), PackageMaxLength: int(p[2]), Text: string(p[4:])}, nil
}

// ValidateNativeText 在不访问设备的情况下验证已适配的单包原生静态文字限制。
func ValidateNativeText(text string) error {
	if !utf8.ValidString(text) {
		return errors.New("原生文字必须是有效 UTF-8")
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("请输入非空的单行原生文字")
	}
	for _, r := range text {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return errors.New("原生文字不能包含换行、制表符或控制字符")
		}
	}
	characters := utf8.RuneCountInString(text)
	if characters > nativeTextMaxLength || len(text) > nativeTextMaxBytes {
		return fmt.Errorf("原生静态文字最多 %d 个字符且 UTF-8 不超过 %d 字节（当前 %d 个字符、%d 字节）；不会截断或分包发送", nativeTextMaxLength, nativeTextMaxBytes, characters, len(text))
	}
	return nil
}

// SetNativeText 仅发送已经验证的单包原生静态文字，不截断、不分包、不重试写入。
func (d *Device) SetNativeText(text string) error {
	if err := ValidateNativeText(text); err != nil {
		return err
	}
	before, err := d.ScreenState()
	if err != nil {
		return err
	}
	info, err := d.NativeText()
	if err != nil {
		return err
	}
	if info.MaxLength != nativeTextMaxLength || info.PackageMaxLength != nativeTextPackageMaxLength {
		return fmt.Errorf("尚未适配的原生文字能力：最大长度 %d，分包长度 %d；当前仅验证 16/50", info.MaxLength, info.PackageMaxLength)
	}
	payload := append([]byte{0, byte(len(text))}, []byte(text)...)
	if err = d.send(0xe8, payload); err != nil {
		return fmt.Errorf("USB 原生文字写入 E8：%w；未自动重试，请检查设备状态", err)
	}
	if err = d.receiveNativeTextAck(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("原生文字已发送但未获有效确认：%w；未自动重试，请检查设备状态", err)
	}
	afterText, err := d.NativeText()
	if err != nil {
		return fmt.Errorf("原生文字已发送，但完整回读失败：%w", err)
	}
	if afterText != (NativeTextInfo{MaxLength: info.MaxLength, PackageMaxLength: info.PackageMaxLength, Text: text}) {
		return errors.New("原生文字已发送，但音箱全文回读或文字能力不一致；请检查设备状态")
	}
	after, err := d.ScreenState()
	if err != nil {
		return fmt.Errorf("原生文字已发送，但屏幕状态回读失败：%w", err)
	}
	if !bytes.Equal(after[4:7], []byte{0, 2, 0}) {
		return errors.New("原生文字已发送，但音箱未确认原生静态文字模式；未额外切换模式，请检查设备状态")
	}
	if !bytes.Equal(before[1:4], after[1:4]) {
		return errors.New("原生文字已发送，但音箱 RGB 颜色发生变化；请检查设备状态")
	}
	if !bytes.Equal(before[9:12], after[9:12]) {
		return errors.New("原生文字已发送，但音箱音乐可视化选项发生变化；请检查设备状态")
	}
	return nil
}

// CC 仅在 E8 专用接收路径中接受，不能扩展通用 Parse 的响应类型。
func parseNativeTextAck(p []byte) error {
	if len(p) < 8 || len(p) > 64 {
		return errors.New("原生文字确认帧不完整或过长")
	}
	if !bytes.Equal(p[:6], []byte{0x2f, 0xcc, 0xec, 0xe8, 0, 1}) {
		return errors.New("原生文字确认帧头、类型、命令或长度不匹配")
	}
	var sum byte
	for _, b := range p[1:7] {
		sum += b
	}
	if p[7] != sum {
		return errors.New("原生文字确认校验失败")
	}
	if p[6] != 1 {
		return fmt.Errorf("音箱拒绝原生文字：%02x", p[6])
	}
	for _, b := range p[8:] {
		if b != 0 {
			return errors.New("原生文字确认帧包含非零填充")
		}
	}
	return nil
}

func (d *Device) receiveNativeTextAck(deadline time.Time) error {
	for time.Now().Before(deadline) {
		p, err := d.transport.Read(min(time.Until(deadline), 100*time.Millisecond))
		if err != nil {
			return err
		}
		if len(p) == 0 {
			continue
		}
		return parseNativeTextAck(p)
	}
	return fmt.Errorf("%w，请退出 TempoHub 并检查 USB 连接", errReplyTimeout)
}
