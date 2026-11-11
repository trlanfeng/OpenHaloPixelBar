import unittest
from unittest.mock import patch
from PIL import Image
from pixelbar import encode_image, encode_animation, decode_image, prepare_image, packet, parse_report, PixelBar

class ProtocolTests(unittest.TestCase):
    def test_animation_records_keep_frame_timing_and_global_index(self):
        frames = [Image.new('1', (256,32)), Image.new('1', (256,32), 1)]
        data = encode_animation(frames, [100,200])
        self.assertEqual(len(data), 2128)
        self.assertEqual(data[:5], bytes.fromhex('06 00 00 00 64'))
        self.assertEqual(data[1064:1070], bytes.fromhex('06 00 08 00 c8 ff'))
        self.assertEqual(data[-133:-128], bytes.fromhex('06 00 0f 00 c8'))
        with self.assertRaises(ValueError):
            encode_animation(frames, [100])
        with self.assertRaises(ValueError):
            encode_animation(frames, [0,100])
        with self.assertRaises(ValueError):
            encode_animation(frames*5, [100]*10)

    def test_bit_order_and_record_boundaries(self):
        image = Image.new('1', (256, 32))
        for point in [(0, 0), (127, 7), (128, 8), (255, 31)]:
            image.putpixel(point, 1)
        data = encode_image(image)
        self.assertEqual(len(data), 1064)
        self.assertEqual(data[:6], bytes.fromhex('03 00 00 ff ff 01'))
        self.assertEqual(data[5+127], 128)
        self.assertEqual(data[3*133:3*133+6], bytes.fromhex('03 00 03 ff ff 01'))
        self.assertEqual(data[-1], 128)
        self.assertEqual(decode_image(data).tobytes(), image.tobytes())

    def test_known_wire_report(self):
        self.assertEqual(packet(0x65)[:7], bytes.fromhex('2e aa ec 65 00 00 fb'))
        response = bytes.fromhex('2f bb ec 65 00 04 02 00 00 40 52')
        self.assertEqual(parse_report(response), (0x65, bytes.fromhex('02 00 00 40')))
        with self.assertRaises(RuntimeError):
            parse_report(response[:-1]+b'\x00')
        self.assertIsNone(parse_report(response[:8]))
        with self.assertRaises(ValueError):
            packet(0x17, bytes(58))

    def test_alpha_is_black_and_proportions_preserved(self):
        image = prepare_image(Image.new('RGBA', (32, 32), (255,255,255,255)))
        self.assertEqual(image.getbbox(), (112,0,144,32))
        image = prepare_image(Image.new('RGBA', (256,32), (255,255,255,0)))
        self.assertIsNone(image.getbbox())

    def test_upload_requires_final_ack(self):
        device = PixelBar()
        state = bytes.fromhex('00 00 96 04 00 01 03 06 ff 01 00 00')
        with patch.object(device, 'request', side_effect=[state, b'\x00\x01\x00\x32']), \
             patch.object(device, 'send'), \
             patch.object(device, 'receive', side_effect=[b'\x00\x03']*21+[b'\x00\x07']):
            with self.assertRaisesRegex(RuntimeError, '校验'):
                device.upload(Image.new('1',(256,32)))

    def test_upload_requires_matching_screen_state(self):
        device = PixelBar()
        state = bytes.fromhex('00 00 96 04 00 01 08 02 ff 01 00 00')
        with patch.object(device, 'request', side_effect=[state,b'\x00\x01\x00\x32',b'',state]), \
             patch.object(device, 'send'), \
             patch.object(device, 'receive', side_effect=[b'\x00\x03']*21+[b'\x00\x05']):
            with self.assertRaisesRegex(RuntimeError, '切换'):
                device.upload(Image.new('1',(256,32)))

if __name__ == '__main__':
    unittest.main()
