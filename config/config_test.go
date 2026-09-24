package config

import "testing"

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
