package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/service"

	"github.com/gin-gonic/gin"
)

type PointsLogController struct {
	pointsLogService service.PointsLogService
}

func NewPointsLogController() *PointsLogController {
	return &PointsLogController{pointsLogService: service.NewPointsLogService()}
}

// @Summary 积分明细列表
// @Tags 积分明细
// @Produce json
// @Security BearerAuth
// @Param memberId query int false "会员ID"
// @Param type query int false "类型 1获取 2消费"
// @Param page query int true "页码"
// @Param pageSize query int true "每页条数"
// @Success 200 {object} common.Response
// @Router /member/points/list [get]
func (ctl *PointsLogController) FindList(c *gin.Context) {
	var req dto.PointsLogListRequest
	if err := common.BindPage(c, &req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	tenantID := common.GetTenantID(c)
	list, total, err := ctl.pointsLogService.FindList(tenantID, &req)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}
