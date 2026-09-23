package service

import (
	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"
	"go-admin/internal/module/member/repository"
)

type PointsLogService interface {
	FindList(tenantID uint, req *dto.PointsLogListRequest) ([]model.PointsLog, int64, error)
}

type pointsLogService struct {
	pointsLogRepo repository.PointsLogRepository
}

func NewPointsLogService() PointsLogService {
	return &pointsLogService{
		pointsLogRepo: repository.NewPointsLogRepository(),
	}
}

func (s *pointsLogService) FindList(tenantID uint, req *dto.PointsLogListRequest) ([]model.PointsLog, int64, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	req.Page, req.PageSize = common.NormalizePageParams(req.Page, req.PageSize)
	return s.pointsLogRepo.FindList(tenantID, req.MemberID, req.Type, req.Page, req.PageSize)
}
