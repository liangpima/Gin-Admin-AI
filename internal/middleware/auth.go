package middleware

import (
	"context"
	"fmt"
	"strings"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/pkg/auth"

	"github.com/gin-gonic/gin"
)

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			common.Unauthorized(c, "请先登录")
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			common.Unauthorized(c, "Token格式错误")
			c.Abort()
			return
		}

		tokenString := parts[1]

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
