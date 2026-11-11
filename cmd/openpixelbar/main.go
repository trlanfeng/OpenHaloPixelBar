package main

import (
	"flag"
	"fmt"
	"openpixelbar/internal/pixelbar"
	"openpixelbar/internal/studio"
	"os"
)

func main() {
	check := flag.Bool("check", false, "检测 USB 连接后退出")
	upload := flag.String("upload", "", "发送指定图片或 GIF 后退出")
	flag.Parse()
	if *check || *upload != "" {
		d, e := pixelbar.Connect()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		defer d.Close()
		if *upload != "" {
			a, e := pixelbar.Load(*upload)
			if e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
			var frames []pixelbar.Frame
			for _, f := range a.Frames {
				frames = append(frames, pixelbar.Prepare(f, pixelbar.Options{Threshold: 128, Fit: "完整显示"}))
			}
			if e = d.Upload(frames, a.Delays, nil); e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
			fmt.Println("音箱已确认接收与切换图案")
		} else {
			fmt.Println("Halo PixelBar · 固件 1.1.1 · USB 连接正常")
		}
		return
	}
	path := ""
	if flag.NArg() > 0 {
		path = flag.Arg(0)
	}
	if e := studio.Run(path); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
