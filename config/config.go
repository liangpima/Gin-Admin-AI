package config

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	JWT      JWTConfig      `mapstructure:"jwt"`
	Log      LogConfig      `mapstructure:"log"`
	Upload   UploadConfig   `mapstructure:"upload"`
	Casbin   CasbinConfig   `mapstructure:"casbin"`
	CORS     CORSConfig     `mapstructure:"cors"`
	Security SecurityConfig `mapstructure:"security"`
}

// SecurityConfig 安全相关的可调开关。
//
// 这些开关的共同点是「默认值即安全值」，配置项只是给确有需要的部署留出例外通道，
// 因此都必须能区分「没配」与「显式配成不安全的值」—— 用指针或 *bool 实现。
type SecurityConfig struct {
	// LoginFailClosed 决定登录限频在 Redis 不可用时如何取舍（默认 true）。
	//
	// true（默认）：拒绝本次登录。与 middleware/auth.go 的 token 吊销检查
	//   （fail-closed）保持一致 —— 限频失效期间正是暴力破解成本最低的窗口。
	// false：放行，可用性优先。此时限频会静默失效，日志里保留 Error 级痕迹。
	LoginFailClosed *bool `mapstructure:"login_fail_closed"`

	// TokenTransport 决定 token 从哪里**读取**、以及是否**下发** cookie。
	//
	//   header —— 只认 Authorization 头，不下发 cookie（改造前的行为，也是回滚开关）
	//   both   —— 头优先、其次 cookie；两种都接受（**默认**）
	//   cookie —— 只认 cookie
	//
	// 为什么是字符串而不是 bool：这里本质是三态，bool 表达不了。
	//
	// 为什么缺省落到 both 而不是 header：both 让新旧两条路径同时可用 ——
	// 后端先把 cookie 发出去（B1），前端再逐步切换（B2），任何一步出问题
	// 只要把本项改回 header 就立刻回到改造前的行为。若默认 header，
	// 后端发了 cookie 却没人用，等于没上线。
	//
	// 为什么**拼错的值直接拒绝启动**（见 validateSecurityCookies）而不是静默回退：
	// 静默回退会让「明明配了 cookie 却仍在读头」这类问题查很久 ——
	// 与 server.trusted_proxies 的教训同源：配错了比不配更危险。
	TokenTransport string `mapstructure:"token_transport"`

	// CookieSecure 决定 Set-Cookie 是否带 Secure 属性（默认 false）。
	//
	// 默认 false 是刻意的，且**不是**疏忽：本项目自带的 docker 编排是纯 HTTP
	// （deploy/nginx/default.conf 只监听 80），默认 true 会让浏览器完全不发送
	// 这些 cookie —— 登录态根本建立不起来，属于「安全默认值把默认部署打死」。
	// 所以这里的安全责任交给**显式配置 + 启动日志**：CookiePolicySummary()
	// 会把当前取值连同风险提示一起打进启动日志，上 HTTPS 时必须显式改成 true。
	CookieSecure *bool `mapstructure:"cookie_secure"`

	// CookieSameSite 决定 cookie 的 SameSite 属性（默认 lax）。
	//
	// 它是 CSRF 的**主防线**，而正确取值取决于部署形态：
	//   · 前后端同源（本项目 dev 走 Vite proxy、prod 走 nginx /api）→ lax 够用且最安全
	//   · 前后端分域名 → lax 会让 cookie 不被携带，只能改 none + secure，
	//     而那正是 CSRF 风险最高的配置（因此还需要 double-submit 兜底）
	// 这个耦合不能藏在默认值里，故写成配置项并打进启动日志。
	CookieSameSite string `mapstructure:"cookie_same_site"`
}

// token 传输方式的取值。
const (
	// TokenTransportHeader 只认 Authorization 头。改造前的行为，也是回滚开关。
	TokenTransportHeader = "header"
	// TokenTransportBoth 头优先、其次 cookie。默认值。
	TokenTransportBoth = "both"
	// TokenTransportCookie 只认 cookie。Swagger / curl 这类不会自动带 cookie
	// 的调用方会因此失效（这也是 Authorization 头不能删的原因）。
	TokenTransportCookie = "cookie"
)

// cookie SameSite 的取值。
const (
	CookieSameSiteLax    = "lax"
	CookieSameSiteStrict = "strict"
	CookieSameSiteNone   = "none"
)

// IsLoginFailClosed 返回登录限频的失败策略。
//
// 用指针的原因：Go 的零值 false 恰好等于「不安全的那一侧」。
// 若用普通 bool，漏配该项就等于关掉防护，而且没有任何迹象 —— 默认必须由代码给出。
func (c *SecurityConfig) IsLoginFailClosed() bool {
	if c.LoginFailClosed == nil {
		return true
	}
	return *c.LoginFailClosed
}

// Transport 返回归一化后的 token 传输方式，未配置或只写了空白时落到 both。
//
// 注意：**非法取值在这里也会落到 both**，但它同时会被 Validate 拦下并拒绝启动。
// 两处都要有是因为调用方（中间件）不该假设配置一定被校验过 ——
// 测试里直接构造 SecurityConfig 时就走不到 Validate。
func (c *SecurityConfig) Transport() string {
	switch strings.ToLower(strings.TrimSpace(c.TokenTransport)) {
	case TokenTransportHeader:
		return TokenTransportHeader
	case TokenTransportCookie:
		return TokenTransportCookie
	default:
		return TokenTransportBoth
	}
}

// IssuesCookies 判断是否需要下发认证 cookie。
func (c *SecurityConfig) IssuesCookies() bool {
	return c.Transport() != TokenTransportHeader
}

// AcceptsHeaderToken 判断是否接受 Authorization 头里的 token。
func (c *SecurityConfig) AcceptsHeaderToken() bool {
	return c.Transport() != TokenTransportCookie
}

// AcceptsCookieToken 判断是否接受 cookie 里的 token。
func (c *SecurityConfig) AcceptsCookieToken() bool {
	return c.Transport() != TokenTransportHeader
}

// CookieSecureEnabled 返回 cookie 是否带 Secure 属性（默认 false，理由见字段注释）。
func (c *SecurityConfig) CookieSecureEnabled() bool {
	if c.CookieSecure == nil {
		return false
	}
	return *c.CookieSecure
}

// SameSite 返回 cookie 的 SameSite 取值（默认 lax）。
func (c *SecurityConfig) SameSite() http.SameSite {
	switch strings.ToLower(strings.TrimSpace(c.CookieSameSite)) {
	case CookieSameSiteStrict:
		return http.SameSiteStrictMode
	case CookieSameSiteNone:
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

// SameSiteName 返回归一化后的 SameSite 名称，用于日志与断言。
func (c *SecurityConfig) SameSiteName() string {
	switch c.SameSite() {
	case http.SameSiteStrictMode:
		return CookieSameSiteStrict
	case http.SameSiteNoneMode:
		return CookieSameSiteNone
	default:
		return CookieSameSiteLax
	}
}

// CookiePolicySummary 返回一行可直接打进启动日志的传输策略摘要。
//
// 为什么必须打印：SameSite / Secure 的**正确取值取决于部署形态**
// （同源还是分域名、HTTP 还是 HTTPS），而配错的后果 ——
// 「登录态建立不起来」或「CSRF 防线失效」—— 都很难从现象反推配置。
// 与 server.trusted_proxies 的处理方式一致：把当前取值摆到启动日志里，
// 顺带把该取值下最需要注意的风险写在同一行，省得再翻文档。
func (c *SecurityConfig) CookiePolicySummary() string {
	if !c.IssuesCookies() {
		return "token 传输：仅 Authorization 头（不下发 cookie）"
	}

	how := "Authorization 头优先、其次 cookie"
	if c.Transport() == TokenTransportCookie {
		how = "仅 cookie（Swagger / curl 等不带 cookie 的调用方将失效）"
	}
	summary := fmt.Sprintf("token 传输：%s；cookie 属性 SameSite=%s Secure=%t",
		how, c.SameSiteName(), c.CookieSecureEnabled())

	if !c.CookieSecureEnabled() {
		summary += "（Secure=false 只适用于 HTTP 部署；上 HTTPS 后必须置 true，" +
			"否则 cookie 会随明文传输）"
	}
	if c.SameSite() == http.SameSiteNoneMode {
		summary += "（SameSite=None：跨站请求会携带 cookie，CSRF 面最大，" +
			"必须确认 double-submit 校验已启用）"
	}
	return summary
}

type CORSConfig struct {
	AllowOrigins     []string `mapstructure:"allow_origins"`
	AllowMethods     []string `mapstructure:"allow_methods"`
	AllowHeaders     []string `mapstructure:"allow_headers"`
	ExposeHeaders    []string `mapstructure:"expose_headers"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
}

type ServerConfig struct {
	// Host 监听地址，留空表示监听所有网卡（":8080"）。
	//
	// 提供它主要是为了让「开发环境只在本机可达」成为可配置项：
	// debug 模式下 Swagger 与调试信息全开，若同时又监听所有网卡、
	// 且 JWT 密钥仍是默认值，同网段任何人都能伪造 token 登录。
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	Mode         string `mapstructure:"mode"`
	ReadTimeout  int    `mapstructure:"read_timeout"`
	WriteTimeout int    `mapstructure:"write_timeout"`
	// ReadHeaderTimeout 限制读取请求头的时间，用于防 Slowloris：
	// 攻击者保持连接、每次只发几个字节的头，ReadTimeout 会被不断刷新，
	// 连接可被无限占用。缺省（<=0）时由 Validate 填默认值。
	ReadHeaderTimeout int `mapstructure:"read_header_timeout"`
	// TrustedProxies 反向代理的 IP 或 CIDR 列表，决定 X-Forwarded-For 是否可信。
	//
	// 必须显式配置，且**默认空**：gin 默认信任所有代理头，于是 c.ClientIP()
	// 直接取 X-Forwarded-For，攻击者每次伪造一个新 IP 就能重置登录失败计数，
	// 使 IP 维度的锁定完全失效。空列表表示「不信任任何代理头」，
	// ClientIP() 退化为连接对端地址，伪造失效。
	//
	// 部署在 nginx / SLB 之后时填其内网网段（如 10.0.0.0/8、172.16.0.0/12），
	// 不要填 0.0.0.0/0 —— 那等于恢复成「信任一切」，gin 也会直接拒绝。
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

type DatabaseConfig struct {
	Host            string `mapstructure:"host"`
	Port            int    `mapstructure:"port"`
	Username        string `mapstructure:"username"`
	Password        string `mapstructure:"password"`
	DBName          string `mapstructure:"dbname"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime int    `mapstructure:"conn_max_idle_time"`
}

func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		d.Username, d.Password, d.Host, d.Port, d.DBName)
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
}

type JWTConfig struct {
	Secret        string `mapstructure:"secret"`
	AccessExpire  int64  `mapstructure:"access_expire"`
	RefreshExpire int64  `mapstructure:"refresh_expire"`
}

type LogConfig struct {
	Level      string `mapstructure:"level"`
	Filename   string `mapstructure:"filename"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAge     int    `mapstructure:"max_age"`
	Compress   bool   `mapstructure:"compress"`
	// DBRetentionDays 数据库操作日志/登录日志的保留天数，超期由定时任务清理；
	// 小于等于 0 表示不自动清理
	DBRetentionDays int `mapstructure:"db_retention_days"`
}

type UploadConfig struct {
	SavePath  string `mapstructure:"save_path"`
	MaxSize   int    `mapstructure:"max_size"`
	AllowExts string `mapstructure:"allow_exts"`
}

type CasbinConfig struct {
	ModelPath string `mapstructure:"model_path"`
}

var Cfg Config

func Init(path string) error {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := viper.Unmarshal(&Cfg); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 环境变量覆盖敏感配置
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		Cfg.Database.Password = v
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		Cfg.JWT.Secret = v
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		Cfg.Redis.Password = v
	}

	return Validate()
}

// 请求头读取超时的默认值与上限（秒）。
//
// 默认 10s 对正常客户端绰绰有余（局域网内请求头是毫秒级），
// 上限 60s 是为了防止把它配成远大于 ReadTimeout 而失去防护意义。
const (
	defaultReadHeaderTimeout = 10
	maxReadHeaderTimeout     = 60
)

// Validate 校验关键配置项的取值范围，并补齐可缺省的字段。
//
// 为什么需要它：Init 只负责「解析」，解析成功不代表配置可用。
// 端口写成 70000、超时写成 0、JWT 有效期写成负数这类错误，
// 若不在启动时拦下，会一路带到运行期才以难以诊断的方式暴露 ——
// 端口越界表现成 bind 失败，超时为 0 表现成「请求永不超时」，
// 而 Slowloris 这类攻击恰好只需要一个没有超时上限的连接。
//
// 启动即失败，好过带病运行。
func Validate() error {
	var problems []string

	if Cfg.Server.Port < 1 || Cfg.Server.Port > 65535 {
		problems = append(problems, fmt.Sprintf(
			"server.port 非法（%d），应在 1-65535 之间", Cfg.Server.Port))
	}
	if Cfg.Server.ReadTimeout <= 0 {
		problems = append(problems,
			"server.read_timeout 必须为正数（为 0 时慢速连接可无限占用）")
	}
	if Cfg.Server.WriteTimeout <= 0 {
		problems = append(problems, "server.write_timeout 必须为正数")
	}

	// ReadHeaderTimeout 是后加字段，旧配置里没有，因此缺省时自动补默认值，
	// 而不是报错 —— 否则升级后所有既有配置文件都会导致启动失败。
	if Cfg.Server.ReadHeaderTimeout <= 0 {
		Cfg.Server.ReadHeaderTimeout = defaultReadHeaderTimeout
	}
	if Cfg.Server.ReadHeaderTimeout > maxReadHeaderTimeout {
		problems = append(problems, fmt.Sprintf(
			"server.read_header_timeout 过大（%d 秒），不应超过 %d 秒",
			Cfg.Server.ReadHeaderTimeout, maxReadHeaderTimeout))
	}

	problems = append(problems, validateTrustedProxies(Cfg.Server.TrustedProxies)...)
	problems = append(problems, validateSecurityCookies()...)

	if Cfg.Database.Host == "" {
		problems = append(problems, "database.host 不能为空")
	}
	if Cfg.Database.Port < 1 || Cfg.Database.Port > 65535 {
		problems = append(problems, fmt.Sprintf(
			"database.port 非法（%d），应在 1-65535 之间", Cfg.Database.Port))
	}
	if Cfg.Database.DBName == "" {
		problems = append(problems, "database.dbname 不能为空")
	}
	if Cfg.Database.MaxOpenConns < 1 {
		problems = append(problems, "database.max_open_conns 必须大于 0")
	}

	if Cfg.Redis.Addr == "" {
		problems = append(problems, "redis.addr 不能为空（本项目 Redis 为必需依赖）")
	}

	// access token 比 refresh token 还长（或相等）会让续期机制失去意义：
	// 客户端的 access 还没过期，refresh 已先失效，用户仍会被强制重登。
	if Cfg.JWT.AccessExpire <= 0 {
		problems = append(problems, "jwt.access_expire 必须为正数")
	}
	if Cfg.JWT.RefreshExpire <= 0 {
		problems = append(problems, "jwt.refresh_expire 必须为正数")
	}
	if Cfg.JWT.AccessExpire > 0 && Cfg.JWT.RefreshExpire > 0 &&
		Cfg.JWT.AccessExpire >= Cfg.JWT.RefreshExpire {
		problems = append(problems, fmt.Sprintf(
			"jwt.access_expire(%d) 必须小于 jwt.refresh_expire(%d)，否则 refresh token 无意义",
			Cfg.JWT.AccessExpire, Cfg.JWT.RefreshExpire))
	}

	if Cfg.Upload.SavePath == "" {
		problems = append(problems, "upload.save_path 不能为空")
	}
	if Cfg.Upload.MaxSize <= 0 {
		problems = append(problems, "upload.max_size 必须为正数（单位 MB）")
	}

	if Cfg.Casbin.ModelPath == "" {
		problems = append(problems, "casbin.model_path 不能为空")
	}

	if len(problems) > 0 {
		return fmt.Errorf("配置校验未通过：\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// validateTrustedProxies 校验 server.trusted_proxies 的每一项。
//
// 为什么在启动时校验而不是留给 r.SetTrustedProxies 报错：gin 在解析失败时
// 会**保留上一份（初始的「信任一切」）配置**，也就是「配错了反而比不配更危险」。
// 只有把校验前移到配置阶段，才能保证「要么按预期生效，要么起不来」。
//
// 同时显式拒绝 0.0.0.0/0 与 ::/0：写这两项的人通常是想表达「信任所有代理」，
// 但那等于让 X-Forwarded-For 重新变成可任意伪造的输入。
func validateTrustedProxies(proxies []string) []string {
	var problems []string
	for _, p := range proxies {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			problems = append(problems, "server.trusted_proxies 含空项（留空请直接写 []）")
			continue
		}
		if _, _, err := net.ParseCIDR(trimmed); err != nil {
			if net.ParseIP(trimmed) == nil {
				problems = append(problems, fmt.Sprintf(
					"server.trusted_proxies 项 %q 既不是合法 IP 也不是合法 CIDR", trimmed))
				continue
			}
		}
		if trimmed == "0.0.0.0/0" || trimmed == "::/0" {
			problems = append(problems, fmt.Sprintf(
				"server.trusted_proxies 项 %q 会让 X-Forwarded-For 完全可信（可被伪造），请填具体代理网段", trimmed))
		}
	}
	return problems
}

// validateSecurityCookies 校验 security 段里与 token 传输 / cookie 相关的取值。
//
// 单独成函数的原因与 validateTrustedProxies 相同：这属于「配错了比不配更危险」
// 的一类配置，需要能被独立覆盖，也需要在不启动整个服务的前提下断言。
func validateSecurityCookies() []string {
	var problems []string

	switch strings.ToLower(strings.TrimSpace(Cfg.Security.TokenTransport)) {
	case "", TokenTransportHeader, TokenTransportBoth, TokenTransportCookie:
		// 留空 = both（见 Transport 的注释）
	default:
		problems = append(problems, fmt.Sprintf(
			"security.token_transport 取值非法（%q），只能是 %s / %s / %s（留空 = %s）",
			Cfg.Security.TokenTransport,
			TokenTransportHeader, TokenTransportBoth, TokenTransportCookie, TokenTransportBoth))
	}

	switch strings.ToLower(strings.TrimSpace(Cfg.Security.CookieSameSite)) {
	case "", CookieSameSiteLax, CookieSameSiteStrict:
		// 留空 = lax
	case CookieSameSiteNone:
		// 浏览器**拒绝**为 SameSite=None 且不带 Secure 的 cookie 建库
		// （Chrome 起一律丢弃），表现为「登录接口返回成功但下一次请求就是未登录」。
		// 与其上线后去排查「为什么 cookie 没生效」，不如启动即失败。
		if !Cfg.Security.CookieSecureEnabled() {
			problems = append(problems,
				"security.cookie_same_site=none 必须同时设置 security.cookie_secure=true："+
					"浏览器会丢弃 SameSite=None 且不带 Secure 的 cookie，登录态将完全建立不起来")
		}
	default:
		problems = append(problems, fmt.Sprintf(
			"security.cookie_same_site 取值非法（%q），只能是 %s / %s / %s（留空 = %s）",
			Cfg.Security.CookieSameSite,
			CookieSameSiteLax, CookieSameSiteStrict, CookieSameSiteNone, CookieSameSiteLax))
	}

	return problems
}

// ListenAddr 返回 HTTP 服务的监听地址。
func (s ServerConfig) ListenAddr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// ListensOnAllInterfaces 判断监听地址是否对所有网卡可达。
// 空 Host、0.0.0.0、:: 都是「所有网卡」的写法。
func (s ServerConfig) ListensOnAllInterfaces() bool {
	switch s.Host {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}
	return false
}

// DevelopmentWarnings 汇总「开发模式下仍带病运行」的风险点，返回可逐条打印的文案。
//
// 为什么不直接拒绝启动：本地开发本来就需要 debug 模式与默认密钥，
// 一刀切会让「起步体验」和「生产安全」对立起来。但下面这几项叠加时
// （监听所有网卡 + 调试入口开放 + 默认 JWT 密钥），同网段任何人都能
// 伪造任意用户的 token 登录 —— 那已经不是「开发环境风险」，
// 而是一个可被直接利用的入口，因此必须在启动日志里一眼可见。
//
// 生产环境（mode=release）由 ValidateSecurity 直接拒绝启动，故返回空。
func DevelopmentWarnings() []string {
	if IsProduction() {
		return nil
	}

	var warnings []string

	warnings = append(warnings, fmt.Sprintf(
		"开发模式（mode=%s）：Swagger 文档与调试信息已开启，生产环境请设置 mode=release",
		Cfg.Server.Mode))

	if Cfg.Server.ListensOnAllInterfaces() {
		warnings = append(warnings, fmt.Sprintf(
			"服务监听在所有网卡（%s），同网段任何主机都可访问；仅需本机访问可设置 server.host: 127.0.0.1",
			Cfg.Server.ListenAddr()))
	}

	if secret := GetJWTSecret(); secret == "" || secret == defaultJWTSecret {
		warnings = append(warnings, fmt.Sprintf(
			"jwt.secret 仍为默认值（%q）：任何知道该值的人都能伪造任意用户的 token；"+
				"请设置环境变量 JWT_SECRET（生成方式：openssl rand -base64 48）", defaultJWTSecret))
	}

	if Cfg.Database.Password == defaultDBPassword {
		warnings = append(warnings, fmt.Sprintf(
			"database.password 仍为默认值（%q），请设置环境变量 DB_PASSWORD", defaultDBPassword))
	}

	return warnings
}

// GetJWTSecret returns JWT secret, preferring env var
func GetJWTSecret() string {
	if v := os.Getenv("JWT_SECRET"); v != "" {
		return v
	}
	return Cfg.JWT.Secret
}

// IsProduction returns true if mode is release
func IsProduction() bool {
	return strings.EqualFold(Cfg.Server.Mode, "release")
}

// 配置文件中的默认值。这些值公开可见，生产环境必须覆盖。
const (
	defaultJWTSecret  = "change-me-in-production"
	defaultDBPassword = "123456"
)

// ValidateSecurity 校验生产环境的关键密钥是否仍为默认值。
//
// 默认值一旦被带上生产环境，攻击者可据此伪造 JWT（等同于任意用户登录）
// 或直连数据库，因此这里直接拒绝启动，而不是仅打印告警。
// 开发环境不做限制，便于本地起步。
func ValidateSecurity() error {
	if !IsProduction() {
		return nil
	}

	var problems []string

	if secret := GetJWTSecret(); secret == "" || secret == defaultJWTSecret {
		problems = append(problems, "jwt.secret 仍为默认值（请设置环境变量 JWT_SECRET）")
	}
	if Cfg.Database.Password == defaultDBPassword {
		problems = append(problems, "database.password 仍为默认值（请设置环境变量 DB_PASSWORD）")
	}

	if len(problems) > 0 {
		return fmt.Errorf("生产环境安全检查未通过，已拒绝启动：\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}
