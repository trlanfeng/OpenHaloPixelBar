// Package pixelbar implements the verified Halo PixelBar 1.1.1 protocol.
package pixelbar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const Width, Height = 256, 32

var errReplyTimeout = errors.New("音箱回复超时")

type Frame [Width * Height]bool

func Encode(frames []Frame, delays []int) ([]byte, error) {
	if len(frames) < 1 || len(frames) > 8 || len(frames) != len(delays) {
		return nil, errors.New("需要 1～8 帧图像及对应时长")
	}
	category := byte(3)
	if len(frames) > 1 {
		category = 6
	}
	data := make([]byte, 0, len(frames)*1064)
	for f, frame := range frames {
		delay := 65535
		if len(frames) > 1 {
			delay = delays[f]
			if delay < 100 || delay > 65535 {
				return nil, errors.New("每帧时长须为 100～65535 毫秒")
			}
		}
		for block := 0; block < 8; block++ {
			index := f*8 + block
			data = append(data, category, byte(index>>8), byte(index), byte(delay>>8), byte(delay))
			for pos := block * 128; pos < (block+1)*128; pos++ {
				var b byte
				for bit := 0; bit < 8; bit++ {
					if frame[(pos/Width*8+bit)*Width+pos%Width] {
						b |= 1 << bit
					}
				}
				data = append(data, b)
			}
		}
	}
	return data, nil
}

func Packet(command byte, payload []byte) ([]byte, error) {
	if len(payload) > 57 {
		return nil, errors.New("HID payload exceeds 57 bytes")
	}
	p := make([]byte, 64)
	copy(p, []byte{0x2e, 0xaa, 0xec, command, 0, byte(len(payload))})
	copy(p[6:], payload)
	for _, b := range p[1 : 6+len(payload)] {
		p[6+len(payload)] += b
	}
	return p, nil
}

func Parse(p []byte) (byte, []byte, error) {
	if len(p) < 7 || !bytes.Equal(p[:3], []byte{0x2f, 0xbb, 0xec}) {
		return 0, nil, nil
	}
	n := int(binary.BigEndian.Uint16(p[4:6]))
	if n > 57 || len(p) < n+7 {
		return 0, nil, nil
	}
	var sum byte
	for _, b := range p[1 : 6+n] {
		sum += b
	}
	if sum != p[6+n] {
		return 0, nil, errors.New("音箱回复校验失败")
	}
	return p[3], p[6 : 6+n], nil
}

type Transport interface {
	Write([]byte) error
	Read(time.Duration) ([]byte, error)
	Close() error
}
type Device struct{ transport Transport }

func Connect() (*Device, error) {
	t, err := openTransport()
	if err != nil {
		return nil, err
	}
	d := &Device{t}
	if err = d.verify(); err != nil {
		t.Close()
		return nil, err
	}
	return d, nil
}
func (d *Device) Close() error { return d.transport.Close() }
func (d *Device) verify() error {
	p, e := d.request(0x65, nil)
	// A newly scheduled macOS input run loop can miss the very first reply.
	// Retrying this read-only handshake is safe; upload writes are never retried.
	if errors.Is(e, errReplyTimeout) {
		p, e = d.request(0x65, nil)
	}
	if e != nil {
		return e
	}
	if len(p) != 4 || p[1] != 0 || binary.BigEndian.Uint16(p[2:]) != 64 {
		return errors.New("此设备协议与已验证版本不同")
	}
	p, e = d.request(0xc6, nil)
	if e != nil {
		return e
	}
	if !bytes.Equal(p, []byte{1, 1, 1}) {
		return fmt.Errorf("当前仅适配固件 1.1.1，设备返回 %v", p)
	}
	return nil
}
func (d *Device) send(cmd byte, payload []byte) error {
	p, e := Packet(cmd, payload)
	if e != nil {
		return e
	}
	return d.transport.Write(p)
}
func (d *Device) receive(cmd byte, deadline time.Time) ([]byte, error) {
	for time.Now().Before(deadline) {
		p, e := d.transport.Read(min(time.Until(deadline), 100*time.Millisecond))
		if e != nil {
			return nil, e
		}
		c, p, e := Parse(p)
		if e != nil {
			return nil, e
		}
		if c == cmd {
			return p, nil
		}
	}
	return nil, fmt.Errorf("%w，请退出 TempoHub 并检查 USB 连接", errReplyTimeout)
}
func (d *Device) request(cmd byte, payload []byte) ([]byte, error) {
	if e := d.send(cmd, payload); e != nil {
		return nil, fmt.Errorf("USB 请求 %02X：%w", cmd, e)
	}
	p, e := d.receive(cmd, time.Now().Add(5*time.Second))
	if e != nil {
		return nil, fmt.Errorf("USB 请求 %02X：%w", cmd, e)
	}
	return p, nil
}

func (d *Device) Upload(frames []Frame, delays []int, progress func(float64)) error {
	data, e := Encode(frames, delays)
	if e != nil {
		return e
	}
	state, e := d.request(0xee, nil)
	if e != nil {
		return e
	}
	if len(state) < 9 {
		return errors.New("无法读取当前屏幕设置")
	}
	start := make([]byte, 6)
	binary.BigEndian.PutUint32(start[2:], uint32(len(data)))
	ready, e := d.request(0x17, start)
	if e != nil {
		return e
	}
	if len(ready) < 4 || !bytes.Equal(ready[:2], []byte{0, 1}) {
		return fmt.Errorf("音箱未接受上传请求：%x", ready)
	}
	size := int(binary.BigEndian.Uint16(ready[2:4]))
	if size < 1 || size > 53 {
		return fmt.Errorf("尚未适配的分包长度 %d", size)
	}
	for index, offset := 0, 0; offset < len(data); index, offset = index+1, offset+size {
		p := []byte{0, 2, byte(index >> 8), byte(index)}
		p = append(p, data[offset:min(offset+size, len(data))]...)
		if e = d.send(0x17, p); e != nil {
			return e
		}
		if progress != nil {
			progress(float64(min(offset+size, len(data))) / float64(len(data)) * .95)
		}
		if offset+size < len(data) {
			p, e = d.receive(0x17, time.Now().Add(5*time.Second))
			if e != nil {
				return e
			}
			if len(p) < 2 || !bytes.Equal(p[:2], []byte{0, 3}) {
				return fmt.Errorf("音箱拒绝了图案数据：%x", p)
			}
		}
	}
	if e = d.send(0x17, []byte{0, 4}); e != nil {
		return e
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		p, e := d.receive(0x17, deadline)
		if e != nil {
			return e
		}
		if len(p) < 2 {
			return errors.New("音箱最终确认不完整")
		}
		if bytes.Equal(p[:2], []byte{0, 5}) {
			break
		}
		if !bytes.Equal(p[:2], []byte{0, 3}) {
			return fmt.Errorf("音箱未确认图案校验成功：%x", p)
		}
	}
	category, option := byte(3), byte(6)
	if len(frames) > 1 {
		category, option = 6, 52
	}
	selection := append([]byte{1}, state[1:5]...)
	selection = append(selection, 1, category, option, 255)
	if _, e = d.request(0xef, selection); e != nil {
		return e
	}
	current, e := d.request(0xee, nil)
	if e != nil {
		return e
	}
	if len(current) < 8 || !bytes.Equal(current[5:8], []byte{1, category, option}) {
		return errors.New("图案已传输，但音箱尚未确认切换场景")
	}
	if progress != nil {
		progress(1)
	}
	return nil
}
