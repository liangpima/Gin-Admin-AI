package service

import (
	"time"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

// mockUserRepo 是 UserRepository 的可控替身。
//
// 用函数字段而不是固定行为：测试只关心某几个方法时，
// 其余保持零值即可（返回零值/未找到），不必为每个用例写一整套桩。
type mockUserRepo struct {
	createFn            func(*model.SysUser) error
	findByIDFn          func(tenantID, id uint) (*model.SysUser, error)
	findListFn          func(tenantID uint, username, phone string, status *int8, deptID uint, page, pageSize int) ([]model.SysUser, int64, error)
	countByUsernameFn   func(username string, excludeID uint) (int64, error)
	replaceRolesFn      func(userID uint, roleIDs []uint) error
	replacePostsFn      func(userID uint, postIDs []uint) error
	resetPasswordFn     func(tenantID, id uint, password string) error
	findRoleIDsByUserFn func(userIDs []uint) (map[uint][]uint, error)

	// 记录调用情况，供断言
	createdUsers    []*model.SysUser
	updatedUsers    []*model.SysUser
	replacedRoles   []uint
	resetPwdCalls   int
	lastResetPwdVal string
}

func (m *mockUserRepo) Create(user *model.SysUser) error {
	if m.createFn != nil {
		return m.createFn(user)
	}
	m.createdUsers = append(m.createdUsers, user)
	return nil
}

func (m *mockUserRepo) FindByID(tenantID, id uint) (*model.SysUser, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(tenantID, id)
	}
	return nil, gorm.ErrRecordNotFound
}

func (m *mockUserRepo) FindByUsername(tenantID uint, username string) (*model.SysUser, error) {
	return nil, gorm.ErrRecordNotFound
}

func (m *mockUserRepo) FindByUsernameForAuth(username string) (*model.SysUser, error) {
	return nil, gorm.ErrRecordNotFound
}

func (m *mockUserRepo) FindList(tenantID uint, username, phone string, status *int8, deptID uint, page, pageSize int) ([]model.SysUser, int64, error) {
	if m.findListFn != nil {
		return m.findListFn(tenantID, username, phone, status, deptID, page, pageSize)
	}
	return nil, 0, nil
}

func (m *mockUserRepo) Update(user *model.SysUser) error {
	// 记录快照而非指针：Service 在 Update 前就地修改同一个对象，
	// 只存指针的话断言时看到的永远是最终状态，检测不出「字段被清成零值」
	snapshot := *user
	m.updatedUsers = append(m.updatedUsers, &snapshot)
	return nil
}

func (m *mockUserRepo) Delete(tenantID, id uint) error { return nil }

func (m *mockUserRepo) UpdateStatus(tenantID, id uint, status int8) error { return nil }

func (m *mockUserRepo) UpdateLoginTime(tenantID, id uint, t time.Time) error { return nil }

func (m *mockUserRepo) ResetPassword(tenantID, id uint, password string) error {
	m.resetPwdCalls++
	m.lastResetPwdVal = password
	if m.resetPasswordFn != nil {
		return m.resetPasswordFn(tenantID, id, password)
	}
	return nil
}

func (m *mockUserRepo) ReplaceRoles(userID uint, roleIDs []uint) error {
	m.replacedRoles = roleIDs
	if m.replaceRolesFn != nil {
		return m.replaceRolesFn(userID, roleIDs)
	}
	return nil
}

func (m *mockUserRepo) ReplacePosts(userID uint, postIDs []uint) error {
	if m.replacePostsFn != nil {
		return m.replacePostsFn(userID, postIDs)
	}
	return nil
}

func (m *mockUserRepo) FindRoleIDsByUserID(userID uint) ([]uint, error) { return nil, nil }

func (m *mockUserRepo) FindRoleIDsByUserIDs(userIDs []uint) (map[uint][]uint, error) {
	if m.findRoleIDsByUserFn != nil {
		return m.findRoleIDsByUserFn(userIDs)
	}
	return map[uint][]uint{}, nil
}

// CountByUsername 不做租户过滤（用户名是全局唯一索引），签名与实现一致。
func (m *mockUserRepo) CountByUsername(username string, excludeID uint) (int64, error) {
	if m.countByUsernameFn != nil {
		return m.countByUsernameFn(username, excludeID)
	}
	return 0, nil
}

func newTestUserService(repo *mockUserRepo) *userService {
	return &userService{userRepo: repo}
}

// errNotFound 用于模拟 GORM 的「记录不存在」，验证它被转成 404 业务错误
var errNotFound = gorm.ErrRecordNotFound

// assertBizError 断言是业务错误（400/404），而不是系统错误（500）。
// 错误语义是本项目的硬约定：业务问题回 400/404 且文案可见，
// 系统问题回 500 且不泄漏细节。
func assertBizError(t interface {
	Helper()
	Errorf(string, ...interface{})
}, err error, wantCode int) {
	t.Helper()
	be, ok := common.AsBizError(err)
	if !ok {
		t.Errorf("应为业务错误，实际: %T %v", err, err)
		return
	}
	if wantCode != 0 && be.Code != wantCode {
		t.Errorf("业务码应为 %d，实际 %d（%s）", wantCode, be.Code, be.Msg)
	}
}
