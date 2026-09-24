package common

import (
	"strconv"
	"strings"
	"sync"

	"go-admin/internal/logger"

	"github.com/gin-gonic/gin"
)

// NormalizeIP 将 IPv6 回环地址转为 IPv4 格式，提升可读性
func NormalizeIP(ip string) string {
	if ip == "::1" || ip == "::ffff:127.0.0.1" {
		return "127.0.0.1"
	}
	// 处理 ::ffff:x.x.x.x 格式
	if strings.HasPrefix(ip, "::ffff:") {
		return ip[7:]
	}
	return ip
}

// TenantIDFrom 读取上下文中的租户 ID，ok=false 表示**上下文里根本没有租户信息**。
//
// 为什么要区分「没有」与「是 0」：两者在下游的表现完全不同。
//   - 是 0：平台级账号（tenant_id=0），`TenantScope(db, 0)` 不过滤是**预期行为**
//   - 没有：该路由没有经过 Auth 中间件（或中间件顺序错了）。
//     此时任何一次 `GetTenantID` 都会返回 0，于是租户过滤被静默跳过 ——
//     一个本该只看到本租户数据的接口会返回全表。
//
// 需要区分两者的调用方用这个函数；只关心「用哪个租户查数据」的用 GetTenantID。
func TenantIDFrom(c *gin.Context) (uint, bool) {
	if id, exists := c.Get(ContextKeyTenantID); exists {
		if v, ok := id.(uint); ok {
			return v, true
		}
	}
	return 0, false
}

// missingTenantWarned 记录已经告警过的路由，避免同一个漏挂 Auth 的路由刷满日志
// （它会命中每个请求，而日志轮转是按大小切分的，刷屏会挤掉真正有用的日志）。
var missingTenantWarned sync.Map

// GetTenantID 读取上下文中的租户 ID，取不到返回 0。
//
// ⚠️ 返回 0 有**两种**含义：平台级账号，或上下文缺失。后者会让
// `TenantScope(db, 0)` 静默退化为全表查询 —— 这是本项目多租户隔离最典型的失效成因。
// 因此这里对「上下文缺失」留一条 Error 日志（每个路由只报一次），
// 让它表现为一条能直接指向路由的告警，而不是「某个接口莫名返回了别人的数据」。
//
// 需要明确区分两者时用 TenantIDFrom。
func GetTenantID(c *gin.Context) uint {
	id, ok := TenantIDFrom(c)
	if !ok {
		// 取路由标识用于告警。必须容忍 Request 为空：
		// 这个函数被上百处调用，其中不乏手工构造上下文的场景（测试、内部任务），
		// 为了一条告警把调用方打挂不值得。
		method, path := "UNKNOWN", c.FullPath()
		if c.Request != nil {
			method = c.Request.Method
			if path == "" {
				path = c.Request.URL.Path
			}
		}
		key := method + " " + path
		if _, loaded := missingTenantWarned.LoadOrStore(key, struct{}{}); !loaded {
			logger.Log.Errorf(
				"[tenant] 请求上下文缺少租户信息，租户过滤会被静默跳过（该路由是否漏挂 Auth 中间件？）: %s",
				key)
		}
	}
	return id
}

func GetCurrentUserID(c *gin.Context) uint {
	if id, exists := c.Get(ContextKeyUserID); exists {
		if v, ok := id.(uint); ok {
			return v
		}
	}
	return 0
}

func GetCurrentUsername(c *gin.Context) string {
	if name, exists := c.Get(ContextKeyUsername); exists {
		if v, ok := name.(string); ok {
			return v
		}
	}
	return ""
}

func GetDeptID(c *gin.Context) uint {
	if id, exists := c.Get(ContextKeyDeptID); exists {
		if v, ok := id.(uint); ok {
			return v
		}
	}
	return 0
}

func GetUintParam(c *gin.Context, key string) (uint, error) {
	val := c.Param(key)
	id, err := strconv.ParseUint(val, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint(id), nil
}

// GetPageInfo 从查询参数读取分页参数并归一化（query 风格入口）。
func GetPageInfo(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))
	return NormalizePageParams(page, pageSize)
}

// 分页的默认值与上限。
//
// 上限是硬约束：pageSize 不设上限时，`?pageSize=100000000` 会让服务端
// 把整表查进内存再序列化，单个匿名请求即可打满内存，属于低成本 DoS。
// 各 Controller 自行散落地写「<1 才归位」的写法漏掉了「>100 要归位」，
// 因此这里统一收口。
const (
	DefaultPageSize = 10
	MaxPageSize     = 100
)

// NormalizePageParams 归一化分页参数，返回合法的 (page, pageSize)。
//
// 这是**唯一**的分页参数收口点，DTO 风格（req.Page/req.PageSize）与
// query 风格（GetPageInfo）都应经过它。此前存在三套写法，其中
// 「只归一 pageSize、不归一 page」那套会让 page=0 或负数算出负 offset，
// 轻则 SQL 报错、重则返回异常结果 —— 统一收口顺带修掉了这个问题。
func NormalizePageParams(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		pageSize = DefaultPageSize
	}
	return page, pageSize
}
