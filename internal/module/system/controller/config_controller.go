package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/service"
	"go-admin/pkg/upload"

	"github.com/gin-gonic/gin"
)

type ConfigController struct {
	configService service.ConfigService
}

func NewConfigController() *ConfigController {
	return &ConfigController{configService: service.NewConfigService()}
}

// @Summary 创建参数
// @Tags 参数配置
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param name body string true "参数名称"
// @Param key body string true "参数键名"
// @Param value body string false "参数键值"
// @Param type body int false "是否系统内置：0 是 / 1 否"
// @Success 200 {object} common.Response
// @Router /system/config [post]
func (ctl *ConfigController) Create(c *gin.Context) {
	var req struct {
		Name  string `json:"name" binding:"required"`
		Key   string `json:"key" binding:"required"`
		Value string `json:"value"`
		Type  int8   `json:"type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	if err := ctl.configService.Create(req.Name, req.Key, req.Value, req.Type, common.GetCurrentUserID(c)); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 更新参数
// @Tags 参数配置
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id body int true "参数 ID"
// @Param name body string true "参数名称"
// @Param key body string true "参数键名"
// @Param value body string false "参数键值"
// @Param type body int false "是否系统内置"
// @Success 200 {object} common.Response
// @Router /system/config [put]
func (ctl *ConfigController) Update(c *gin.Context) {
	var req struct {
		ID    uint   `json:"id" binding:"required"`
		Name  string `json:"name" binding:"required"`
		Key   string `json:"key" binding:"required"`
		Value string `json:"value"`
		Type  int8   `json:"type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	if err := ctl.configService.Update(req.ID, req.Name, req.Key, req.Value, req.Type, common.GetCurrentUserID(c)); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 删除参数
// @Tags 参数配置
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/config/{id} [delete]
func (ctl *ConfigController) Delete(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	if err := ctl.configService.Delete(id); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 参数列表
// @Tags 参数配置
// @Produce json
// @Security BearerAuth
// @Param name query string false "参数名称（模糊）"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /system/config/list [get]
func (ctl *ConfigController) FindList(c *gin.Context) {
	var req struct {
		Name     string `form:"name"`
		// 分页参数统一内嵌：绑定与归一化走 common.BindPage
		common.PageQuery
	}
	if err := common.BindPage(c, &req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	list, total, err := ctl.configService.FindList(req.Name, req.Page, req.PageSize)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}

// @Summary 站点公开信息
// @Description 返回 `site.` 前缀的配置（站点名、logo 等），登录页也要用，故无需鉴权
// @Tags 站点
// @Produce json
// @Success 200 {object} common.Response
// @Router /site/info [get]
func (ctl *ConfigController) SiteInfo(c *gin.Context) {
	list, err := ctl.configService.FindByPrefix("site.")
	if err != nil {
		common.FailWith(c, err)
		return
	}
	result := map[string]string{}
	for _, item := range list {
		switch cfg := item.(type) {
		case model.SysConfig:
			result[cfg.ConfigKey] = cfg.Value
		case *model.SysConfig:
			result[cfg.ConfigKey] = cfg.Value
		}
	}
	common.Success(c, result)
}

// @Summary 按前缀取参数
// @Tags 参数配置
// @Produce json
// @Security BearerAuth
// @Param prefix query string true "键名前缀，如 site."
// @Success 200 {object} common.Response
// @Router /system/config/prefix [get]
func (ctl *ConfigController) FindByPrefix(c *gin.Context) {
	prefix := c.Query("prefix")
	if prefix == "" {
		common.Error(c, common.CodeBadRequest, "prefix不能为空")
		return
	}
	list, err := ctl.configService.FindByPrefix(prefix)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, list)
}

// @Summary 批量保存参数
// @Description 按前缀成组保存（如站点设置表单一次提交多项）
// @Tags 参数配置
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param prefix body string true "键名前缀"
// @Param items body array true "键值对列表，元素形如 {key, value}"
// @Success 200 {object} common.Response
// @Router /system/config/batch [put]
func (ctl *ConfigController) BatchSave(c *gin.Context) {
	var req struct {
		Prefix string `json:"prefix" binding:"required"`
		Items  []struct {
			Key   string `json:"key" binding:"required"`
			Value string `json:"value"`
		} `json:"items" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	operatorID := common.GetCurrentUserID(c)
	items := make([]service.ConfigItem, len(req.Items))
	for i, item := range req.Items {
		items[i] = service.ConfigItem{Key: item.Key, Value: item.Value}
	}
	if err := ctl.configService.BatchSave(req.Prefix, items, operatorID); err != nil {
		common.FailWith(c, err)
		return
	}

	if req.Prefix == "oss." {
		upload.Reload(service.LoadOSSConfig())
	}
	common.Success(c, nil)
}

// @Summary 上传支付证书
// @Tags 参数配置
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "证书文件"
// @Success 200 {object} common.Response
// @Router /system/config/upload [post]
func (ctl *ConfigController) UploadCert(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "请选择文件")
		return
	}

	ext := filepath.Ext(file.Filename)
	allowedExts := map[string]bool{".pem": true, ".key": true, ".crt": true, ".cer": true}
	if !allowedExts[ext] {
		common.Error(c, common.CodeBadRequest, "仅支持 .pem/.key/.crt/.cer 文件")
		return
	}

	if file.Size > 2*1024*1024 {
		common.Error(c, common.CodeBadRequest, "文件大小不能超过 2MB")
		return
	}

	// 证书必须存放在静态服务目录之外：
	// uploads/ 通过 r.Static("/uploads") 对外匿名可读，
	// 把商户私钥/证书放在那里等同于公密钥（GET /uploads/certs/xxx.key 即可下载）。
	saveDir := filepath.Join("runtime", "certs")
	if err := os.MkdirAll(saveDir, 0700); err != nil {
		common.FailWith(c, err)
		return
	}

	filename := fmt.Sprintf("wechat_%d%s", time.Now().UnixMilli(), ext)
	savePath := filepath.Join(saveDir, filename)

	if err := c.SaveUploadedFile(file, savePath); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, gin.H{
		"path":     savePath,
		"filename": file.Filename,
	})
}
