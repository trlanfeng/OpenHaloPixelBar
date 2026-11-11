// Package branding contains the application's name, icons and shared notices.
package branding

import _ "embed"

const Name = "OpenPixelBar"
const Version = "0.2.0"
const Disclaimer = "OpenPixelBar 是独立开发的非官方工具，与 EDIFIER / 花再无隶属关系。请仅使用你有权使用的素材。使用本程序可能导致显示异常、设置丢失或音箱损坏；在适用法律允许的范围内，因使用本程序造成音箱损坏或其他损失，作者及贡献者概不负责，风险由使用者自行承担。目前仅验证 Halo PixelBar 固件 1.1.1。"

//go:embed icon.png
var IconPNG []byte
