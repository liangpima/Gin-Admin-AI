package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/big"
	"time"

	"go-admin/internal/cache"
	"go-admin/internal/logger"
	"go-admin/internal/module/captcha/model"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	captchaPrefix = "captcha:"
	captchaExpiry = 5 * time.Minute

	// bgWidth / bgHeight 出图尺寸，保持 3.2:1 比例。
	//
	// 这两个值直接决定手机上的可读性与可点性：前端按容器宽度等比缩放，
	// 原图越宽、缩放比越小。640 宽在手机上要缩到约 0.51 倍，字符墨迹只剩
	// 12x18px，命中区也只有约 30px（低于 44px 的可点下限），点起来很费劲。
	// 收敛到 480x150 后缩放比约 0.68，配合放大字号与容差，
	// 手机上墨迹约 20x31px、命中区约 54px。
	//
	// 比例保持不变是刻意的：前端用 aspect-ratio 占位，
	// 改比例会让占位高度与实际出图不符（弹窗高度会跳）。
	bgWidth  = 480
	bgHeight = 150

	charCount = 3

	// charScale 字符放大倍数。Face7x13 是 7x13 的点阵字体，
	// 放大 5 倍后墨迹约 30x45（原来放大 4 倍只有 24x36，手机上偏小）。
	charScale = 5

	// pointMarginX / pointMarginY 目标点距画布边缘的最小距离。
	// 必须留出半个字符墨迹（约 15x23），否则字符会被画到画布外而缺角。
	pointMarginX = 45
	pointMarginY = 30

	// tolerancePx 校验时允许的点击偏差（原图像素）。
	//
	// 手机上的实际命中区 = 2 * tolerancePx * 显示缩放比，取值需保证
	// 缩放后仍不小于 44px（移动端可点最小尺寸）：
	// 2 * 40 * 0.68 ≈ 54px ✓。原值 30 在手机上只有约 30px，很容易点不中。
	tolerancePx = 40

	// minPointGap 两个目标点之间的最小间距（矩形避让）。
	//
	// 必须大于 2*tolerancePx：校验时以目标点为中心、±tolerancePx 的矩形是
	// 命中区，间距不足会让两个命中区重叠 —— 用户点在 A 附近却被判成命中 B，
	// 表现为「明明点对了却验证失败」。
	// 90 同时大于字符墨迹（30x45），保证两个字符视觉上也不重叠。
	minPointGap = 90

	// maxPlaceAttempts 单个目标点的最大重试次数。
	//
	// 必须有上限：拒采样在空间紧张时可能连续失败，无上限就成了
	// 「生成验证码把 CPU 占满」的隐患。宁可返回错误让用户重试一次。
	// 画布收敛后可用位置变少（第三个点最差只有约 8% 的成功率），
	// 故上限提到 500，把整体失败概率压到 1e-18 量级。
	maxPlaceAttempts = 500
)

var charPool = []rune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789")

type CaptchaService interface {
	Generate() (*model.CaptchaGenerateResponse, error)
	Verify(token string, points []model.Point) (*model.CaptchaVerifyResponse, error)
}

type captchaService struct{}

type captchaData struct {
	Points []model.Point `json:"points"`
	Chars  string        `json:"chars"`
}

func NewCaptchaService() CaptchaService {
	return &captchaService{}
}

func (s *captchaService) Generate() (*model.CaptchaGenerateResponse, error) {
	chars, err := s.randomChars(charCount)
	if err != nil {
		return nil, err
	}

	points, err := s.randomPoints(charCount)
	if err != nil {
		return nil, err
	}

	bgImg := s.generateBackground(chars, points)
	bgBase64 := imageToBase64(bgImg)

	token := generateToken()

	data := captchaData{Points: points, Chars: chars}
	dataBytes, _ := json.Marshal(data)
	if err := cache.Set(context.Background(), captchaPrefix+token, string(dataBytes), captchaExpiry); err != nil {
		return nil, fmt.Errorf("缓存验证码失败: %w", err)
	}

	return &model.CaptchaGenerateResponse{
		Token:    token,
		Bg:       "data:image/png;base64," + bgBase64,
		BgWidth:  bgWidth,
		BgHeight: bgHeight,
		Chars:    chars,
	}, nil
}

func (s *captchaService) Verify(token string, points []model.Point) (*model.CaptchaVerifyResponse, error) {
	key := captchaPrefix + token
	val, err := cache.Get(context.Background(), key)
	if err != nil {
		return &model.CaptchaVerifyResponse{
			Success: false,
			Message: "验证码已过期，请重新获取",
		}, nil
	}

	cache.Del(context.Background(), key)

	var data captchaData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return &model.CaptchaVerifyResponse{
			Success: false,
			Message: "验证码数据异常",
		}, nil
	}

	if len(points) != len(data.Points) {
		return &model.CaptchaVerifyResponse{
			Success: false,
			Message: "点击数量不正确",
		}, nil
	}

	for i, p := range points {
		expected := data.Points[i]
		if absInt(p.X-expected.X) > tolerancePx || absInt(p.Y-expected.Y) > tolerancePx {
			return &model.CaptchaVerifyResponse{
				Success: false,
				Message: "验证失败，请重试",
			}, nil
		}
	}

	newToken := generateToken()

	// 记录「该 token 已通过人机校验」。
	//
	// 校验结果必须落盘，否则前端拿到的 newToken 只是一个无意义的随机串 ——
	// 登录接口无从判断它是否真的通过过验证，攻击者直接 POST /auth/login
	// 就能完全绕过验证码，人机校验形同虚设。
	if err := cache.Set(context.Background(), verifiedKey(newToken), "1", captchaExpiry); err != nil {
		return &model.CaptchaVerifyResponse{
			Success: false,
			Message: "验证状态保存失败，请重试",
		}, nil
	}

	return &model.CaptchaVerifyResponse{
		Success: true,
		Token:   newToken,
		Message: "验证成功",
	}, nil
}

func verifiedKey(token string) string {
	return captchaPrefix + "verified:" + token
}

// ConsumeVerifiedToken 消费一次性的人机校验凭证。
//
// 登录接口调用：凭证存在则删除并返回 true（一次性，防止重放），
// 不存在说明未通过验证或已用过，返回 false。
func ConsumeVerifiedToken(token string) bool {
	if token == "" {
		return false
	}
	ctx := context.Background()
	key := verifiedKey(token)

	exists, err := cache.Exists(ctx, key)
	if err != nil || !exists {
		return false
	}

	// 删除失败时返回 false（fail-closed），而不是放行：
	// 凭证是一次性的，删不掉就意味着它还能被复用（TTL 内），
	// 那「一次验证一次登录」的保证就破了。
	// 代价只是让用户重做一次验证码，比留一个可复用的凭证划算。
	if err := cache.Del(ctx, key); err != nil {
		logger.Log.Errorf("[captcha] 一次性凭证删除失败，已拒绝本次消费: key=%s err=%v", key, err)
		return false
	}
	return true
}

// randomChars 从字符池中不重复地随机抽取 n 个字符。
//
// 必须保证互不相同：本验证码是「按提示顺序依次点击」，
// 一旦出现重复字符（如 ABA），图上会存在两个相同的 A，
// 用户无法分辨应先点击哪一个，只能靠猜，会直接导致验证失败。
func (s *captchaService) randomChars(n int) (string, error) {
	if n > len(charPool) {
		return "", fmt.Errorf("验证码长度 %d 超过字符池容量 %d", n, len(charPool))
	}

	// 复制字符池后做 Fisher-Yates 洗牌，取前 n 个即为不放回抽样结果
	pool := make([]rune, len(charPool))
	copy(pool, charPool)

	for i := len(pool) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		k := j.Int64()
		pool[i], pool[k] = pool[k], pool[i]
	}

	return string(pool[:n]), nil
}

// randomPoints 生成 n 个互不重叠的目标点。
//
// 采用「拒采样 + 重试上限」：随机取点，与已放置的点做矩形避让检测，
// 冲突就重取。此前的实现有两个真实缺陷 —— 重取时把 randInt 的错误丢掉了
// （`x, _ = randInt(...)`），且外层用 `j = -1` 重来，**没有重试上限**，
// 空间紧张时会一直转下去。现在空间不足直接返回错误：让用户重新点一次
// 「换一张」，远好过请求挂住。
func (s *captchaService) randomPoints(n int) ([]model.Point, error) {
	points := make([]model.Point, 0, n)
	for i := 0; i < n; i++ {
		placed := false
		for attempt := 0; attempt < maxPlaceAttempts; attempt++ {
			x, err := randInt(pointMarginX, bgWidth-pointMarginX)
			if err != nil {
				return nil, fmt.Errorf("生成目标点横坐标失败: %w", err)
			}
			y, err := randInt(pointMarginY, bgHeight-pointMarginY)
			if err != nil {
				return nil, fmt.Errorf("生成目标点纵坐标失败: %w", err)
			}
			if hasCollision(x, y, points) {
				continue
			}
			points = append(points, model.Point{X: x, Y: y})
			placed = true
			break
		}
		if !placed {
			return nil, fmt.Errorf(
				"放置第 %d 个目标点失败：%d 次尝试内找不到与已有 %d 个点互不冲突的位置",
				i+1, maxPlaceAttempts, len(points))
		}
	}
	return points, nil
}

// hasCollision 判断候选点与已放置点是否冲突。
//
// 用矩形判定而不是圆形：字符按矩形墨迹绘制，矩形判定与视觉一致；
// 圆形判定会在对角线方向放过「看起来仍然挨在一起」的点。
func hasCollision(x, y int, placed []model.Point) bool {
	for _, p := range placed {
		if absInt(x-p.X) < minPointGap && absInt(y-p.Y) < minPointGap {
			return true
		}
	}
	return false
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (s *captchaService) generateBackground(chars string, points []model.Point) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, bgWidth, bgHeight))

	for y := 0; y < bgHeight; y++ {
		for x := 0; x < bgWidth; x++ {
			r := uint8(220 + (x*7+y*3)%36)
			g := uint8(220 + (x*3+y*7)%36)
			b := uint8(220 + (x*5+y*5)%36)
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}

	// 干扰线与噪点的数量随画布面积等比缩放。
	// 画布缩小后若数量不变，密度会上升约 1.8 倍，字符反而更难辨认 ——
	// 干扰的目的是防机器识别，不该把真人一起干扰掉。
	for i := 0; i < 12; i++ {
		sx, _ := randInt(0, bgWidth)
		sy, _ := randInt(0, bgHeight)
		ex, _ := randInt(0, bgWidth)
		ey, _ := randInt(0, bgHeight)
		cr := uint8(150 + i*4)
		cg := uint8(150 + i*3)
		cb := uint8(150 + i*2)
		s.drawLine(img, sx, sy, ex, ey, color.RGBA{R: cr, G: cg, B: cb, A: 120})
	}

	for i := 0; i < 30; i++ {
		px, _ := randInt(0, bgWidth)
		py, _ := randInt(0, bgHeight)
		img.SetRGBA(px, py, color.RGBA{
			R: uint8(100 + i*2),
			G: uint8(100 + i*2),
			B: uint8(100 + i*2),
			A: 180,
		})
	}

	for i, ch := range chars {
		if i < len(points) {
			s.drawChar(img, points[i].X, points[i].Y, ch)
		}
	}

	return img
}

func (s *captchaService) drawChar(img *image.RGBA, x, y int, ch rune) {
	scale := charScale
	face := basicfont.Face7x13

	// 先把字符绘制到临时画布上，便于测量其真实墨迹范围
	charImg := image.NewRGBA(image.Rect(0, 0, 12, 18))
	charDrawer := &font.Drawer{
		Dst:  charImg,
		Src:  image.NewUniform(color.RGBA{R: 255, G: 255, B: 255, A: 255}),
		Face: face,
		Dot:  fixed.P(2, 13),
	}
	charDrawer.DrawString(string(ch))

	// 求墨迹包围盒。字形并未填满 12x18 画布，且不同字符范围不同，
	// 因此必须按实际墨迹居中，否则字符会整体偏离目标点。
	minX, minY, maxX, maxY := 12, 18, -1, -1
	for dy := 0; dy < 18; dy++ {
		for dx := 0; dx < 12; dx++ {
			if _, _, _, a := charImg.At(dx, dy).RGBA(); a != 0 {
				if dx < minX {
					minX = dx
				}
				if dx > maxX {
					maxX = dx
				}
				if dy < minY {
					minY = dy
				}
				if dy > maxY {
					maxY = dy
				}
			}
		}
	}
	if maxX < 0 {
		return // 空白字形，理论上不会出现
	}

	// 让墨迹中心正好落在目标点上，保证用户点击字符中心即可命中 Verify 的容差
	offsetX := x - (minX+maxX+1)*scale/2
	offsetY := y - (minY+maxY+1)*scale/2

	for dy := minY; dy <= maxY; dy++ {
		for dx := minX; dx <= maxX; dx++ {
			if _, _, _, a := charImg.At(dx, dy).RGBA(); a == 0 {
				continue
			}
			for sy := 0; sy < scale; sy++ {
				for sx := 0; sx < scale; sx++ {
					px := offsetX + dx*scale + sx
					py := offsetY + dy*scale + sy
					if px >= 0 && px < bgWidth && py >= 0 && py < bgHeight {
						img.SetRGBA(px, py, color.RGBA{R: 10, G: 40, B: 100, A: 255})
					}
				}
			}
		}
	}
}

func (s *captchaService) drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx - dy

	for {
		if x0 >= 0 && x0 < bgWidth && y0 >= 0 && y0 < bgHeight {
			img.SetRGBA(x0, y0, c)
		}
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

func imageToBase64(img image.Image) string {
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func randInt(min, max int) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()) + min, nil
}
