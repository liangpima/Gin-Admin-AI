package service

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"
	"go-admin/pkg/upload"

	"gorm.io/gorm"
)

// maskedValue 敏感配置项在接口返回时使用的占位符。
//
// 前端若原样回传该值，说明用户没有修改它，BatchSave 会跳过写入，
// 以免把真实密钥覆盖成占位符本身。
const maskedValue = "******"

// sensitiveKeyFragments 配置项 key 命中以下片段即视为敏感信息，对外返回时打码。
// 宁可多打码（管理员仍可覆写），也不要让密钥回传到浏览器。
var sensitiveKeyFragments = []string{"secret", "password", "passwd", "key", "pem", "private", "token"}

func isSensitiveConfigKey(key string) bool {
	lower := strings.ToLower(key)
	for _, frag := range sensitiveKeyFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// maskConfig 对敏感配置项的值打码
func maskConfig(c model.SysConfig) model.SysConfig {
	if c.Value != "" && isSensitiveConfigKey(c.ConfigKey) {
		c.Value = maskedValue
	}
	return c
}

type ConfigItem struct {
	Key   string
	Value string
}

type ConfigService interface {
	Create(name, key, value string, typ int8, operatorID uint) error
	Update(id uint, name, key, value string, typ int8, operatorID uint) error
	Delete(id uint) error
	FindByID(id uint) (interface{}, error)
	FindByKey(key string) (interface{}, error)
	FindList(name string, page, pageSize int) ([]interface{}, int64, error)
	// FindByPrefix 供接口使用，敏感项的值会被打码
	FindByPrefix(prefix string) ([]interface{}, error)
	// FindByPrefixRaw 供内部读取配置使用，返回真实值
	FindByPrefixRaw(prefix string) ([]interface{}, error)
	BatchSave(prefix string, items []ConfigItem, operatorID uint) error
	// UploadCert 保存支付证书（商户私钥 / 平台证书），返回落盘路径
	UploadCert(file *multipart.FileHeader) (*CertUploadResult, error)
}

// 支付证书上传的约束。
//
// 证书是敏感凭证，与普通业务附件（pkg/upload 的白名单、uploads/ 目录）
// 走完全不同的存储策略，所以这里不复用那份白名单。
const (
	// certMaxSize 证书大小上限（2MB）。证书与私钥都是 KB 级，
	// 这个上限只是给误传大文件一个明确的拒绝点。
	certMaxSize = 2 * 1024 * 1024
	// certSaveDir 证书保存目录。
	//
	// 必须在静态服务目录**之外**：`uploads/` 由 router 以 r.Static("/uploads")
	// 对外匿名可读，把商户私钥放进去等于公开发布
	// （GET /uploads/certs/xxx.key 即可下载）。这里用 runtime/ 下的独立目录，权限 0700。
	certSaveDir = "runtime/certs"
)

// certAllowedExts 证书允许的扩展名，仅这四种。
var certAllowedExts = map[string]bool{".pem": true, ".key": true, ".crt": true, ".cer": true}

// CertUploadResult 证书上传结果。
//
// 用结构体而不是 gin.H：Service 层不得依赖 gin（规则 2）。
// 字段名与既有接口响应保持一致，前端无需改动。
type CertUploadResult struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
}

// UploadCert 校验并保存支付证书。
//
// 这套逻辑原先整体写在 Controller 里（扩展名白名单、大小限制、MkdirAll、
// SaveUploadedFile、文件名生成），属于业务规则，已按规则 1 下沉。
// Controller 现在只做「取文件 → 调这里 → 返回」。
func (s *configService) UploadCert(file *multipart.FileHeader) (*CertUploadResult, error) {
	ext := filepath.Ext(file.Filename)
	if !certAllowedExts[ext] {
		return nil, common.NewBizError("仅支持 .pem/.key/.crt/.cer 文件")
	}
	if file.Size > certMaxSize {
		return nil, common.NewBizError("文件大小不能超过 2MB")
	}

	if err := os.MkdirAll(certSaveDir, 0o700); err != nil {
		return nil, err
	}

	// 落盘文件名只由「时间戳 + 已校验的扩展名」拼成，**不用原始文件名**：
	// 原始名可含路径分隔符或 `..`，拼进路径就能写到目录之外
	// （filepath.Join 会规整 `..`）。原始名仍然原样回给前端展示用。
	filename := fmt.Sprintf("wechat_%d%s", time.Now().UnixMilli(), ext)
	savePath := filepath.Join(certSaveDir, filename)

	if err := saveMultipartFile(file, savePath); err != nil {
		return nil, fmt.Errorf("保存证书失败: %w", err)
	}

	return &CertUploadResult{Path: savePath, Filename: file.Filename}, nil
}

// saveMultipartFile 把上传文件写到 dst。
//
// 等价于 gin 的 `c.SaveUploadedFile`，但 Service 层拿不到 gin.Context，
// 也不该为了这个动作去依赖它。
func saveMultipartFile(file *multipart.FileHeader, dst string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return err
	}
	// 显式返回 Close 的错误而不是 defer 掉：写入的落盘错误在这里才暴露
	return out.Close()
}

type configService struct {
	configRepo repository.ConfigRepository
}

func NewConfigService() ConfigService {
	return &configService{
		configRepo: repository.NewConfigRepository(),
	}
}

func (s *configService) Create(name, key, value string, typ int8, operatorID uint) error {
	config := &model.SysConfig{
		BaseModel: common.BaseModel{
			CreateBy: operatorID,
			UpdateBy: operatorID,
		},
		Name:      name,
		ConfigKey: key,
		Value:     value,
		Type:      typ,
	}

	if err := s.configRepo.Create(config); err != nil {
		// config_key 全局唯一（uk_config_key），冲突时给出可读提示而非 500
		if errors.Is(err, common.ErrDuplicateKey) {
			return common.NewBizError("配置项已存在，请更换配置键名")
		}
		return err
	}
	return nil
}

func (s *configService) Update(id uint, name, key, value string, typ int8, operatorID uint) error {
	config, err := s.configRepo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFoundError("配置不存在")
		}
		return err
	}

	config.Name = name
	config.ConfigKey = key
	config.UpdateBy = operatorID
	config.Type = typ

	// 敏感项原样回传打码占位符 = 用户没有修改它，保留库里的真实值。
	//
	// 必须判断：FindList / FindByPrefix 返回的是打码后的 "******"，
	// 管理员在编辑框里只改了名称就保存时，前端会把 "******" 一起提交回来。
	// 若直接落库，真实的支付/OSS/短信密钥会被覆盖成字面量 "******"，
	// 之后的表现是「签名失败」「上传失败」，完全指不到是配置被写坏了。
	// BatchSave 早已有此保护，Update 这条路径之前漏了，这里补齐。
	if !isMaskedSensitiveSubmit(value, key, config.ConfigKey) {
		config.Value = value
	}

	if err := s.configRepo.Update(config); err != nil {
		// 改名撞 uk_config_key 时给出可读提示，而不是落到 500
		if errors.Is(err, common.ErrDuplicateKey) {
			return common.NewBizError("配置项已存在，请更换配置键名")
		}
		return err
	}
	return nil
}

// isMaskedSensitiveSubmit 判断本次提交是否只是「把打码占位符原样交回来」。
//
// 是 → 保留库里的真实密钥（用户只改了别的字段，没动密钥）；
// 否 → 按提交值更新。
// 抽成具名函数而不是写成一串 !(a && (b || c))：那个写法读起来要绕两圈，
// 而这里判断错了的后果是「真实密钥被覆盖成 ******」，且很难从现象倒推。
func isMaskedSensitiveSubmit(value, submittedKey, storedKey string) bool {
	if value != maskedValue {
		return false
	}
	return isSensitiveConfigKey(submittedKey) || isSensitiveConfigKey(storedKey)
}

func (s *configService) Delete(id uint) error {
	// 仓储在记录不存在时返回 gorm.ErrRecordNotFound → 转成 404 而不是 500
	return common.NotFoundOrErr(s.configRepo.Delete(id), "配置不存在")
}

func (s *configService) FindByID(id uint) (interface{}, error) {
	config, err := s.configRepo.FindByID(id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "配置不存在")
	}
	return config, nil
}

func (s *configService) FindByKey(key string) (interface{}, error) {
	return s.configRepo.FindByKey(key)
}

// FindList 配置分页列表；敏感项的值会打码后再返回
func (s *configService) FindList(name string, page, pageSize int) ([]interface{}, int64, error) {
	configs, total, err := s.configRepo.FindList(name, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	result := make([]interface{}, len(configs))
	for i, c := range configs {
		result[i] = maskConfig(c)
	}
	return result, total, nil
}

// FindByPrefix 按前缀查询配置（接口用），敏感项的值会打码
func (s *configService) FindByPrefix(prefix string) ([]interface{}, error) {
	configs, err := s.configRepo.FindByKeyPrefix(prefix)
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(configs))
	for i, c := range configs {
		result[i] = maskConfig(c)
	}
	return result, nil
}

// FindByPrefixRaw 按前缀查询配置并返回真实值，仅供服务内部读取密钥等配置使用
func (s *configService) FindByPrefixRaw(prefix string) ([]interface{}, error) {
	configs, err := s.configRepo.FindByKeyPrefix(prefix)
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(configs))
	for i, c := range configs {
		result[i] = c
	}
	return result, nil
}

// BatchSave 批量保存配置。
//
// 整批必须一次提交：逐条 UpsertByKey 各自开事务时，中途失败会留下
// 「前几项已生效、后面几项没写」的混合状态。对支付/OSS 这类成组配置尤其危险 ——
// 密钥写了一半的现象是「签名失败」「上传失败」，完全指不到是配置没存全。
func (s *configService) BatchSave(prefix string, items []ConfigItem, operatorID uint) error {
	err := s.configRepo.Transaction(func(txRepo repository.ConfigRepository) error {
		for _, item := range items {
			// 前端原样回传打码占位符，说明该项未被修改，跳过以保留原值
			if item.Value == maskedValue {
				continue
			}

			config := &model.SysConfig{
				BaseModel: common.BaseModel{
					UpdateBy: operatorID,
				},
				ConfigKey: prefix + item.Key,
				Value:     item.Value,
				Type:      1,
			}
			if err := txRepo.UpsertByKey(config); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 保存的是 OSS 配置时，立刻把新值推给上传模块。
	//
	// 这个动作必须放在**事务提交之后**（事务里推会导致回滚后上传模块带着
	// 一份不存在的配置运行），也必须放在 Service 层 —— 早前它写在 Controller 里，
	// 属于「保存配置」这条业务规则的收尾步骤，落进 Controller 就违反了规则 1。
	if prefix == "oss." {
		upload.Reload(LoadOSSConfig())
	}
	return nil
}

// LoadOSSConfig 从 sys_config 表读取 oss.* 配置，返回 key-value map。
// 需要真实密钥，因此使用 FindByPrefixRaw。
func LoadOSSConfig() map[string]string {
	svc := NewConfigService()
	results, err := svc.FindByPrefixRaw("oss.")
	if err != nil {
		// 不静默：读不到配置会让上传模块回退到本地存储（或带着空凭据初始化），
		// 排查时表现为「配置明明填了却没用上」，必须能从日志看出是查库失败。
		// 仍然返回空 map 而不是让调用方崩溃：上传属于非核心路径，
		// 配置缺失时回退本地存储是既有行为。
		logger.Log.Errorf("[config] 读取 oss.* 配置失败，将使用空配置: %v", err)
	}

	cfgMap := make(map[string]string)
	for _, r := range results {
		if cfg, ok := r.(model.SysConfig); ok {
			key := cfg.ConfigKey
			if len(key) > 4 && key[:4] == "oss." {
				cfgMap[key[4:]] = cfg.Value
			}
		}
	}
	return cfgMap
}
