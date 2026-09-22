package middleware

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSanitizeRequestBodyMasksPasswords 验证请求体中的密码等敏感字段被脱敏。
//
// 这是安全要求：操作日志曾把创建用户 / 重置密码 / 修改密码接口的明文密码写入数据库。
func TestSanitizeRequestBodyMasksPasswords(t *testing.T) {
	body := []byte(`{"username":"alice","password":"Secret123","profile":{"oldPassword":"Old123","nickname":"A"}}`)

	got := sanitizeRequestBody(body)

	if strings.Contains(got, "Secret123") || strings.Contains(got, "Old123") {
		t.Fatalf("敏感值未被脱敏: %s", got)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("脱敏结果不是合法 JSON: %v (结果=%s)", err, got)
	}
	if parsed["password"] != maskedValue {
		t.Errorf("password 应被替换为 %q，实际 %v", maskedValue, parsed["password"])
	}
	if parsed["username"] != "alice" {
		t.Errorf("非敏感字段 username 不应被改动，实际 %v", parsed["username"])
	}

	profile, ok := parsed["profile"].(map[string]interface{})
	if !ok {
		t.Fatalf("profile 结构异常: %v", parsed["profile"])
	}
	if profile["oldPassword"] != maskedValue {
		t.Errorf("嵌套的 oldPassword 应被脱敏，实际 %v", profile["oldPassword"])
	}
	if profile["nickname"] != "A" {
		t.Errorf("嵌套的非敏感字段不应被改动，实际 %v", profile["nickname"])
	}
}

// TestSanitizeRequestBodyCaseInsensitive 验证字段名匹配不区分大小写
func TestSanitizeRequestBodyCaseInsensitive(t *testing.T) {
	body := []byte(`{"Password":"X1","NEWPASSWORD":"X2","AccessKey":"X3","token":"X4","privateKey":"X5"}`)

	got := sanitizeRequestBody(body)

	for _, secret := range []string{"X1", "X2", "X3", "X4", "X5"} {
		if strings.Contains(got, secret) {
			t.Errorf("敏感值 %s 未被脱敏: %s", secret, got)
		}
	}
}

// TestSanitizeRequestBodyArray 验证数组元素中的敏感字段也会被处理
func TestSanitizeRequestBodyArray(t *testing.T) {
	body := []byte(`{"items":[{"password":"P1"},{"nested":{"secret":"P2"}}]}`)

	got := sanitizeRequestBody(body)

	if strings.Contains(got, "P1") || strings.Contains(got, "P2") {
		t.Errorf("数组内的敏感值未被脱敏: %s", got)
	}
}

// TestSanitizeRequestBodyNonJSON 验证非 JSON 请求体不记录内容
func TestSanitizeRequestBodyNonJSON(t *testing.T) {
	body := []byte("------WebKitFormBoundary\r\nContent-Disposition: form-data; name=\"file\"\r\n\r\nsecret-content")

	got := sanitizeRequestBody(body)

	if strings.Contains(got, "secret-content") {
		t.Errorf("非 JSON 请求体不应记录内容，实际: %s", got)
	}
}

// TestSanitizeRequestBodyEmpty 验证空请求体返回空串
func TestSanitizeRequestBodyEmpty(t *testing.T) {
	if got := sanitizeRequestBody(nil); got != "" {
		t.Errorf("nil 请求体应返回空串，实际 %q", got)
	}
	if got := sanitizeRequestBody([]byte{}); got != "" {
		t.Errorf("空请求体应返回空串，实际 %q", got)
	}
}

// TestSanitizeRequestBodyTruncates 验证超长请求体被截断
func TestSanitizeRequestBodyTruncates(t *testing.T) {
	long := strings.Repeat("a", maxBodyLogLength*2)
	body := []byte(`{"nickname":"` + long + `"}`)

	got := sanitizeRequestBody(body)

	if !strings.HasSuffix(got, "...[truncated]") {
		t.Errorf("超长请求体应带截断标记，实际结尾: %q", tail(got, 30))
	}
	if n := len([]rune(got)); n > maxBodyLogLength+len("...[truncated]") {
		t.Errorf("截断后长度 %d 超出上限 %d", n, maxBodyLogLength)
	}
}

// TestSanitizeRequestBodyTruncateRuneSafe 验证截断不会切断多字节字符
func TestSanitizeRequestBodyTruncateRuneSafe(t *testing.T) {
	long := strings.Repeat("中", maxBodyLogLength)
	body := []byte(`{"nickname":"` + long + `"}`)

	got := sanitizeRequestBody(body)

	if !utf8.ValidString(got) {
		t.Error("截断产生了非法 UTF-8，说明切断了多字节字符")
	}
}

// TestSanitizeRequestBodyKeepsShortBodyIntact 验证短请求体不被改动结构
func TestSanitizeRequestBodyKeepsShortBodyIntact(t *testing.T) {
	body := []byte(`{"a":1,"b":"x"}`)

	got := sanitizeRequestBody(body)

	if strings.Contains(got, "truncated") {
		t.Errorf("短请求体不应被截断: %s", got)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("结果应为合法 JSON: %v", err)
	}
	if parsed["b"] != "x" {
		t.Errorf("普通字段应原样保留，实际 %v", parsed["b"])
	}
}

func tail(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// TestSanitizeRequestBodyMasksSiblingValue 验证「名称 + 取值」分离结构的脱敏。
//
// 配置批量保存的 body 形如 {"items":[{"key":"secret_key","value":"真实密钥"}]}，
// 密钥放在通用的 value 字段里，必须结合同级的 key 判断是否敏感。
// 回归用例：该场景曾导致 OSS/支付密钥明文落库。
func TestSanitizeRequestBodyMasksSiblingValue(t *testing.T) {
	body := []byte(`{"prefix":"oss.","items":[{"key":"secret_key","value":"REAL_SECRET_1"},{"key":"access_key","value":"REAL_AK_2"}]}`)

	got := sanitizeRequestBody(body)

	if strings.Contains(got, "REAL_SECRET_1") || strings.Contains(got, "REAL_AK_2") {
		t.Fatalf("配置密钥未被脱敏（密钥会明文落库）: %s", got)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("脱敏结果不是合法 JSON: %v (结果=%s)", err, got)
	}
	items, ok := parsed["items"].([]interface{})
	if !ok || len(items) != 2 {
		t.Fatalf("items 结构异常: %v", parsed["items"])
	}
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			t.Fatalf("item 结构异常: %v", it)
		}
		if m["value"] != maskedValue {
			t.Errorf("敏感配置项的 value 应被脱敏，实际 %v", m["value"])
		}
	}
}

// TestSanitizeRequestBodyKeepsNonSensitiveConfigValue 非敏感配置项的值不应被误脱敏
func TestSanitizeRequestBodyKeepsNonSensitiveConfigValue(t *testing.T) {
	body := []byte(`{"prefix":"site.","items":[{"key":"name","value":"我的网站"}]}`)

	got := sanitizeRequestBody(body)

	if !strings.Contains(got, "我的网站") {
		t.Errorf("非敏感配置项的值不应被脱敏: %s", got)
	}
}

// TestSanitizeRequestBodyNestedJSONString 验证把 JSON 当字符串传递时也能脱敏
func TestSanitizeRequestBodyNestedJSONString(t *testing.T) {
	body := []byte(`{"data":"{\"password\":\"PWD_IN_STRING\"}"}`)

	got := sanitizeRequestBody(body)

	if strings.Contains(got, "PWD_IN_STRING") {
		t.Errorf("嵌套 JSON 字符串中的敏感值未被脱敏: %s", got)
	}
}

// TestResolveTitleMapsToChinese 模块标题必须是可读中文，不能把 URL 段直接拼出来。
//
// 这是日志页「模块标题」列展示的内容，也是该列筛选框 LIKE 查询的目标：
// 存英文标识时非技术用户既看不懂、也没法和侧边栏菜单名对上。
func TestResolveTitleMapsToChinese(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/v1/system/user/list", "用户管理"},
		{"/api/v1/system/user", "用户管理"},
		{"/api/v1/system/role/list", "角色管理"},
		{"/api/v1/system/dict/data/list", "数据字典"},
		{"/api/v1/system/config/batch", "参数管理"},
		{"/api/v1/system/agreement", "协议管理"},
		{"/api/v1/system/pay/order/refund", "支付订单"},
		{"/api/v1/member/list", "会员列表"},
		{"/api/v1/member/tag/list", "会员标签"},
		{"/api/v1/member/tags", "会员标签"},
		{"/api/v1/member/level/all", "会员等级"},
		{"/api/v1/member/points/list", "积分明细"},
		{"/api/v1/auth/logout", "退出登录"},
		{"/api/v1/auth/userInfo", "用户信息"},
		{"/api/v1/dashboard/stats", "工作台统计"},
	}

	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := resolveTitle(c.path); got != c.want {
				t.Errorf("resolveTitle(%q) = %q，期望 %q", c.path, got, c.want)
			}
		})
	}
}

// TestResolveTitleIgnoresPathParam 路径参数不能漏进标题。
//
// /api/v1/member/:id 的第 5 段是 ":id"，早期实现把它拼成了「member-:id」，
// 库里确实出现过这种脏数据 —— 同一个 handler 的 GET/PUT/DELETE 都记成它。
func TestResolveTitleIgnoresPathParam(t *testing.T) {
	for _, path := range []string{"/api/v1/member/:id", "/api/v1/member/12"} {
		if got := resolveTitle(path); got != "会员列表" {
			t.Errorf("resolveTitle(%q) = %q，期望「会员列表」", path, got)
		}
	}
}

// TestResolveTitleFallsBackToRawKey 未登记的模块返回原标识而不是空白。
//
// 新增模块时若忘了往 resourceTitles 里补名字，日志里至少还能看到
// 标识本身可供排查；返回空字符串则会让这一列彻底失去信息。
func TestResolveTitleFallsBackToRawKey(t *testing.T) {
	if got := resolveTitle("/api/v1/report/daily/list"); got != "report-daily" {
		t.Errorf("未登记模块应返回原标识，实际 %q", got)
	}
	// 路径段不足时也不能 panic
	for _, path := range []string{"", "/api", "/api/v1"} {
		if got := resolveTitle(path); got == "" {
			t.Errorf("resolveTitle(%q) 不应返回空字符串", path)
		}
	}
}
