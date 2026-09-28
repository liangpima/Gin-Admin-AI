package service

import (
	"errors"
	"strings"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"

	"gorm.io/gorm"
)

// 本文件钉住 AGENTS 规则 5 的错误语义边界（P2-13 / M4·M5）。
//
// 两个方向都会出错，而且方向相反、都很隐蔽：
//
//	M4 过度转换：无条件 `NewNotFoundError` → 数据库故障被报成「XX 不存在」。
//	   用户按提示反复刷新，监控里一条 5xx 都没有，故障可以静默持续。
//	M5 转换缺失：把 `gorm.ErrRecordNotFound` 原样透出 → 用户传了个不存在的 ID，
//	   拿到的却是「服务器内部错误」，完全不知道是自己传错了。
//
// 所以每个被测方法都要**同时**断言两条路径：
// 不存在 → 404 业务错误；其他错误 → 不是业务错误（会被 FailWith 转成 500）。
//
// 只断言其中一条的话，把 NotFoundOrErr 换成无条件 NewNotFoundError
// （或反过来直接 return err）都能蒙混过关。

// errDBFailure 模拟数据库故障：既不是 ErrRecordNotFound，也不属于任何业务错误。
var errDBFailure = errors.New("dial tcp: connection refused")

// assertSystemError 断言 err 不是业务错误（即会走 500 分支），
// 且原始错误没有被吞掉 —— 否则排查时连日志里都看不到原因。
func assertSystemError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("应为系统错误，实际返回 nil")
	}
	if _, ok := common.AsBizError(err); ok {
		t.Errorf("数据库故障不应被包装成业务错误，实际: %v", err)
	}
}

// ---- M4：不得把系统错误一律说成「不存在」 ----

func TestUpdateRolesErrorSemantics(t *testing.T) {
	t.Run("用户不存在返回 404", func(t *testing.T) {
		repo := &mockUserRepo{findByIDFn: func(tenantID, id uint) (*model.SysUser, error) {
			return nil, gorm.ErrRecordNotFound
		}}
		svc := newTestUserService(repo)

		assertBizError(t, svc.UpdateRoles(1, 1, &dto.UpdateUserRolesRequest{ID: 9}), common.CodeNotFound)
	})

	t.Run("数据库故障不得被说成用户不存在", func(t *testing.T) {
		repo := &mockUserRepo{findByIDFn: func(tenantID, id uint) (*model.SysUser, error) {
			return nil, errDBFailure
		}}
		svc := newTestUserService(repo)

		assertSystemError(t, svc.UpdateRoles(1, 1, &dto.UpdateUserRolesRequest{ID: 9}))
	})
}

func TestUpdateDeptErrorSemantics(t *testing.T) {
	t.Run("用户不存在返回 404", func(t *testing.T) {
		repo := &mockUserRepo{findByIDFn: func(tenantID, id uint) (*model.SysUser, error) {
			return nil, gorm.ErrRecordNotFound
		}}
		svc := newTestUserService(repo)

		assertBizError(t, svc.UpdateDept(1, &dto.UpdateUserDeptRequest{ID: 9, DeptID: 1}), common.CodeNotFound)
	})

	t.Run("数据库故障不得被说成用户不存在", func(t *testing.T) {
		repo := &mockUserRepo{findByIDFn: func(tenantID, id uint) (*model.SysUser, error) {
			return nil, errDBFailure
		}}
		svc := newTestUserService(repo)

		assertSystemError(t, svc.UpdateDept(1, &dto.UpdateUserDeptRequest{ID: 9, DeptID: 1}))
	})
}

// ---- M5：gorm.ErrRecordNotFound 必须转成 404 ----

func TestDeptDeleteErrorSemantics(t *testing.T) {
	t.Run("记录不存在返回 404", func(t *testing.T) {
		repo := &mockDeptRepo{deleteFn: func(id uint) error { return gorm.ErrRecordNotFound }}
		svc := &deptService{deptRepo: repo}

		assertBizError(t, svc.Delete(1, 9), common.CodeNotFound)
	})

	t.Run("数据库故障仍走 500", func(t *testing.T) {
		repo := &mockDeptRepo{deleteFn: func(id uint) error { return errDBFailure }}
		svc := &deptService{deptRepo: repo}

		assertSystemError(t, svc.Delete(1, 9))
	})

	t.Run("有子部门时仍是业务错误", func(t *testing.T) {
		repo := &mockDeptRepo{countByParentFn: func(parentID uint) (int64, error) { return 2, nil }}
		svc := &deptService{deptRepo: repo}

		if err := svc.Delete(1, 9); err == nil {
			t.Error("存在子部门时应拒绝删除")
		} else if _, ok := common.AsBizError(err); !ok {
			t.Errorf("「存在子部门」是业务错误，实际: %v", err)
		}
	})
}

func TestConfigDeleteErrorSemantics(t *testing.T) {
	t.Run("记录不存在返回 404", func(t *testing.T) {
		repo := newMockConfigRepo()
		repo.deleteFn = func(id uint) error { return gorm.ErrRecordNotFound }
		svc := &configService{configRepo: repo}

		assertBizError(t, svc.Delete(9), common.CodeNotFound)
	})

	t.Run("数据库故障仍走 500", func(t *testing.T) {
		repo := newMockConfigRepo()
		repo.deleteFn = func(id uint) error { return errDBFailure }
		svc := &configService{configRepo: repo}

		assertSystemError(t, svc.Delete(9))
	})
}

// TestPasswordLengthLimitIsBusinessError 超长密码必须是 400，而不是 500。
//
// bcrypt 的输入上限是 72 **字节**，超过时 GenerateFromPassword 直接返回
// ErrPasswordTooLong（**不是**截断）。此前没有长度上限，于是「密码太长」
// 会以 500「服务器内部错误」结束 —— 用户只是密码长了一点，
// 却拿不到任何可操作的提示，只能反复重试。
//
// 必须按字节而不是字符判断：DTO 上的 `max=128` 是**字符**数，
// 一个汉字占 3 字节，128 个汉字是 384 字节，照样超限。
func TestPasswordLengthLimitIsBusinessError(t *testing.T) {
	t.Run("刚好 72 字节可用", func(t *testing.T) {
		pw := strings.Repeat("a", 71) + "1" // 72 字节，且含小写+数字两种
		if err := validatePasswordStrength(pw); err != nil {
			t.Errorf("72 字节密码应通过校验，实际: %v", err)
		}
	})

	t.Run("超过 72 字节是业务错误", func(t *testing.T) {
		assertBizError(t, validatePasswordStrength(strings.Repeat("a", 72)+"1"), common.CodeBadRequest)
	})

	t.Run("多字节字符按字节数计算", func(t *testing.T) {
		// 25 个汉字 = 75 字节 > 72，尽管只有 25 个「字符」
		pw := strings.Repeat("密", 25) + "a1"
		if len(pw) <= maxPasswordBytes {
			t.Fatalf("用例前提不成立: %d 字节", len(pw))
		}
		assertBizError(t, validatePasswordStrength(pw), common.CodeBadRequest)
	})

	t.Run("过短仍是原有提示", func(t *testing.T) {
		assertBizError(t, validatePasswordStrength("Ab1"), common.CodeBadRequest)
	})
}
