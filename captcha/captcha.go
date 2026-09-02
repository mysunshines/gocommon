package captcha

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/big"
	"time"

	"github.com/mysunshines/gocommon/cache"
)

const (
	// keyPrefix 验证码在 Redis 中的键前缀（会被 cache 包再叠加服务级 KeyPrefix）。
	keyPrefix = "captcha:"
	// defaultTTL 验证码有效期，过期作废，防止无限期可用。
	defaultTTL = 5 * time.Minute
	// captchaLen 验证码字符长度。
	captchaLen = 4
	// chars 验证码字符集：去除易混淆的 0/O/1/I/L 等，降低误识别。
	chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

// Generate 生成一张随机验证码图片，并将答案存入 Redis（TTL 5min）。
// 返回验证码 ID（前端需回传用于校验）与 base64 PNG 图片（含 data URI 前缀）。
func Generate(ctx context.Context) (id string, base64PNG string, err error) {
	code := randCode()
	id = randID()
	if err := cache.Set(ctx, keyPrefix+id, code, defaultTTL); err != nil {
		return "", "", fmt.Errorf("captcha store failed: %w", err)
	}
	pngBytes, err := draw(code)
	if err != nil {
		return "", "", err
	}
	base64PNG = "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	return id, base64PNG, nil
}

// Verify 校验验证码。大小写不敏感；校验成功后该验证码立即作废（一次性），
// 防止同一验证码被重复使用。id 或 input 为空直接返回 false。
func Verify(ctx context.Context, id, input string) (bool, error) {
	if id == "" || input == "" {
		return false, nil
	}
	stored, err := cache.Get(ctx, keyPrefix+id)
	if err != nil || stored == "" {
		return false, err
	}
	ok := equalFold(stored, input)
	// 无论成功失败都删除，避免重放
	_ = cache.Delete(ctx, keyPrefix+id)
	return ok, nil
}

func randCode() string {
	n := big.NewInt(int64(len(chars)))
	b := make([]byte, captchaLen)
	for i := range b {
		idx, err := rand.Int(rand.Reader, n)
		if err != nil {
			// 退化到固定字符，极低概率
			b[i] = chars[i%len(chars)]
			continue
		}
		b[i] = chars[idx.Int64()]
	}
	return string(b)
}

func randID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	const hexd = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexd[v>>4]
		out[i*2+1] = hexd[v&0x0f]
	}
	return string(out)
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if ca >= 'a' && ca <= 'z' {
			ca -= 'a' - 'A'
		}
		if cb >= 'a' && cb <= 'z' {
			cb -= 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// draw 用标准库生成带干扰线/噪点的验证码 PNG，无需任何第三方依赖。
func draw(code string) ([]byte, error) {
	const width, height = 120, 40
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// 背景：浅色随机底
	bg := color.RGBA{R: 245, G: 245, B: 245, A: 255}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, bg)
		}
	}

	// 干扰线
	for i := 0; i < 4; i++ {
		c := randColor(false)
		x0 := randInt(width)
		y0 := randInt(height)
		x1 := randInt(width)
		y1 := randInt(height)
		drawLine(img, x0, y0, x1, y1, c)
	}

	// 字符：每个字符随机颜色、基线抖动，宽度均分
	step := width / len(code)
	for i, ch := range code {
		c := randColor(true)
		bx := i*step + step/2
		by := height/2 + randInt(8) - 4
		drawChar(img, ch, bx, by, c)
	}

	// 噪点
	for i := 0; i < 60; i++ {
		img.Set(randInt(width), randInt(height), randColor(false))
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func randInt(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

func randColor(dark bool) color.RGBA {
	if dark {
		return color.RGBA{
			R: uint8(randInt(156) + 40),
			G: uint8(randInt(156) + 40),
			B: uint8(randInt(156) + 40),
			A: 255,
		}
	}
	return color.RGBA{
		R: uint8(randInt(200) + 55),
		G: uint8(randInt(200) + 55),
		B: uint8(randInt(200) + 55),
		A: 255,
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx, sy := 1, 1
	if x0 >= x1 {
		sx = -1
	}
	if y0 >= y1 {
		sy = -1
	}
	err := dx - dy
	for {
		img.Set(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// drawChar 用简单像素点阵绘制一个字符（无字体依赖）。
// 这里采用 5x7 点阵字模覆盖 A-Z 与 2-9，未覆盖字符跳过（极少发生）。
func drawChar(img *image.RGBA, ch rune, bx, by int, c color.RGBA) {
	font, ok := font5x7[normalize(ch)]
	if !ok {
		return
	}
	for row := 0; row < 7; row++ {
		bits := font[row]
		for col := 0; col < 5; col++ {
			if bits&(1<<(4-col)) != 0 {
				px, py := bx+col-2, by+row-3
				if px >= 0 && py >= 0 && px < img.Bounds().Dx() && py < img.Bounds().Dy() {
					img.Set(px, py, c)
				}
			}
		}
	}
}

func normalize(ch rune) rune {
	if ch >= 'a' && ch <= 'z' {
		return ch - ('a' - 'A')
	}
	return ch
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// font5x7 5x7 点阵字模：覆盖验证码字符集（A-Z 2-9，去除了易混淆项）。
var font5x7 = map[rune][7]byte{
	'A': {0x0F, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11},
	'B': {0x1E, 0x11, 0x11, 0x1E, 0x11, 0x11, 0x1E},
	'C': {0x0E, 0x11, 0x10, 0x10, 0x10, 0x11, 0x0E},
	'D': {0x1E, 0x11, 0x11, 0x11, 0x11, 0x11, 0x1E},
	'E': {0x1F, 0x10, 0x10, 0x1E, 0x10, 0x10, 0x1F},
	'F': {0x1F, 0x10, 0x10, 0x1E, 0x10, 0x10, 0x10},
	'G': {0x0E, 0x11, 0x10, 0x17, 0x11, 0x11, 0x0F},
	'H': {0x11, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11},
	'J': {0x07, 0x02, 0x02, 0x02, 0x02, 0x12, 0x0C},
	'K': {0x11, 0x12, 0x14, 0x18, 0x14, 0x12, 0x11},
	'L': {0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x1F},
	'M': {0x11, 0x1B, 0x15, 0x15, 0x11, 0x11, 0x11},
	'N': {0x11, 0x19, 0x15, 0x15, 0x13, 0x11, 0x11},
	'P': {0x1E, 0x11, 0x11, 0x1E, 0x10, 0x10, 0x10},
	'Q': {0x0E, 0x11, 0x11, 0x11, 0x15, 0x12, 0x0D},
	'R': {0x1E, 0x11, 0x11, 0x1E, 0x14, 0x12, 0x11},
	'S': {0x0F, 0x10, 0x10, 0x0E, 0x01, 0x01, 0x1E},
	'U': {0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E},
	'V': {0x11, 0x11, 0x11, 0x11, 0x11, 0x0A, 0x04},
	'W': {0x11, 0x11, 0x11, 0x15, 0x15, 0x1B, 0x11},
	'X': {0x11, 0x11, 0x0A, 0x04, 0x0A, 0x11, 0x11},
	'Y': {0x11, 0x11, 0x0A, 0x04, 0x04, 0x04, 0x04},
	'Z': {0x1F, 0x01, 0x02, 0x04, 0x08, 0x10, 0x1F},
	'2': {0x1E, 0x01, 0x01, 0x0E, 0x10, 0x10, 0x1F},
	'3': {0x1E, 0x01, 0x01, 0x0E, 0x01, 0x01, 0x1E},
	'4': {0x11, 0x11, 0x11, 0x1F, 0x01, 0x01, 0x01},
	'5': {0x1F, 0x10, 0x10, 0x1E, 0x01, 0x01, 0x1E},
	'6': {0x0E, 0x10, 0x10, 0x1E, 0x11, 0x11, 0x0E},
	'7': {0x1F, 0x01, 0x02, 0x04, 0x04, 0x04, 0x04},
	'8': {0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E},
	'9': {0x0E, 0x11, 0x11, 0x0F, 0x01, 0x01, 0x0E},
}
