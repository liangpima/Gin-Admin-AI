package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// 支付宝回调解析与验签的测试（P2-1）。
//
// 为什么这组用例值得写：`HandleNotify` 会把回调结果落成「订单已支付」。
// 验签是这条链路上唯一的真伪判据 —— 一旦它能被绕过，攻击者伪造一个 POST
// 就能把任意订单标记为已支付（钱没到账）。而这段代码此前**完全没有测试**：
// 覆盖率为 0，等于「改坏了也没人知道」。
//
// 用例用**测试期生成的 RSA 密钥对**自签自验，不依赖任何外部服务与真实密钥。

var (
	testKeyOnce sync.Once
	testPrivKey *rsa.PrivateKey
)

// testKeyPair 生成一对测试用 RSA 密钥，并转成配置里存放的格式。
//
// 格式必须与生产一致（这是本测试能发现真实问题的前提）：
//   - 私钥：base64(PKCS8 DER)，parsePrivateKey 会补上 "PRIVATE KEY" 头
//   - 公钥：base64(PKIX DER)，getPublicKey 会剥掉头尾再交给 parsePublicKey
func testKeyPair(t *testing.T) (privB64, pubB64 string) {
	t.Helper()
	testKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("生成测试密钥失败: %v", err)
		}
		testPrivKey = k
	})
	if testPrivKey == nil {
		t.Fatal("测试密钥未初始化")
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(testPrivKey)
	if err != nil {
		t.Fatalf("序列化私钥失败: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&testPrivKey.PublicKey)
	if err != nil {
		t.Fatalf("序列化公钥失败: %v", err)
	}
	return base64.StdEncoding.EncodeToString(privDER), base64.StdEncoding.EncodeToString(pubDER)
}

const testAlipayAppID = "2021000000000000"

func newTestAlipayGateway(t *testing.T) *AlipayGateway {
	t.Helper()
	priv, pub := testKeyPair(t)
	return NewAlipayGateway(AlipayConfig{
		AppID:       testAlipayAppID,
		PrivateKey:  priv,
		PublicKeyID: pub,
		NotifyURL:   "https://example.com/api/v1/pay/notify/alipay",
	})
}

// signedNotifyBody 构造一条**已正确签名**的回调报文。
//
// 签名口径必须与 verify 完全一致：剔除 sign/sign_type，按 key 排序，
// 跳过空值，用 k=v 以 & 连接。这里直接复用 g.sign，避免测试自己重写一遍
// 签名逻辑（那样只能证明「我写的两遍一致」，证明不了与实现一致）。
func signedNotifyBody(t *testing.T, g *AlipayGateway, params map[string]string) []byte {
	t.Helper()
	sig, err := g.sign(params)
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	form.Set("sign", sig)
	form.Set("sign_type", "RSA2")
	return []byte(form.Encode())
}

func validNotifyParams() map[string]string {
	return map[string]string{
		"app_id":       testAlipayAppID,
		"out_trade_no": "ORDER20260924001",
		"trade_no":     "2026092422001400000000000001",
		"trade_status": "TRADE_SUCCESS",
		"total_amount": "19.99",
		"gmt_payment":  "2026-09-24 14:20:00",
	}
}

// TestAlipayParseNotifyAcceptsValidSignature 合法签名的回调必须被接受，
// 且各字段被正确解析（金额按分、支付时间解析成 time.Time）。
func TestAlipayParseNotifyAcceptsValidSignature(t *testing.T) {
	g := newTestAlipayGateway(t)

	result, err := g.ParseNotify(signedNotifyBody(t, g, validNotifyParams()))
	if err != nil {
		t.Fatalf("合法签名应通过验签: %v", err)
	}

	if result.Status != "success" {
		t.Errorf("TRADE_SUCCESS 应映射为 success，实际 %q", result.Status)
	}
	if result.OrderNo != "ORDER20260924001" {
		t.Errorf("商户订单号解析错误: %q", result.OrderNo)
	}
	if result.TradeNo != "2026092422001400000000000001" {
		t.Errorf("支付宝交易号解析错误: %q", result.TradeNo)
	}
	// 19.99 元 → 1999 分。用整数运算解析，不能出现 1998 这种浮点误差
	if result.Amount != 1999 {
		t.Errorf("金额应为 1999 分，实际 %d", result.Amount)
	}
	if result.PaidAt == nil {
		t.Fatal("gmt_payment 合法时应解析出支付时间")
	}
	// 关键：gmt_payment 的时区是 GMT+8（支付宝接口规范），不是 UTC。
	// 用 time.Parse 会整体差 8 小时 —— 界面上表现为「支付时间在未来」。
	// Equal 比较的是时间点，因此这个断言与本机时区无关。
	want := time.Date(2026, 9, 24, 14, 20, 0, 0, time.FixedZone("GMT+8", 8*60*60))
	if !result.PaidAt.Equal(want) {
		t.Errorf("支付时间解析错误：期望 %v，实际 %v（差 %v）",
			want, result.PaidAt, result.PaidAt.Sub(want))
	}
	if result.RawData == "" {
		t.Error("应保留原始报文用于排查")
	}
}

// TestAlipayParseNotifyRejectsTamperedAmount 签名后改动金额必须被拒。
//
// 这是验签的核心意义：回调体在传输中（或由攻击者构造时）被改动，
// 签名就对不上了。若这里能通过，攻击者可以把 19.99 元的订单
// 伪造成「已支付 0.01 元」。
func TestAlipayParseNotifyRejectsTamperedAmount(t *testing.T) {
	g := newTestAlipayGateway(t)

	body := signedNotifyBody(t, g, validNotifyParams())
	// 把签名后的金额改掉（签名不变）
	tampered := strings.Replace(string(body), "total_amount=19.99", "total_amount=0.01", 1)
	if tampered == string(body) {
		t.Fatal("前置条件不成立：篡改未生效")
	}

	if _, err := g.ParseNotify([]byte(tampered)); err == nil {
		t.Fatal("金额被篡改后必须验签失败")
	}
}

// TestAlipayParseNotifyRejectsForeignAppID 验签通过但 app_id 不匹配必须被拒。
//
// 验签只能证明「报文来自支付宝且未被篡改」，不能证明「这笔交易属于本应用」：
// 同一支付宝账号体系下的其他应用（服务商模式代管的子应用尤其常见）
// 同样能生成通过验签的通知。不比对 app_id 时，别家应用的支付通知
// 就能把我们的订单标记为已支付 —— 钱没进我们的账户，订单却入账了。
func TestAlipayParseNotifyRejectsForeignAppID(t *testing.T) {
	g := newTestAlipayGateway(t)

	params := validNotifyParams()
	params["app_id"] = "9999999999999999" // 别家应用（签名仍然合法）
	body := signedNotifyBody(t, g, params)

	_, err := g.ParseNotify(body)
	if err == nil {
		t.Fatal("app_id 不匹配必须拒绝")
	}
	if !strings.Contains(err.Error(), "app_id") {
		t.Errorf("错误信息应指向 app_id，便于排查: %v", err)
	}
}

// TestAlipayParseNotifyRejectsMissingSignature 缺签名/签名错误必须被拒。
func TestAlipayParseNotifyRejectsMissingSignature(t *testing.T) {
	g := newTestAlipayGateway(t)

	t.Run("完全没有 sign 字段", func(t *testing.T) {
		form := url.Values{}
		for k, v := range validNotifyParams() {
			form.Set(k, v)
		}
		if _, err := g.ParseNotify([]byte(form.Encode())); err == nil {
			t.Fatal("缺签名必须拒绝")
		}
	})

	t.Run("sign 是垃圾串", func(t *testing.T) {
		form := url.Values{}
		for k, v := range validNotifyParams() {
			form.Set(k, v)
		}
		form.Set("sign", "not-a-base64-signature!!")
		if _, err := g.ParseNotify([]byte(form.Encode())); err == nil {
			t.Fatal("非法签名必须拒绝")
		}
	})

	t.Run("用别人的私钥签的名", func(t *testing.T) {
		// 攻击者用自己的密钥对签名：即使格式合法，公钥也对不上
		attacker, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("生成攻击者密钥失败: %v", err)
		}
		attackerGW := NewAlipayGateway(AlipayConfig{
			AppID:      testAlipayAppID,
			PrivateKey: encodePKCS8(t, attacker),
		})
		body := signedNotifyBody(t, attackerGW, validNotifyParams())

		if _, err := g.ParseNotify(body); err == nil {
			t.Fatal("非本应用私钥签发的报文必须验签失败")
		}
	})
}

// TestAlipayParseNotifyHandlesNonSuccessStatus 非成功状态要能解析出来但状态为 fail。
//
// 解析成功不代表订单已支付：回调也可能是「交易关闭」「等待付款」。
// 上层靠 Status 判断，这里锁定映射关系。
func TestAlipayParseNotifyHandlesNonSuccessStatus(t *testing.T) {
	g := newTestAlipayGateway(t)

	cases := []struct {
		tradeStatus string
		want        string
	}{
		{"TRADE_SUCCESS", "success"},
		{"TRADE_FINISHED", "success"},
		{"WAIT_BUYER_PAY", "fail"},
		{"TRADE_CLOSED", "fail"},
		{"", "fail"},
	}

	for _, tc := range cases {
		t.Run(tc.tradeStatus, func(t *testing.T) {
			params := validNotifyParams()
			if tc.tradeStatus == "" {
				delete(params, "trade_status")
			} else {
				params["trade_status"] = tc.tradeStatus
			}

			result, err := g.ParseNotify(signedNotifyBody(t, g, params))
			if err != nil {
				t.Fatalf("验签应通过: %v", err)
			}
			if result.Status != tc.want {
				t.Errorf("trade_status=%q 应映射为 %q，实际 %q", tc.tradeStatus, tc.want, result.Status)
			}
		})
	}
}

// TestAlipayParseNotifyTolerantToBadPaymentTime 支付时间格式异常时不报错，
// 只把 PaidAt 留空。
//
// 支付时间只用于展示/对账，不该因为一个格式问题把「已经收到的钱」判为无效 ——
// 那会导致订单不入账，比时间字段缺失严重得多。
func TestAlipayParseNotifyTolerantToBadPaymentTime(t *testing.T) {
	g := newTestAlipayGateway(t)

	params := validNotifyParams()
	params["gmt_payment"] = "2026/09/24 14:20"
	result, err := g.ParseNotify(signedNotifyBody(t, g, params))
	if err != nil {
		t.Fatalf("时间格式异常不应导致验签失败: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("订单状态仍应为 success，实际 %q", result.Status)
	}
	if result.PaidAt != nil {
		t.Errorf("非法时间应留空而不是猜一个值，实际 %v", result.PaidAt)
	}
}

// TestAlipayParseNotifyRejectsMalformedBody 完全不是表单编码的报文要报错。
func TestAlipayParseNotifyRejectsMalformedBody(t *testing.T) {
	g := newTestAlipayGateway(t)

	if _, err := g.ParseNotify([]byte("%zz=1&bad=%")); err == nil {
		t.Fatal("非法表单编码应返回错误")
	}
}

// TestAlipayParseNotifyAmountOptional 缺 total_amount 时不报错（金额留 0）。
//
// 上层会拿 Amount 与订单金额比对，缺失时按不匹配处理 —— 但解析本身不该失败，
// 否则连「哪个订单」都拿不到，日志里无法定位。
func TestAlipayParseNotifyAmountOptional(t *testing.T) {
	g := newTestAlipayGateway(t)

	params := validNotifyParams()
	delete(params, "total_amount")
	result, err := g.ParseNotify(signedNotifyBody(t, g, params))
	if err != nil {
		t.Fatalf("缺金额不应导致解析失败: %v", err)
	}
	if result.Amount != 0 {
		t.Errorf("缺金额时应为 0，实际 %d", result.Amount)
	}
	if result.OrderNo == "" {
		t.Error("仍应解析出订单号，否则无法定位订单")
	}
}

func encodePKCS8(t *testing.T, k *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatalf("序列化私钥失败: %v", err)
	}
	return base64.StdEncoding.EncodeToString(der)
}
