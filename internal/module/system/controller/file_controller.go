package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/system/service"

	"github.com/gin-gonic/gin"
)

type FileController struct {
	fileService service.FileService
}

func NewFileController() *FileController {
	return &FileController{fileService: service.NewFileService()}
}

// @Summary 上传文件
// @Tags 文件
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "文件"
// @Success 200 {object} common.Response
// @Router /system/file/upload [post]
func (ctl *FileController) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "请选择文件")
		return
	}

	// 大小校验、落库、存储细节都在 Service（规则 1：Controller 只取参与返回）
	dbFile, err := ctl.fileService.Upload(
		common.GetTenantID(c), common.GetCurrentUserID(c), file)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, gin.H{
		"id":   dbFile.ID,
		"name": dbFile.Name,
		"url":  dbFile.URL,
		"size": dbFile.Size,
	})
}

// @Summary 文件列表
// @Tags 文件
// @Produce json
// @Security BearerAuth
// @Param name query string false "文件名"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /system/file/list [get]
func (ctl *FileController) FindList(c *gin.Context) {
	name := c.Query("name")
	mimeType := c.Query("mimeType")
	sortOrder := c.DefaultQuery("sortOrder", "desc")
	page, pageSize, err := common.GetPageInfo(c)
	if err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	list, total, err := ctl.fileService.FindList(tenantID, name, mimeType, sortOrder, page, pageSize)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, page, pageSize)
}

// @Summary 获取文件详情
// @Tags 文件
// @Produce json
// @Security BearerAuth
// @Param id path int true "文件ID"
// @Success 200 {object} common.Response
// @Router /system/file/{id} [get]
func (ctl *FileController) FindByID(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	tenantID := common.GetTenantID(c)
	file, err := ctl.fileService.FindByID(tenantID, id)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, file)
}

// @Summary 删除文件
// @Tags 文件
// @Produce json
// @Security BearerAuth
// @Param id path int true "文件ID"
// @Success 200 {object} common.Response
// @Router /system/file/{id} [delete]
func (ctl *FileController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	tenantID := common.GetTenantID(c)
	// 顺带删除磁盘文件由 Service 负责（规则 1）：它不是「参数接收或返回结果」，
	// 而是删除这条业务规则的收尾步骤，已下沉到 fileService.Delete。
	if err := ctl.fileService.Delete(tenantID, id); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}
