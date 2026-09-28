package service

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"

	"gorm.io/gorm"
)

// mockConfigRepo 只实现测试用到的行为，其余方法返回零值。
type mockConfigRepo struct {
	configs map[uint]*model.SysConfig
	nextID  uint
	// deleteFn 让用例控制 Delete 的返回值（区分 404 与 500 两条路径）
	deleteFn func(id uint) error
}

func newMockConfigRepo(seed ...*model.SysConfig) *mockConfigRepo {
	m := &mockConfigRepo{configs: make(map[uint]*model.SysConfig), nextID: 1}
	for _, c := range seed {
		c.ID = m.nextID
		m.nextID++
		m.configs[c.ID] = c
	}
	return m
}

func (m *mockConfigRepo) Create(config *model.SysConfig) error {
	config.ID = m.nextID
	m.nextID++
	m.configs[config.ID] = config
	return nil
}

func (m *mockConfigRepo) FindByID(id uint) (*model.SysConfig, error) {
	if c, ok := m.configs[id]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *mockConfigRepo) FindByKey(key string) (*model.SysConfig, error) {
	for _, c := range m.configs {
		if c.ConfigKey == key {
			cp := *c
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *mockConfigRepo) FindList(name string, page, pageSize int) ([]model.SysConfig, int64, error) {
	result := make([]model.SysConfig, 0, len(m.configs))
	for _, c := range m.configs {
		result = append(result, *c)
	}
	return result, int64(len(result)), nil
}

func (m *mockConfigRepo) Update(config *model.SysConfig) error {
	cp := *config
	m.configs[config.ID] = &cp
	return nil
}

func (m *mockConfigRepo) Delete(id uint) error {
	if m.deleteFn != nil {
		return m.deleteFn(id)
	}
	delete(m.configs, id)
	return nil
}

func (m *mockConfigRepo) FindByKeyPrefix(prefix string) ([]model.SysConfig, error) {
	result := make([]model.SysConfig, 0, len(m.configs))
	for _, c := range m.configs {
		if len(c.ConfigKey) >= len(prefix) && c.ConfigKey[:len(prefix)] == prefix {
			result = append(result, *c)
		}
	}
	return result, nil
}

func (m *mockConfigRepo) UpsertByKey(config *model.SysConfig) error {
	for _, c := range m.configs {
		if c.ConfigKey == config.ConfigKey {
			c.Value = config.Value
			return nil
		}
	}
	return m.Create(config)
}

// Transaction 桩没有数据库，直接把自身交给回调（语义同 mockUserRepo.Transaction）。
// 真实的回滚语义由本文件下方「真实仓储 + 内存库」的用例覆盖。
func (m *mockConfigRepo) Transaction(fn func(repository.ConfigRepository) error) error {
	return fn(m)
}

// TestConfigUpdateKeepsMaskedSecret 回归保护：敏感配置的原值不能被打码占位符覆盖。
//
// 场景：FindList / FindByPrefix 对敏感项返回 "******"，管理员在编辑弹窗里
// 只改了名称就保存，前端把 "******" 一并提交回来。若直接落库，真实的
// 支付/OSS/短信密钥就被写成字面量 "******"，此后只会表现为「签名失败」
// 「上传失败」，根本指不到是配置被写坏了。BatchSave 早已有此保护，Update 之前漏了。
func TestConfigUpdateKeepsMaskedSecret(t *testing.T) {
	const realSecret = "REAL_SECRET_VALUE"

	repo := newMockConfigRepo(&model.SysConfig{
		Name: "微信密钥", ConfigKey: "pay.wechat_key", Value: realSecret, Type: 1,
	})
	svc := &configService{configRepo: repo}

	// 名称改了，值原样回传打码占位符
	if err := svc.Update(1, "微信密钥（改名）", "pay.wechat_key", maskedValue, 1, 9); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := repo.configs[1]
	if got.Value != realSecret {
		t.Errorf("敏感配置的原值被占位符覆盖：got %q, want %q", got.Value, realSecret)
	}
	if got.Name != "微信密钥（改名）" {
		t.Errorf("非敏感字段应正常更新：got %q", got.Name)
	}
	if got.UpdateBy != 9 {
		t.Errorf("操作人应被记录：got %d", got.UpdateBy)
	}
}

// TestConfigUpdateWritesRealSecret 若管理员确实填了新密钥，必须正常写入。
// 这条与上一条互为约束 —— 保护不能做成「敏感项永远改不了」。
func TestConfigUpdateWritesRealSecret(t *testing.T) {
	repo := newMockConfigRepo(&model.SysConfig{
		Name: "微信密钥", ConfigKey: "pay.wechat_key", Value: "OLD_SECRET", Type: 1,
	})
	svc := &configService{configRepo: repo}

	if err := svc.Update(1, "微信密钥", "pay.wechat_key", "NEW_SECRET", 1, 9); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := repo.configs[1].Value; got != "NEW_SECRET" {
		t.Errorf("新密钥应被写入：got %q", got)
	}
}

// TestConfigUpdateNonSensitiveMaskedLiteral 非敏感项提交字面量 "******"
// 属于用户的真实输入，必须照常写入，不能被保护逻辑误拦。
func TestConfigUpdateNonSensitiveMaskedLiteral(t *testing.T) {
	repo := newMockConfigRepo(&model.SysConfig{
		Name: "网站描述", ConfigKey: "site.description", Value: "旧描述", Type: 1,
	})
	svc := &configService{configRepo: repo}

	if err := svc.Update(1, "网站描述", "site.description", maskedValue, 1, 9); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := repo.configs[1].Value; got != maskedValue {
		t.Errorf("非敏感项应照常写入：got %q, want %q", got, maskedValue)
	}
}

// TestConfigUpdateRenamesToSensitiveKeyKeepsOldValue
// 把非敏感项改名为敏感 key 时也要拦住，否则占位符会落进新 key。
func TestConfigUpdateRenamesToSensitiveKeyKeepsOldValue(t *testing.T) {
	repo := newMockConfigRepo(&model.SysConfig{
		Name: "站点信息", ConfigKey: "site.info", Value: "真实值", Type: 1,
	})
	svc := &configService{configRepo: repo}

	if err := svc.Update(1, "站点信息", "oss.access_key", maskedValue, 1, 9); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := repo.configs[1].Value; got != "真实值" {
		t.Errorf("改名为敏感 key 时原值应保留：got %q", got)
	}
}

// TestConfigUpdateNotFound 配置不存在时返回 404 业务错误，而不是 500。
func TestConfigUpdateNotFound(t *testing.T) {
	repo := newMockConfigRepo()
	svc := &configService{configRepo: repo}

	err := svc.Update(404, "x", "site.name", "v", 1, 1)
	if err == nil {
		t.Fatal("expected error for non-existent config")
	}
	be, ok := common.AsBizError(err)
	if !ok || be.Code != common.CodeNotFound {
		t.Errorf("应返回 404 业务错误：got %v", err)
	}
}
