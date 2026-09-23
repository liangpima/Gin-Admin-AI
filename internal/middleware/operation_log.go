package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"go-admin/internal/common"
	"go-admin/internal/logger"

	"github.com/gin-gonic/gin"
)

const (
	// maskedValue 敏感字段在日志中的占位符
	maskedValue = "******"
	// maxBodyLogLength 请求体落库的最大长度（字符），防止大 body 撑爆日志表
	maxBodyLogLength = 2000
)

// sensitiveNameFragments 判定「字段名 / 配置项名」是否敏感所用的片段（子串匹配）。
//
// 用子串而非全名精确匹配，是为了覆盖 clientSecret、userPassword、apiKey 这类
// 驼峰或带前缀的命名。刻意不包含裸 "key"：它在 {"key":"site.name","value":"..."}
// 这类结构里只是配置项名，本身不是密文。
var sensitiveNameFragments = []string{
	"password", "passwd", "secret", "token",
	"credential", "privatekey", "accesskey", "apiv3key", "secretkey", "pem",
}

// sensitiveNameSuffixes 配置项名的**末段**命中即视为敏感。
//
// 为什么需要它：片段表靠子串匹配，覆盖不到本项目真实的支付密钥命名 ——
// pay.alipay_key、pay.wechat_key 归一化后是 alipaykey / wechatkey，
// 不含 secret/accesskey/privatekey 任何片段，于是私钥原文会被明文写进
// sys_operation_log.request_param（已实际发生过）。
//
// 用「末段」而不是子串：sys_config 里 site.name、oss.bucket、
// sms.tpl_verify_code、pay.wechat_mch_id 这类非敏感项不能误伤，
// 而 pay.alipay_key、oss.secret_key、pay.wechat_cert_pem 必须命中。
var sensitiveNameSuffixes = []string{
	"key", "secret", "pwd", "pass", "password", "token", "credential", "pem",
}

// isSensitiveName 判断字段名或配置项名是否敏感。
//
// 先归一化（去下划线/连字符）再匹配，使 access_key、access-key、accessKey、
// accesskey 这几种写法都能命中同一个片段。
func isSensitiveName(name string) bool {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(name))
	for _, frag := range sensitiveNameFragments {
		if strings.Contains(normalized, frag) {
			return true
		}
	}
	return false
}

// isSensitiveConfigName 判断「配置项名」是否敏感，用于 key/value 分离的结构。
//
// 与 isSensitiveName 的区别在于判断对象：这里判断的是配置项**名称**
// （如 pay.alipay_key），不是 JSON 字段名，因此可以安全地按「末段」判定。
// 裸 "key" 这种规则不能用在字段名上 —— {"key":"site.name","value":...} 里的
// key 是结构字段，若据此打码会连配置项名一起抹掉，日志就失去排查价值。
func isSensitiveConfigName(name string) bool {
	if isSensitiveName(name) {
		return true
	}

	last := name
	if i := strings.LastIndexAny(name, "._-"); i >= 0 {
		last = name[i+1:]
	}
	last = strings.ToLower(strings.TrimSpace(last))
	for _, suf := range sensitiveNameSuffixes {
		if last == suf {
			return true
		}
	}
	return false
}

// isNameField 判断该字段是否为「配置项名称」字段
func isNameField(key string) bool {
	switch strings.ToLower(key) {
	case "key", "configkey", "config_key", "name", "config_name":
		return true
	}
	return false
}

// isValueField 判断该字段是否为「配置项取值」字段
func isValueField(key string) bool {
	switch strings.ToLower(key) {
	case "value", "configvalue", "config_value", "val":
		return true
	}
	return false
}

// maskSensitiveFields 递归替换 JSON 结构中的敏感字段值，返回处理后的值。
//
// 除按字段名脱敏外，还处理两类容易漏掉的情况：
//  1. 「名称 + 取值」分离：配置批量保存的 body 形如
//     {"items":[{"key":"secret_key","value":"真实密钥"}]}，
//     密钥在通用的 value 字段里，必须结合同级的 key 字段判断。
//  2. 嵌套 JSON 字符串：{"data":"{\"password\":\"x\"}"} 这类把 JSON 当字符串传的写法，
//     需要递归解析后再脱敏，否则会被整体跳过。
func maskSensitiveFields(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		// 先看同级是否存在敏感的「名称」字段，若有则其对应的「取值」也要脱敏。
		// 用 isSensitiveConfigName 而非 isSensitiveName：这里的 item 是配置项名
		// （pay.alipay_key），可以按末段判定；字段名本身不能套这条规则。
		sensitiveValue := false
		for k, item := range val {
			if !isNameField(k) {
				continue
			}
			if name, ok := item.(string); ok && isSensitiveConfigName(name) {
				sensitiveValue = true
				break
			}
		}

		for k, item := range val {
			if isSensitiveName(k) || (sensitiveValue && isValueField(k)) {
				val[k] = maskedValue
				continue
			}
			val[k] = maskSensitiveFields(item)
		}
		return val

	case []interface{}:
		for i, item := range val {
			val[i] = maskSensitiveFields(item)
		}
		return val

	case string:
		trimmed := strings.TrimSpace(val)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var inner interface{}
			if err := json.Unmarshal([]byte(trimmed), &inner); err == nil {
				if out, err := json.Marshal(maskSensitiveFields(inner)); err == nil {
					return string(out)
				}
			}
		}
		return val
	}

	return v
}

// sanitizeRequestBody 对请求体脱敏并按长度截断后再落库
func sanitizeRequestBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var parsed interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		// 非 JSON（如文件上传），不记录具体内容
		return "[non-json body omitted]"
	}

	parsed = maskSensitiveFields(parsed)

	out, err := json.Marshal(parsed)
	if err != nil {
		return "[unparsable body omitted]"
	}

	// 按字符截断，避免截断多字节字符
	runes := []rune(string(out))
	if len(runes) > maxBodyLogLength {
		return string(runes[:maxBodyLogLength]) + "...[truncated]"
	}
	return string(runes)
}

// OperationLogEntry 审计日志条目 —— middleware 侧的最小数据契约。
//
// 为什么不直接用 system 模块的 model.SysOperationLog：
// 中间件属于横切关注点，直接依赖业务模型意味着「业务表加一个字段」
// 会牵动中间件的编译与测试，依赖方向也被倒置。
// 这里只描述审计所需的信息，落库细节（表名、字段映射）交给业务侧适配器。
type OperationLogEntry struct {
	TenantID      uint
	Title         string
	Action        string
	RequestMethod string
	RequestURL    string
	RequestParam  string
	Status        int8
	IP            string
	UserAgent     string
	OperatorID    uint
	OperatorName  string
	CostTime      int64
	ErrorMsg      string
}

// OperationLogWriter 把审计条目落库的能力。
//
// 实现见 internal/module/system/service/operation_log_writer.go，
// 由 cmd/server/main.go 在启动时注入。
type OperationLogWriter interface {
	WriteOperationLog(entry *OperationLogEntry) error
}

// operationLogWriter 已注入的实现。启动阶段写入一次，运行期只读。
var operationLogWriter OperationLogWriter

// SetOperationLogWriter 注入审计日志写入实现，应在开始处理请求之前调用。
func SetOperationLogWriter(w OperationLogWriter) {
	operationLogWriter = w
}

var skipPaths = []string{
	"/api/v1/auth/userInfo",
	"/api/v1/system/log",
	"/api/v1/captcha",
	"/uploads/",
}

// 敏感 GET 路径也需要记录日志
var sensitiveGetPaths = []string{
	"/api/v1/system/user/",
	"/api/v1/system/role/",
	"/api/v1/system/config/",
	"/api/v1/system/file/",
	"/api/v1/member/",
	"/api/v1/system/pay/",
}

// isJSONContentType 判断请求体是否为 JSON。
//
// 只有 JSON 才需要读取并脱敏落库 —— sanitizeRequestBody 对非 JSON 本来就返回
// "[non-json body omitted]"。若不加这道判断，每个上传请求都会把完整 body
// （按 upload.max_size 最大 10MB）读进内存再丢弃，纯属浪费。
func isJSONContentType(ct string) bool {
	if ct == "" {
		return false
	}
	mediaType := ct
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		mediaType = ct[:i]
	}
	mediaType = strings.TrimSpace(strings.ToLower(mediaType))
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func OperationLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 非敏感 GET 请求跳过
		if c.Request.Method == "GET" {
			isSensitive := false
			for _, sp := range sensitiveGetPaths {
				if strings.HasPrefix(path, sp) {
					isSensitive = true
					break
				}
			}
			if !isSensitive {
				c.Next()
				return
			}
		}

		for _, skip := range skipPaths {
			if strings.HasPrefix(path, skip) {
				c.Next()
				return
			}
		}

		var bodyBytes []byte
		if c.Request.Body != nil && isJSONContentType(c.GetHeader("Content-Type")) {
			bodyBytes, _ = io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		start := time.Now()
		c.Next()
		latency := time.Since(start).Milliseconds()

		statusCode := c.Writer.Status()
		method := c.Request.Method
		clientIP := common.NormalizeIP(c.ClientIP())
		userAgent := c.Request.UserAgent()

		operatorID := common.GetCurrentUserID(c)
		operatorName := common.GetCurrentUsername(c)

		// 标题用**路由模板**（c.FullPath()，形如 /api/v1/member/:id）推导，
		// 而不是真实路径：真实路径里带的是实际 ID（/member/12），
		// 拼进标题会得到 member-12 这类值，同一接口的每次操作都不同，
		// 既看不懂也让「模块标题」筛选失效。
		// RequestURL 仍记录真实路径 —— 审计需要精确到具体操作了哪个资源。
		titlePath := c.FullPath()
		if titlePath == "" {
			// 未匹配到路由（如 404）时回退，至少留下点线索
			titlePath = path
		}
		title := resolveTitle(titlePath)

		entry := &OperationLogEntry{
			TenantID:      common.GetTenantID(c),
			Title:         title,
			Action:        method,
			RequestMethod: method,
			RequestURL:    path,
			RequestParam:  sanitizeRequestBody(bodyBytes),
			Status:        1,
			IP:            clientIP,
			UserAgent:     userAgent,
			OperatorID:    operatorID,
			OperatorName:  operatorName,
			CostTime:      latency,
		}

		if statusCode >= 400 {
			entry.Status = 0
			entry.ErrorMsg = "HTTP " + strings.TrimSpace(c.Errors.ByType(gin.ErrorTypePrivate).String())
		}

		// 审计写入是旁路能力：实现未注入或写库失败都不应把正常请求变成 500。
		// 但必须留下明确日志 —— 否则「审计静默失效」会长期无人察觉，
		// 等到需要追溯操作记录时才发现一片空白。
		if operationLogWriter == nil {
			logger.Log.Errorf("[operation-log] OperationLogWriter 未注入，审计日志未记录: %s %s", method, path)
			return
		}
		if err := operationLogWriter.WriteOperationLog(entry); err != nil {
			logger.Log.Errorf("[operation-log] 审计日志写入失败: %s %s: %v", method, path, err)
		}
	}
}

// resourceTitles 接口模块标识 → 中文名。
//
// 为什么需要这张表：resolveTitle 是从 URL 机械推导的，结果是 system-user、
// member-tag 这类**代码标识**，而日志页面「模块标题」列直接展示它 ——
// 非技术用户看不懂，也没法和侧边栏菜单名对上。
//
// 为什么在**写入时**翻译而不是查询时映射：title 同时是日志页筛选框的
// LIKE 查询目标。若库里存英文、界面显示中文，用户按中文搜不到任何记录。
//
// 命名取侧边栏菜单名（sys_menu.title），不额外加「系统管理-」这类前缀：
// 该列宽仅 100px，且资源名本身已足够区分。
// 未登记的模块返回原标识 —— 宁可显示得生硬，也不要变成空白。
var resourceTitles = map[string]string{
	// 权限管理
	"system-user": "用户管理",
	"system-role": "角色管理",
	"system-menu": "菜单管理",
	"system-dept": "部门管理",
	"system-post": "岗位管理",
	"system-file": "附件管理",
	// 系统设置
	"system-config":    "参数管理",
	"system-dict":      "数据字典",
	"system-log":       "操作日志",
	"system-agreement": "协议管理",
	// 支付管理
	"system-pay": "支付订单",
	// 会员管理
	"member":        "会员列表",
	"member-level":  "会员等级",
	"member-tag":    "会员标签",
	"member-tags":   "会员标签",
	"member-points": "积分明细",
	"member-status": "会员状态",
	"member-visit":  "会员访问",
	// 认证与首页
	"auth-logout":     "退出登录",
	"auth-userInfo":   "用户信息",
	"dashboard-stats": "工作台统计",
}

// resolveTitle 从**路由模板**推导操作日志的模块标题。
//
// 模板形如 /api/v1/system/user/list：第 4 段是模块、第 5 段是资源。
// 资源段的两种情况要忽略：
//   - `list` 只是动作，模块名已能表达（system-user → 用户管理）
//   - 路径参数（模板里写作 ":id"），早期实现会把它拼进标题，
//     于是 /api/v1/member/:id 记成了「member-:id」
func resolveTitle(routePath string) string {
	parts := strings.Split(routePath, "/")
	if len(parts) < 4 {
		return "未知模块"
	}

	module := parts[3]
	key := module
	if len(parts) >= 5 {
		resource := parts[4]
		if resource != "" && resource != "list" && !isPathParam(resource) {
			key = module + "-" + resource
		}
	}

	if name, ok := resourceTitles[key]; ok {
		return name
	}
	return key
}

// isPathParam 判断路径段是参数而非资源名。
//
// 模板里的参数写作 ":id"；但未匹配到路由时标题会回退用真实路径，
// 参数位置上就是实际取值（如 "12"）—— 那会生成 member-12、member-13
// 这样无限增殖的标题，既看不懂，也让「模块标题」筛选失去意义，
// 故一并按「纯数字」识别。
func isPathParam(segment string) bool {
	if strings.HasPrefix(segment, ":") {
		return true
	}
	if segment == "" {
		return false
	}
	for _, r := range segment {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
