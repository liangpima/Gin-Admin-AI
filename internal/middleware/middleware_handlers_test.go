package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-admin/config"
	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/testsupport"
	"go-admin/pkg/auth"

	"github.com/gin-gonic/gin"
)

// 本文件补齐**中间件处理函数本身**的覆盖率。
//
// 本包此前已有不少用例，但全部集中在辅助函数上（sanitizeRequestBody、
// resolveTitle、casbin 内部、RequireAdminRole 判定），于是
// `go tool cover` 显示 Auth 23.4%、Recovery / OperationLog / UploadSecurity /
// Tenant / Cors / Logger 全是 0% —— 恰恰是真正挂在路由上的那一层。
//
// 这些用例的共同取舍：
//   · 全部走 gin 引擎的真实 ServeHTTP，而不是直接调函数 —— 中间件的行为
//     有一半在「c.Abort() 之后下游还跑不跑」上，只有走引擎才测得到
//   · 断言「拒绝」时同时断言 body 里的业务码与「处理函数没执行」，
//     不只看 HTTP 状态码
//   · 触碰包级变量（cache.RDB / roleResolver / operationLogWriter /
//     config.Cfg）的用例一律 save & restore，避免同包用例互相污染

func init() { gin.SetMode(gin.TestMode) }

// ─────────────────────────── 测试脚手架 ───────────────────────────

type reqOpt func(*http.Request)

// serveThrough 用一条最小路由跑一遍中间件，返回响应与「业务处理函数是否执行」。
//
// routePath 与 reqPath 分开传：OperationLog 的标题取自**路由模板**
// （c.FullPath()）而不是真实路径，只有把两者设成不同值才断言得到。
func serveThrough(
	mw gin.HandlerFunc,
	method, routePath, reqPath, body, contentType string,
	prepare reqOpt,
) (*httptest.ResponseRecorder, bool) {
	ran := false
	r := gin.New()
	r.Use(mw)
	r.Handle(method, routePath, func(c *gin.Context) {
		ran = true
		c.Status(http.StatusOK)
	})

	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, reqPath, rdr)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if prepare != nil {
		prepare(req)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, ran
}

// bodyCode 取出统一响应体里的业务码。
//
// 断言「拒绝」要看 body 里的 code，而不是只看 HTTP 状态码：本项目里
// 业务错误一律走 200 + body.code，只有 401/403 用真实 HTTP 状态。
func bodyCode(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var resp common.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v（body=%s）", err, w.Body.String())
	}
	return resp.Code
}

// withNilRedis 显式把 cache 的客户端置空，模拟「Redis 不可用」。
// 实现见 testsupport：member 模块也要用同一套门控，两处各写一份会漂移。
func withNilRedis(t *testing.T) {
	t.Helper()
	testsupport.WithNilRedis(t)
}

// withTestRedis 连到本机/CI 的 Redis；连不上就跳过。
func withTestRedis(t *testing.T) {
	t.Helper()
	testsupport.WithTestRedis(t)
}

// withJWTTTL 临时把 token 有效期设为正常值。
//
// 测试进程不加载 config.yaml，AccessExpire 是 0 → 签发出来的 token
// 立即过期，只会走到「token 无效」这一条分支，后面几步都覆盖不到。
func withJWTTTL(t *testing.T) {
	t.Helper()
	prevAccess, prevRefresh := config.Cfg.JWT.AccessExpire, config.Cfg.JWT.RefreshExpire
	config.Cfg.JWT.AccessExpire, config.Cfg.JWT.RefreshExpire = 3600, 7200
	t.Cleanup(func() {
		config.Cfg.JWT.AccessExpire, config.Cfg.JWT.RefreshExpire = prevAccess, prevRefresh
	})
}

// ─────────────────────────── Auth ───────────────────────────

// TestAuthMiddlewareFailClosed Auth 在「Redis 不可用」时的行为。
//
// 这是 P0-2 定下的取舍：吊销检查失败必须**拒绝**而不是放行。
// 早前用 `revoked, _ :=` 吞掉错误，Redis 抖动期间所有已登出的 access token
// 会重新生效 —— 而那恰恰是吊销机制最该起作用的时刻。
func TestAuthMiddlewareFailClosed(t *testing.T) {
	withNilRedis(t)

	cases := []struct {
		name     string
		header   string
		wantHTTP int
		wantCode int
		wantMsg  string
	}{
		{"缺少 Authorization 头", "", http.StatusUnauthorized, common.CodeUnauthorized, "请先登录"},
		{"只有 scheme 没有空格", "Bearer", http.StatusUnauthorized, common.CodeUnauthorized, "Token格式错误"},
		{"scheme 不是 Bearer", "Basic dXNlcjpwYXNz", http.StatusUnauthorized, common.CodeUnauthorized, "Token格式错误"},
		// 格式正确但查不了吊销状态 → 拒绝且不放行。
		// HTTP 状态是 200（业务错误走 body.code，见 common.Error 的约定），
		// 语义由 code=500 表达：「鉴权设施不可用」与「凭据不对」要能区分开，
		// 否则监控上会把基础设施故障看成用户在输错密码。
		{"格式正确但 Redis 不可用", "Bearer some.jwt.token", http.StatusOK, common.CodeInternalError, "鉴权服务暂时不可用"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, ran := serveThrough(Auth(), http.MethodGet, "/api/v1/system/user/list", "/api/v1/system/user/list", "", "",
				func(r *http.Request) {
					if tc.header != "" {
						r.Header.Set("Authorization", tc.header)
					}
				})

			if w.Code != tc.wantHTTP {
				t.Errorf("HTTP 状态应为 %d，实际 %d（body=%s）", tc.wantHTTP, w.Code, w.Body.String())
			}
			if got := bodyCode(t, w); got != tc.wantCode {
				t.Errorf("业务码应为 %d，实际 %d", tc.wantCode, got)
			}
			if !strings.Contains(w.Body.String(), tc.wantMsg) {
				t.Errorf("响应应包含 %q，实际 %s", tc.wantMsg, w.Body.String())
			}
			if ran {
				t.Error("鉴权未通过时业务处理函数不应执行")
			}
		})
	}
}

// TestAuthMiddlewareRedisDownRejectsValidToken Redis 不可用时，**合法** token 也必须被拒。
//
// 这是 fail-closed 真正的意义所在。上一条用例用的是伪造 token，即使把错误
// 吞掉，后面 ParseToken 仍会拦下 —— 看结果是 401，看不出问题。真正危险的是
// 「token 完全合法、只是查不了黑名单」：吞掉错误就等于放行，于是 Redis 抖动
// 期间所有已登出的 access token 重新生效，而那恰恰是吊销机制最该起作用的时刻。
func TestAuthMiddlewareRedisDownRejectsValidToken(t *testing.T) {
	withNilRedis(t)
	withJWTTTL(t)

	token, err := auth.GenerateAccessToken(7, "alice", 1, 3)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}

	w, ran := serveThrough(Auth(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
	})

	if ran {
		t.Error("Redis 不可用时合法 token 也不得放行（否则已登出的 token 会重新生效）")
	}
	if got := bodyCode(t, w); got != common.CodeInternalError {
		t.Errorf("应返回 500 语义（鉴权设施不可用），实际 %d（body=%s）", got, w.Body.String())
	}
}

// TestAuthMiddlewareInjectsClaims 合法 token 时把身份写进上下文。
func TestAuthMiddlewareInjectsClaims(t *testing.T) {
	withTestRedis(t)
	withJWTTTL(t)

	token, err := auth.GenerateAccessToken(7, "alice", 1, 3)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}

	var got struct {
		userID, tenantID, deptID uint
		username                 string
	}
	r := gin.New()
	r.Use(Auth())
	r.GET("/api/v1/system/user/list", func(c *gin.Context) {
		got.userID = common.GetCurrentUserID(c)
		got.username = common.GetCurrentUsername(c)
		got.tenantID = common.GetTenantID(c)
		got.deptID = common.GetDeptID(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/user/list", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("合法 token 应放行，实际 %d（body=%s）", w.Code, w.Body.String())
	}
	if got.userID != 7 || got.username != "alice" || got.tenantID != 1 || got.deptID != 3 {
		t.Errorf("上下文身份不正确: %+v", got)
	}
}

// TestAuthMiddlewareRejectsRevokedTokens 两类吊销都要拦下。
func TestAuthMiddlewareRejectsRevokedTokens(t *testing.T) {
	withTestRedis(t)
	withJWTTTL(t)

	t.Run("token 级吊销", func(t *testing.T) {
		token, err := auth.GenerateAccessToken(7, "alice", 1, 3)
		if err != nil {
			t.Fatalf("签发 token 失败: %v", err)
		}
		if err := cache.RevokeToken(context.Background(), token, time.Minute); err != nil {
			t.Fatalf("吊销 token 失败: %v", err)
		}
		t.Cleanup(func() {
			_ = cache.Del(context.Background(), "token:blacklist:"+token)
		})

		w, ran := serveThrough(Auth(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
		})

		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Token已失效") {
			t.Errorf("已吊销 token 应返回 401 Token已失效，实际 %d（body=%s）", w.Code, w.Body.String())
		}
		if ran {
			t.Error("已吊销 token 不应放行到业务处理函数")
		}
	})

	t.Run("用户级吊销（改密/禁用）", func(t *testing.T) {
		token, err := auth.GenerateAccessToken(8, "bob", 1, 3)
		if err != nil {
			t.Fatalf("签发 token 失败: %v", err)
		}
		key := fmt.Sprintf("user:token_revoked:%d", 8)
		if err := cache.Set(context.Background(), key, "1", time.Minute); err != nil {
			t.Fatalf("写入用户级吊销标记失败: %v", err)
		}
		t.Cleanup(func() { _ = cache.Del(context.Background(), key) })

		w, ran := serveThrough(Auth(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
		})

		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "请重新登录") {
			t.Errorf("用户级已吊销应返回 401 并提示重新登录，实际 %d（body=%s）", w.Code, w.Body.String())
		}
		if ran {
			t.Error("用户级已吊销不应放行到业务处理函数")
		}
	})

	t.Run("Refresh Token 冒充 Access Token", func(t *testing.T) {
		// 签发者不同（go-admin-refresh vs go-admin），必须被 ParseToken 拒绝 ——
		// 否则长效的 refresh token 可以直接拿去访问受保护接口。
		token, err := auth.GenerateRefreshToken(7, "alice", 1, 3)
		if err != nil {
			t.Fatalf("签发 refresh token 失败: %v", err)
		}

		w, ran := serveThrough(Auth(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
		})

		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Token无效或已过期") {
			t.Errorf("refresh token 冒充 access token 应被拒，实际 %d（body=%s）", w.Code, w.Body.String())
		}
		if ran {
			t.Error("refresh token 不应放行")
		}
	})
}

// TestAuthMiddlewarePlatformIdentity tenantID=0 的平台级旁路（P1-3）。
//
// TenantScope(db, 0) 的语义是「不过滤租户」，所以一张 tenantID=0 的 token
// 等价于跨租户读写能力。光有 token 里写着 0 不够，还必须持有 admin 角色。
func TestAuthMiddlewarePlatformIdentity(t *testing.T) {
	withTestRedis(t)
	withJWTTTL(t)

	t.Run("tenantID=0 且非 admin 被拒", func(t *testing.T) {
		withRoleResolver(t, stubRoleResolver{codes: []string{"editor"}})

		token, err := auth.GenerateAccessToken(9, "carol", 0, 0)
		if err != nil {
			t.Fatalf("签发 token 失败: %v", err)
		}

		w, ran := serveThrough(Auth(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
		})

		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "账号租户信息异常") {
			t.Errorf("tenantID=0 的非 admin 账号应被拒，实际 %d（body=%s）", w.Code, w.Body.String())
		}
		if ran {
			t.Error("平台级身份校验未通过时不应放行")
		}
	})

	t.Run("tenantID=0 且持有 admin 放行", func(t *testing.T) {
		withRoleResolver(t, stubRoleResolver{codes: []string{AdminRoleCode}})

		token, err := auth.GenerateAccessToken(1, "root", 0, 0)
		if err != nil {
			t.Fatalf("签发 token 失败: %v", err)
		}

		w, ran := serveThrough(Auth(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
		})

		if w.Code != http.StatusOK || !ran {
			t.Errorf("平台级 admin 应放行，实际 %d（body=%s）", w.Code, w.Body.String())
		}
	})

	t.Run("普通租户账号不解析角色", func(t *testing.T) {
		// 解析器返回错误，但 tenantID != 0 时不该走到角色解析 ——
		// 若走了，这里会因为解析失败而 401，用例即转红。
		withRoleResolver(t, stubRoleResolver{err: fmt.Errorf("不该被调用")})

		token, err := auth.GenerateAccessToken(7, "alice", 1, 3)
		if err != nil {
			t.Fatalf("签发 token 失败: %v", err)
		}

		w, ran := serveThrough(Auth(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+token)
		})

		if w.Code != http.StatusOK || !ran {
			t.Errorf("普通租户账号不应受角色解析影响，实际 %d（body=%s）", w.Code, w.Body.String())
		}
	})
}

// ─────────────────────────── Recovery ───────────────────────────

// TestRecoveryMiddlewareHidesPanicDetail panic 不能把内部细节回给调用方。
func TestRecoveryMiddlewareHidesPanicDetail(t *testing.T) {
	const secret = "secret-dsn=postgres://user:pw@db/internal"

	r := gin.New()
	r.Use(Recovery())
	r.GET("/boom", func(c *gin.Context) {
		panic(secret)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("panic 应返回 500，实际 %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret-dsn") {
		t.Errorf("响应体泄漏了 panic 细节: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "服务器内部错误") {
		t.Errorf("响应体应为通用文案，实际 %s", w.Body.String())
	}
	if got := bodyCode(t, w); got != common.CodeInternalError {
		t.Errorf("业务码应为 %d，实际 %d", common.CodeInternalError, got)
	}
}

// TestRecoveryMiddlewarePassesThrough 正常请求不受影响。
func TestRecoveryMiddlewarePassesThrough(t *testing.T) {
	w, ran := serveThrough(Recovery(), http.MethodGet, "/ok", "/ok", "", "", nil)
	if w.Code != http.StatusOK || !ran {
		t.Errorf("正常请求应原样通过，实际 %d", w.Code)
	}
}

// TestSanitizeRequest sanitizeRequest 的脱敏规则。
//
// panic 日志里会 dump 整个请求头，Authorization 与 Cookie 必须打码 ——
// 否则一次 panic 就把所有人的 token 写进日志文件。
func TestSanitizeRequest(t *testing.T) {
	cases := []struct {
		name       string
		req        string
		wantSub    []string
		wantAbsent []string
	}{
		{
			name:       "Authorization 被打码",
			req:        "GET /x HTTP/1.1\nAuthorization: Bearer eyJhbGciOi.abc.def\nHost: x",
			wantSub:    []string{"Authorization: Bearer [REDACTED]"},
			wantAbsent: []string{"eyJhbGciOi.abc.def"},
		},
		{
			name:       "Cookie 被打码",
			req:        "GET /x HTTP/1.1\nCookie: token=super-secret; theme=dark",
			wantSub:    []string{"Cookie: [REDACTED]"},
			wantAbsent: []string{"super-secret", "theme=dark"},
		},
		{
			name:       "大小写不敏感",
			req:        "GET /x HTTP/1.1\nauthorization: Bearer leakme",
			wantSub:    []string{"Authorization: Bearer [REDACTED]"},
			wantAbsent: []string{"leakme"},
		},
		{
			name:    "普通头原样保留",
			req:     "GET /x HTTP/1.1\nUser-Agent: curl/8\nAccept: */*",
			wantSub: []string{"User-Agent: curl/8", "Accept: */*"},
		},
		{
			name:    "无敏感头时原样返回",
			req:     "GET /x HTTP/1.1",
			wantSub: []string{"GET /x HTTP/1.1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeRequest(tc.req)
			for _, s := range tc.wantSub {
				if !strings.Contains(got, s) {
					t.Errorf("应包含 %q，实际 %q", s, got)
				}
			}
			for _, s := range tc.wantAbsent {
				if strings.Contains(got, s) {
					t.Errorf("不应包含 %q，实际 %q", s, got)
				}
			}
		})
	}
}

// ─────────────────────────── UploadSecurity ───────────────────────────

// TestUploadSecurityHeaders 静态上传目录的安全响应头。
//
// 上传白名单包含 .svg，而 SVG 可以内嵌 <script>；该目录又是匿名可访问的，
// 同源下打开即等于存储型 XSS。这里用 CSP 把「当文档打开」的行为限制住。
func TestUploadSecurityHeaders(t *testing.T) {
	w, ran := serveThrough(UploadSecurity(), http.MethodGet, "/uploads/a.svg", "/uploads/a.svg", "", "", nil)

	if !ran {
		t.Fatal("UploadSecurity 不应阻断请求")
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options 应为 nosniff，实际 %q", got)
	}
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"sandbox", "default-src 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP 应包含 %q，实际 %q", want, csp)
		}
	}
}

// ─────────────────────────── Tenant ───────────────────────────

// TestTenantMiddlewareIsNoop Tenant 不再从 Header/Query 读取租户。
//
// 这条断言看着像在测「什么都没做」，但它守的是一个安全边界：
// 一旦有人把 tenant_id 重新改成从请求头读取，租户隔离就变成客户端可自选的，
// 本用例会立刻转红。
func TestTenantMiddlewareIsNoop(t *testing.T) {
	var tenantID uint
	var hasKey bool

	r := gin.New()
	r.Use(Tenant())
	r.GET("/x", func(c *gin.Context) {
		_, hasKey = c.Get(common.ContextKeyTenantID)
		tenantID = common.GetTenantID(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x?tenant_id=99", nil)
	req.Header.Set("X-Tenant-Id", "99")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if hasKey {
		t.Error("Tenant 中间件不应写入租户上下文（租户只能来自 JWT claims）")
	}
	if tenantID != 0 {
		t.Errorf("租户 ID 不应来自请求参数或请求头，实际 %d", tenantID)
	}
}

// ─────────────────────────── Cors ───────────────────────────

// TestCorsMiddleware 跨域头与预检请求。
func TestCorsMiddleware(t *testing.T) {
	prevMode := config.Cfg.Server.Mode
	prevCORS := config.Cfg.CORS
	config.Cfg.Server.Mode = "debug"
	config.Cfg.CORS = config.CORSConfig{}
	t.Cleanup(func() {
		config.Cfg.Server.Mode = prevMode
		config.Cfg.CORS = prevCORS
	})

	t.Run("实际请求带上允许来源", func(t *testing.T) {
		w, ran := serveThrough(Cors(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Origin", "http://localhost:3000")
		})

		if !ran {
			t.Fatal("Cors 不应阻断实际请求")
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got == "" {
			t.Error("应返回 Access-Control-Allow-Origin")
		}
	})

	t.Run("预检请求返回允许的方法", func(t *testing.T) {
		w, _ := serveThrough(Cors(), http.MethodOptions, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Origin", "http://localhost:3000")
			r.Header.Set("Access-Control-Request-Method", "POST")
		})

		if w.Code != http.StatusNoContent {
			t.Errorf("预检应返回 204，实际 %d", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
			t.Errorf("Allow-Methods 应包含 POST，实际 %q", got)
		}
	})

	t.Run("未配置白名单且开启凭证时收紧为同源", func(t *testing.T) {
		// AllowAllOrigins 与 AllowCredentials 同时生效，等价于对任意站点
		// 开放「带凭证」访问 —— 必须避免该组合。
		config.Cfg.CORS = config.CORSConfig{AllowCredentials: true}

		w, ran := serveThrough(Cors(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Origin", "http://evil.example")
		})

		// 来源不被允许时 gin-contrib/cors 会 AbortWithStatus(403)，
		// 业务处理函数不会执行 —— 这是「拒绝」而不是「放行但不给头」。
		if ran {
			t.Error("来源不被允许时业务处理函数不应执行")
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("来源不被允许时应返回 403，实际 %d", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got == "http://evil.example" {
			t.Errorf("开启了 allow_credentials 时不应回显任意来源，实际 %q", got)
		}
	})

	// 这条用例是本次补测时发现的**真 bug** 的回归测试。
	//
	// cors.go 原先的写法是：生产模式（或开了 allow_credentials）且未配
	// allow_origins 时，只打一条日志，然后把 AllowOrigins 留空传给
	// cors.New —— 而 gin-contrib/cors 的 Validate 对「不允许全部来源 +
	// 无 AllowOriginFunc + AllowOrigins 为空」是直接 **panic**。
	// Cors() 又是在 router.Setup 里调用的，所以配成生产模式却没写
	// cors.allow_origins 的后果是**服务启动即崩**，而不是注释里写的
	// 「拒绝全部跨域请求」。现在改用 AllowOriginFunc 恒 false 表达拒绝。
	t.Run("生产模式未配白名单：拒绝跨域而不是 panic", func(t *testing.T) {
		config.Cfg.Server.Mode = "release"
		config.Cfg.CORS = config.CORSConfig{}

		// 这里若 panic，用例直接失败 —— 这正是要守住的行为
		var mw gin.HandlerFunc
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Cors() 不应 panic（生产模式 + 未配白名单），实际 panic: %v", r)
				}
			}()
			mw = Cors()
		}()

		w, ran := serveThrough(mw, http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Origin", "http://evil.example")
		})

		if ran {
			t.Error("生产模式未配白名单时业务处理函数不应执行")
		}
		if w.Code != http.StatusForbidden {
			t.Errorf("生产模式未配白名单时应返回 403，实际 %d", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("生产模式未配白名单时应拒绝全部跨域，实际回了 %q", got)
		}
	})

	t.Run("配置了白名单时只放行白名单来源", func(t *testing.T) {
		config.Cfg.CORS = config.CORSConfig{AllowOrigins: []string{"https://admin.example"}}

		w, _ := serveThrough(Cors(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Origin", "https://admin.example")
		})
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example" {
			t.Errorf("白名单内来源应放行，实际 %q", got)
		}

		w2, _ := serveThrough(Cors(), http.MethodGet, "/x", "/x", "", "", func(r *http.Request) {
			r.Header.Set("Origin", "https://evil.example")
		})
		if got := w2.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("白名单外来源不应放行，实际 %q", got)
		}
	})
}

// ─────────────────────────── Logger ───────────────────────────

// TestLoggerMiddleware 访问日志中间件不影响请求。
func TestLoggerMiddleware(t *testing.T) {
	w, ran := serveThrough(Logger(), http.MethodGet, "/api/v1/system/user/list", "/api/v1/system/user/list?page=1", "", "", nil)

	if w.Code != http.StatusOK || !ran {
		t.Errorf("请求应正常通过，实际 %d", w.Code)
	}
}

// TestLoggerMiddlewareDoesNotConsumeBody 访问日志不读请求体。
//
// 早前这里 io.ReadAll 了整个 body 再塞回去，但结果从未被使用 ——
// 上传接口按 upload.max_size 最大 10MB，N 个并发上传就是 10N MB 的额外占用。
// 这里用「读完之后还能不能再读」来守住「没有被消费掉」这件事。
func TestLoggerMiddlewareDoesNotConsumeBody(t *testing.T) {
	const payload = `{"username":"alice"}`
	var seen string

	r := gin.New()
	r.Use(Logger())
	r.POST("/x", func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		seen = string(b)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if seen != payload {
		t.Errorf("下游应读到完整请求体，实际 %q", seen)
	}
}

// ─────────────────────────── OperationLog ───────────────────────────

type recordingWriter struct {
	entries []*OperationLogEntry
	err     error
}

func (w *recordingWriter) WriteOperationLog(e *OperationLogEntry) error {
	w.entries = append(w.entries, e)
	return w.err
}

// withOperationLogWriter 注入审计写入实现并在用例结束时还原。
// operationLogWriter 是包级变量，不还原会污染同包其它用例。
func withOperationLogWriter(t *testing.T, w OperationLogWriter) {
	t.Helper()
	prev := operationLogWriter
	SetOperationLogWriter(w)
	t.Cleanup(func() { SetOperationLogWriter(prev) })
}

// TestIsJSONContentType 只有 JSON 才需要读请求体。
//
// 若不加这道判断，每个上传请求都会把完整 body（最大 10MB）读进内存再丢弃。
func TestIsJSONContentType(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"APPLICATION/JSON", true},
		{" application/json ", true},
		{"application/vnd.api+json", true},
		{"application/problem+json", true},
		{"", false},
		{"text/plain", false},
		{"multipart/form-data; boundary=----x", false},
		{"application/x-www-form-urlencoded", false},
		// 只是前缀相同，不能误判
		{"application/jsonp", false},
	}

	for _, tc := range cases {
		if got := isJSONContentType(tc.ct); got != tc.want {
			t.Errorf("isJSONContentType(%q) = %v，期望 %v", tc.ct, got, tc.want)
		}
	}
}

// TestOperationLogSkipsNonSensitivePaths 不该记的不记。
func TestOperationLogSkipsNonSensitivePaths(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"非敏感 GET", http.MethodGet, "/api/v1/dashboard/stats"},
		{"skipPaths：用户信息", http.MethodPost, "/api/v1/auth/userInfo"},
		{"skipPaths：验证码", http.MethodPost, "/api/v1/captcha/generate"},
		{"skipPaths：日志查询", http.MethodPost, "/api/v1/system/log/list"},
		{"skipPaths：上传目录", http.MethodGet, "/uploads/a.png"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingWriter{}
			withOperationLogWriter(t, rec)

			w, ran := serveThrough(OperationLog(), tc.method, tc.path, tc.path, "", "", nil)
			if !ran || w.Code != http.StatusOK {
				t.Fatalf("请求应正常通过，实际 %d", w.Code)
			}
			if len(rec.entries) != 0 {
				t.Errorf("不应写入审计日志，实际写了 %d 条: %+v", len(rec.entries), rec.entries[0])
			}
		})
	}
}

// TestOperationLogRecordsSensitiveGet 敏感 GET 需要记录（读取也属于审计范围）。
func TestOperationLogRecordsSensitiveGet(t *testing.T) {
	rec := &recordingWriter{}
	withOperationLogWriter(t, rec)

	w, ran := serveThrough(OperationLog(), http.MethodGet, "/api/v1/system/user/list",
		"/api/v1/system/user/list", "", "", nil)
	if !ran || w.Code != http.StatusOK {
		t.Fatalf("请求应正常通过，实际 %d", w.Code)
	}
	if len(rec.entries) != 1 {
		t.Fatalf("敏感 GET 应写入 1 条审计日志，实际 %d 条", len(rec.entries))
	}
	if got := rec.entries[0].Title; got != "用户管理" {
		t.Errorf("模块标题应为「用户管理」，实际 %q", got)
	}
}

// TestOperationLogEntryFields 逐字段钉住审计条目的装配。
func TestOperationLogEntryFields(t *testing.T) {
	rec := &recordingWriter{}
	withOperationLogWriter(t, rec)

	const payload = `{"username":"alice","password":"P@ssw0rd!"}`

	ran := false
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(common.ContextKeyUserID, uint(7))
		c.Set(common.ContextKeyUsername, "alice")
		c.Set(common.ContextKeyTenantID, uint(1))
	})
	r.Use(OperationLog())
	// 路由模板带参数、真实路径带实际 ID —— 用来断言标题取的是模板。
	r.POST("/api/v1/member/:id", func(c *gin.Context) {
		ran = true
		// 下游必须仍能读到完整请求体：中间件读完之后要还原
		b, _ := io.ReadAll(c.Request.Body)
		if string(b) != payload {
			t.Errorf("下游应读到完整请求体，实际 %q", string(b))
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/member/12", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "curl/8.0")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !ran {
		t.Fatal("业务处理函数未执行")
	}
	if len(rec.entries) != 1 {
		t.Fatalf("应写入 1 条审计日志，实际 %d 条", len(rec.entries))
	}

	e := rec.entries[0]
	if e.TenantID != 1 {
		t.Errorf("TenantID 应为 1，实际 %d", e.TenantID)
	}
	if e.Action != http.MethodPost || e.RequestMethod != http.MethodPost {
		t.Errorf("Action/RequestMethod 应为 POST，实际 %q/%q", e.Action, e.RequestMethod)
	}
	// RequestURL 存**真实路径**（审计要精确到操作了哪个资源）
	if e.RequestURL != "/api/v1/member/12" {
		t.Errorf("RequestURL 应为真实路径，实际 %q", e.RequestURL)
	}
	// Title 存**路由模板**推导出的中文名（真实路径会拼出 member-12）
	if e.Title != "会员列表" {
		t.Errorf("Title 应由路由模板推导，实际 %q", e.Title)
	}
	if e.Status != 1 {
		t.Errorf("2xx 请求 Status 应为 1，实际 %d", e.Status)
	}
	if e.OperatorID != 7 || e.OperatorName != "alice" {
		t.Errorf("操作者应为 7/alice，实际 %d/%s", e.OperatorID, e.OperatorName)
	}
	if e.UserAgent != "curl/8.0" {
		t.Errorf("UserAgent 应为 curl/8.0，实际 %q", e.UserAgent)
	}
	if strings.Contains(e.RequestParam, "P@ssw0rd!") {
		t.Errorf("审计参数里不能出现明文密码，实际 %q", e.RequestParam)
	}
	if !strings.Contains(e.RequestParam, "alice") {
		t.Errorf("非敏感字段应保留，实际 %q", e.RequestParam)
	}
}

// TestOperationLogMarksFailure 失败请求要标记出来。
func TestOperationLogMarksFailure(t *testing.T) {
	rec := &recordingWriter{}
	withOperationLogWriter(t, rec)

	r := gin.New()
	r.Use(OperationLog())
	r.POST("/api/v1/system/user", func(c *gin.Context) {
		_ = c.Error(fmt.Errorf("用户名已存在"))
		c.Status(http.StatusBadRequest)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/user", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("状态码应为 400，实际 %d", w.Code)
	}
	if len(rec.entries) != 1 {
		t.Fatalf("应写入 1 条审计日志，实际 %d 条", len(rec.entries))
	}
	if rec.entries[0].Status != 0 {
		t.Errorf("失败请求 Status 应为 0，实际 %d", rec.entries[0].Status)
	}
	if rec.entries[0].ErrorMsg == "" {
		t.Error("失败请求应带上 ErrorMsg")
	}
}

// TestOperationLogNonJSONBody 非 JSON 请求体不读取，只留占位说明。
func TestOperationLogNonJSONBody(t *testing.T) {
	rec := &recordingWriter{}
	withOperationLogWriter(t, rec)

	const payload = "binary-ish content that must not be stored"

	w, _ := serveThrough(OperationLog(), http.MethodPost, "/api/v1/system/user",
		"/api/v1/system/user", payload, "text/plain", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("请求应正常通过，实际 %d", w.Code)
	}
	if len(rec.entries) != 1 {
		t.Fatalf("应写入 1 条审计日志，实际 %d 条", len(rec.entries))
	}
	if strings.Contains(rec.entries[0].RequestParam, "binary-ish") {
		t.Errorf("非 JSON 请求体不应被记录，实际 %q", rec.entries[0].RequestParam)
	}
}

// TestOperationLogIsBestEffort 审计是旁路能力，不能把正常请求变成 500。
//
// 但两种失效都必须留下日志 —— 否则「审计静默失效」会长期无人察觉，
// 等到需要追溯操作记录时才发现一片空白。
func TestOperationLogIsBestEffort(t *testing.T) {
	t.Run("writer 未注入", func(t *testing.T) {
		withOperationLogWriter(t, nil)

		w, ran := serveThrough(OperationLog(), http.MethodPost, "/api/v1/system/user",
			"/api/v1/system/user", `{}`, "application/json", nil)
		if w.Code != http.StatusOK || !ran {
			t.Errorf("writer 未注入时请求仍应正常，实际 %d", w.Code)
		}
	})

	t.Run("writer 写入失败", func(t *testing.T) {
		withOperationLogWriter(t, &recordingWriter{err: fmt.Errorf("数据库不可用")})

		w, ran := serveThrough(OperationLog(), http.MethodPost, "/api/v1/system/user",
			"/api/v1/system/user", `{}`, "application/json", nil)
		if w.Code != http.StatusOK || !ran {
			t.Errorf("审计写入失败时请求仍应正常，实际 %d", w.Code)
		}
	})
}

// TestOperationLogFallsBackToRealPath 未匹配路由时回退用真实路径。
func TestOperationLogFallsBackToRealPath(t *testing.T) {
	rec := &recordingWriter{}
	withOperationLogWriter(t, rec)

	// NoRoute 上挂中间件：此时 c.FullPath() 是空字符串
	r := gin.New()
	r.Use(OperationLog())
	r.NoRoute(func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/does-not-exist", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("状态码应为 404，实际 %d", w.Code)
	}
	if len(rec.entries) != 1 {
		t.Fatalf("应写入 1 条审计日志，实际 %d 条", len(rec.entries))
	}
	if rec.entries[0].Title != "does-not-exist" {
		t.Errorf("未登记模块的标题应回退为原标识，实际 %q", rec.entries[0].Title)
	}
	if rec.entries[0].RequestURL != "/api/v1/does-not-exist" {
		t.Errorf("RequestURL 应为真实路径，实际 %q", rec.entries[0].RequestURL)
	}
}

// ─────────────────────────── 包级注入点 ───────────────────────────

// TestSetRoleResolverAndClearRoleCache 两个此前 0% 的包级函数。
func TestSetRoleResolverAndClearRoleCache(t *testing.T) {
	t.Run("SetRoleResolver 生效", func(t *testing.T) {
		// 显式置空 Redis：这样「缓存必然未命中」是确定的。
		// 此前这里没有固定 Redis 状态，而 Redis 里可能残留着上一次运行写入的
		// rbac:roles:1:7（TTL 60 秒）—— 命中缓存时解析器根本不被调用，
		// 用例照样通过，包覆盖率却在 92.2% / 92.7% 之间摆动。
		withNilRedis(t)

		resolver := &countingRoleResolver{codes: []string{"editor"}}
		// 必须走 SetRoleResolver 本身：此前用的是 withRoleResolver（直接给包级变量
		// 赋值），于是 SetRoleResolver 始终 0% —— 用例名宣称覆盖了它，实际没有。
		prev := roleResolver
		SetRoleResolver(resolver)
		t.Cleanup(func() { roleResolver = prev })

		got, err := RoleCodesFor(1, 7)
		if err != nil {
			t.Fatalf("解析角色失败: %v", err)
		}
		if len(got) != 1 || got[0] != "editor" {
			t.Errorf("应解析出 editor，实际 %v", got)
		}
		// 断言解析器真的被调用过 —— 只断言返回值的话，
		// 「命中残留缓存」也能让用例通过，测试就悄悄失去了区分力。
		if resolver.calls != 1 {
			t.Errorf("缓存未命中时必须查一次解析器，实际 %d 次", resolver.calls)
		}
	})

	t.Run("解析器未注入时拒绝", func(t *testing.T) {
		withNilRedis(t)
		// roleResolver 为 nil 属启动配置错误。此时必须返回错误（上层按无角色
		// 拒绝 → 403），而不是返回空角色列表被当成「有权限」。
		withRoleResolver(t, nil)

		if _, err := RoleCodesFor(1, 7); err == nil {
			t.Error("解析器未注入时必须返回错误（fail-closed），不能静默放行")
		}
	})

	t.Run("userID 为 0 直接返回空", func(t *testing.T) {
		// 未登录/系统内部调用没有用户身份，不该去查库或查缓存
		got, err := RoleCodesFor(1, 0)
		if err != nil || got != nil {
			t.Errorf("userID=0 应返回 (nil, nil)，实际 (%v, %v)", got, err)
		}
	})

	t.Run("ClearRoleCache 在 Redis 不可用时只记日志", func(t *testing.T) {
		withNilRedis(t)

		// 不 panic 即可 —— 清理失败必须留下日志（见函数内注释），
		// 但不能把调用方打挂。
		ClearRoleCache(1, 7)
	})

	t.Run("ClearRoleCache 在 Redis 可用时清掉缓存键", func(t *testing.T) {
		withTestRedis(t)

		key := fmt.Sprintf("rbac:roles:%d:%d", 1, 7)
		if err := cache.Set(context.Background(), key, "editor", time.Minute); err != nil {
			t.Fatalf("准备缓存失败: %v", err)
		}

		ClearRoleCache(1, 7)

		exists, err := cache.Exists(context.Background(), key)
		if err != nil {
			t.Fatalf("查询缓存失败: %v", err)
		}
		if exists {
			t.Error("ClearRoleCache 应删除角色缓存键")
		}
	})
}

// TestRoleCodesForCaching 角色解析的两条路径：未命中则查解析器并回填、命中则不查。
//
// 与上面那条用例分开写，是为了让「缓存命中」这条路径也能**确定性**覆盖 ——
// 它此前是靠 Redis 残留键偶然命中的，无法复现也就无法回归。
func TestRoleCodesForCaching(t *testing.T) {
	const (
		tenant uint = 1
		user   uint = 7
	)
	key := fmt.Sprintf("rbac:roles:%d:%d", tenant, user)

	t.Run("未命中查解析器并回填，随后命中", func(t *testing.T) {
		withTestRedis(t)
		// 清掉可能残留的键，保证起点是「未命中」
		ClearRoleCache(tenant, user)
		t.Cleanup(func() { ClearRoleCache(tenant, user) })

		resolver := &countingRoleResolver{codes: []string{"editor"}}
		withRoleResolver(t, resolver)

		got, err := RoleCodesFor(tenant, user)
		if err != nil || len(got) != 1 || got[0] != "editor" {
			t.Fatalf("首次解析失败: got=%v err=%v", got, err)
		}
		if resolver.calls != 1 {
			t.Fatalf("首次应查一次解析器，实际 %d 次", resolver.calls)
		}

		// 回填后再调一次：应直接命中缓存
		got, err = RoleCodesFor(tenant, user)
		if err != nil || len(got) != 1 || got[0] != "editor" {
			t.Fatalf("二次解析失败: got=%v err=%v", got, err)
		}
		if resolver.calls != 1 {
			t.Errorf("回填后应命中缓存，解析器不应再被调用（实际 %d 次）", resolver.calls)
		}
	})

	t.Run("命中缓存时不查解析器", func(t *testing.T) {
		withTestRedis(t)

		if err := cache.Set(context.Background(), key, "cached-role", time.Minute); err != nil {
			t.Fatalf("准备缓存失败: %v", err)
		}
		t.Cleanup(func() { ClearRoleCache(tenant, user) })

		// 解析器故意返回错误：一旦被调用，用例即转红
		withRoleResolver(t, stubRoleResolver{err: fmt.Errorf("命中缓存时不该查解析器")})

		got, err := RoleCodesFor(tenant, user)
		if err != nil {
			t.Fatalf("命中缓存时不应报错: %v", err)
		}
		if len(got) != 1 || got[0] != "cached-role" {
			t.Errorf("应返回缓存里的角色，实际 %v", got)
		}
	})
}

// countingRoleResolver 记录被调用次数，用于区分「走了缓存」与「走了解析器」。
//
// stubRoleResolver 是值类型、没有计数器，无法回答「解析器到底有没有被调用」——
// 而这恰恰是缓存类逻辑唯一值得断言的东西。
type countingRoleResolver struct {
	codes []string
	err   error
	calls int
}

func (r *countingRoleResolver) RoleCodesOf(uint, uint) ([]string, error) {
	r.calls++
	return r.codes, r.err
}
