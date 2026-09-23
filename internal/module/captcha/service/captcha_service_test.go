package service

import (
	"image"
	"image/color"
	"testing"

	"go-admin/internal/module/captcha/model"
)

// TestRandomCharsNoDuplicate 验证生成的验证码字符互不重复。
//
// 该约束是功能正确性的前提：验证码为「按提示顺序依次点击」，
// 若出现重复字符（如 ABA），图上会有两个相同的 A，
// 用户无法分辨应先点击哪一个，只能靠猜，会直接导致验证失败。
func TestRandomCharsNoDuplicate(t *testing.T) {
	s := &captchaService{}
	const iterations = 2000

	for i := 0; i < iterations; i++ {
		chars, err := s.randomChars(charCount)
		if err != nil {
			t.Fatalf("第 %d 次生成失败: %v", i, err)
		}

		runes := []rune(chars)
		if len(runes) != charCount {
			t.Fatalf("期望 %d 个字符，实际 %d 个: %q", charCount, len(runes), chars)
		}

		seen := make(map[rune]bool, charCount)
		for _, ch := range runes {
			if seen[ch] {
				t.Fatalf("第 %d 次生成出现重复字符 %q: %q", i, ch, chars)
			}
			seen[ch] = true
		}
	}
}

// TestRandomCharsAllFromPool 验证生成的字符全部来自字符池
func TestRandomCharsAllFromPool(t *testing.T) {
	s := &captchaService{}

	pool := make(map[rune]bool, len(charPool))
	for _, ch := range charPool {
		pool[ch] = true
	}

	for i := 0; i < 500; i++ {
		chars, err := s.randomChars(charCount)
		if err != nil {
			t.Fatalf("生成失败: %v", err)
		}
		for _, ch := range chars {
			if !pool[ch] {
				t.Fatalf("字符 %q 不在字符池中: %q", ch, chars)
			}
		}
	}
}

// TestRandomCharsExceedsPool 验证请求长度超过字符池容量时返回错误，
// 而不是生成带重复字符的结果
func TestRandomCharsExceedsPool(t *testing.T) {
	s := &captchaService{}
	if _, err := s.randomChars(len(charPool) + 1); err == nil {
		t.Fatal("请求长度超过字符池容量时应返回错误")
	}
}

// newOpaqueCanvas 生成一张不透明画布，模拟真实背景（真实背景 alpha 恒为 255）
func newOpaqueCanvas() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, bgWidth, bgHeight))
	for y := 0; y < bgHeight; y++ {
		for x := 0; x < bgWidth; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	return img
}

// inkBounds 返回相对背景色之外的像素包围盒
func inkBounds(img *image.RGBA) (minX, minY, maxX, maxY int, ok bool) {
	minX, minY = bgWidth, bgHeight
	maxX, maxY = -1, -1
	for y := 0; y < bgHeight; y++ {
		for x := 0; x < bgWidth; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r == 0xffff && g == 0xffff && b == 0xffff {
				continue // 背景像素
			}
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	return minX, minY, maxX, maxY, maxX >= 0
}

// TestDrawCharCenteredOnPoint 验证字符墨迹的视觉中心与记录的目标点一致。
//
// Verify 是按 tolerancePx 比对「用户点击坐标」与「记录坐标」的。
// 如果字符画偏了，用户照着屏幕点击字符中心也会产生固定偏差，
// 偏差再叠加点击误差就会超出容差，导致明明点对了却验证失败。
func TestDrawCharCenteredOnPoint(t *testing.T) {
	s := &captchaService{}
	const px, py = 300, 100
	const maxOffset = 2 // 允许的居中误差（像素）

	for _, ch := range charPool {
		img := newOpaqueCanvas()
		s.drawChar(img, px, py, ch)

		minX, minY, maxX, maxY, ok := inkBounds(img)
		if !ok {
			t.Fatalf("字符 %q 未绘制出任何像素", ch)
		}

		centerX := float64(minX+maxX+1) / 2
		centerY := float64(minY+maxY+1) / 2
		dx := centerX - px
		dy := centerY - py
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}

		t.Logf("字符 %q 墨迹范围 x[%d,%d] y[%d,%d]，中心 (%.1f, %.1f)，相对目标点偏移 (%.1f, %.1f)",
			ch, minX, maxX, minY, maxY, centerX, centerY, dx, dy)

		if dx > maxOffset || dy > maxOffset {
			t.Errorf("字符 %q 墨迹中心偏移 (%.1f, %.1f) 超过允许值 %d，会挤占 Verify 的容差 %d",
				ch, dx, dy, maxOffset, tolerancePx)
		}
	}
}

// TestDrawnGlyphsMatchRecordedPoints 端到端几何校验：
// 完整走一遍「生成字符 -> 生成坐标 -> 渲染背景 -> 从图中还原字符位置」，
// 模拟用户照着自己看到的字符点击，检测到的视觉中心必须落在 Verify 的容差内。
func TestDrawnGlyphsMatchRecordedPoints(t *testing.T) {
	s := &captchaService{}
	const rounds = 20

	for round := 0; round < rounds; round++ {
		chars, err := s.randomChars(charCount)
		if err != nil {
			t.Fatalf("生成字符失败: %v", err)
		}
		points, err := s.randomPoints(charCount)
		if err != nil {
			t.Fatalf("生成坐标失败: %v", err)
		}

		img := s.generateBackground(chars, points)

		// 字符颜色固定为 RGB(10,40,100)，背景渐变/干扰线/噪点颜色均与之不同，
		// 因此可据此精确分离出字符墨迹
		type pixel struct{ x, y int }
		ink := make([]pixel, 0, 4096)
		for y := 0; y < bgHeight; y++ {
			for x := 0; x < bgWidth; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r == 10*257 && g == 40*257 && b == 100*257 {
					ink = append(ink, pixel{x, y})
				}
			}
		}
		if len(ink) == 0 {
			t.Fatalf("第 %d 轮：背景图中未找到任何字符墨迹", round)
		}

		// 按最近的目标点把墨迹归属到各字符，再求每个字符的视觉中心
		type acc struct{ sx, sy, n int }
		groups := make([]acc, len(points))
		for _, p := range ink {
			best, bestDist := -1, 0
			for i, pt := range points {
				dx, dy := p.x-pt.X, p.y-pt.Y
				d := dx*dx + dy*dy
				if best < 0 || d < bestDist {
					best, bestDist = i, d
				}
			}
			groups[best].sx += p.x
			groups[best].sy += p.y
			groups[best].n++
		}

		runes := []rune(chars)
		for i, g := range groups {
			if g.n == 0 {
				t.Fatalf("第 %d 轮：字符 %q 没有墨迹像素", round, string(runes[i]))
			}

			cx := float64(g.sx) / float64(g.n)
			cy := float64(g.sy) / float64(g.n)
			dx := cx - float64(points[i].X)
			dy := cy - float64(points[i].Y)
			if dx < 0 {
				dx = -dx
			}
			if dy < 0 {
				dy = -dy
			}

			if dx > tolerancePx || dy > tolerancePx {
				t.Fatalf("第 %d 轮字符 %q：视觉中心 (%.1f,%.1f) 距记录点 (%d,%d) 偏移 (%.1f,%.1f)，超出容差 %d",
					round, string(runes[i]), cx, cy, points[i].X, points[i].Y, dx, dy, tolerancePx)
			}
		}
	}
}

// TestMinPointGapExceedsHitArea 钉住「间距」与「点击容差」之间的不变量。
//
// 校验时以目标点为中心、±tolerancePx 的矩形为命中区。若两点间距
// 小于 2*tolerancePx，两个命中区就会重叠：用户点在 A 的容差边缘，
// 可能同时落在 B 的容差内，判定顺序一变结果就变 —— 表现为
// 「明明点对了却验证失败」，且难以复现。改任一常量都必须满足该关系。
func TestMinPointGapExceedsHitArea(t *testing.T) {
	if minPointGap <= 2*tolerancePx {
		t.Fatalf("目标点最小间距 %d 必须大于 2×点击容差 %d（= %d），否则命中区重叠",
			minPointGap, tolerancePx, 2*tolerancePx)
	}
}

// TestHasCollision 避让判定必须是「矩形相交」，而不是圆形距离。
func TestHasCollision(t *testing.T) {
	placed := []model.Point{{X: 300, Y: 100}}

	cases := []struct {
		name      string
		x, y      int
		wantClash bool
	}{
		{"完全重合", 300, 100, true},
		{"横向过近", 300 + minPointGap - 1, 100, true},
		{"纵向过近", 300, 100 + minPointGap - 1, true},
		{"横向刚好达标", 300 + minPointGap, 100, false},
		{"纵向刚好达标", 300, 100 + minPointGap, false},
		{"对角线方向：两轴都过近", 300 + minPointGap - 1, 100 + minPointGap - 1, true},
		{"远距离", 50, 190, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasCollision(c.x, c.y, placed); got != c.wantClash {
				t.Errorf("hasCollision(%d,%d) = %v，期望 %v", c.x, c.y, got, c.wantClash)
			}
		})
	}
}

// TestRandomPointsNoCollision 多轮生成，断言任意两点都满足避让约束。
//
// 单轮通过可能是碰巧，所以跑多轮；同时覆盖边界内的随机性。
func TestRandomPointsNoCollision(t *testing.T) {
	const rounds = 300
	s := &captchaService{}

	for round := 0; round < rounds; round++ {
		points, err := s.randomPoints(charCount)
		if err != nil {
			t.Fatalf("第 %d 轮生成坐标失败: %v", round, err)
		}
		if len(points) != charCount {
			t.Fatalf("第 %d 轮应生成 %d 个点，实际 %d 个", round, charCount, len(points))
		}

		for i := 0; i < len(points); i++ {
			for j := i + 1; j < len(points); j++ {
				dx := absInt(points[i].X - points[j].X)
				dy := absInt(points[i].Y - points[j].Y)
				if dx < minPointGap && dy < minPointGap {
					t.Fatalf("第 %d 轮第 %d/%d 点相撞：(%d,%d) 与 (%d,%d)，dx=%d dy=%d（均需 ≥ %d）",
						round, i+1, j+1, points[i].X, points[i].Y, points[j].X, points[j].Y,
						dx, dy, minPointGap)
				}
			}
		}
	}
}

// TestRandomPointsWithinMargins 目标点必须落在预留边距内，
// 否则字符会被画到画布外（截图里表现为字符被裁掉一半）。
func TestRandomPointsWithinMargins(t *testing.T) {
	s := &captchaService{}

	for round := 0; round < 100; round++ {
		points, err := s.randomPoints(charCount)
		if err != nil {
			t.Fatalf("生成坐标失败: %v", err)
		}
		for i, p := range points {
			if p.X < pointMarginX || p.X > bgWidth-pointMarginX {
				t.Fatalf("第 %d 轮第 %d 点横坐标 %d 越界（应在 [%d, %d]）",
					round, i+1, p.X, pointMarginX, bgWidth-pointMarginX)
			}
			if p.Y < pointMarginY || p.Y > bgHeight-pointMarginY {
				t.Fatalf("第 %d 轮第 %d 点纵坐标 %d 越界（应在 [%d, %d]）",
					round, i+1, p.Y, pointMarginY, bgHeight-pointMarginY)
			}
		}
	}
}

// TestRandomPointsAllDifferent 3 个点必须互不相同。
//
// 验证码是「按提示顺序点击」，两个目标点重合会让用户无论点哪都只能命中一个，
// 必然失败。
func TestRandomPointsAllDifferent(t *testing.T) {
	s := &captchaService{}

	for round := 0; round < 100; round++ {
		points, err := s.randomPoints(charCount)
		if err != nil {
			t.Fatalf("生成坐标失败: %v", err)
		}
		seen := make(map[model.Point]bool, len(points))
		for i, p := range points {
			if seen[p] {
				t.Fatalf("第 %d 轮第 %d 点 (%d,%d) 与前面的点重合", round, i+1, p.X, p.Y)
			}
			seen[p] = true
		}
	}
}

// 移动端显示参数：与前端实际渲染保持一致，改动任一侧都要同步这里。
//
//	弹窗宽 = 视口宽 × 92%（项目 assets/styles/index.scss 的移动端全局约定）
//	图片显示宽 = 弹窗宽 − body 左右内边距（全局约定 16px × 2）
const (
	mobileViewportWidth = 390 // iPhone 12/13/14 逻辑宽度
	mobileDialogRatio   = 0.92
	mobileBodyPaddingX  = 32
)

func mobileDisplayWidth() float64 {
	return mobileViewportWidth*mobileDialogRatio - mobileBodyPaddingX
}

// TestMobileTapTargetSize 钉住「手机上点得中」这个可用性要求。
//
// 前端把原图等比缩放到容器宽度，命中区随之缩小：
//
//	手机命中区(px) = 2 × tolerancePx × (显示宽 / 出图宽)
//
// 必须不小于 44px（移动端可点最小尺寸）。出图 640 宽时它只有约 30px，
// 用户反复点不中——这正是本用例要防住的回归。
func TestMobileTapTargetSize(t *testing.T) {
	displayWidth := mobileDisplayWidth()
	scale := displayWidth / float64(bgWidth)
	hitBox := 2 * float64(tolerancePx) * scale

	t.Logf("出图 %dx%d，手机显示宽 %.0fpx，缩放比 %.2f，命中区 %.1fpx",
		bgWidth, bgHeight, displayWidth, scale, hitBox)

	if hitBox < 44 {
		t.Errorf("手机上命中区仅 %.1fpx，低于 44px 可点下限：出图宽 %d、容差 %d、显示宽 %.0f",
			hitBox, bgWidth, tolerancePx, displayWidth)
	}
}

// TestMobileGlyphReadable 字符墨迹在手机上要看得清。
//
// 出图尺寸决定缩放比，字号决定墨迹大小；两者共同决定用户看到的字有多大。
// 门槛取 24px 高：低于此值字符明显偏小，用户需要凑近才能辨认。
func TestMobileGlyphReadable(t *testing.T) {
	s := &captchaService{}
	img := newOpaqueCanvas()
	s.drawChar(img, bgWidth/2, bgHeight/2, 'A')

	minX, minY, maxX, maxY, ok := inkBounds(img)
	if !ok {
		t.Fatal("字符未绘制出任何像素")
	}
	inkW := maxX - minX + 1
	inkH := maxY - minY + 1

	scale := mobileDisplayWidth() / float64(bgWidth)
	onScreenH := float64(inkH) * scale
	onScreenW := float64(inkW) * scale

	t.Logf("原图墨迹 %dx%d，手机显示约 %.0fx%.0fpx（缩放比 %.2f）",
		inkW, inkH, onScreenW, onScreenH, scale)

	if onScreenH < 24 {
		t.Errorf("手机上字符高仅 %.1fpx，偏小难辨认（原图墨迹高 %d，缩放比 %.2f）",
			onScreenH, inkH, scale)
	}
}

// TestGlyphFitsWithinMargins 半个字符墨迹必须小于边距，
// 否则字符会越过画布边界被裁掉（表现为字符缺角，用户看不清是什么字）。
func TestGlyphFitsWithinMargins(t *testing.T) {
	s := &captchaService{}
	img := newOpaqueCanvas()
	s.drawChar(img, bgWidth/2, bgHeight/2, 'W') // W 是最宽的字形之一

	minX, minY, maxX, maxY, ok := inkBounds(img)
	if !ok {
		t.Fatal("字符未绘制出任何像素")
	}
	inkW := maxX - minX + 1
	inkH := maxY - minY + 1

	if inkW/2+1 > pointMarginX {
		t.Errorf("半个字符宽 %d 超过横向边距 %d，字符会被裁切", inkW/2, pointMarginX)
	}
	if inkH/2+1 > pointMarginY {
		t.Errorf("半个字符高 %d 超过纵向边距 %d，字符会被裁切", inkH/2, pointMarginY)
	}
}
