package service

import (
	"errors"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"

	"gorm.io/gorm"
)

// PostService 岗位业务逻辑。
//
// sys_post 是多租户表，tenantID 从 Controller（JWT claims）一路透传到 Repository。
type PostService interface {
	Create(tenantID uint, name, code string, sort int, status int8, operatorID uint) error
	Update(tenantID, id uint, name, code string, sort int, status int8, operatorID uint) error
	Delete(tenantID, id uint) error
	FindByID(tenantID, id uint) (interface{}, error)
	// FindByIDs 供用户分配岗位时校验岗位归属
	FindByIDs(tenantID uint, ids []uint) ([]model.SysPost, error)
	FindAll(tenantID uint) ([]model.SysPost, error)
	FindList(tenantID uint, name string, status *int8, page, pageSize int) ([]interface{}, int64, error)
	UpdateStatus(tenantID, id uint, status int8) error
}

type postService struct {
	postRepo repository.PostRepository
}

func NewPostService() PostService {
	return &postService{
		postRepo: repository.NewPostRepository(),
	}
}

func (s *postService) Create(tenantID uint, name, code string, sort int, status int8, operatorID uint) error {
	if count, err := s.postRepo.CountByCode(tenantID, code, 0); err != nil {
		return err
	} else if count > 0 {
		return common.NewBizError("岗位编码已存在")
	}

	post := &model.SysPost{
		TenantBaseModel: common.TenantBaseModel{
			BaseModel: common.BaseModel{
				CreateBy: operatorID,
				UpdateBy: operatorID,
			},
			TenantID: tenantID,
		},
		Code:   code,
		Name:   name,
		Sort:   sort,
		Status: status,
	}

	if err := s.postRepo.Create(post); err != nil {
		// 上面 Count 校验有时间窗口，并发下靠 (tenant_id, code) 唯一索引兜底
		if errors.Is(err, common.ErrDuplicateKey) {
			return common.NewBizError("岗位编码已存在")
		}
		return err
	}
	return nil
}

func (s *postService) Update(tenantID, id uint, name, code string, sort int, status int8, operatorID uint) error {
	if count, err := s.postRepo.CountByCode(tenantID, code, id); err != nil {
		return err
	} else if count > 0 {
		return common.NewBizError("岗位编码已存在")
	}

	post, err := s.postRepo.FindByID(tenantID, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFoundError("岗位不存在")
		}
		return err
	}

	post.Name = name
	post.Code = code
	post.Sort = sort
	post.Status = status
	post.UpdateBy = operatorID

	if err := s.postRepo.Update(tenantID, post); err != nil {
		// 改编码时可能撞 (tenant_id, code) 唯一索引
		if errors.Is(err, common.ErrDuplicateKey) {
			return common.NewBizError("岗位编码已存在")
		}
		return err
	}
	return nil
}

func (s *postService) Delete(tenantID, id uint) error {
	return common.NotFoundOrErr(s.postRepo.Delete(tenantID, id), "岗位不存在")
}

func (s *postService) FindByID(tenantID, id uint) (interface{}, error) {
	post, err := s.postRepo.FindByID(tenantID, id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "岗位不存在")
	}
	return post, nil
}

func (s *postService) FindByIDs(tenantID uint, ids []uint) ([]model.SysPost, error) {
	return s.postRepo.FindByIDs(tenantID, ids)
}

func (s *postService) FindAll(tenantID uint) ([]model.SysPost, error) {
	return s.postRepo.FindAll(tenantID)
}

func (s *postService) FindList(tenantID uint, name string, status *int8, page, pageSize int) ([]interface{}, int64, error) {
	posts, total, err := s.postRepo.FindList(tenantID, name, status, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	result := make([]interface{}, len(posts))
	for i, p := range posts {
		result[i] = p
	}
	return result, total, nil
}

func (s *postService) UpdateStatus(tenantID, id uint, status int8) error {
	post, err := s.postRepo.FindByID(tenantID, id)
	if err != nil {
		return common.NotFoundOrErr(err, "岗位不存在")
	}
	post.Status = status
	return s.postRepo.Update(tenantID, post)
}
