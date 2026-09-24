package service

import (
	"errors"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"
	"go-admin/pkg/sanitize"

	"gorm.io/gorm"
)

// AgreementService 协议业务。
//
// 协议是租户内数据，因此每个方法都必须接收 tenantID 并透传到 Repository
// （见 AGENTS.md 规则 7）。tenantID 为 0 表示平台级账号（不过滤），
// 这一语义由 middleware.Auth 在入口按身份把住。
type AgreementService interface {
	Create(title, content, typ string, sort int, status int8, operatorID, tenantID uint) error
	Update(id uint, title, content, typ string, sort int, status int8, operatorID, tenantID uint) error
	Delete(tenantID, id uint) error
	FindByID(tenantID, id uint) (*model.SysAgreement, error)
	FindByType(tenantID uint, typ string) (*model.SysAgreement, error)
	FindList(tenantID uint, name, typ string, status *int8, page, pageSize int) ([]model.SysAgreement, int64, error)
}

type agreementService struct {
	agreementRepo repository.AgreementRepository
}

func NewAgreementService() AgreementService {
	return &agreementService{
		agreementRepo: repository.NewAgreementRepository(),
	}
}

func (s *agreementService) Create(title, content, typ string, sort int, status int8, operatorID, tenantID uint) error {
	agreement := &model.SysAgreement{
		TenantBaseModel: common.TenantBaseModel{
			BaseModel: common.BaseModel{
				CreateBy: operatorID,
				UpdateBy: operatorID,
			},
			// 租户取自操作者的登录上下文，不能由请求体指定
			TenantID: tenantID,
		},
		Title: title,
		// 入库前净化：content 由富文本编辑器产出，是**原始 HTML**，
		// 必须在这里（写入口）过滤，而不是指望各渲染点自己处理。
		// 放大因素详见 pkg/sanitize 包注释：内容会原样渲染到页面上，
		// 而 token 存在非 httpOnly Cookie 里，一次 XSS 即可接管账号。
		Content: sanitize.RichText(content),
		Type:    typ,
		Sort:    sort,
		Status:  status,
	}
	return s.agreementRepo.Create(agreement)
}

func (s *agreementService) Update(id uint, title, content, typ string, sort int, status int8, operatorID, tenantID uint) error {
	agreement, err := s.agreementRepo.FindByID(tenantID, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFoundError("记录不存在")
		}
		return err
	}

	agreement.Title = title
	// 同 Create：更新路径也必须净化，否则可以先存干净内容再改成恶意内容绕过
	agreement.Content = sanitize.RichText(content)
	agreement.Type = typ
	agreement.Sort = sort
	agreement.Status = status
	agreement.UpdateBy = operatorID

	return s.agreementRepo.Update(tenantID, agreement)
}

func (s *agreementService) Delete(tenantID, id uint) error {
	return s.agreementRepo.Delete(tenantID, id)
}

func (s *agreementService) FindByID(tenantID, id uint) (*model.SysAgreement, error) {
	agreement, err := s.agreementRepo.FindByID(tenantID, id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "记录不存在")
	}
	return agreement, nil
}

func (s *agreementService) FindByType(tenantID uint, typ string) (*model.SysAgreement, error) {
	return s.agreementRepo.FindByType(tenantID, typ)
}

func (s *agreementService) FindList(tenantID uint, name, typ string, status *int8, page, pageSize int) ([]model.SysAgreement, int64, error) {
	return s.agreementRepo.FindList(tenantID, name, typ, status, page, pageSize)
}
