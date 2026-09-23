package service

import (

	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"
	"go-admin/internal/module/member/repository"
)

type MemberLevelService interface {
	Create(req *dto.CreateMemberLevelRequest, operatorID, tenantID uint) error
	Update(req *dto.UpdateMemberLevelRequest, operatorID, tenantID uint) error
	Delete(tenantID, id uint) error
	FindList(tenantID uint, req *dto.MemberLevelListRequest) ([]model.MemberLevel, int64, error)
	FindAll(tenantID uint) ([]model.MemberLevel, error)
}

type memberLevelService struct {
	levelRepo repository.MemberLevelRepository
}

func NewMemberLevelService() MemberLevelService {
	return &memberLevelService{
		levelRepo: repository.NewMemberLevelRepository(),
	}
}

func (s *memberLevelService) Create(req *dto.CreateMemberLevelRequest, operatorID, tenantID uint) error {
	level := &model.MemberLevel{
		TenantBaseModel: common.TenantBaseModel{
			BaseModel: common.BaseModel{
				CreateBy: operatorID,
				UpdateBy: operatorID,
			},
			TenantID: tenantID,
		},
		Name:      req.Name,
		MinPoints: req.MinPoints,
		Discount:  req.Discount,
		Icon:      req.Icon,
		Sort:      req.Sort,
		Status:    req.Status,
	}
	return s.levelRepo.Create(level)
}

func (s *memberLevelService) Update(req *dto.UpdateMemberLevelRequest, operatorID, tenantID uint) error {
	level, err := s.levelRepo.FindByID(tenantID, req.ID)
	if err != nil {
		return common.NewNotFoundError("等级不存在")
	}
	level.Name = req.Name
	level.MinPoints = req.MinPoints
	level.Discount = req.Discount
	level.Icon = req.Icon
	level.Sort = req.Sort
	level.Status = req.Status
	level.UpdateBy = operatorID
	return s.levelRepo.Update(tenantID, level)
}

func (s *memberLevelService) Delete(tenantID, id uint) error {
	return s.levelRepo.Delete(tenantID, id)
}

func (s *memberLevelService) FindList(tenantID uint, req *dto.MemberLevelListRequest) ([]model.MemberLevel, int64, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	req.Page, req.PageSize = common.NormalizePageParams(req.Page, req.PageSize)
	return s.levelRepo.FindList(tenantID, req.Name, req.Page, req.PageSize)
}

func (s *memberLevelService) FindAll(tenantID uint) ([]model.MemberLevel, error) {
	return s.levelRepo.FindAll(tenantID)
}
