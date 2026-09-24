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
