import queue
import threading
import sys
import tkinter as tk
from tkinter import ttk, filedialog, messagebox
from PIL import Image, ImageTk, ImageDraw, ImageFont
from pixelbar import prepare_image, load_asset, PixelBar

def demo_image():
    image = Image.new('RGB', (256, 32), 'black')
    draw = ImageDraw.Draw(image)
    font = ImageFont.truetype('C:/Windows/Fonts/consolab.ttf', 23)
    draw.text((7, 2), 'HELLO, PixelBar!', font=font, fill='white')
    draw.rectangle((0, 0, 255, 31), outline='white')
    return image

class App:
    def __init__(self, root):
        self.root = root
        root.title('PixelBar 自定义图片')
        root.geometry('860x490')
        root.resizable(False, False)
        self.events = queue.Queue()
        self.sources = [demo_image()]
        self.durations = [100]
        self.preview_timer = None
        self.preview_index = 0
        self.busy = False
        self.threshold = tk.IntVar(value=128)
        self.invert = tk.BooleanVar(value=False)
        self.fit = tk.StringVar(value='完整显示')
        self.status = tk.StringVar(value='请先选择图片，预览满意后点击“发送到音箱”。')
        self.name = tk.StringVar(value='示例：HELLO, PixelBar!')
        style = ttk.Style()
        style.theme_use('vista')
        style.configure('TLabel', font=('Microsoft YaHei UI', 10))
        style.configure('TButton', font=('Microsoft YaHei UI', 10), padding=7)
        body = ttk.Frame(root, padding=24)
        body.pack(fill='both', expand=True)
        ttk.Label(body, text='把自己的图片放上音箱', font=('Microsoft YaHei UI', 20, 'bold')).pack(anchor='w')
        ttk.Label(body, text='Halo PixelBar · 256 × 32 点阵 · 图片与循环动画', foreground='#626262').pack(anchor='w', pady=(5, 18))
        self.preview_label = tk.Label(body, bg='black', bd=8)
        self.preview_label.pack(fill='x')
        ttk.Label(body, textvariable=self.name).pack(anchor='w', pady=(8, 12))
        options = ttk.Frame(body)
        options.pack(fill='x')
        self.open_button = ttk.Button(options, text='选择图片…', command=self.choose)
        self.open_button.pack(side='left')
        box = ttk.Combobox(options, textvariable=self.fit, values=['完整显示', '铺满裁切', '拉伸填满'], state='readonly', width=10)
        box.pack(side='left', padx=12)
        box.bind('<<ComboboxSelected>>', lambda e: self.render())
        ttk.Checkbutton(options, text='反色', variable=self.invert, command=self.render).pack(side='left', padx=8)
        ttk.Label(options, text='亮度阈值').pack(side='left', padx=(14, 5))
        ttk.Scale(options, from_=1, to=254, variable=self.threshold, command=lambda v: self.render(), length=170).pack(side='left')
        ttk.Label(body, text='沿用音箱当前屏幕颜色。GIF 最多 8 帧循环播放，每帧至少 100 毫秒。', foreground='#626262').pack(anchor='w', pady=(15, 3))
        ttk.Label(body, text='发送会替换当前显示内容；可在 TempoHub 中重新选择官方图案。', foreground='#626262').pack(anchor='w')
        actions = ttk.Frame(body)
        actions.pack(fill='x', pady=(16, 7))
        self.send_button = ttk.Button(actions, text='发送到音箱', command=self.upload)
        self.send_button.pack(side='left')
        self.check_button = ttk.Button(actions, text='检测连接', command=self.check)
        self.check_button.pack(side='left', padx=10)
        self.bar = ttk.Progressbar(actions, maximum=100, length=330)
        self.bar.pack(side='right')
        ttk.Label(body, textvariable=self.status, wraplength=790).pack(anchor='w')
        self.render()
        root.after(100, self.poll)
        root.protocol('WM_DELETE_WINDOW', self.close)

    def render(self):
        if self.preview_timer is not None:
            self.root.after_cancel(self.preview_timer)
            self.preview_timer = None
        self.frames = [prepare_image(source, self.threshold.get(), self.invert.get(), {'完整显示':'contain','铺满裁切':'cover','拉伸填满':'stretch'}[self.fit.get()]) for source in self.sources]
        self.preview_index = 0
        self.show_frame()

    def show_frame(self):
        self.preview_timer = None
        index = self.preview_index
        self.photo = ImageTk.PhotoImage(self.frames[index].resize((768, 96), Image.Resampling.NEAREST))
        self.preview_label.configure(image=self.photo)
        if len(self.frames) > 1:
            self.preview_index = (index+1) % len(self.frames)
            self.preview_timer = self.root.after(self.durations[index], self.show_frame)

    def load_file(self, path):
        frames, durations = load_asset(path)
        self.sources, self.durations = frames, durations
        self.name.set(path + (f' · {len(frames)} 帧循环动画' if len(frames)>1 else ''))
        self.render()

    def choose(self):
        path = filedialog.askopenfilename(filetypes=[('图片', '*.png *.jpg *.jpeg *.bmp *.gif *.webp'), ('全部文件', '*.*')])
        if path:
            try:
                self.load_file(path)
            except Exception as error:
                messagebox.showerror('无法打开图片', str(error))

    def start(self, work):
        if self.busy:
            return
        self.busy = True
        for button in (self.send_button, self.check_button, self.open_button):
            button.configure(state='disabled')
        self.bar['value'] = 0
        self.status.set('正在连接音箱…')
        def run():
            try:
                with PixelBar() as device:
                    result = work(device)
                self.events.put(('done', result))
            except Exception as error:
                self.events.put(('error', str(error)))
        threading.Thread(target=run, daemon=True).start()

    def check(self):
        self.start(lambda device: '已连接 Halo PixelBar，固件 ' + '.'.join(map(str, device.version)))

    def upload(self):
        frames = [frame.copy() for frame in self.frames]
        durations = list(self.durations)
        def work(device):
            progress = lambda p: self.events.put(('progress', p))
            if len(frames)>1:
                device.upload_animation(frames, durations, progress)
            else:
                device.upload(frames[0], progress)
            return '音箱已确认接收并切换场景，请查看实际屏幕效果。'
        self.start(work)

    def poll(self):
        try:
            while True:
                event, value = self.events.get_nowait()
                if event == 'progress':
                    self.bar['value'] = value*100
                    self.status.set('正在发送图片…' if value < 1 else '正在确认屏幕状态…')
                else:
                    self.busy = False
                    for button in (self.send_button, self.check_button, self.open_button):
                        button.configure(state='normal')
                    self.status.set(value if event == 'done' else '发送或连接失败：' + value)
        except queue.Empty:
            pass
        self.root.after(100, self.poll)

    def close(self):
        if self.busy:
            self.status.set('正在与音箱通信，请等待完成后关闭。')
        else:
            self.root.destroy()

if __name__ == '__main__':
    root = tk.Tk()
    app = App(root)
    if len(sys.argv)>1:
        try:
            app.load_file(sys.argv[1])
        except Exception as error:
            messagebox.showerror('无法打开图片', str(error))
    root.mainloop()
