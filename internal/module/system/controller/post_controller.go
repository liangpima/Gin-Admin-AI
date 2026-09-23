package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/system/service"

	"github.com/gin-gonic/gin"
)

type PostController struct {
	postService service.PostService
}

func NewPostController() *PostController {
	return &PostController{postService: service.NewPostService()}
}

// tenantID 从 JWT claims 取（Auth 中间件注入），是全链路租户隔离的起点：
// sys_post 为多租户表，漏传会让 TenantScope 退化为全表查询。
func (ctl *PostController) Create(c *gin.Context) {
	var req struct {
		Code   string `json:"code" binding:"required"`
		Name   string `json:"name" binding:"required"`
		Sort   int    `json:"sort"`
		Status int8   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	tenantID := common.GetTenantID(c)
	if err := ctl.postService.Create(tenantID, req.Name, req.Code, req.Sort, req.Status, common.GetCurrentUserID(c)); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

func (ctl *PostController) Update(c *gin.Context) {
	var req struct {
		ID     uint   `json:"id" binding:"required"`
		Code   string `json:"code" binding:"required"`
		Name   string `json:"name" binding:"required"`
		Sort   int    `json:"sort"`
		Status int8   `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	tenantID := common.GetTenantID(c)
	if err := ctl.postService.Update(tenantID, req.ID, req.Name, req.Code, req.Sort, req.Status, common.GetCurrentUserID(c)); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

func (ctl *PostController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	if err := ctl.postService.Delete(common.GetTenantID(c), id); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

func (ctl *PostController) FindList(c *gin.Context) {
	var req struct {
		Name     string `form:"name"`
		Page     int    `form:"page"`
		PageSize int    `form:"pageSize"`
	}
	c.ShouldBindQuery(&req)
	req.Page, req.PageSize = common.NormalizePageParams(req.Page, req.PageSize)
	list, total, err := ctl.postService.FindList(common.GetTenantID(c), req.Name, nil, req.Page, req.PageSize)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}
