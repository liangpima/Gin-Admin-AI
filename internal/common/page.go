package common

import "github.com/gin-gonic/gin"

// PageQuery 分页查询的公共入参。
//
// 各模块的列表请求内嵌它即可获得统一的分页语义（见 BindPage），
// 不必各自声明 page/pageSize 字段 —— 那样最容易出两类问题：
//   - 忘了归一化，把 pageSize=100000 直接透给数据库
//   - 忘了检查绑定错误，`page=abc` 被当成「没传」而静默使用默认值
//
// 字段标签同时带 form 与 json：form 供查询绑定，json 供回显请求结构。
type PageQuery struct {
	Page     int `form:"page" json:"page"`
	PageSize int `form:"pageSize" json:"pageSize"`
}

func (p *PageQuery) GetPage() int      { return p.Page }
func (p *PageQuery) SetPage(v int)     { p.Page = v }
func (p *PageQuery) GetPageSize() int  { return p.PageSize }
func (p *PageQuery) SetPageSize(v int) { p.PageSize = v }

// Paged 由内嵌 PageQuery 的请求结构体满足。
//
// 用接口而不是反射：反射能少写几行，但「哪个结构体支持分页」就变成运行期才知道的事，
// 而接口让编译器替我们检查。
type Paged interface {
	GetPage() int
	SetPage(int)
	GetPageSize() int
	SetPageSize(int)
}

// BindPage 绑定查询参数并归一化分页，是列表接口取参的唯一入口。
//
// 它把原先散落在 14 个 Controller 里的三步合成一步：
//
//	绑定 → 检查错误 → 归一化
//
// 之所以要收口：这三步此前有 7 处只做了第一步（`c.ShouldBindQuery(&req)` 直接
// 丢掉返回值），于是 `?page=abc` 不会报错，而是被当成「没传」并按默认分页返回 ——
// 用户以为筛选生效了，实际看到的是第一页全量数据。归一化也漏过：
// 有一处把 page/pageSize 硬编码成 1/10，前端传 pageSize=100 被静默忽略，
// 字典数据超过 10 条就再也看不到。
//
// 绑定失败原样返回 error，由调用方决定如何响应（各 Controller 统一转 400）。
func BindPage(c *gin.Context, req Paged) error {
	if err := c.ShouldBindQuery(req); err != nil {
		return err
	}

	page, pageSize := NormalizePageParams(req.GetPage(), req.GetPageSize())
	req.SetPage(page)
	req.SetPageSize(pageSize)
	return nil
}
