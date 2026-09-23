package service

import (

	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"
	"go-admin/internal/module/member/repository"
)

type MemberTagService interface {
	Create(req *dto.CreateMemberTagRequest, operatorID, tenantID uint) error
	Update(req *dto.UpdateMemberTagRequest, operatorID, tenantID uint) error
	Delete(tenantID, id uint) error
	FindList(tenantID uint, req *dto.MemberTagListRequest) ([]model.MemberTag, int64, error)
	FindAll(tenantID uint) ([]model.MemberTag, error)
}

type memberTagService struct {
	tagRepo repository.MemberTagRepository
}

func NewMemberTagService() MemberTagService {
	return &memberTagService{
		tagRepo: repository.NewMemberTagRepository(),
	}
}

func (s *memberTagService) Create(req *dto.CreateMemberTagRequest, operatorID, tenantID uint) error {
	tag := &model.MemberTag{
		TenantBaseModel: common.TenantBaseModel{
			BaseModel: common.BaseModel{
				CreateBy: operatorID,
				UpdateBy: operatorID,
			},
			TenantID: tenantID,
		},
		Name:   req.Name,
		Color:  req.Color,
		Sort:   req.Sort,
		Status: req.Status,
	}
	return s.tagRepo.Create(tag)
}

func (s *memberTagService) Update(req *dto.UpdateMemberTagRequest, operatorID, tenantID uint) error {
	tag, err := s.tagRepo.FindByID(tenantID, req.ID)
	if err != nil {
		return common.NewNotFoundError("标签不存在")
	}
	if req.Name != "" {
		tag.Name = req.Name
	}
	if req.Color != nil {
		tag.Color = *req.Color
	}
	if req.Sort != nil {
		tag.Sort = *req.Sort
	}
	if req.Status != nil {
		tag.Status = *req.Status
	}
	tag.UpdateBy = operatorID
	return s.tagRepo.Update(tenantID, tag)
}

func (s *memberTagService) Delete(tenantID, id uint) error {
	return s.tagRepo.Delete(tenantID, id)
}

func (s *memberTagService) FindList(tenantID uint, req *dto.MemberTagListRequest) ([]model.MemberTag, int64, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	req.Page, req.PageSize = common.NormalizePageParams(req.Page, req.PageSize)
	return s.tagRepo.FindList(tenantID, req.Name, req.Page, req.PageSize)
}

func (s *memberTagService) FindAll(tenantID uint) ([]model.MemberTag, error) {
	return s.tagRepo.FindAll(tenantID)
}
