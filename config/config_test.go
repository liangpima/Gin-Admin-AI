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
	}{
		{"本地开发配置", "../config/config.yaml", []string{}},
		{"容器部署配置", "../deploy/config.docker.yaml", []string{"172.16.0.0/12"}},
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
		})
	}
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
