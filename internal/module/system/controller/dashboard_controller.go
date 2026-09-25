package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/system/service"

	"github.com/gin-gonic/gin"
)

type DashboardController struct {
	dashboardService service.DashboardService
}

func NewDashboardController() *DashboardController {
	return &DashboardController{
		dashboardService: service.NewDashboardService(),
	}
}

// @Summary 获取统计数据
// @Tags 仪表盘
// @Produce json
// @Success 200 {object} common.Response{data=model.DashboardStats}
// @Router /dashboard/stats [get]
func (ctl *DashboardController) GetStats(c *gin.Context) {
	tenantID := common.GetTenantID(c)
	stats, err := ctl.dashboardService.GetStats(tenantID)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, stats)
}
