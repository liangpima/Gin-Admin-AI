package controller

import (
	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/service"

	"github.com/gin-gonic/gin"
)

type DictController struct {
	dictService service.DictService
}

func NewDictController() *DictController {
	return &DictController{dictService: service.NewDictService()}
}

// @Summary 创建字典类型
// @Tags 字典
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.CreateDictTypeRequest true "字典类型信息"
// @Success 200 {object} common.Response
// @Router /system/dict/type [post]
func (ctl *DictController) CreateType(c *gin.Context) {
	var req dto.CreateDictTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	if err := ctl.dictService.CreateType(&req, common.GetCurrentUserID(c)); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 更新字典类型
// @Tags 字典
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateDictTypeRequest true "字典类型信息（含 id）"
// @Success 200 {object} common.Response
// @Router /system/dict/type/{id} [put]
func (ctl *DictController) UpdateType(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	var req dto.UpdateDictTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	if err := ctl.dictService.UpdateType(id, &req, common.GetCurrentUserID(c)); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 删除字典类型
// @Tags 字典
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/dict/type/{id} [delete]
func (ctl *DictController) DeleteType(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	if err := ctl.dictService.DeleteType(id); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 字典类型列表
// @Tags 字典
// @Produce json
// @Security BearerAuth
// @Param name query string false "字典名称（模糊）"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /system/dict/type/list [get]
func (ctl *DictController) FindTypeList(c *gin.Context) {
	var req struct {
		Name     string `form:"name"`
		// 分页参数统一内嵌：绑定与归一化走 common.BindPage
		common.PageQuery
	}
	if err := common.BindPage(c, &req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	list, total, err := ctl.dictService.FindTypeList(req.Name, req.Page, req.PageSize)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}

// @Summary 创建字典数据
// @Tags 字典
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.CreateDictDataRequest true "字典项信息"
// @Success 200 {object} common.Response
// @Router /system/dict/data [post]
func (ctl *DictController) CreateData(c *gin.Context) {
	var req dto.CreateDictDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	if err := ctl.dictService.CreateData(&req, common.GetCurrentUserID(c)); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 更新字典数据
// @Tags 字典
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body dto.UpdateDictDataRequest true "字典项信息（含 id）"
// @Success 200 {object} common.Response
// @Router /system/dict/data/{id} [put]
func (ctl *DictController) UpdateData(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	var req dto.UpdateDictDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	if err := ctl.dictService.UpdateData(id, &req, common.GetCurrentUserID(c)); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// @Summary 删除字典数据
// @Tags 字典
// @Produce json
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} common.Response
// @Router /system/dict/data/{id} [delete]
func (ctl *DictController) DeleteData(c *gin.Context) {
	id, err := common.GetUintParam(c, "id")
	if err != nil {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	if err := ctl.dictService.DeleteData(id); err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, nil)
}

// FindDataByType 按类型取「启用中」的字典选项，供业务页面渲染下拉框 / 标签。
//
// 与 /dict/data/list 的区别：那个是管理端的带条件分页列表（含停用项、需要 dict:list 权限），
// 这个只返回 status=1 并按 sort 排序，是纯引用数据 —— 任何登录用户都要用，
// 所以路由只要求登录态。若也卡 dict:list，普通操作员进用户管理页时会因为
// 拿不到「状态」下拉选项而看到空下拉。
// @Summary 按类型取字典项
// @Description 下拉选项用；前端有缓存，改字典后需要清缓存
// @Tags 字典
// @Produce json
// @Security BearerAuth
// @Param type path string true "字典类型编码"
// @Success 200 {object} common.Response
// @Router /system/dict/data/type/{type} [get]
func (ctl *DictController) FindDataByType(c *gin.Context) {
	typ := c.Param("type")
	if typ == "" {
		common.Error(c, common.CodeBadRequest, "参数错误")
		return
	}
	list, err := ctl.dictService.FindDataByType(typ)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.Success(c, list)
}

// @Summary 字典项列表
// @Tags 字典
// @Produce json
// @Security BearerAuth
// @Param dictType query string false "字典类型编码"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /system/dict/data/list [get]
func (ctl *DictController) FindDataList(c *gin.Context) {
	var req struct {
		DictType string `form:"dictType"`
		// 分页参数统一内嵌：绑定与归一化走 common.BindPage
		common.PageQuery
	}
	if err := common.BindPage(c, &req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}
	// 分页参数必须取自请求：此前这里硬编码 page=1/pageSize=10，
	// 前端传 pageSize=100 被静默忽略，字典数据超过 10 条就再也看不到。

	list, total, err := ctl.dictService.FindDataList(req.DictType, req.Page, req.PageSize)
	if err != nil {
		common.FailWith(c, err)
		return
	}
	common.SuccessWithPage(c, list, total, req.Page, req.PageSize)
}
