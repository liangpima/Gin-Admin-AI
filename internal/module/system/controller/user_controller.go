package controller

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/service"
	"go-admin/pkg/excel"

	"github.com/gin-gonic/gin"
)

type UserController struct {
	userService service.UserService
}

func NewUserController() *UserController {
	return &UserController{
		userService: service.NewUserService(),
	}
}

// @Summary 创建用户
// @Tags 管理员
// @Accept json
// @Produce json
// @Param body body dto.CreateUserRequest true "用户信息"
// @Success 200 {object} common.Response
// @Router /api/v1/system/user [post]
func (ctl *UserController) Create(c *gin.Context) {
	var req dto.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	operatorID := common.GetCurrentUserID(c)
	if err := ctl.userService.Create(tenantID, &req, operatorID); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 更新用户
// @Tags 管理员
// @Accept json
// @Produce json
// @Param body body dto.UpdateUserRequest true "用户信息"
// @Success 200 {object} common.Response
// @Router /api/v1/system/user [put]
func (ctl *UserController) Update(c *gin.Context) {
	var req dto.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	operatorID := common.GetCurrentUserID(c)
	if err := ctl.userService.Update(tenantID, &req, operatorID); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 删除用户
// @Tags 管理员
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} common.Response
// @Router /api/v1/system/user/{id} [delete]
func (ctl *UserController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}

	tenantID := common.GetTenantID(c)
	if err := ctl.userService.Delete(tenantID, id); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 获取用户详情
// @Tags 管理员
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} common.Response
// @Router /api/v1/system/user/{id} [get]
func (ctl *UserController) FindByID(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}

	tenantID := common.GetTenantID(c)
	user, err := ctl.userService.FindByID(tenantID, id)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, user)
}

// @Summary 用户列表
// @Tags 管理员
// @Produce json
// @Param username query string false "用户名"
// @Param phone query string false "手机号"
// @Param status query int false "状态"
// @Param deptId query int false "部门ID"
// @Param page query int true "页码"
// @Param pageSize query int true "每页条数"
// @Success 200 {object} common.Response{data=common.PageData}
// @Router /api/v1/system/user/list [get]
func (ctl *UserController) FindList(c *gin.Context) {
	var req dto.UserListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	users, total, err := ctl.userService.FindList(tenantID, &req)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.SuccessWithPage(c, users, total, req.Page, req.PageSize)
}

// @Summary 导出用户列表
// @Tags 管理员
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security BearerApiAuth
// @Param username query string false "用户名"
// @Param phone query string false "手机号"
// @Param status query int false "状态"
// @Param deptId query int false "部门ID"
// @Success 200 {file} binary
// @Router /api/v1/system/user/export [get]
func (ctl *UserController) Export(c *gin.Context) {
	var req dto.UserListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	rows, err := ctl.userService.ExportList(tenantID, &req)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	exp, err := excel.NewExporter("用户列表")
	if err != nil {
		common.FailWith(c, err)
		return
	}
	defer func() {
		if cerr := exp.Close(); cerr != nil {
			logger.Log.Warnf("关闭导出器失败: %v", cerr)
		}
	}()

	headers := []string{"ID", "用户名", "昵称", "邮箱", "手机号", "部门ID", "状态", "角色", "创建时间"}
	if err := exp.SetHeaders(headers); err != nil {
		common.FailWith(c, err)
		return
	}

	for _, row := range rows {
		u, ok := row.(service.UserWithRoles)
		if !ok {
			continue
		}

		status := "启用"
		if u.Status != common.StatusEnabled {
			status = "禁用"
		}

		roleNames := make([]string, 0, len(u.Roles))
		for _, r := range u.Roles {
			roleNames = append(roleNames, r.Name)
		}

		if err := exp.AddRow(
			u.ID, u.Username, u.Nickname, u.Email, u.Phone,
			u.DeptID, status, strings.Join(roleNames, "、"),
			u.CreatedAt.Format("2006-01-02 15:04:05"),
		); err != nil {
			common.FailWith(c, err)
			return
		}
	}

	// 导出是文件下载，不走统一 JSON Response（规则5 约束的是业务数据）。
	// 响应头必须在写入正文之前设置，因此这里先设头再写流。
	filename := fmt.Sprintf("users_%s.xlsx", time.Now().Format("20060102150405"))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("X-Export-Rows", strconv.Itoa(exp.RowCount()))

	if err := exp.WriteToWriter(c.Writer); err != nil {
		// 响应头已发出，无法再改状态码，只能记录日志
		logger.Log.Errorf("写出导出文件失败: %v", err)
	}
}

// @Summary 修改用户状态
// @Tags 管理员
// @Accept json
// @Produce json
// @Param body body dto.StatusRequest true "状态"
// @Success 200 {object} common.Response
// @Router /api/v1/system/user/status [put]
func (ctl *UserController) UpdateStatus(c *gin.Context) {
	var req dto.StatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	if err := ctl.userService.UpdateStatus(tenantID, &req); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 重置密码
// @Tags 管理员
// @Accept json
// @Produce json
// @Param body body dto.ResetPasswordRequest true "新密码"
// @Success 200 {object} common.Response
// @Router /api/v1/system/user/resetPwd [put]
func (ctl *UserController) ResetPassword(c *gin.Context) {
	var req dto.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	operatorID := common.GetCurrentUserID(c)
	if req.ID != operatorID {
		common.Forbidden(c, "无权重置其他用户密码")
		return
	}

	if err := ctl.userService.ResetPassword(tenantID, &req); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 修改用户角色
// @Tags 管理员
// @Accept json
// @Produce json
// @Param body body dto.UpdateUserRolesRequest true "角色"
// @Success 200 {object} common.Response
// @Router /api/v1/system/user/roles [put]
func (ctl *UserController) UpdateRoles(c *gin.Context) {
	var req dto.UpdateUserRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	operatorID := common.GetCurrentUserID(c)
	if err := ctl.userService.UpdateRoles(tenantID, operatorID, &req); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 修改用户部门
// @Tags 管理员
// @Accept json
// @Produce json
// @Param body body dto.UpdateUserDeptRequest true "部门"
// @Success 200 {object} common.Response
// @Router /api/v1/system/user/dept [put]
func (ctl *UserController) UpdateDept(c *gin.Context) {
	var req dto.UpdateUserDeptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	if err := ctl.userService.UpdateDept(tenantID, &req); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 修改密码
// @Tags 管理员
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param body body dto.ChangePasswordRequest true "密码信息"
// @Success 200 {object} common.Response
// @Router /api/v1/system/user/changePwd [put]
func (ctl *UserController) ChangePassword(c *gin.Context) {
	var req dto.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	userID := common.GetCurrentUserID(c)
	if err := ctl.userService.ChangePassword(userID, &req); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}
