package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/system/service"

	"github.com/gin-gonic/gin"
)

type LogController struct {
	logService service.LogService
}

func NewLogController() *LogController {
	return &LogController{logService: service.NewLogService()}
}

// @Summary 操作日志列表
// @Tags 日志
// @Produce json
// @Security BearerAuth
// @Param title query string false "模块标题"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /system/log/operation [get]
func (ctl *LogController) FindOperationLogList(c *gin.Context) {
	var req struct {
		Title    string `form:"title"`
		Page     int    `form:"page"`
		PageSize int    `form:"pageSize"`
	}
	c.ShouldBindQuery(&req)
	req.Page, req.PageSize = common.NormalizePageParams(req.Page, req.PageSize)
	tenantID := common.GetTenantID(c)
	list, total, err := ctl.logService.FindOperationLogList(tenantID, req.Title, nil, req.Page, req.PageSize)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}

// @Summary 登录日志列表
// @Tags 日志
// @Produce json
// @Security BearerAuth
// @Param username query string false "用户名"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /system/log/login [get]
func (ctl *LogController) FindLoginLogList(c *gin.Context) {
	var req struct {
		Username string `form:"username"`
		Page     int    `form:"page"`
		PageSize int    `form:"pageSize"`
	}
	c.ShouldBindQuery(&req)
	req.Page, req.PageSize = common.NormalizePageParams(req.Page, req.PageSize)
	tenantID := common.GetTenantID(c)
	list, total, err := ctl.logService.FindLoginLogList(tenantID, req.Username, nil, req.Page, req.PageSize)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}

// @Summary 清空操作日志
// @Tags 日志
// @Produce json
// @Security BearerAuth
// @Success 200 {object} common.Response
// @Router /system/log/operation [delete]
func (ctl *LogController) ClearOperationLogs(c *gin.Context) {
	tenantID := common.GetTenantID(c)
	if err := ctl.logService.ClearOperationLogs(tenantID); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 清空登录日志
// @Tags 日志
// @Produce json
// @Security BearerAuth
// @Success 200 {object} common.Response
// @Router /system/log/login [delete]
func (ctl *LogController) ClearLoginLogs(c *gin.Context) {
	tenantID := common.GetTenantID(c)
	if err := ctl.logService.ClearLoginLogs(tenantID); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}
