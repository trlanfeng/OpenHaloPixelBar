"""Send only documented-in-client, read-only identification queries."""
import json
import time
from pathlib import Path
import hid

def packet(command, payload=b'', family=0xec):
    frame = bytes([0x2e, 0xaa, family, command]) + len(payload).to_bytes(2, 'big') + payload
    frame += bytes([sum(frame[1:]) & 255])
    if len(frame) > 64:
        raise ValueError('Report too long')
    return frame.ljust(64, b'\0')

def main():
    records = []
    for info in hid.enumerate(0x2d99, 0xa160):
        if info['usage_page'] != 0xff14:
            continue
        dev = hid.device()
        try:
            dev.open_path(info['path'])
            for family, cmd in [(0xec, 0x65), (0xec, 0xc6), (0xec, 0xd8), (0xec, 0xee), (0xed, 0x01)]:
                dev.write(packet(cmd, family=family))
                replies = []
                until = time.monotonic() + 1
                while time.monotonic() < until:
                    data = dev.read(64, 150)
                    if data:
                        replies.append(bytes(data).hex(' '))
                record = {'family': hex(family), 'command': hex(cmd), 'replies': replies}
                records.append(record)
                print(record)
        finally:
            dev.close()
    Path('research/probe.json').write_text(json.dumps(records, indent=2), encoding='utf-8')

if __name__ == '__main__':
    main()
