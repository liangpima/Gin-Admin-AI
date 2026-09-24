package service

import (
	"errors"

	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"

	"gorm.io/gorm"
)

// ErrDeptHasChildren 删除部门时存在下级部门。
//
// 用哨兵错误暴露出来，让 Controller 能把它归为 400（参数/前置条件不满足），
// 而不是笼统地返回 500 —— 这是调用方的用法问题，不是服务端故障。
var ErrDeptHasChildren = common.NewBizError("存在下级部门，请先删除下级部门")

// DeptService 部门业务。
//
// 部门是租户内数据，因此每个方法都必须接收 tenantID 并透传到 Repository
// （见 AGENTS.md 规则 7）。tenantID 为 0 表示平台级账号（不过滤），
// 这一语义由 middleware.Auth 在入口按身份把住（只有 admin 角色能拿到 0）。
type DeptService interface {
	Create(req *dto.CreateDeptRequest, operatorID, tenantID uint) error
	Update(req *dto.UpdateDeptRequest, operatorID, tenantID uint) error
	Delete(tenantID, id uint) error
	FindByID(tenantID, id uint) (interface{}, error)
	FindTree(tenantID uint) ([]model.SysDept, error)
}

type deptService struct {
	deptRepo repository.DeptRepository
}

func NewDeptService() DeptService {
	return &deptService{
		deptRepo: repository.NewDeptRepository(),
	}
}

func (s *deptService) Create(req *dto.CreateDeptRequest, operatorID, tenantID uint) error {
	if err := s.ensureParentExists(tenantID, req.ParentID); err != nil {
		return err
	}

	dept := &model.SysDept{
		TenantBaseModel: common.TenantBaseModel{
			BaseModel: common.BaseModel{
				CreateBy: operatorID,
				UpdateBy: operatorID,
			},
			// 租户取自操作者的登录上下文，**不能**由请求体指定 ——
			// 否则租户 A 可以把部门建到租户 B 名下
			TenantID: tenantID,
		},
		ParentID: req.ParentID,
		Name:     req.Name,
		Sort:     req.Sort,
		Leader:   req.Leader,
		Phone:    req.Phone,
		Email:    req.Email,
		Status:   req.Status,
	}

	return s.deptRepo.Create(dept)
}

// ensureParentExists 校验上级节点存在（parentID 为 0 表示挂到根）。
//
// 不校验的后果非常隐蔽：parent_id 指向一个不存在的 ID 时，
// INSERT 本身会成功，但 FindTree 是从 parent_id=0 出发构建的，
// 这个节点永远不可达 —— 表现为「提示创建成功，列表里却找不到」，
// 数据却真实留在库里，既看不见也删不掉。
//
// 查询必须带租户：挂到**其他租户**的部门之下，后果与上面完全相同
// （FindTree 只加载本租户的部门，父节点不在结果里 → 该节点不可达），
// 而且它还会把本租户的部门结构信息挂到别人的树上。带上租户过滤后，
// 跨租户的父节点会直接表现为「上级部门不存在」。
func (s *deptService) ensureParentExists(tenantID, parentID uint) error {
	if parentID == 0 {
		return nil
	}
	if _, err := s.deptRepo.FindByID(tenantID, parentID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewBizError("上级部门不存在")
		}
		return err
	}
	return nil
}

func (s *deptService) Update(req *dto.UpdateDeptRequest, operatorID, tenantID uint) error {
	dept, err := s.deptRepo.FindByID(tenantID, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFoundError("部门不存在")
		}
		return err
	}

	// ParentID 为 nil 表示本次不移动部门，父级相关校验一并跳过
	if req.ParentID != nil {
		if err := s.ensureParentExists(tenantID, *req.ParentID); err != nil {
			return err
		}

		// 同菜单：禁止把部门挂到自己或自己的下级之下，避免产生遍历不到的孤儿子树
		if cycle, err := hasCycleInHierarchy(req.ID, *req.ParentID,
			func(id uint) (uint, bool, error) { return s.deptRepo.FindParentID(tenantID, id) }); err != nil {
			return err
		} else if cycle {
			return common.NewBizError("不能将部门移动到它自己或它的下级之下")
		}
		dept.ParentID = *req.ParentID
	}

	if req.Name != "" {
		dept.Name = req.Name
	}
	if req.Sort != nil {
		dept.Sort = *req.Sort
	}
	if req.Leader != nil {
		dept.Leader = *req.Leader
	}
	if req.Phone != nil {
		dept.Phone = *req.Phone
	}
	if req.Email != nil {
		dept.Email = *req.Email
	}
	if req.Status != nil {
		dept.Status = *req.Status
	}
	dept.UpdateBy = operatorID

	return s.deptRepo.Update(tenantID, dept)
}

// Delete 删除部门。
//
// 存在子部门时拒绝删除：直接删父节点会让子部门的 parent_id 悬空，
// 而 FindTree 从 parent_id=0 递归构建，这棵子树会「从界面上消失」，
// 数据却还在库里，既看不见也删不掉，成为孤儿数据。
func (s *deptService) Delete(tenantID, id uint) error {
	children, err := s.deptRepo.CountByParentID(tenantID, id)
	if err != nil {
		return err
	}
	if children > 0 {
		return ErrDeptHasChildren
	}
	return s.deptRepo.Delete(tenantID, id)
}

func (s *deptService) FindByID(tenantID, id uint) (interface{}, error) {
	dept, err := s.deptRepo.FindByID(tenantID, id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "部门不存在")
	}
	return dept, nil
}

func (s *deptService) FindTree(tenantID uint) ([]model.SysDept, error) {
	depts, err := s.deptRepo.FindAll(tenantID)
	if err != nil {
		return nil, err
	}
	return buildDeptTree(depts, 0), nil
}

// buildDeptTree 把扁平部门列表组装成树。
//
// 用 BuildTreeForest 而不是 BuildTree：按租户过滤后，父节点**合法地**可能
// 不在结果集里（历史数据的父部门仍留在平台级 tenant_id=0，见该函数注释），
// 此时 BuildTree 会返回空树，表现为「部门管理页一片空白」。
// 把这类节点提升为根，层级关系不丢，只是多出几个顶层节点。
func buildDeptTree(depts []model.SysDept, parentID uint) []model.SysDept {
	return common.BuildTreeForest(depts, parentID,
		func(d model.SysDept) uint { return d.ID },
		func(d model.SysDept) uint { return d.ParentID },
		func(d *model.SysDept, children []model.SysDept) { d.Children = children },
	)
}
