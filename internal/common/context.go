package common

import (
	"strconv"
	"strings"

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

func GetTenantID(c *gin.Context) uint {
	if id, exists := c.Get(ContextKeyTenantID); exists {
		if v, ok := id.(uint); ok {
			return v
		}
	}
	return 0
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
