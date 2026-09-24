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
	"strings"
	"time"

	"go-admin/internal/cache"
	"go-admin/internal/common"
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

	// captchaRatePrefix 生成接口的限流键前缀
	captchaRatePrefix = captchaPrefix + "rate:ip:"
	// captchaRateLimit 单个 IP 在 captchaRateWindow 内允许的生成次数。
	//
	// 为什么必须限流：chars 是「请依次点击 X Y Z」的提示语，必须下发给前端
	// （前端要靠它渲染与计数，坐标只存服务端），因此脚本拿到 chars + bg 后
	// 可以离线做模板匹配、免人工点选。限流不能让人工点击重新变得必要，
	// 但能把「批量刷验证码」的成本抬到需要真实 IP 资源的量级。
	//
	// 取 10 是「远高于真人需求、远低于脚本收益」的值：真人失败几次就刷新一次，
	// 一分钟内很难超过 10 次；而撞库脚本每秒就需要数百张图。
	//
	// ⚠️ 部署在反向代理之后时，ClientIP 可能是代理自身地址，本限流会退化成
	// 全站共用额度 —— 必须在 server.trusted_proxies 中填上代理网段。
	captchaRateLimit = 10
	// captchaRateWindow 限流窗口长度
	captchaRateWindow = time.Minute
)

var charPool = []rune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789")

type CaptchaService interface {
	// Generate 生成一张验证码图。clientIP 用于生成频率限流，
	// 由 Controller 从请求上下文采集（Service 不依赖 gin）。
	Generate(clientIP string) (*model.CaptchaGenerateResponse, error)
	// Verify 校验点击结果。clientIP 必须与生成时一致，否则视为凭证被转手。
	Verify(clientIP, token string, points []model.Point) (*model.CaptchaVerifyResponse, error)
}

type captchaService struct{}

type captchaData struct {
	Points []model.Point `json:"points"`
	Chars  string        `json:"chars"`
	// IP 生成该验证码的客户端地址，用于把凭证绑死在同一个客户端上。
	// 老数据（本字段为空）不做拦截，避免升级瞬间让在途验证码全部失效。
	IP string `json:"ip,omitempty"`
}

func NewCaptchaService() CaptchaService {
	return &captchaService{}
}

func (s *captchaService) Generate(clientIP string) (*model.CaptchaGenerateResponse, error) {
	if err := checkGenerateRate(clientIP); err != nil {
		return nil, err
	}

	chars, err := s.randomChars(charCount)
	if err != nil {
		return nil, err
	}

	points, err := s.randomPoints(charCount)
	if err != nil {
		return nil, err
	}

	bgImg := s.generateBackground(chars, points)
	bgBase64, err := imageToBase64(bgImg)
	if err != nil {
		return nil, err
	}

	token := generateToken()

	data := captchaData{Points: points, Chars: chars, IP: clientIP}
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

// checkGenerateRate 按客户端 IP 限制生成频率。
//
// 失败方向是 fail-closed：限流设施不可用时拒绝生成，而不是放行。
// 这里不会引入新的不可用场景 —— Generate 紧接着就要往 Redis 写验证码数据，
// Redis 真的不可用时它本来就会失败；反过来说，若此时放行，攻击者只要让计数
// 这一步失败就能无限刷图。
func checkGenerateRate(clientIP string) error {
	if clientIP == "" {
		// 取不到 IP 时不计数（否则所有取不到 IP 的请求会共用一个键而互相拖累），
		// 但仍留下日志：正常情况下 ClientIP 一定有值。
		logger.Log.Warnf("[captcha] 生成验证码时未取到客户端 IP，已跳过频率限制")
		return nil
	}

	count, err := cache.IncrWindow(context.Background(), captchaRatePrefix+clientIP, captchaRateWindow)
	if err != nil {
		logger.Log.Errorf("[captcha] 生成频率计数失败，按 fail-closed 拒绝: ip=%s err=%v", clientIP, err)
		return fmt.Errorf("验证码服务暂不可用: %w", err)
	}
	if count > int64(captchaRateLimit) {
		logger.Log.Warnf("[captcha] 验证码生成过于频繁，已拒绝: ip=%s count=%d", clientIP, count)
		return common.NewBizError("请求过于频繁，请稍后再试")
	}
	return nil
}

func (s *captchaService) Verify(clientIP, token string, points []model.Point) (*model.CaptchaVerifyResponse, error) {
	key := captchaPrefix + token
	val, err := cache.Get(context.Background(), key)
	if err != nil {
		return &model.CaptchaVerifyResponse{
			Success: false,
			Message: "验证码已过期，请重新获取",
		}, nil
	}

	// 先作废再校验：无论成功失败，本次点击结果都只能用一次。
	//
	// 不这样做的话，token 在 TTL（5 分钟）内可以反复提交 ——
	// 攻击者可以对同一张图穷举点击坐标，把「3 个点、每个点 ±40px」
	// 的搜索空间摊薄成多次尝试，一次通过的代价大幅下降。
	// 作废失败必须留痕：删不掉就意味着该 token 在 TTL（5 分钟）内
	// 仍可重复提交，「一次验证一次提交」的保证被破 —— 这是可被利用的窗口，
	// 而不是「少了一次清理」这种无关紧要的事。
	if err := cache.Del(context.Background(), key); err != nil {
		logger.Log.Errorf("[captcha] 验证码作废失败，该 token 在 TTL 内仍可重复提交: key=%s err=%v", key, err)
	}

	var data captchaData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return &model.CaptchaVerifyResponse{
			Success: false,
			Message: "验证码数据异常",
		}, nil
	}

	// 凭证必须与生成它的客户端同源。
	//
	// 目的不是防「脚本自己做模板匹配」（那需要限流 + 提高识别成本），
	// 而是防「把图发给打码平台、拿回通过凭证」这种转手使用：
	// 绑到服务端观测到的 IP 后，平台方必须与调用方处于同一出口地址才用得上。
	//
	// data.IP 为空表示是本次升级之前生成的在途验证码，放行以免打断正在登录的用户。
	if data.IP != "" && clientIP != "" && data.IP != clientIP {
		logger.Log.Warnf("[captcha] 验证码凭证来源与生成时不符，已拒绝: gen=%s now=%s", data.IP, clientIP)
		return &model.CaptchaVerifyResponse{
			Success: false,
			Message: "验证码已失效，请重新获取",
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

	// 记录「该 token 已通过人机校验」，并把来源 IP 一并绑定。
	//
	// 校验结果必须落盘，否则前端拿到的 newToken 只是一个无意义的随机串 ——
	// 登录接口无从判断它是否真的通过过验证，攻击者直接 POST /auth/login
	// 就能完全绕过验证码，人机校验形同虚设。
	//
	// 存成 "1|ip" 形式：登录侧消费时比对来源，使「验证」与「登录」两步
	// 也发生在同一个客户端上。
	if err := cache.Set(context.Background(), verifiedKey(newToken), verifiedValue(clientIP), captchaExpiry); err != nil {
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

// verifiedValue 已通过校验的凭证内容：标记 + 来源 IP。
// 用 | 分隔而不是 JSON，是为了让「老格式（纯 "1"）」也能被兼容解析。
func verifiedValue(clientIP string) string {
	return "1|" + clientIP
}

// verifiedIPOf 从凭证内容里取出绑定的来源 IP，老格式返回空串。
func verifiedIPOf(val string) string {
	if idx := strings.Index(val, "|"); idx >= 0 {
		return val[idx+1:]
	}
	return ""
}

func verifiedKey(token string) string {
	return captchaPrefix + "verified:" + token
}

// ConsumeVerifiedToken 消费一次性的人机校验凭证。
//
// 登录接口调用：凭证存在且来源一致则删除并返回 true，
// 不存在、来源不符或删除失败都返回 false。
func ConsumeVerifiedToken(clientIP, token string) bool {
	if token == "" {
		return false
	}
	ctx := context.Background()
	key := verifiedKey(token)

	val, err := cache.Get(ctx, key)
	if err != nil {
		return false
	}

	// 来源比对：老格式（不含 |）跳过比对，保证升级期间在途凭证仍可用
	if ip := verifiedIPOf(val); ip != "" && clientIP != "" && ip != clientIP {
		logger.Log.Warnf("[captcha] 一次性凭证来源不符，已拒绝: gen=%s now=%s", ip, clientIP)
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

// imageToBase64 把出图编码成 base64。
//
// 返回 error 而不是吞掉 png.Encode 的失败：编码失败时 buf 是空的，
// 前端拿到 "data:image/png;base64," 会显示一张空白图 ——
// 用户看到的是「验证码刷不出来」，排查时完全指不到编码这一步。
func imageToBase64(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("编码验证码图片失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
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
