package pixelbar

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"strings"
	"time"
	"unsafe"
)

var hidDLL = windows.NewLazySystemDLL("hid.dll")
var setupDLL = windows.NewLazySystemDLL("setupapi.dll")
var hidGuid = hidDLL.NewProc("HidD_GetHidGuid")
var hidPreparsed = hidDLL.NewProc("HidD_GetPreparsedData")
var hidFree = hidDLL.NewProc("HidD_FreePreparsedData")
var hidCaps = hidDLL.NewProc("HidP_GetCaps")
var enumInterfaces = setupDLL.NewProc("SetupDiEnumDeviceInterfaces")
var interfaceDetail = setupDLL.NewProc("SetupDiGetDeviceInterfaceDetailW")

type interfaceData struct {
	Size     uint32
	Guid     windows.GUID
	Flags    uint32
	Reserved uintptr
}
type deviceCaps struct {
	Usage, UsagePage, Input, Output, Feature uint16
	Reserved                                 [17]uint16
	Extra                                    [10]uint16
}
type winTransport struct{ handle windows.Handle }

func openTransport() (Transport, error) {
	var guid windows.GUID
	hidGuid.Call(uintptr(unsafe.Pointer(&guid)))
	set, e := windows.SetupDiGetClassDevsEx(&guid, "", 0, windows.DIGCF_PRESENT|windows.DIGCF_DEVICEINTERFACE, 0, "")
	if e != nil {
		return nil, e
	}
	defer set.Close()
	var paths []string
	for i := uint32(0); ; i++ {
		item := interfaceData{}
		item.Size = uint32(unsafe.Sizeof(item))
		ok, _, callErr := enumInterfaces.Call(uintptr(set), 0, uintptr(unsafe.Pointer(&guid)), uintptr(i), uintptr(unsafe.Pointer(&item)))
		if ok == 0 {
			if callErr == windows.ERROR_NO_MORE_ITEMS {
				break
			}
			return nil, callErr
		}
		var needed uint32
		interfaceDetail.Call(uintptr(set), uintptr(unsafe.Pointer(&item)), 0, 0, uintptr(unsafe.Pointer(&needed)), 0)
		if needed < 6 || needed > 65536 {
			continue
		}
		buf := make([]byte, needed)
		cbSize := uint32(6)
		if unsafe.Sizeof(uintptr(0)) == 8 {
			cbSize = 8
		}
		*(*uint32)(unsafe.Pointer(&buf[0])) = cbSize
		ok, _, _ = interfaceDetail.Call(uintptr(set), uintptr(unsafe.Pointer(&item)), uintptr(unsafe.Pointer(&buf[0])), uintptr(needed), 0, 0)
		if ok == 0 {
			continue
		}
		path := windows.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[4])), (len(buf)-4)/2))
		low := strings.ToLower(path)
		if !strings.Contains(low, "vid_2d99&pid_a160") {
			continue
		}
		ptr, _ := windows.UTF16PtrFromString(path)
		h, e := windows.CreateFile(ptr, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
		if e != nil {
			continue
		}
		var pp uintptr
		ok, _, _ = hidPreparsed.Call(uintptr(h), uintptr(unsafe.Pointer(&pp)))
		var caps deviceCaps
		if ok != 0 {
			status, _, _ := hidCaps.Call(pp, uintptr(unsafe.Pointer(&caps)))
			hidFree.Call(pp)
			if status == 0x110000 && caps.UsagePage == 0xff14 && caps.Input == 64 && caps.Output == 64 {
				paths = append(paths, path)
			}
		}
		windows.CloseHandle(h)
	}
	if len(paths) != 1 {
		return nil, errors.New("请通过 USB 连接一台 Halo PixelBar，并切换到声卡输入")
	}
	ptr, _ := windows.UTF16PtrFromString(paths[0])
	h, e := windows.CreateFile(ptr, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if e != nil {
		return nil, fmt.Errorf("无法打开音箱，请退出 TempoHub：%w", e)
	}
	return &winTransport{h}, nil
}
func (t *winTransport) Close() error { return windows.CloseHandle(t.handle) }
func (t *winTransport) transfer(p []byte, read bool, timeout time.Duration) (int, error) {
	event, e := windows.CreateEvent(nil, 1, 0, nil)
	if e != nil {
		return 0, e
	}
	defer windows.CloseHandle(event)
	ov := windows.Overlapped{HEvent: event}
	var n uint32
	if read {
		e = windows.ReadFile(t.handle, p, &n, &ov)
	} else {
		e = windows.WriteFile(t.handle, p, &n, &ov)
	}
	if e == windows.ERROR_IO_PENDING {
		result, waitErr := windows.WaitForSingleObject(event, uint32(max(1, timeout.Milliseconds())))
		if waitErr != nil || result == uint32(windows.WAIT_TIMEOUT) {
			windows.CancelIoEx(t.handle, &ov)
			windows.GetOverlappedResult(t.handle, &ov, &n, true)
			if waitErr != nil {
				return 0, waitErr
			}
			if read {
				return 0, nil
			}
			return 0, errors.New("USB 写入超时")
		}
		e = windows.GetOverlappedResult(t.handle, &ov, &n, false)
	}
	return int(n), e
}
func (t *winTransport) Write(p []byte) error {
	n, e := t.transfer(p, false, 5*time.Second)
	if e == nil && n != len(p) {
		return errors.New("USB 写入未完成")
	}
	return e
}
func (t *winTransport) Read(timeout time.Duration) ([]byte, error) {
	p := make([]byte, 64)
	n, e := t.transfer(p, true, timeout)
	return p[:n], e
}
