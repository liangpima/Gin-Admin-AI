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
// @Summary 创建岗位
// @Tags 岗位
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param code body string true "岗位编码"
// @Param name body string true "岗位名称"
// @Param sort body int false "排序"
// @Param status body int false "状态：0 停用 / 1 正常"
// @Success 200 {object} common.Response
// @Router /system/post [post]
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

// @Summary 更新岗位
// @Tags 岗位
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id body int true "岗位 ID"
// @Param code body string true "岗位编码"
// @Param name body string true "岗位名称"
// @Param sort body int false "排序"
// @Param status body int false "状态：0 停用 / 1 正常"
// @Success 200 {object} common.Response
// @Router /system/post [put]
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

// @Summary 删除岗位
// @Tags 岗位
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/post/{id} [delete]
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

// @Summary 岗位列表
// @Tags 岗位
// @Produce json
// @Security BearerAuth
// @Param name query string false "岗位名称（模糊）"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /system/post/list [get]
func (ctl *PostController) FindList(c *gin.Context) {
	var req struct {
		Name     string `form:"name"`
		// 分页参数统一内嵌：绑定与归一化走 common.BindPage
		common.PageQuery
	}
	if err := common.BindPage(c, &req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	list, total, err := ctl.postService.FindList(common.GetTenantID(c), req.Name, nil, req.Page, req.PageSize)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}
