package common

import (
	"fmt"
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
//
// 参数非法时返回 error 而不是静默回落默认分页 —— 与 BindPage（DTO 风格入口）
// 的行为对齐。此前这里用 `_` 丢掉了 Atoi 的错误，于是 `?page=abc` 被当成
// 「没传」并按第一页返回：用户以为自己翻到了某一页、实际看到的是第一页，
// 既不报错也无从察觉；而同一个参数走 DTO 风格的接口却是 400。
//
// 「缺省」与「非法」必须分开：参数缺失或为空串是正常情况（前端可能传空值），
// 按默认值处理；只有**明确写了非数字**才算非法。
func GetPageInfo(c *gin.Context) (int, int, error) {
	page, err := atoiOrDefault(c.Query("page"), 1)
	if err != nil {
		return 0, 0, fmt.Errorf("page 必须是数字")
	}
	pageSize, err := atoiOrDefault(c.Query("pageSize"), DefaultPageSize)
	if err != nil {
		return 0, 0, fmt.Errorf("pageSize 必须是数字")
	}
	page, pageSize = NormalizePageParams(page, pageSize)
	return page, pageSize, nil
}

// atoiOrDefault 解析查询参数；空白串视为「未提供」并用 fallback 兜底。
func atoiOrDefault(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
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

// MaxPage 页码上限。
//
// 它存在的理由不是「业务上不可能有这么多页」，而是**整数溢出**。
// offset 由 `(page-1)*pageSize` 算出，而 page 直接来自用户输入，
// `strconv.Atoi` 在 64 位平台可以一路返回到 9.2e18。一旦乘出 int64 的表示范围，
// offset 变成**负数**，而 GORM 对负 offset 的处理是**直接省略 OFFSET 子句** ——
// 于是 `?page=99999999999999999` 返回的是**第一页数据**：
// 既不报错，调用方也无从察觉自己拿到的不是想要的那一页。
// 「静默返回错误的数据」比「报错」危险得多，所以这里夹住而不是放行。
//
// 取 100 万：× MaxPageSize(100) = 1e8，离 int64 上限还有 11 个数量级，
// 任何真实数据集都不可能触及，因此不会误伤合法请求。
// 超出时夹到上限，结果是一页空数据 —— 语义上比「第一页」正确。
const MaxPage = 1_000_000

// NormalizePageParams 归一化分页参数，返回合法的 (page, pageSize)。
//
// 这是**唯一**的分页参数收口点，DTO 风格（req.Page/req.PageSize）与
// query 风格（GetPageInfo）都应经过它。此前存在三套写法，其中
// 「只归一 pageSize、不归一 page」那套会让 page=0 或负数算出负 offset，
// 轻则 SQL 报错、重则返回异常结果 —— 统一收口顺带修掉了这个问题。
//
// page 的上限（MaxPage）同理：见该常量的注释。
func NormalizePageParams(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	} else if page > MaxPage {
		page = MaxPage
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		pageSize = DefaultPageSize
	}
	return page, pageSize
}
