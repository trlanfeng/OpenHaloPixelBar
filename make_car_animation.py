"""Draw an original, seamlessly looping 256x32 driving animation for PixelBar."""
from pathlib import Path
from PIL import Image, ImageDraw

def car_frames():
    frames = []
    for frame in range(8):
        image = Image.new('1', (256, 32))
        d = ImageDraw.Draw(image)
        # Distant hills and clouds form a quiet backdrop for the moving road.
        d.line([(0,20),(12,16),(21,18),(36,8),(53,20),(66,14),(81,22)], fill=1)
        d.line([(173,22),(189,12),(203,19),(222,7),(242,20),(255,15)], fill=1)
        d.line([(31,13),(36,8),(42,13)], fill=1)
        d.line([(217,12),(222,7),(228,12)], fill=1)
        d.line([(62,5),(67,5),(69,3),(76,3),(78,5),(83,5)], fill=1)
        d.line([(169,5),(173,5),(175,3),(181,3),(183,5),(188,5)], fill=1)
        d.line((0,25,255,25), fill=1)
        # 32 px spacing, 4 px/frame: last-to-first motion is seamless.
        for x in range(-32,288,32):
            x -= frame*4
            d.line((x,30,x+13,30), fill=1)

        x = 93 + [0,0,1,1,0,0,-1,-1][frame]
        bounce = [0,0,1,1,0,0,1,1][frame]
        y = 5 + bounce
        # Compact coupe, nose pointing right. The opaque body covers the scenery.
        body = [(x,y+9),(x+8,y+8),(x+17,y+1),(x+39,y+1),
                (x+48,y+8),(x+62,y+9),(x+66,y+12),(x+66,y+17),
                (x,y+17)]
        d.polygon(body, fill=0, outline=1)
        d.polygon([(x+13,y+8),(x+19,y+3),(x+27,y+3),(x+27,y+8)],fill=1)
        d.polygon([(x+30,y+3),(x+38,y+3),(x+44,y+8),(x+30,y+8)],fill=1)
        d.line((x+29,y+10,x+29,y+15),fill=1)
        d.line((x+34,y+11,x+38,y+11),fill=1)
        d.line((x+49,y+11,x+59,y+11),fill=1)
        d.rectangle((x+62,y+10,x+65,y+12),fill=1)
        d.line((x+1,y+11,x+3,y+11),fill=1)
        for wx in [x+13,x+53]:
            d.rectangle((wx-5,y+13,wx+5,y+19),fill=0)
            d.ellipse((wx-4,y+12,wx+4,y+20),fill=0,outline=1)
            cy=y+16
            if frame % 2:
                d.line((wx-2,cy-2,wx+2,cy+2),fill=1)
                d.line((wx-2,cy+2,wx+2,cy-2),fill=1)
            else:
                d.line((wx-2,cy,wx+2,cy),fill=1)
                d.line((wx,cy-2,wx,cy+2),fill=1)
        # Short speed lines and a changing little exhaust puff.
        for sx,sy,length in [(x-20,y+11,9),(x-15,y+15,7)]:
            d.line((sx-frame%3,sy,sx+length-frame%3,sy),fill=1)
        d.point((x-6-frame%3,y+18),fill=1)
        frames.append(image)
    return frames

def main():
    out = Path(__file__).parent / 'dist' / 'animations'
    out.mkdir(parents=True,exist_ok=True)
    frames = car_frames()
    frames[0].save(out/'car-on-road.gif',save_all=True,append_images=frames[1:],
                   duration=100,loop=0,optimize=False,disposal=2)
    previews = [frame.resize((1024,128),Image.Resampling.NEAREST) for frame in frames]
    previews[0].save(out/'car-on-road-preview.gif',save_all=True,append_images=previews[1:],
                    duration=100,loop=0,optimize=False,disposal=2)
    sheet=Image.new('1',(768,96*4))
    for i in range(4):
        sheet.paste(frames[i*2].resize((768,96),Image.Resampling.NEAREST),(0,i*96))
    sheet.save(Path(__file__).parent/'research'/'car-contact-sheet.png')
    print(out/'car-on-road.gif')

if __name__=='__main__':
    main()
