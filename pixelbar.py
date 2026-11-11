"""Independent Halo PixelBar (2D99:A160, firmware 1.1.1) image transport."""
import time
from PIL import Image, ImageOps, ImageSequence
import hid

WIDTH, HEIGHT = 256, 32

def load_asset(path):
    with Image.open(path) as source:
        count = getattr(source, 'n_frames', 1)
        if count > 8:
            raise ValueError('当前动画版支持最多 8 帧，请先精简 GIF 帧数。')
        if source.width*source.height*count > 40_000_000:
            raise ValueError('图片所有帧合计需小于 4000 万像素。')
        frames, durations = [], []
        for frame in ImageSequence.Iterator(source):
            # Pillow's seek composes GIF disposal/transparency before conversion.
            frames.append(ImageOps.exif_transpose(frame).convert('RGBA'))
            durations.append(min(65535, max(100, int(frame.info.get('duration', 100) or 100))))
    return frames, durations

def prepare_image(source, threshold=128, invert=False, fit='contain'):
    source = ImageOps.exif_transpose(source).convert('RGBA')
    background = Image.new('RGBA', source.size, (0, 0, 0, 255))
    gray = Image.alpha_composite(background, source).convert('L')
    if fit == 'cover':
        gray = ImageOps.fit(gray, (WIDTH, HEIGHT), Image.Resampling.LANCZOS)
    elif fit == 'stretch':
        gray = gray.resize((WIDTH, HEIGHT), Image.Resampling.LANCZOS)
    else:
        small = ImageOps.contain(gray, (WIDTH, HEIGHT), Image.Resampling.LANCZOS)
        gray = Image.new('L', (WIDTH, HEIGHT), 0)
        gray.paste(small, ((WIDTH-small.width)//2, (HEIGHT-small.height)//2))
    if invert:
        gray = ImageOps.invert(gray)
    return gray.point(lambda p: 255 if p >= threshold else 0, mode='1')

def _pixel_bytes(image):
    if image.size != (WIDTH, HEIGHT):
        raise ValueError('Image must be 256 x 32')
    mono = image.convert('1')
    return bytes(sum((1 << bit) if mono.getpixel((x, page*8+bit)) else 0
                       for bit in range(8)) for page in range(4) for x in range(WIDTH))

def encode_image(image):
    pixels = _pixel_bytes(image)
    # Eight 128-byte records: scene 3, big-endian record index, duration 65535 ms.
    return b''.join(bytes([3]) + i.to_bytes(2, 'big') + b'\xff\xff' + pixels[i*128:(i+1)*128]
                    for i in range(8))

def encode_animation(frames, durations):
    # Eight frames is a conservative initial limit, matching a local stock animation.
    if not 2 <= len(frames) <= 8 or len(durations) != len(frames):
        raise ValueError('动画需要 2～8 帧，每帧对应一个时长。')
    records = []
    for frame_index, (image, duration) in enumerate(zip(frames, durations)):
        if not isinstance(duration, int) or not 100 <= duration <= 65535:
            raise ValueError('动画每帧时长须为 100～65535 毫秒。')
        pixels = _pixel_bytes(image)
        for block in range(8):
            index = frame_index*8+block
            records.append(b'\x06' + index.to_bytes(2, 'big') + duration.to_bytes(2, 'big')
                           + pixels[block*128:(block+1)*128])
    return b''.join(records)

def decode_image(data):
    if len(data) != 1064:
        raise ValueError('Expected one 1064-byte static frame')
    pixels = b''.join(data[i+5:i+133] for i in range(0, 1064, 133))
    image = Image.new('1', (WIDTH, HEIGHT))
    for y in range(HEIGHT):
        for x in range(WIDTH):
            image.putpixel((x, y), bool(pixels[(y//8)*WIDTH+x] & (1 << (y%8))))
    return image

def packet(command, payload=b''):
    frame = bytes([0x2e, 0xaa, 0xec, command]) + len(payload).to_bytes(2, 'big') + payload
    frame += bytes([sum(frame[1:]) & 255])
    if len(frame) > 64:
        raise ValueError('HID report exceeds 64 bytes')
    return frame.ljust(64, b'\0')

def parse_report(data):
    data = bytes(data)
    if len(data) < 7 or data[:3] != b'\x2f\xbb\xec':
        return None
    length = int.from_bytes(data[4:6], 'big')
    if length > 57 or len(data) < 7+length:
        return None
    if (sum(data[1:6+length]) & 255) != data[6+length]:
        raise RuntimeError('音箱回复校验失败')
    return data[3], data[6:6+length]

class PixelBar:
    def __enter__(self):
        matches = [d for d in hid.enumerate(0x2d99, 0xa160) if d['usage_page'] == 0xff14]
        if len(matches) != 1:
            raise RuntimeError('请通过 USB 连接一台 Halo PixelBar，并切换到声卡输入。')
        self.dev = hid.device()
        try:
            self.dev.open_path(matches[0]['path'])
            base = self.request(0x65)
            if len(base) != 4 or base[1] != 0 or int.from_bytes(base[2:], 'big') != 64:
                raise RuntimeError('这台设备的协议与已验证版本不同，已停止发送。')
            self.version = self.request(0xc6)
            if self.version != b'\x01\x01\x01':
                raise RuntimeError('当前仅验证固件 1.1.1；此设备版本尚未适配。')
        except Exception:
            self.dev.close()
            raise
        return self

    def __exit__(self, *args):
        self.dev.close()

    def send(self, command, payload=b''):
        if self.dev.write(packet(command, payload)) != 64:
            raise RuntimeError('USB 写入未完成')

    def receive(self, command, timeout=5):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            data = self.dev.read(64, 100)
            if data:
                parsed = parse_report(data)
                if parsed and parsed[0] == command:
                    return parsed[1]
        raise TimeoutError('音箱回复超时，请退出 TempoHub 后重试。')

    def request(self, command, payload=b''):
        self.send(command, payload)
        return self.receive(command)

    def upload(self, image, progress=lambda value: None):
        return self._upload_data(encode_image(image), 3, 6, progress)

    def upload_animation(self, frames, durations, progress=lambda value: None):
        return self._upload_data(encode_animation(frames, durations), 6, 52, progress)

    def _upload_data(self, data, category, option, progress):
        screen = self.request(0xee)
        if len(screen) < 9:
            raise RuntimeError('无法读取当前屏幕设置')
        ready = self.request(0x17, b'\x00\x00' + len(data).to_bytes(4, 'big'))
        if len(ready) < 4 or ready[:2] != b'\x00\x01':
            raise RuntimeError('音箱没有接受图案上传请求：' + ready.hex(' '))
        size = int.from_bytes(ready[2:4], 'big')
        if not 1 <= size <= 53:
            raise RuntimeError(f'音箱请求了尚未适配的分包长度：{size}')
        for index, offset in enumerate(range(0, len(data), size)):
            self.send(0x17, b'\x00\x02' + index.to_bytes(2, 'big') + data[offset:offset+size])
            progress(min((offset+size)/len(data), 1) * 0.95)
            if offset+size < len(data):
                reply = self.receive(0x17)
                if len(reply) < 2 or reply[:2] != b'\x00\x03':
                    raise RuntimeError('音箱拒绝了图案数据：' + reply.hex(' '))
        self.send(0x17, b'\x00\x04')
        deadline = time.monotonic()+5
        while time.monotonic() < deadline:
            reply = self.receive(0x17, max(0.1, deadline-time.monotonic()))
            if reply[:2] == b'\x00\x05':
                break
            if reply[:2] != b'\x00\x03':
                raise RuntimeError('音箱未确认图案校验成功：' + reply.hex(' '))
        else:
            raise TimeoutError('未收到音箱的最终接收确认')
        # Use scene slots whose respective static/animated file layouts were verified.
        selection = bytes([1]) + screen[1:5] + bytes([1, category, option, 255])
        self.request(0xef, selection)
        current = self.request(0xee)
        if len(current) < 8 or current[5:8] != bytes([1, category, option]):
            raise RuntimeError('图片已传输，但音箱未确认切换到图案场景。')
        progress(1)
        return current
