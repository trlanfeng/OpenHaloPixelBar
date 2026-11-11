package pixelbar

import (
	"context"
	"errors"
	"github.com/go-macos/iokit/hid"
	"time"
)

type macTransport struct {
	dev       *hid.Device
	reports   chan []byte
	done      chan struct{}
	cancel    context.CancelFunc
	streamErr error
}

func openTransport() (Transport, error) {
	devices, e := hid.Devices(hid.Filter{VendorID: 0x2d99, ProductIDs: []uint16{0xa160}, UsagePage: 0xff14})
	if e != nil {
		return nil, e
	}
	if len(devices) != 1 {
		for _, d := range devices {
			d.Close()
		}
		return nil, errors.New("请通过 USB 连接一台 Halo PixelBar，并切换到声卡输入")
	}
	d := devices[0]
	if e = d.Open(); e != nil {
		d.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &macTransport{dev: d, reports: make(chan []byte, 128), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(t.done)
		t.streamErr = hid.Stream(ctx, func(_ *hid.Device, p []byte) {
			select {
			case t.reports <- append([]byte(nil), p...):
			case <-ctx.Done():
			}
		}, d)
	}()
	return t, nil
}
func (t *macTransport) Close() error         { t.cancel(); <-t.done; return t.dev.Close() }
func (t *macTransport) Write(p []byte) error { return t.dev.SetReport(hid.Output, p[0], p) }
func (t *macTransport) Read(timeout time.Duration) ([]byte, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case p := <-t.reports:
		return p, nil
	case <-timer.C:
		return nil, nil
	case <-t.done:
		if t.streamErr != nil {
			return nil, t.streamErr
		}
		return nil, errors.New("USB 接收已停止")
	}
}
