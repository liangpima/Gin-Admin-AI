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

// TestNormalizePageParamsPreventsOffsetOverflow page 上限防的是**整数溢出**。
//
// offset 由 `(page-1)*pageSize` 算出，page 直接来自用户输入
// （Atoi 在 64 位平台能返回到 9.2e18）。乘出 int64 范围后 offset 变负，
// 而 GORM 对负 offset 的处理是**直接省略 OFFSET 子句** ——
// 于是 `?page=99999999999999999` 返回的是第一页数据：不报错，
// 调用方也无从察觉自己拿到的不是想要的那一页。
//
// 断言「算出来的 offset 不会溢出」而不是断言某个具体页码：
// 前者才是这个上限存在的理由。
func TestNormalizePageParamsPreventsOffsetOverflow(t *testing.T) {
	huge := []int{1 << 40, 1 << 55, int(^uint(0) >> 1)}

	for _, page := range huge {
		gotPage, gotSize := NormalizePageParams(page, MaxPageSize)
		offset := (gotPage - 1) * gotSize
		if offset < 0 {
			t.Errorf("page=%d 归一后为 %d，offset 溢出为负数 %d", page, gotPage, offset)
		}
		if gotPage > MaxPage {
			t.Errorf("page=%d 应被夹到上限 %d，实际 %d", page, MaxPage, gotPage)
		}
	}

	if got, _ := NormalizePageParams(MaxPage+1, DefaultPageSize); got != MaxPage {
		t.Errorf("超过上限应夹到 %d，实际 %d", MaxPage, got)
	}
	if got, _ := NormalizePageParams(MaxPage, DefaultPageSize); got != MaxPage {
		t.Errorf("正好等于上限应保留，实际 %d", got)
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
		{"?pageSize=99999", 1, DefaultPageSize},
		// 空值视为「未提供」：前端可能传空串，按默认分页处理而不是报错
		{"?page=&pageSize=", 1, DefaultPageSize},
		// 超过页码上限：夹到上限（返回空页），而不是溢出成负数
		{"?page=99999999999999999", MaxPage, DefaultPageSize},
	}

	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest("GET", "/x"+c.query, nil)

			page, size, err := GetPageInfo(ctx)
			if err != nil {
				t.Fatalf("GetPageInfo(%q) 不应报错: %v", c.query, err)
			}
			if page != c.wantPage || size != c.wantPageSize {
				t.Errorf("GetPageInfo(%q) = (%d, %d)，期望 (%d, %d)",
					c.query, page, size, c.wantPage, c.wantPageSize)
			}
		})
	}
}

// TestGetPageInfoRejectsNonNumeric 非数字参数必须报错，而不是静默按第一页返回。
//
// 此前这里用 `_` 丢掉了 Atoi 的错误，于是 `?page=abc` 被当成「没传」并按第一页
// 返回：用户以为自己翻到了某一页、实际看到的是第一页，既不报错也无从察觉；
// 而同一个参数走 DTO 风格的接口（BindPage → ShouldBindQuery）却是 400。
// 两个入口对同一个非法输入给出不同行为，本身就是缺陷。
func TestGetPageInfoRejectsNonNumeric(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, query := range []string{"?page=abc", "?pageSize=xyz", "?page=1.5", "?page=1&pageSize=xyz"} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest("GET", "/x"+query, nil)

			if _, _, err := GetPageInfo(ctx); err == nil {
				t.Errorf("GetPageInfo(%q) 应报错（非数字分页参数）", query)
			}
		})
	}
}
