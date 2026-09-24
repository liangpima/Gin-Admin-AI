package common

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// BindPage 的测试（P2-3）。
//
// 收口前的两类真实问题都要锁住：
//  1. 绑定失败被丢掉 —— `?page=abc` 不报错，而是被当成「没传」按默认分页返回，
//     用户以为筛选生效了，实际看到的是第一页全量数据（14 处里有 7 处如此）
//  2. 归一化漏做 —— 有一处把 page/pageSize 硬编码成 1/10，前端传 pageSize=100
//     被静默忽略，字典数据超过 10 条就再也看不到

// bindPageReq 模拟业务请求结构体：内嵌 PageQuery + 自己的筛选字段
type bindPageReq struct {
	Name string `form:"name"`
	PageQuery
}

func bindPageCtx(t *testing.T, query string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/list?"+query, nil)
	return c
}

// TestBindPageBindsEmbeddedFields 内嵌的 PageQuery 必须能被绑定到。
//
// 这是整个收口方案的前提：gin 的查询绑定要能递归进匿名嵌入结构体。
// 一旦驱动/框架行为变化导致它失效，page/pageSize 会静默变回默认值 ——
// 界面上表现为「翻页没反应、每页条数改不了」，且没有任何报错。
func TestBindPageBindsEmbeddedFields(t *testing.T) {
	var req bindPageReq
	if err := BindPage(bindPageCtx(t, "name=abc&page=3&pageSize=25"), &req); err != nil {
		t.Fatalf("绑定失败: %v", err)
	}

	if req.Name != "abc" {
		t.Errorf("业务字段未绑定: %q", req.Name)
	}
	if req.Page != 3 || req.PageSize != 25 {
		t.Errorf("内嵌分页字段未绑定: page=%d pageSize=%d", req.Page, req.PageSize)
	}
}

// TestBindPageNormalizesOutOfRange 非法范围要归位到默认值。
//
// 不归位时 pageSize=100000 会直接把整表查进内存（低成本 DoS），
// 而 page=0/负数会算出负 offset（轻则 SQL 报错、重则返回异常结果）。
func TestBindPageNormalizesOutOfRange(t *testing.T) {
	cases := []struct {
		name         string
		query        string
		wantPage     int
		wantPageSize int
	}{
		{"正常范围原样保留", "page=5&pageSize=50", 5, 50},
		{"缺省归位到默认", "", 1, DefaultPageSize},
		{"page=0 归位", "page=0&pageSize=20", 1, 20},
		{"page 负数归位", "page=-3&pageSize=20", 1, 20},
		{"pageSize=0 归位", "page=2&pageSize=0", 2, DefaultPageSize},
		{"pageSize 超上限归位", "page=2&pageSize=100000", 2, DefaultPageSize},
		{"pageSize 负数归位", "page=2&pageSize=-1", 2, DefaultPageSize},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req bindPageReq
			if err := BindPage(bindPageCtx(t, tc.query), &req); err != nil {
				t.Fatalf("绑定失败: %v", err)
			}
			if req.Page != tc.wantPage || req.PageSize != tc.wantPageSize {
				t.Errorf("query=%q 应归位为 (%d,%d)，实际 (%d,%d)",
					tc.query, tc.wantPage, tc.wantPageSize, req.Page, req.PageSize)
			}
		})
	}
}

// TestBindPageReturnsErrorOnBadParam 非法参数必须返回错误，而不是静默用默认值。
//
// 这是本项修复的核心：吞掉绑定错误时 `page=abc` 会以「第一页、默认条数」
// 成功返回，用户完全看不出参数写错了。
func TestBindPageReturnsErrorOnBadParam(t *testing.T) {
	for _, query := range []string{"page=abc", "pageSize=xyz", "page=1.5"} {
		var req bindPageReq
		if err := BindPage(bindPageCtx(t, query), &req); err == nil {
			t.Errorf("query=%q 绑定失败必须返回错误，否则调用方无从得知参数非法", query)
		}
	}
}

// TestPageQueryIsPaged 内嵌 PageQuery 的请求结构体必须满足 Paged 接口。
//
// 这是编译期契约：不满足就无法传给 BindPage。
func TestPageQueryIsPaged(t *testing.T) {
	var _ Paged = &bindPageReq{}

	pq := &PageQuery{Page: 2, PageSize: 30}
	if pq.GetPage() != 2 || pq.GetPageSize() != 30 {
		t.Fatalf("读取器实现有误: %d/%d", pq.GetPage(), pq.GetPageSize())
	}
	pq.SetPage(7)
	pq.SetPageSize(70)
	if pq.Page != 7 || pq.PageSize != 70 {
		t.Errorf("写入器实现有误: %d/%d", pq.Page, pq.PageSize)
	}
}
