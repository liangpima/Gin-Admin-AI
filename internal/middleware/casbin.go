package middleware

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/logger"

	"github.com/casbin/casbin/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// enforcerPtr 当前生效的 enforcer。
//
// 策略变更时构建**全新的 enforcer** 再原子替换，而不是原地修改现有实例：
// casbin 的 Enforcer 不支持「一边 AddPolicies 写模型、一边 Enforce 读模型」，
// 并发下会触发 fatal error: concurrent map read and map write（Recovery 抓不住）。
var enforcerPtr atomic.Pointer[casbin.Enforcer]

// casbinModelPath 模型文件路径，重建 enforcer 时使用
var casbinModelPath string

// syncMu 串行化策略同步，避免并发同步互相覆盖
var syncMu sync.Mutex

const (
	// rbacDomain 与 config/casbin/model.conf 中的 dom 对应
	rbacDomain = "default"
	// adminRoleCode 超级管理员角色 code，始终持有通配策略
	adminRoleCode = "admin"
	// rbacRoleCacheTTL 用户角色缓存时长，角色变更后最多这么久生效
	rbacRoleCacheTTL = 60 * time.Second
	// maxRuleValueLen casbin_rule 各策略列的长度上限
	maxRuleValueLen = 200
)

func InitCasbin(modelPath string) error {
	casbinModelPath = modelPath

	// 确保策略表存在（幂等），避免部署时因漏执行建表语句导致鉴权失效
	if err := database.DB.AutoMigrate(&CasbinRule{}); err != nil {
		return fmt.Errorf("创建 casbin_rule 表失败: %w", err)
	}

	enforcer, err := casbin.NewEnforcer(modelPath, newGormAdapter(database.DB))
	if err != nil {
		return err
	}
	enforcerPtr.Store(enforcer)

	return SyncPoliciesFromRoleMenus()
}

// currentEnforcer 取当前生效的 enforcer
func currentEnforcer() *casbin.Enforcer {
	return enforcerPtr.Load()
}

// SyncPoliciesFromRoleMenus 依据「角色-菜单」授权关系重建权限策略。
//
// 菜单上配置的 permission 即权限码，路由通过 protected() 登记所需权限码，
// 因此给角色分配菜单就等同于分配权限，无需手工维护 casbin_rule。
// 在启动时以及角色/菜单发生变更后调用。
func SyncPoliciesFromRoleMenus() error {
	syncMu.Lock()
	defer syncMu.Unlock()

	// 1. 读取「角色 code -> 权限码」映射（仅启用中的角色、未删除的菜单）
	type permRow struct {
		RoleCode   string
		Permission string
	}
	var rows []permRow
	if err := database.DB.Table("sys_role_menu AS rm").
		Select("r.code AS role_code, m.permission AS permission").
		Joins("JOIN sys_role AS r ON r.id = rm.role_id AND r.deleted_at IS NULL").
		Joins("JOIN sys_menu AS m ON m.id = rm.menu_id AND m.deleted_at IS NULL").
		Where("m.permission <> ''").
		Where("r.status = ?", 1).
		Scan(&rows).Error; err != nil {
		return fmt.Errorf("读取角色菜单权限失败: %w", err)
	}

	// 2. 先在内存里把规则算全并校验长度。
	//
	// 必须**校验通过后才动数据库**：否则一旦中途失败，会出现
	// 「旧策略已清空、新策略没写进去」→ 除 admin 外全站 403 的严重后果。
	rules := [][]string{{adminRoleCode, rbacDomain, "*", "*"}}
	seen := map[string]bool{adminRoleCode + "\x00*": true}

	for _, r := range rows {
		if r.RoleCode == "" || r.Permission == "" {
			continue
		}
		for _, v := range []string{r.RoleCode, rbacDomain, r.Permission, "*"} {
			if len([]rune(v)) > maxRuleValueLen {
				return fmt.Errorf("权限策略超长（列上限 %d 字符）：角色 %q 权限 %q",
					maxRuleValueLen, r.RoleCode, r.Permission)
			}
		}

		// 用 \x00 作分隔符：角色 code 或权限码本身可能含 | 等可见字符
		key := r.RoleCode + "\x00" + r.Permission
		if seen[key] {
			continue
		}
		seen[key] = true
		rules = append(rules, []string{r.RoleCode, rbacDomain, r.Permission, "*"})
	}

	// 3. 事务内全量重写策略表
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&CasbinRule{}).Error; err != nil {
			return err
		}
		recs := make([]CasbinRule, 0, len(rules))
		for _, rule := range rules {
			recs = append(recs, buildRule("p", rule))
		}
		return tx.Create(&recs).Error
	}); err != nil {
		return fmt.Errorf("写入权限策略失败: %w", err)
	}

	// 4. 用新策略构建全新 enforcer 并原子替换。
	//    失败时保留旧 enforcer，权限维持原状（不会出现「清空了但没写回」）。
	newEnforcer, err := casbin.NewEnforcer(casbinModelPath, newGormAdapter(database.DB))
	if err != nil {
		return fmt.Errorf("重建 enforcer 失败: %w", err)
	}
	enforcerPtr.Store(newEnforcer)

	logger.Log.Infof("[casbin] 已根据角色-菜单关系同步 %d 条权限策略", len(rules))
	return nil
}

func CasbinAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		enforcer := currentEnforcer()
		if enforcer == nil {
			// 鉴权是安全控制：未就绪时拒绝，而不是放行
			logger.Log.Errorf("[casbin] enforcer 未初始化，拒绝请求: %s %s", c.Request.Method, c.FullPath())
			common.Error(c, common.CodeInternalError, "鉴权服务未就绪")
			c.Abort()
			return
		}

		// 依据路由权限登记表确定所需权限码
		code, registered := routePermission(c.Request.Method, c.FullPath())
		if !registered {
			// 未登记的路由默认拒绝，避免新增接口漏配权限后被放行
			logger.Log.Warnf("[casbin] 路由未登记权限码，已拒绝: %s %s", c.Request.Method, c.FullPath())
			common.Forbidden(c, "该接口未配置访问权限")
			c.Abort()
			return
		}
		// 空权限码表示仅要求登录态（自助接口）
		if code == "" {
			c.Next()
			return
		}

		roles := resolveRoleCodes(c)
		if len(roles) == 0 {
			common.Forbidden(c, "当前账号未分配角色，无法访问")
			c.Abort()
			return
		}

		// model.conf 的 request_definition 为四元组 (sub, dom, obj, act)。
		// 用户可能拥有多个角色，任一角色命中该权限码即放行。
		for _, role := range roles {
			ok, err := enforcer.Enforce(role, rbacDomain, code, c.Request.Method)
			if err != nil {
				logger.Log.Errorf("[casbin] 权限校验出错: %v", err)
				common.Error(c, common.CodeInternalError, "权限校验失败")
				c.Abort()
				return
			}
			if ok {
				c.Next()
				return
			}
		}

		common.Forbidden(c, fmt.Sprintf("没有权限访问（需要 %s）", code))
		c.Abort()
	}
}

// RoleResolver 提供「用户在当前租户下拥有的角色 code」这一能力。
//
// 为什么在这里声明窄接口，而不是直接 new 业务 Repository：
// 鉴权中间件属于横切关注点，直接构造 system 模块的 UserRepository /
// RoleRepository 会让依赖方向倒置（基础设施层反向依赖业务层）。
// 后果有两个：中间件的单测必须准备数据库；其它模块无法替换这层实现
// （例如把角色来源改为外部权限中心时，得改中间件本身）。
//
// 改为「声明所需的最小能力 + 由调用方注入」后，middleware 只依赖一个方法签名，
// 不再依赖 system 的模型与数据访问。
//
// 实现见 internal/module/system/service/rbac_resolver.go，
// 由 cmd/server/main.go 在启动时注入。
type RoleResolver interface {
	RoleCodesOf(tenantID, userID uint) ([]string, error)
}

// roleResolver 已注入的实现。
// 只在启动阶段写入一次，运行期只读，因此无需加锁。
var roleResolver RoleResolver

// SetRoleResolver 注入角色解析实现，应在开始处理请求之前调用。
func SetRoleResolver(r RoleResolver) {
	roleResolver = r
}

// resolveRoleCodes 解析当前用户的角色 code 列表，作为 RBAC 的匹配主体。
// 结果短时缓存于 Redis，避免每个请求都查库。
func resolveRoleCodes(c *gin.Context) []string {
	userID := common.GetCurrentUserID(c)
	if userID == 0 {
		return nil
	}
	tenantID := common.GetTenantID(c)

	ctx := context.Background()
	cacheKey := fmt.Sprintf("rbac:roles:%d:%d", tenantID, userID)

	if v, err := cache.Get(ctx, cacheKey); err == nil {
		if codes := splitRoleCodes(v); len(codes) > 0 {
			return codes
		}
	}

	// 未注入实现属启动配置错误。返回 nil 会让上层按「无角色」拒绝（403），
	// 比放行更安全，同时留下能指向根因的日志。
	if roleResolver == nil {
		logger.Log.Errorf("[casbin] RoleResolver 未注入，无法解析用户 %d 的角色", userID)
		return nil
	}

	codes, err := roleResolver.RoleCodesOf(tenantID, userID)
	if err != nil {
		// 查库失败绝不能当作「有权限」：返回 nil，由上层按无角色拒绝
		logger.Log.Errorf("[casbin] 解析用户 %d 的角色失败: %v", userID, err)
		return nil
	}

	if len(codes) > 0 {
		// 缓存写失败只影响性能（下次调用重新查库），不影响正确性，
		// 因此不打断请求；但也不静默丢弃 —— 留 Warn 以便发现 Redis 异常。
		if err := cache.Set(ctx, cacheKey, strings.Join(codes, ","), rbacRoleCacheTTL); err != nil {
			logger.Log.Warnf("[casbin] 角色缓存写入失败（仅影响性能）: userID=%d err=%v", userID, err)
		}
	}
	return codes
}

func splitRoleCodes(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	codes := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			codes = append(codes, p)
		}
	}
	return codes
}

// ClearRoleCache 清除指定用户的角色缓存。
// 角色授权/编码变更后调用，避免缓存导致权限延迟生效。
func ClearRoleCache(tenantID, userID uint) {
	// 这个错误不能静默：清理失败意味着被撤销的角色仍会命中缓存，
	// 直到 TTL 到期前权限变更都不生效 —— 属于「撤销不生效」的安全问题。
	if err := cache.Del(context.Background(), fmt.Sprintf("rbac:roles:%d:%d", tenantID, userID)); err != nil {
		logger.Log.Errorf("[casbin] 角色缓存清理失败，权限变更可能延迟生效: tenant=%d user=%d err=%v",
			tenantID, userID, err)
	}
}
