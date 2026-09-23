package common

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestNormalizePageParamsClampsBoth 分页参数必须**两个都**归一化。
//
// 这是统一分页收口时修掉的一个真实缺陷：此前有一套写法只归一 pageSize、
// 不归一 page，于是 ?page=0 或 page=-5 会算出负 offset，
// 轻则 SQL 报错、重则返回异常结果。
func TestNormalizePageParamsClampsBoth(t *testing.T) {
	cases := []struct {
		name                   string
		page, pageSize         int
		wantPage, wantPageSize int
	}{
		{"正常值原样返回", 3, 20, 3, 20},
		{"page 为 0 归一到 1", 0, 20, 1, 20},
		{"page 为负归一到 1", -5, 20, 1, 20},
		{"pageSize 为 0 归一到默认", 3, 0, 3, DefaultPageSize},
		{"pageSize 为负归一到默认", 3, -1, 3, DefaultPageSize},
		{"pageSize 超上限归一到默认", 3, MaxPageSize + 1, 3, DefaultPageSize},
		{"pageSize 正好等于上限应保留", 3, MaxPageSize, 3, MaxPageSize},
		{"两个都非法", 0, 0, 1, DefaultPageSize},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotPage, gotSize := NormalizePageParams(c.page, c.pageSize)
			if gotPage != c.wantPage || gotSize != c.wantPageSize {
				t.Errorf("NormalizePageParams(%d, %d) = (%d, %d)，期望 (%d, %d)",
					c.page, c.pageSize, gotPage, gotSize, c.wantPage, c.wantPageSize)
			}
		})
	}
}

// TestNormalizePageParamsPreventsHugePageSize pageSize 上限是 DoS 防线。
//
// 不设上限时 `?pageSize=100000000` 会让服务端把整表查进内存再序列化，
// 单个匿名请求即可打满内存。
func TestNormalizePageParamsPreventsHugePageSize(t *testing.T) {
	if _, size := NormalizePageParams(1, 100000000); size > MaxPageSize {
		t.Fatalf("超大 pageSize 未被限制，实际 %d（上限 %d）", size, MaxPageSize)
	}
}

// TestGetPageInfoReadsQuery 查询参数风格的入口，与 DTO 风格共用同一套归一化。
func TestGetPageInfoReadsQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		query                  string
		wantPage, wantPageSize int
	}{
		{"", 1, DefaultPageSize},
		{"?page=2&pageSize=50", 2, 50},
		{"?page=0&pageSize=0", 1, DefaultPageSize},
		{"?page=abc&pageSize=xyz", 1, DefaultPageSize},
		{"?pageSize=99999", 1, DefaultPageSize},
	}

	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest("GET", "/x"+c.query, nil)

			page, size := GetPageInfo(ctx)
			if page != c.wantPage || size != c.wantPageSize {
				t.Errorf("GetPageInfo(%q) = (%d, %d)，期望 (%d, %d)",
					c.query, page, size, c.wantPage, c.wantPageSize)
			}
		})
	}
}
