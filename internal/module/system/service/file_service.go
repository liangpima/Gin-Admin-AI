package service

import (
	"fmt"
	"mime/multipart"

	"go-admin/config"
	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"
	"go-admin/pkg/upload"
)

type FileService interface {
	Upload(tenantID, operatorID uint, file *multipart.FileHeader) (*model.SysFile, error)
	Create(tenantID uint, file *model.SysFile) error
	FindByID(tenantID, id uint) (*model.SysFile, error)
	FindList(tenantID uint, name, mimeType, sortOrder string, page, pageSize int) ([]model.SysFile, int64, error)
	Delete(tenantID, id uint) error
}

type fileService struct {
	fileRepo repository.FileRepository
}

func NewFileService() FileService {
	return &fileService{
		fileRepo: repository.NewFileRepository(),
	}
}

// Create 创建文件记录，自动绑定租户
// defaultMaxUploadSize 未配置 upload.max_size 时的兜底上限（10MB）
const defaultMaxUploadSize int64 = 10 * 1024 * 1024

// Upload 校验并保存上传的文件。
//
// 原先这套逻辑写在 Controller 里（大小校验、构造 model、调存储、拼响应），
// 属于业务规则，已按规则 1 下沉。Controller 现在只负责取文件与返回结果。
func (s *fileService) Upload(tenantID, operatorID uint, file *multipart.FileHeader) (*model.SysFile, error) {
	maxSize := int64(config.Cfg.Upload.MaxSize) * 1024 * 1024
	if maxSize <= 0 {
		maxSize = defaultMaxUploadSize
	}
	if file.Size > maxSize {
		return nil, common.NewBizErrorWithCode(common.CodeFileTooLarge,
			fmt.Sprintf("文件大小不能超过 %dMB", maxSize/1024/1024))
	}

	storagePath, err := upload.Upload(file)
	if err != nil {
		// 原样返回会走 FailWith 的 500 分支，对外只给通用文案，
		// 真实原因（存储路径、OSS 凭据等）只进日志 —— 这里用 %w 保留错误链供排查
		return nil, fmt.Errorf("保存上传文件失败: %w", err)
	}

	dbFile := &model.SysFile{
		Name:     file.Filename,
		Path:     storagePath,
		URL:      upload.GetURL(storagePath),
		Size:     file.Size,
		MimeType: file.Header.Get("Content-Type"),
	}
	dbFile.CreateBy = operatorID
	dbFile.UpdateBy = operatorID
	// 仓储的 Create 不接收 tenantID，归属由 Service 赋值（与 Create 方法一致）
	dbFile.TenantID = tenantID

	if err := s.fileRepo.Create(dbFile); err != nil {
		return nil, err
	}
	return dbFile, nil
}

func (s *fileService) Create(tenantID uint, file *model.SysFile) error {
	file.TenantID = tenantID
	return s.fileRepo.Create(file)
}

func (s *fileService) FindByID(tenantID, id uint) (*model.SysFile, error) {
	file, err := s.fileRepo.FindByID(tenantID, id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "文件不存在")
	}
	return file, nil
}

func (s *fileService) FindList(tenantID uint, name, mimeType, sortOrder string, page, pageSize int) ([]model.SysFile, int64, error) {
	return s.fileRepo.FindList(tenantID, name, mimeType, sortOrder, page, pageSize)
}

func (s *fileService) Delete(tenantID, id uint) error {
	return s.fileRepo.Delete(tenantID, id)
}
