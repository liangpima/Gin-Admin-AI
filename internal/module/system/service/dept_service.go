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

type DeptService interface {
	Create(req *dto.CreateDeptRequest, operatorID uint) error
	Update(req *dto.UpdateDeptRequest, operatorID uint) error
	Delete(id uint) error
	FindByID(id uint) (interface{}, error)
	FindTree() ([]model.SysDept, error)
}

type deptService struct {
	deptRepo repository.DeptRepository
}

func NewDeptService() DeptService {
	return &deptService{
		deptRepo: repository.NewDeptRepository(),
	}
}

func (s *deptService) Create(req *dto.CreateDeptRequest, operatorID uint) error {
	if err := s.ensureParentExists(req.ParentID); err != nil {
		return err
	}

	dept := &model.SysDept{
		BaseModel: common.BaseModel{
			CreateBy: operatorID,
			UpdateBy: operatorID,
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
func (s *deptService) ensureParentExists(parentID uint) error {
	if parentID == 0 {
		return nil
	}
	if _, err := s.deptRepo.FindByID(parentID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewBizError("上级部门不存在")
		}
		return err
	}
	return nil
}

func (s *deptService) Update(req *dto.UpdateDeptRequest, operatorID uint) error {
	dept, err := s.deptRepo.FindByID(req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFoundError("部门不存在")
		}
		return err
	}

	// ParentID 为 nil 表示本次不移动部门，父级相关校验一并跳过
	if req.ParentID != nil {
		if err := s.ensureParentExists(*req.ParentID); err != nil {
			return err
		}

		// 同菜单：禁止把部门挂到自己或自己的下级之下，避免产生遍历不到的孤儿子树
		if cycle, err := hasCycleInHierarchy(req.ID, *req.ParentID, s.deptRepo.FindParentID); err != nil {
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

	return s.deptRepo.Update(dept)
}

// Delete 删除部门。
//
// 存在子部门时拒绝删除：直接删父节点会让子部门的 parent_id 悬空，
// 而 FindTree 从 parent_id=0 递归构建，这棵子树会「从界面上消失」，
// 数据却还在库里，既看不见也删不掉，成为孤儿数据。
func (s *deptService) Delete(id uint) error {
	children, err := s.deptRepo.CountByParentID(id)
	if err != nil {
		return err
	}
	if children > 0 {
		return ErrDeptHasChildren
	}
	return s.deptRepo.Delete(id)
}

func (s *deptService) FindByID(id uint) (interface{}, error) {
	dept, err := s.deptRepo.FindByID(id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "部门不存在")
	}
	return dept, nil
}

func (s *deptService) FindTree() ([]model.SysDept, error) {
	depts, err := s.deptRepo.FindAll()
	if err != nil {
		return nil, err
	}
	return buildDeptTree(depts, 0), nil
}

// buildDeptTree 把扁平部门列表组装成树。
//
// 实现已抽到 common.BuildTree（O(n) 的 map 索引版本），
// 与菜单共用同一份逻辑，避免两处各修一遍。
func buildDeptTree(depts []model.SysDept, parentID uint) []model.SysDept {
	return common.BuildTree(depts, parentID,
		func(d model.SysDept) uint { return d.ID },
		func(d model.SysDept) uint { return d.ParentID },
		func(d *model.SysDept, children []model.SysDept) { d.Children = children },
	)
}
