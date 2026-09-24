package service

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-admin/internal/database"
	systemModel "go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"
)

// 支付宝网关的其余可离线验证部分（P2-1）。
//
// 这三块此前覆盖率都是 0，但各自都有明确的失败后果：
//   - validateReturnURL：returnURL 由调用方传入，校验缺失就是开放重定向
//   - doRequest：HTTP 200 不代表业务成功，但非 2xx 也不能一律当成「无响应体」
//   - generateNonceStr：nonce 参与微信支付签名，弱随机等于签名可预测

// TestValidateReturnURL validateReturnURL 的开放重定向防护。
//
// returnURL 是「支付完成后跳回哪个页面」，由前端传入。
// 不校验协议与主机名时，攻击者可以把 returnURL 设成自己的站点 ——
// 用户付完钱被跳到钓鱼页（地址栏还是从正规站点跳过来的），
// 或者 javascript: 伪协议直接在页面里执行脚本。
func TestValidateReturnURL(t *testing.T) {
	cases := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{"https 正常地址", "https://example.com/pay/result", false},
		{"http 正常地址", "http://example.com/pay/result?orderNo=1", false},
		{"带端口", "https://example.com:8443/result", false},
		{"本地调试地址", "http://127.0.0.1:3000/result", false},

		{"javascript 伪协议", "javascript:alert(document.cookie)", true},
		{"data 伪协议", "data:text/html;base64,PHNjcmlwdD4=", true},
		{"vbscript 伪协议", "vbscript:msgbox(1)", true},
		{"file 协议", "file:///etc/passwd", true},
		{"无协议相对路径", "/pay/result", true},
		{"缺主机名", "https:///pay/result", true},
		{"空串", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReturnURL(tc.rawURL)
			if tc.wantErr && err == nil {
				t.Errorf("%q 应被拒绝（开放重定向风险）", tc.rawURL)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("%q 应被放行，实际: %v", tc.rawURL, err)
			}
		})
	}
}

// TestAlipayDoRequestNonJSONErrorBodyIsReturned 非 2xx 且响应体不是 JSON 时报错，
// 且错误信息带上状态码与响应体。
func TestAlipayDoRequestNonJSONErrorBodyIsReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer srv.Close()

	g := newTestAlipayGateway(t)
	_, err := g.doRequest(context.Background(), http.MethodPost, srv.URL, map[string]string{"a": "1"})
	if err == nil {
		t.Fatal("非 2xx 且响应体非 JSON 时应报错")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("错误信息应带上状态码便于排查，实际: %v", err)
	}
}

// TestAlipayDoRequestJSONErrorBodyPassesThrough 非 2xx 但响应体是 JSON 时**返回给调用方**。
//
// 这是刻意的：支付宝把业务错误码放在 JSON 的 code 字段里，
// 网关层统一报错会把「签名错误」「订单不存在」这类可读原因丢掉，
// 只剩一个「请求失败(400)」。业务错误码由 checkAlipayResponse 解析后给出。
func TestAlipayDoRequestJSONErrorBodyPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"40002","sub_msg":"invalid signature"}`))
	}))
	defer srv.Close()

	g := newTestAlipayGateway(t)
	body, err := g.doRequest(context.Background(), http.MethodPost, srv.URL, map[string]string{"a": "1"})
	if err != nil {
		t.Fatalf("JSON 响应体应交给调用方解析，不应在网关层报错: %v", err)
	}
	if !strings.Contains(string(body), "invalid signature") {
		t.Errorf("应原样返回响应体，实际 %q", string(body))
	}
}

// TestAlipayDoRequestNilContextIsSafe nil ctx 必须降级为 Background 而不是 panic。
//
// 资金链路不能因为上层漏传 ctx 就崩掉。NewRequestWithContext 收到 nil 会 panic。
func TestAlipayDoRequestNilContextIsSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"10000"}`))
	}))
	defer srv.Close()

	g := newTestAlipayGateway(t)
	//nolint:staticcheck // 本用例专门验证「上层漏传 ctx」这条防御路径：
	// doRequest 必须把 nil 降级为 Background，而不是让 NewRequestWithContext panic。
	if _, err := g.doRequest(nil, http.MethodPost, srv.URL, map[string]string{"a": "1"}); err != nil {
		t.Fatalf("nil ctx 应降级为 Background，实际报错: %v", err)
	}
}

// TestGenerateNonceStr nonce 的长度、字符集与随机性。
//
// 它参与微信支付签名：字符集过窄或可预测都会削弱签名的抗猜测性。
// 这里锁定「长度 32、只用安全字符集、短期内不重复」三条可离线断言的性质。
func TestGenerateNonceStr(t *testing.T) {
	const iterations = 200
	seen := make(map[string]struct{}, iterations)

	for i := 0; i < iterations; i++ {
		s, err := generateNonceStr()
		if err != nil {
			t.Fatalf("生成 nonce 失败: %v", err)
		}
		if len(s) != 32 {
			t.Fatalf("nonce 长度应为 32，实际 %d（%q）", len(s), s)
		}
		for _, ch := range s {
			if !strings.ContainsRune(nonceChars, ch) {
				t.Fatalf("nonce 含非法字符 %q: %q", ch, s)
			}
		}
		if _, dup := seen[s]; dup {
			t.Fatalf("200 次生成出现重复 nonce（随机性不足）: %q", s)
		}
		seen[s] = struct{}{}
	}
}

// TestBase64StdRoundTrip 标准 base64 编解码必须对称（签名传输依赖它）。
func TestBase64StdRoundTrip(t *testing.T) {
	// 含 + / = 的字节序列，覆盖标准字母表与填充
	raw := []byte{0xfb, 0xff, 0x3e, 0x3f, 0x00, 0x10}
	encoded := base64EncodeStd(raw)
	if encoded == "" {
		t.Fatal("编码结果不应为空")
	}
	decoded, err := base64DecodeStd(encoded)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if string(decoded) != string(raw) {
		t.Errorf("往返不一致: %v -> %q -> %v", raw, encoded, decoded)
	}

	if _, err := base64DecodeStd("!!!not-base64!!!"); err == nil {
		t.Error("非法 base64 应返回错误，而不是静默给出空数据")
	}
}

// TestMinInt64 退款金额取小值（不超过剩余可退额度）。
func TestMinInt64(t *testing.T) {
	cases := []struct{ a, b, want int64 }{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{-1, 1, -1},
		{0, 3, 0},
	}
	for _, tc := range cases {
		if got := minInt64(tc.a, tc.b); got != tc.want {
			t.Errorf("minInt64(%d, %d) = %d，期望 %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestBuildFormQuery 表单查询串的构造规则。
//
// 它用于「把参数拼成待签名字符串」：顺序必须按 key 排序（否则双方签名不一致）、
// 值必须 URL 编码（含 & = 的值会破坏结构）、空值必须跳过
// （与 sign/verify 的口径一致，否则签出来的名永远验不过）。
func TestBuildFormQuery(t *testing.T) {
	got := buildFormQuery(map[string]string{
		"b": "2",
		"a": "1",
		"c": "",
		"d": "x y&z=1",
	})

	want := "a=1&b=2&d=x+y%26z%3D1"
	if got != want {
		t.Errorf("表单串构造错误\n期望: %s\n实际: %s", want, got)
	}
	if strings.Contains(got, "c=") {
		t.Error("空值参数必须跳过（与签名口径一致）")
	}
}

// TestPayCtxHasDeadline 资金调用必须有明确的等待上限。
//
// 此前网关方法签名上有 ctx 但调用方一律传 nil、内部也忽略它，
// 属于「看起来有超时、实际没有」—— 渠道不响应时请求会一直挂着。
func TestPayCtxHasDeadline(t *testing.T) {
	ctx, cancel := payCtx()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("payCtx 必须带超时，否则渠道无响应时调用会永久挂住")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > payGatewayTimeout {
		t.Errorf("超时时长不合理: 剩余 %v，上限 %v", remaining, payGatewayTimeout)
	}
}

// TestParsePublicKey 公钥解析的三种结果：合法、格式非法、类型不对。
func TestParsePublicKey(t *testing.T) {
	_, pubB64 := testKeyPair(t)

	if _, err := parsePublicKey(pubB64); err != nil {
		t.Fatalf("合法 PKIX 公钥应解析成功: %v", err)
	}

	if _, err := parsePublicKey("这不是 base64"); err == nil {
		t.Error("非法内容应返回错误")
	}

	// 合法的 PKIX 但不是 RSA（这里用私钥的 DER 冒充）
	privDER, err := x509.MarshalPKCS8PrivateKey(testPrivKey)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if _, err := parsePublicKey(base64.StdEncoding.EncodeToString(privDER)); err == nil {
		t.Error("非 RSA 公钥应返回错误，而不是给出一个不可用的 key")
	}
}

// TestAlipayGetPublicKeyRequiresConfig 未配置公钥时必须报错并指明配置项。
//
// 静默返回空 key 会让验签以「签名无效」收场，排查时完全看不出
// 真实原因是「没配公钥」。
func TestAlipayGetPublicKeyRequiresConfig(t *testing.T) {
	g := NewAlipayGateway(AlipayConfig{AppID: testAlipayAppID})

	_, err := g.getPublicKey()
	if err == nil {
		t.Fatal("未配置公钥必须报错")
	}
	if !strings.Contains(err.Error(), "alipay_public_key") {
		t.Errorf("错误信息应指明该配哪一项，实际: %v", err)
	}
}

// TestLoadPayConfigMapsPrefixedKeys 从 sys_config 读取 pay.* 配置并去掉前缀。
//
// 这层映射没有编译期保护：key 名写错（例如 wechat_app_id 写成 wx_app_id）
// 只会得到空字符串，然后以「签名失败」「渠道参数缺失」的形式在运行期暴露，
// 排查成本很高。用例把每个 key 的映射关系固定下来。
func TestLoadPayConfigMapsPrefixedKeys(t *testing.T) {
	testsupport.NewDB(t, &systemModel.SysConfig{})

	seed := []struct{ key, value string }{
		{"pay.wechat_app_id", "wx-app-id"},
		{"pay.wechat_mch_id", "wx-mch-id"},
		{"pay.wechat_key", "wx-key"},
		{"pay.wechat_apiv3_key", "wx-v3-key"},
		{"pay.wechat_serial_no", "wx-serial"},
		{"pay.alipay_app_id", "ali-app-id"},
		{"pay.alipay_key", "ali-private-key"},
		{"pay.alipay_public_key", "ali-public-key"},
		{"pay.notify_url", "https://example.com/notify"},
		{"pay.return_url", "https://example.com/return"},
		// 非 pay. 前缀：不应被读入
		{"site.name", "站点名"},
	}
	for i, s := range seed {
		row := &systemModel.SysConfig{
			Name:      s.key,
			ConfigKey: s.key,
			Value:     s.value,
			Type:      1,
		}
		row.ID = uint(i + 1)
		if err := database.DB.Create(row).Error; err != nil {
			t.Fatalf("准备配置失败: %v", err)
		}
	}

	wx := LoadWechatPayConfig()
	if wx.AppID != "wx-app-id" || wx.MchID != "wx-mch-id" || wx.Key != "wx-key" ||
		wx.APIv3Key != "wx-v3-key" || wx.SerialNo != "wx-serial" {
		t.Errorf("微信配置映射错误: %+v", wx)
	}
	if wx.NotifyURL != "https://example.com/notify" {
		t.Errorf("notify_url 映射错误: %q", wx.NotifyURL)
	}

	ali := LoadAlipayConfig()
	if ali.AppID != "ali-app-id" || ali.PrivateKey != "ali-private-key" ||
		ali.PublicKeyID != "ali-public-key" {
		t.Errorf("支付宝配置映射错误: %+v", ali)
	}
	if ali.ReturnURL != "https://example.com/return" {
		t.Errorf("return_url 映射错误: %q", ali.ReturnURL)
	}
}

// TestLoadPayConfigEmptyWhenNothingConfigured 未配置时返回空值而不是 panic。
//
// 首次部署（还没填渠道参数）时调用方会走到这条路径：
// 必须是「配置为空 → 上层给出可读的提示」，而不是空指针或崩溃。
func TestLoadPayConfigEmptyWhenNothingConfigured(t *testing.T) {
	testsupport.NewDB(t, &systemModel.SysConfig{})

	if cfg := LoadWechatPayConfig(); cfg == nil || cfg.AppID != "" {
		t.Errorf("无配置时应返回空配置而非 nil/panic，实际 %+v", cfg)
	}
	if cfg := LoadAlipayConfig(); cfg == nil || cfg.AppID != "" {
		t.Errorf("无配置时应返回空配置而非 nil/panic，实际 %+v", cfg)
	}
}
