package config

import (
	"strings"
	"testing"
)

// withCfg 临时替换全局配置并在用例结束时还原。
// 这些用例都围绕「启动期护栏」的判定，必须能独立构造各种组合。
func withCfg(t *testing.T, mutate func(c *Config)) {
	t.Helper()
	prev := Cfg
	mutate(&Cfg)
	t.Cleanup(func() { Cfg = prev })
}

// TestListenAddrAndAllInterfaces 监听地址的两种写法要能区分。
func TestListenAddrAndAllInterfaces(t *testing.T) {
	cases := []struct {
		host        string
		wantAddr    string
		wantAllNics bool
	}{
		{"", ":8080", true},
		{"0.0.0.0", "0.0.0.0:8080", true},
		{"::", ":::8080", true},
		{"127.0.0.1", "127.0.0.1:8080", false},
		{"10.0.0.5", "10.0.0.5:8080", false},
	}

	for _, tc := range cases {
		s := ServerConfig{Host: tc.host, Port: 8080}
		if got := s.ListenAddr(); got != tc.wantAddr {
			t.Errorf("host=%q 监听地址应为 %q，实际 %q", tc.host, tc.wantAddr, got)
		}
		if got := s.ListensOnAllInterfaces(); got != tc.wantAllNics {
			t.Errorf("host=%q 是否监听所有网卡应为 %v，实际 %v", tc.host, tc.wantAllNics, got)
		}
	}
}

// TestDevelopmentWarnings 开发模式的启动告警（P0-4）。
//
// 为什么要有它：生产环境的默认密钥由 ValidateSecurity 直接拒绝启动，
// 但开发模式放行 —— 于是「debug + 监听所有网卡 + 默认密钥」这个组合
// 会安静地跑起来，而同网段任何人都能据此伪造任意用户的 token。
// 拒绝启动会破坏本地起步体验，所以退一步：把风险打印到启动日志里，
// 但不能因为「反正只是告警」就漏掉其中任意一项。
func TestDevelopmentWarnings(t *testing.T) {
	hasSubstring := func(warnings []string, sub string) bool {
		for _, w := range warnings {
			if strings.Contains(w, sub) {
				return true
			}
		}
		return false
	}

	t.Run("生产环境不产生开发告警", func(t *testing.T) {
		withCfg(t, func(c *Config) {
			c.Server.Mode = "release"
			c.Server.Host = ""
			c.JWT.Secret = defaultJWTSecret
		})
		if w := DevelopmentWarnings(); len(w) != 0 {
			t.Errorf("生产环境应由 ValidateSecurity 拒绝启动，不应走告警路径: %v", w)
		}
	})

	t.Run("监听所有网卡时给出提示", func(t *testing.T) {
		withCfg(t, func(c *Config) {
			c.Server.Mode = "debug"
			c.Server.Host = ""
			c.JWT.Secret = "a-strong-secret"
			c.Database.Password = "not-default"
		})
		w := DevelopmentWarnings()
		if !hasSubstring(w, "监听在所有网卡") {
			t.Errorf("应提示监听范围，实际: %v", w)
		}
		if hasSubstring(w, "jwt.secret 仍为默认值") {
			t.Errorf("密钥已自定义，不应再提示默认密钥: %v", w)
		}
	})

	t.Run("绑定本机地址后不再提示监听范围", func(t *testing.T) {
		withCfg(t, func(c *Config) {
			c.Server.Mode = "debug"
			c.Server.Host = "127.0.0.1"
			c.JWT.Secret = "a-strong-secret"
			c.Database.Password = "not-default"
		})
		if w := DevelopmentWarnings(); hasSubstring(w, "监听在所有网卡") {
			t.Errorf("已限定为回环地址，不应再提示监听范围: %v", w)
		}
	})

	t.Run("默认密钥与默认库密码都要点名", func(t *testing.T) {
		withCfg(t, func(c *Config) {
			c.Server.Mode = "debug"
			c.Server.Host = "127.0.0.1"
			c.JWT.Secret = defaultJWTSecret
			c.Database.Password = defaultDBPassword
		})
		w := DevelopmentWarnings()
		if !hasSubstring(w, "jwt.secret 仍为默认值") {
			t.Errorf("应点名默认 JWT 密钥: %v", w)
		}
		if !hasSubstring(w, "database.password 仍为默认值") {
			t.Errorf("应点名默认库密码: %v", w)
		}
		if !hasSubstring(w, "openssl rand -base64 48") {
			t.Errorf("应给出密钥生成方式，否则用户知道有风险也不知道怎么办: %v", w)
		}
	})
}

// TestConfigTemplatesParse 两份配置模板必须能被**真实解析并通过校验**。
//
// 为什么值得单测：config.yaml 与 deploy/config.docker.yaml 是两份独立的 YAML，
// 改了一处很容易漏掉另一处（本次 P0-2/P0-3 恰好两边都要改）。
// 原先只有 deploy/validate.py 做静态语法检查，且它依赖 PyYAML、不在 CI 里跑；
// 这里用真正的加载器跑一遍，YAML 写错、字段名拼错、必填项漏填都会立刻失败。
func TestConfigTemplatesParse(t *testing.T) {
	cases := []struct {
		name string
		path string
		// wantTrustedProxies 该模板应解析出的可信代理列表。
		// 断言取值而不只是「能解析」：mapstructure 的字段名写错、键名拼错时
		// YAML 依然能解析成功，只是字段静默为空 —— 那正是最危险的失败方式
		// （docker 部署会因此退化成全站共用 IP 额度，且没有任何报错）。
		wantTrustedProxies []string
		// wantTokenTransport / wantSameSite 同理，而且这次**已经抓到过一次真实缺口**：
		// deploy/config.docker.yaml 此前根本没有 security 段，
		// 于是容器里的 token 传输策略一直走代码默认值，与本地配置「看起来一样、
		// 实际是靠两套不同的机制凑出来的」。断言取值才能让漏配立刻失败。
		wantTokenTransport string
		wantSameSite       string
	}{
		{"本地开发配置", "../config/config.yaml", []string{}, TokenTransportBoth, CookieSameSiteLax},
		{"容器部署配置", "../deploy/config.docker.yaml", []string{"172.16.0.0/12"}, TokenTransportBoth, CookieSameSiteLax},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 容器配置刻意把密钥留空、由环境变量注入，否则 release 模式下
			// ValidateSecurity 会（正确地）拒绝启动 —— 这里模拟一遍注入
			t.Setenv("JWT_SECRET", "parse-check-only-not-a-real-secret")
			t.Setenv("DB_PASSWORD", "parse-check-only")
			t.Setenv("REDIS_PASSWORD", "parse-check-only")

			prev := Cfg
			t.Cleanup(func() { Cfg = prev })

			if err := Init(tc.path); err != nil {
				t.Fatalf("配置模板必须能被解析并通过校验: %v", err)
			}
			if err := ValidateSecurity(); err != nil {
				t.Fatalf("注入环境变量后生产安全检查应通过: %v", err)
			}

			got := Cfg.Server.TrustedProxies
			if len(got) != len(tc.wantTrustedProxies) {
				t.Fatalf("server.trusted_proxies 应为 %v，实际 %v", tc.wantTrustedProxies, got)
			}
			for i := range got {
				if got[i] != tc.wantTrustedProxies[i] {
					t.Errorf("server.trusted_proxies[%d] 应为 %q，实际 %q",
						i, tc.wantTrustedProxies[i], got[i])
				}
			}

			if Cfg.Security.TokenTransport != tc.wantTokenTransport {
				t.Errorf("security.token_transport 应为 %q，实际 %q",
					tc.wantTokenTransport, Cfg.Security.TokenTransport)
			}
			if Cfg.Security.SameSiteName() != tc.wantSameSite {
				t.Errorf("security.cookie_same_site 应为 %q，实际 %q",
					tc.wantSameSite, Cfg.Security.SameSiteName())
			}
			// cookie_secure 必须在模板里**显式写出**（不能靠代码默认值兜底）：
			// 它是「HTTP 部署」与「HTTPS 部署」的分水岭，
			// 藏起来的话容器编排到底是哪种形态就没人在配置里看得出来了。
			if Cfg.Security.CookieSecure == nil {
				t.Error("security.cookie_secure 必须在模板里显式配置（不要依赖代码默认值）")
			}

			// cors.allow_headers 必须包含 CSRF 头（P3-B3）。
			//
			// 这条断言存在的理由与上面几项同源：**两份模板必须同步改**。
			// 漏配的后果很隐蔽 —— 同源部署（dev 的 Vite proxy、prod 的 nginx 反代）
			// 根本不发 CORS 预检，所以漏了也一切正常；只有当有人把前端挪到
			// 另一个域名、自定义头开始触发预检时，所有写操作才会集体失败，
			// 而那时没人会想到是几个月前漏了一个 YAML 条目。
			if !containsString(Cfg.CORS.AllowHeaders, "X-CSRF-Token") {
				t.Errorf("cors.allow_headers 必须包含 X-CSRF-Token，实际 %v", Cfg.CORS.AllowHeaders)
			}
		})
	}
}

// containsString 报告 slice 里是否含 item（大小写不敏感，因为 HTTP 头名不区分大小写）。
func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, item) {
			return true
		}
	}
	return false
}

// TestValidateTrustedProxies 反向代理白名单的启动期校验（P0-2）。
//
// 为什么必须在校验阶段拦下：gin 的 SetTrustedProxies 解析失败时会保留
// 上一份配置，也就是「配错了反而比不配更危险」（不配 = 不信任任何代理头）。
// 只有让非法配置直接启动失败，才能保证「要么按预期生效，要么起不来」。
func TestValidateTrustedProxies(t *testing.T) {
	cases := []struct {
		name    string
		proxies []string
		wantBad bool
	}{
		{"空列表合法（默认值）", nil, false},
		{"单个 IP 合法", []string{"10.0.0.1"}, false},
		{"IPv4 网段合法", []string{"10.0.0.0/8", "172.16.0.0/12"}, false},
		{"IPv6 网段合法", []string{"fd00::/8"}, false},
		{"空项非法", []string{""}, true},
		{"非 IP 非 CIDR 非法", []string{"nginx"}, true},
		{"掩码越界非法", []string{"10.0.0.0/33"}, true},
		{"0.0.0.0/0 非法（等于信任一切）", []string{"0.0.0.0/0"}, true},
		{"::/0 非法（等于信任一切）", []string{"::/0"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := validateTrustedProxies(tc.proxies)
			if tc.wantBad && len(problems) == 0 {
				t.Errorf("应被判定为非法: %v", tc.proxies)
			}
			if !tc.wantBad && len(problems) > 0 {
				t.Errorf("应被判定为合法，却报出: %v", problems)
			}
		})
	}
}

// TestIsLoginFailClosedDefaultsToTrue 未配置时必须是 fail-closed。
//
// 用指针表达该项的原因就在这里：Go 的零值 false 等于「不设防」，
// 漏配必须落到安全的一侧。
func TestIsLoginFailClosedDefaultsToTrue(t *testing.T) {
	var unset SecurityConfig
	if !unset.IsLoginFailClosed() {
		t.Error("未配置时应默认为 fail-closed")
	}

	on := true
	if !(&SecurityConfig{LoginFailClosed: &on}).IsLoginFailClosed() {
		t.Error("显式 true 应生效")
	}

	off := false
	if (&SecurityConfig{LoginFailClosed: &off}).IsLoginFailClosed() {
		t.Error("显式 false 应生效（可用性优先的部署需要它）")
	}
}

// ---- P3-B1：token 传输方式与 cookie 属性 ----

// TestSecurityTokenTransport token 传输方式的归一化。
//
// 这里的关键不是「三种取值都能读出来」，而是**缺省落到 both**：
// 该值是 B1 阶段的开关 —— both 让「Authorization 头」与「cookie」两条路径并存，
// 前端可以逐步切换（B2），任何一步出问题把本项改回 header 就回到改造前行为。
func TestSecurityTokenTransport(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"未配置落到 both", "", TokenTransportBoth},
		{"只有空白也落到 both", "   ", TokenTransportBoth},
		{"显式 header", "header", TokenTransportHeader},
		{"显式 both", "both", TokenTransportBoth},
		{"显式 cookie", "cookie", TokenTransportCookie},
		{"大小写不敏感", "HeAdEr", TokenTransportHeader},
		{"首尾空白容错", "  cookie  ", TokenTransportCookie},
		// 非法值在这里也会落到 both（调用方不该假设配置一定被校验过），
		// 但 Validate 会拒绝启动 —— 两处都有才既安全又不会在测试里炸
		{"非法值归一化到 both（由 Validate 负责拒绝启动）", "cookies", TokenTransportBoth},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &SecurityConfig{TokenTransport: tc.raw}
			if got := s.Transport(); got != tc.want {
				t.Errorf("Transport(%q) = %q，期望 %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestSecurityTokenTransportPredicates 三种取值下「接受头 / 接受 cookie」的组合。
//
// 这三个断言合起来才说明问题：只有 cookie 模式会**拒绝** Authorization 头，
// 而那正是会让 Swagger / curl 失效、因此不能作为默认值的那个取值。
func TestSecurityTokenTransportPredicates(t *testing.T) {
	cases := []struct {
		transport        string
		wantHeader       bool
		wantCookie       bool
		wantIssuesCookie bool
	}{
		{TokenTransportHeader, true, false, false},
		{TokenTransportBoth, true, true, true},
		{TokenTransportCookie, false, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.transport, func(t *testing.T) {
			s := &SecurityConfig{TokenTransport: tc.transport}
			if got := s.AcceptsHeaderToken(); got != tc.wantHeader {
				t.Errorf("AcceptsHeaderToken() = %v，期望 %v", got, tc.wantHeader)
			}
			if got := s.AcceptsCookieToken(); got != tc.wantCookie {
				t.Errorf("AcceptsCookieToken() = %v，期望 %v", got, tc.wantCookie)
			}
			if got := s.IssuesCookies(); got != tc.wantIssuesCookie {
				t.Errorf("IssuesCookies() = %v，期望 %v", got, tc.wantIssuesCookie)
			}
		})
	}
}

// TestSecurityCookieAttributes Secure / SameSite 的默认值。
//
// 两个默认值都是刻意的，且理由不同：
//   - Secure 默认 **false**：本项目自带编排是纯 HTTP，默认 true 会让浏览器
//     完全不保存 cookie（登录态建立不起来）。安全责任交给显式配置 + 启动日志。
//   - SameSite 默认 **lax**：同源部署下够用且最安全，无需权衡。
func TestSecurityCookieAttributes(t *testing.T) {
	var unset SecurityConfig
	if unset.CookieSecureEnabled() {
		t.Error("cookie_secure 未配置时应为 false（默认编排是 HTTP，置 true 会让 cookie 被丢弃）")
	}
	if unset.SameSiteName() != CookieSameSiteLax {
		t.Errorf("cookie_same_site 未配置时应为 lax，实际 %q", unset.SameSiteName())
	}

	on := true
	if !(&SecurityConfig{CookieSecure: &on}).CookieSecureEnabled() {
		t.Error("显式 true 应生效")
	}

	for raw, want := range map[string]string{
		"lax": CookieSameSiteLax, "LAX": CookieSameSiteLax,
		"strict": CookieSameSiteStrict, "none": CookieSameSiteNone,
		"": CookieSameSiteLax, "bogus": CookieSameSiteLax,
	} {
		if got := (&SecurityConfig{CookieSameSite: raw}).SameSiteName(); got != want {
			t.Errorf("SameSiteName(%q) = %q，期望 %q", raw, got, want)
		}
	}
}

// TestValidateSecurityCookies 启动期校验：非法取值必须拒绝启动。
//
// 为什么不能静默回退：这些取值错了以后的表现都很隐蔽 ——
// token_transport 拼错会让「配了 cookie 却仍在读头」，
// same_site=none 配了 Secure=false 会让浏览器直接丢弃 cookie
// （现象是「登录接口返回成功，下一次请求却是未登录」）。
// 两者都很难从现象反推配置，所以宁可在启动时失败。
func TestValidateSecurityCookies(t *testing.T) {
	yes, no := true, false

	cases := []struct {
		name      string
		transport string
		sameSite  string
		secure    *bool
		wantBad   bool
	}{
		{"全部留空（走默认值）", "", "", nil, false},
		{"三种合法取值", TokenTransportCookie, CookieSameSiteStrict, &no, false},
		{"token_transport 拼错", "cookies", "", nil, true},
		{"token_transport 用 bool 写法", "true", "", nil, true},
		{"cookie_same_site 拼错", "", "LaxMode", nil, true},
		// 浏览器会丢弃 SameSite=None 且不带 Secure 的 cookie，登录态完全建立不起来
		{"none 但未开 secure", "", CookieSameSiteNone, nil, true},
		{"none 且显式 secure=false", "", CookieSameSiteNone, &no, true},
		{"none 且 secure=true 合法（分域名部署）", "", CookieSameSiteNone, &yes, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withCfg(t, func(c *Config) {
				c.Security.TokenTransport = tc.transport
				c.Security.CookieSameSite = tc.sameSite
				c.Security.CookieSecure = tc.secure
			})
			problems := validateSecurityCookies()
			if tc.wantBad && len(problems) == 0 {
				t.Errorf("应被判定为非法: transport=%q sameSite=%q secure=%v",
					tc.transport, tc.sameSite, tc.secure)
			}
			if !tc.wantBad && len(problems) > 0 {
				t.Errorf("应合法却报错: %v", problems)
			}
		})
	}
}

// TestCookiePolicySummary 启动日志摘要必须带上当前取值与风险提示。
//
// 这一行是运维唯一能直接看到 cookie 策略的地方（SameSite/Secure 的正确取值
// 取决于部署形态，配错了没有报错）。断言它「不会悄悄丢掉字段」——
// 少打一个 Secure，就没人知道 cookie 正在明文传输。
func TestCookiePolicySummary(t *testing.T) {
	yes := true

	// header 模式：不下发 cookie，摘要里不该出现 cookie 属性
	withCfg(t, func(c *Config) { c.Security.TokenTransport = TokenTransportHeader })
	if s := Cfg.Security.CookiePolicySummary(); !strings.Contains(s, "仅 Authorization 头") {
		t.Errorf("header 模式摘要应说明只认头，实际 %q", s)
	}

	// both + 非 Secure：必须带上「只适用于 HTTP」的提示
	withCfg(t, func(c *Config) {
		c.Security.TokenTransport = TokenTransportBoth
		c.Security.CookieSameSite = CookieSameSiteLax
		c.Security.CookieSecure = nil
	})
	s := Cfg.Security.CookiePolicySummary()
	for _, want := range []string{"头优先", "SameSite=lax", "Secure=false", "HTTPS"} {
		if !strings.Contains(s, want) {
			t.Errorf("摘要应包含 %q，实际 %q", want, s)
		}
	}

	// SameSite=None 必须提示 CSRF 面最大
	withCfg(t, func(c *Config) {
		c.Security.TokenTransport = TokenTransportCookie
		c.Security.CookieSameSite = CookieSameSiteNone
		c.Security.CookieSecure = &yes
	})
	s = Cfg.Security.CookiePolicySummary()
	for _, want := range []string{"仅 cookie", "SameSite=none", "Secure=true", "CSRF"} {
		if !strings.Contains(s, want) {
			t.Errorf("摘要应包含 %q，实际 %q", want, s)
		}
	}
}
