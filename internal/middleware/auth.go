package middleware

import (
	"context"
	"fmt"
	"strings"

	"go-admin/config"
	"go-admin/internal/authcookie"
	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/pkg/auth"

	"github.com/gin-gonic/gin"
)

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, source, msg := extractToken(c)
		if msg != "" {
			common.Unauthorized(c, msg)
			c.Abort()
			return
		}

		// 检查 Token 是否已被吊销。
		// 查询失败时**拒绝**而不是放行：Redis 抖动期间放行等于让已登出的
		// token 重新生效（fail-open），而这恰恰是吊销机制最该起作用的时刻。
		revoked, err := cache.IsTokenRevoked(context.Background(), tokenString)
		if err != nil {
			logger.Log.Errorf("[auth] 查询 token 吊销状态失败: %v", err)
			common.Error(c, common.CodeInternalError, "鉴权服务暂时不可用，请稍后重试")
			c.Abort()
			return
		}
		if revoked {
			common.Unauthorized(c, "Token已失效")
			c.Abort()
			return
		}

		claims, err := auth.ParseToken(tokenString)
		if err != nil {
			common.Unauthorized(c, "Token无效或已过期")
			c.Abort()
			return
		}

		// 检查用户级别 Token 吊销（密码修改/禁用）。同样 fail-closed：
		// 早前用 `userRevoked, _ :=` 吞掉错误，Redis 异常时会当成「未吊销」，
		// 已停用账号仍可继续访问。
		userRevoked, existsErr := cache.Exists(context.Background(),
			fmt.Sprintf("user:token_revoked:%d", claims.UserID))
		if existsErr != nil {
			logger.Log.Errorf("[auth] 查询用户级 token 吊销标记失败: %v", existsErr)
			common.Error(c, common.CodeInternalError, "鉴权服务暂时不可用，请稍后重试")
			c.Abort()
			return
		}
		if userRevoked {
			common.Unauthorized(c, "Token已失效，请重新登录")
			c.Abort()
			return
		}

		c.Set(common.ContextKeyUserID, claims.UserID)
		c.Set(common.ContextKeyUsername, claims.Username)
		c.Set(common.ContextKeyTenantID, claims.TenantID)
		c.Set(common.ContextKeyDeptID, claims.DeptID)

		// 把「token 来自哪里」与「原文」交给下游：
		//   · CSRF 中间件据此豁免走 Authorization 头的调用方
		//   · 登出接口据此拿到 token 原文去拉黑（cookie 认证时没有 Authorization 头）
		c.Set(common.ContextKeyTokenSource, source)
		c.Set(common.ContextKeyAccessToken, tokenString)

		// 平台级身份校验：租户 ID 为 0 的账号代表「不做租户过滤」，
		// 而 TenantScope(db, 0) 正是「不过滤」的哨兵值 —— 换句话说，
		// 持有一个 tenantID=0 的 token 就等于拿到了跨租户读写能力。
		//
		// 因此这里要求它必须同时持有 admin 角色（当前系统中唯一的平台级身份）：
		// 光有「token 里写着 0」不足以获得这种权限。这堵住了两类情形：
		//   · 账号被误建成 tenant_id=0（历史遗留 / 手工 SQL / 迁移脚本出错）
		//   · 将来某条签发路径把租户信息丢了，签发出一张「无租户」的 token
		// 两者的共同点是把「上下文丢失」伪装成了「平台级账号」，
		// 而下游完全无法分辨 —— 只能在入口按身份把住。
		//
		// 只对 tenantID==0 的请求解析角色（普通租户账号直接短路），
		// 避免给每个请求都多加一次角色查询；解析结果与 CasbinAuth 共用同一份缓存。
		// 角色解析失败时返回空列表，按无权限处理（fail-closed），
		// 与 CasbinAuth 的取舍一致：算不出来就拒绝，绝不放行。
		if claims.TenantID == 0 && !canAccessWithoutTenant(resolveRoleCodes(c)) {
			logger.Log.Warnf(
				"[auth] 拒绝平台级请求：账号无 admin 角色但 token 租户为 0（user=%d username=%s path=%s）",
				claims.UserID, claims.Username, c.FullPath())
			common.Unauthorized(c, "账号租户信息异常，请联系管理员")
			c.Abort()
			return
		}

		c.Next()
	}
}

// extractToken 按 security.token_transport 的配置取出本次请求要校验的 token。
//
// 优先级：Authorization 头 → cookie（access_token）。返回的 source 会写进上下文。
//
// 抽成独立函数而不是内联在 Auth 里，理由与 canAccessWithoutTenant 相同：
// Auth 后面几步依赖 Redis，测试环境没有 Redis 就走不到那里，
// 只有纯函数才能把「三种传输方式 × 有头/无头/坏头/cookie」这张表穷举掉。
//
// 两个刻意的取舍：
//   - **头存在但格式错时不回退到 cookie**。回退会让「调用方带了个坏头」
//     表现成「用 cookie 认证成功了」，问题被掩盖成偶发；
//     明确报错才能立刻定位到是哪个调用方发错了。
//   - **cookie 读不到时不区分「没有」与「空值」**，一律按未登录处理。
//     httpOnly cookie 的值由服务端写、浏览器原样回传，不存在「前端写了个空串」
//     这种情况，区分它只会多一条永远走不到的分支。
func extractToken(c *gin.Context) (token, source, errMsg string) {
	sec := config.Cfg.Security

	if sec.AcceptsHeaderToken() {
		if authHeader := c.GetHeader("Authorization"); authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			// `Bearer` 后面必须有值：`Bearer `（尾随空格）也会被 SplitN 切成两段，
			// 若不校验就会拿着空串往下走，最终报成「Token无效或已过期」——
			// 把「格式写错」说成「凭证过期」，排查时会往完全错误的方向找。
			if len(parts) != 2 || parts[0] != "Bearer" || strings.TrimSpace(parts[1]) == "" {
				return "", common.TokenSourceHeader, "Token格式错误"
			}
			return parts[1], common.TokenSourceHeader, ""
		}
	}

	if sec.AcceptsCookieToken() {
		if v, err := c.Cookie(authcookie.AccessCookieName); err == nil && v != "" {
			return v, common.TokenSourceCookie, ""
		}
	}

	return "", "", "请先登录"
}

// canAccessWithoutTenant 判断「租户 ID 为 0」的请求能否放行。
//
// 抽成独立函数有两个理由：
//   - 它是本项目的**策略点**：平台级身份 = 持有 admin 角色。策略要变（例如将来
//     出现平台级审计员角色）只需改这里，且改动会被 TestCanAccessWithoutTenant 拦下
//   - Auth 前面几步依赖 Redis（吊销检查 fail-closed），测试环境没有 Redis
//     就走不到这里，纯函数才能被直接覆盖
func canAccessWithoutTenant(roleCodes []string) bool {
	return HasAdminRole(roleCodes)
}

// RequireAdminRole 要求当前操作者持有 admin 角色，否则 403。
//
// 用途：**全局表**的写接口。这类表没有 tenant_id（sys_config / sys_dict_type /
// sys_dict_data / sys_agreement 等），任何持有对应权限码的租户管理员都能改写，
// 而改动的效果会作用到**所有租户** —— 例如把平台共用的 OSS / 支付凭据换成自己的，
// 或改掉全平台共用的字典文案。
//
// 为什么不靠 Casbin 解决：Casbin 的策略主体是权限码，只能表达「有没有某个权限码」，
// 表达不了「必须是平台级角色」。若改成用一个不下发给任何菜单的权限码来限制，
// 那层保护是隐式的 —— 谁把该码挂到菜单上，保护就悄悄失效了。
// 显式判定角色，意图和失效边界都写在代码里。
//
// 与 CasbinAuth 是 **AND** 关系（串联在路由上），两者都通过才放行：
// 权限码负责「这个接口归哪类人」，本中间件负责「这类人里只有超管能动全局数据」。
func RequireAdminRole() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !HasAdminRole(resolveRoleCodes(c)) {
			logger.Log.Warnf(
				"[auth] 拒绝平台级数据写入：操作者无 admin 角色（user=%d path=%s）",
				common.GetCurrentUserID(c), c.FullPath())
			common.Forbidden(c, "该操作仅限超级管理员")
			c.Abort()
			return
		}
		c.Next()
	}
}
