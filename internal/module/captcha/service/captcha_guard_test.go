package service

import (
	"strings"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/captcha/model"
)

// 验证码抗自动化（P0-3）的回归测试。
//
// 背景：chars 是「请依次点击 X Y Z」的提示语，必须下发给前端（前端靠它渲染与
// 计数，坐标只存服务端），因此脚本拿到 chars + bg 后可以离线模板匹配、免人工点选。
// 这一项**不能靠删除 chars 解决**（会直接破坏交互），只能从三处抬升成本：
//   ① 生成接口按 IP 限流，批量刷图需要真实 IP 资源
//   ② 一次验证只能提交一次，避免对同一张图穷举坐标
//   ③ 凭证绑定客户端来源，避免「发给打码平台换回凭证」的转手使用
//
// 测试环境不初始化 Redis，因此这里主要锁定「设施不可用时的失败方向」
// 与凭证格式，纯算法部分（随机字符/取点）由同目录其它用例覆盖。

// TestGenerateFailsClosedWhenRateLimitUnavailable 限流设施不可用时必须拒绝生成。
//
// 若此时放行，攻击者只要让计数这一步失败就能无限刷图 ——
// 而 Generate 紧接着本来就要写 Redis，拒绝并不会引入新的不可用场景。
func TestGenerateFailsClosedWhenRateLimitUnavailable(t *testing.T) {
	s := &captchaService{}

	_, err := s.Generate("203.0.113.9")
	if err == nil {
		t.Fatal("限流计数不可用时必须拒绝生成（fail-closed）")
	}
	if !strings.Contains(err.Error(), "验证码服务暂不可用") {
		t.Errorf("错误文案应指向限流设施而非其它环节，实际: %v", err)
	}
	// 属于系统错误：对外应归为 500 + 通用文案，不能包装成「参数错误」
	if common.IsBizError(err) {
		t.Error("限流设施故障是系统错误，不应被标记为业务错误")
	}
}

// TestGenerateSkipsRateLimitWithoutIP 取不到 IP 时跳过限流而不是共用一个键。
//
// 若共用空串作为键，所有取不到 IP 的请求会互相消耗额度，更容易被误伤。
func TestGenerateSkipsRateLimitWithoutIP(t *testing.T) {
	s := &captchaService{}

	_, err := s.Generate("")
	if err == nil {
		t.Fatal("Redis 不可用时生成仍应失败")
	}
	// 跳过限流后失败点应落在「写验证码数据」，说明限流那一步确实被跳过了
	if !strings.Contains(err.Error(), "缓存验证码失败") {
		t.Errorf("无 IP 时应跳过限流、在写缓存处失败，实际: %v", err)
	}
}

// TestVerifyFailsClosedWhenCacheUnavailable 读不到验证码数据时不放行。
func TestVerifyFailsClosedWhenCacheUnavailable(t *testing.T) {
	s := &captchaService{}

	resp, err := s.Verify("203.0.113.9", "whatever", []model.Point{{X: 1, Y: 1}})
	if err != nil {
		t.Fatalf("验证失败属于业务结果，不应返回系统错误: %v", err)
	}
	if resp.Success {
		t.Fatal("缓存不可用时绝不能判定验证通过")
	}
	if resp.Message == "" {
		t.Error("应给出可展示的失败原因")
	}
}

// TestConsumeVerifiedTokenFailClosed 一次性凭证的失败方向全部是「拒绝」。
func TestConsumeVerifiedTokenFailClosed(t *testing.T) {
	if ConsumeVerifiedToken("203.0.113.9", "") {
		t.Error("空 token 必须拒绝")
	}
	if ConsumeVerifiedToken("203.0.113.9", "any-token") {
		t.Error("凭证不存在（Redis 不可用）时必须拒绝")
	}
}

// TestVerifiedValueCarriesClientIP 凭证内容必须带上来源 IP。
//
// 登录侧消费时靠它比对来源；用 | 分隔而非 JSON，是为了兼容升级前
// 已签发的旧格式（纯 "1"），避免升级瞬间把在途用户全部踢回验证码。
func TestVerifiedValueCarriesClientIP(t *testing.T) {
	val := verifiedValue("203.0.113.9")
	if val != "1|203.0.113.9" {
		t.Fatalf("凭证格式不符合预期: %q", val)
	}
	if got := verifiedIPOf(val); got != "203.0.113.9" {
		t.Errorf("应解析出绑定的来源 IP，实际 %q", got)
	}

	// 旧格式：不含分隔符，解析出空串表示「未绑定」，调用方据此跳过比对
	if got := verifiedIPOf("1"); got != "" {
		t.Errorf("旧格式应解析为空串（跳过来源比对），实际 %q", got)
	}
}
