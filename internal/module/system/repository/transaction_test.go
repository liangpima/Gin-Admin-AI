package repository

import (
	"errors"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/system/model"
)

// 本文件验证仓储层的 Transaction 边界（P1-5 / M2·M3）。
//
// 为什么值得单独测：Service 层的一次「建用户」实际要写三张表
// （sys_user + sys_user_role + sys_user_post）。在此之前每个仓储方法各开各的事务，
// 只保证「自己那一笔」原子 —— 主表已落库、关联表失败时会留下半成品：
// 用户建好了、角色是空的。调用方拿到 500 以为整次操作失败，
// 实际那个账号已经可以登录，现象离根因很远。
//
// 这里刻意让回调**在写入之后**返回错误，再断言写入被回滚 ——
// 这是唯一能证明「两次写入真的在同一个事务里」的断言方式：
// 只断言「回调返回了错误」的话，把 Transaction 写成 `return fn(r)`
// （完全不开事务）也能通过。
//
// ⚠️ testsupport.NewDB 会改写包级 database.DB，因此不能 t.Parallel。

var errTxAbort = errors.New("后续步骤失败")

// countRows 统计某张表的行数，用于断言回滚是否真的发生。
func countRows(t *testing.T, dest any) int64 {
	t.Helper()
	var n int64
	if err := database.DB.Model(dest).Count(&n).Error; err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	return n
}

// assertBizErr 断言是业务错误（400/404）而非系统错误（500）。
// 事务包装不得改变错误的业务语义，否则上层无法把它转成可读提示。
func assertBizErr(t *testing.T, err error, wantCode int) {
	t.Helper()
	be, ok := common.AsBizError(err)
	if !ok {
		t.Errorf("应为业务错误，实际 %T %v", err, err)
		return
	}
	if wantCode != 0 && be.Code != wantCode {
		t.Errorf("业务码应为 %d，实际 %d（%s）", wantCode, be.Code, be.Msg)
	}
}

func TestUserRepositoryTransaction(t *testing.T) {
	t.Run("回调出错时主表与关联表一起回滚", func(t *testing.T) {
		repo := newUserRepoWithDB(t)

		err := repo.Transaction(func(txRepo UserRepository) error {
			if err := txRepo.Create(&model.SysUser{
				TenantBaseModel: common.TenantBaseModel{TenantID: testTenant},
				Username:        "atomic", Password: "x", Status: 1,
			}); err != nil {
				return err
			}
			if err := txRepo.ReplaceRoles(1, []uint{1}); err != nil {
				return err
			}
			// 模拟「关联表写完了，后面还有一步失败了」
			return errTxAbort
		})

		if !errors.Is(err, errTxAbort) {
			t.Fatalf("应原样返回回调的错误，实际 %v", err)
		}
		if got := countRows(t, &model.SysUser{}); got != 0 {
			t.Errorf("回滚后不应残留用户，实际 %d 条", got)
		}
		if got := countRows(t, &model.SysUserRole{}); got != 0 {
			t.Errorf("回滚后不应残留角色关联，实际 %d 条", got)
		}
	})

	t.Run("回调成功时全部提交", func(t *testing.T) {
		repo := newUserRepoWithDB(t)

		if err := repo.Transaction(func(txRepo UserRepository) error {
			if err := txRepo.Create(&model.SysUser{
				TenantBaseModel: common.TenantBaseModel{TenantID: testTenant},
				Username:        "committed", Password: "x", Status: 1,
			}); err != nil {
				return err
			}
			return txRepo.ReplaceRoles(1, []uint{7})
		}); err != nil {
			t.Fatalf("事务应提交成功: %v", err)
		}

		if got := countRows(t, &model.SysUser{}); got != 1 {
			t.Errorf("提交后应有 1 条用户，实际 %d 条", got)
		}
		if got := countRows(t, &model.SysUserRole{}); got != 1 {
			t.Errorf("提交后应有 1 条角色关联，实际 %d 条", got)
		}
	})

	t.Run("回调返回业务错误时不改写错误类型", func(t *testing.T) {
		// Service 依赖 errors.Is 把唯一索引冲突转成「用户名已存在」，
		// 因此事务包装不能吞掉或改写回调返回的错误。
		repo := newUserRepoWithDB(t)

		err := repo.Transaction(func(txRepo UserRepository) error {
			return common.NewBizError("用户名已存在")
		})
		assertBizErr(t, err, common.CodeBadRequest)
	})
}

func TestRoleRepositoryTransaction(t *testing.T) {
	repo := newRoleRepoWithDB(t)

	err := repo.Transaction(func(txRepo RoleRepository) error {
		role := &model.SysRole{
			TenantBaseModel: common.TenantBaseModel{TenantID: roleTenantA},
			Name:            "编辑", Code: "editor", Status: 1,
		}
		if err := txRepo.Create(role); err != nil {
			return err
		}
		// ReplaceMenus 会先按租户确认角色归属 —— 这同时验证了
		// 事务内的写入对事务内的读取可见（否则会误报「角色不存在」）
		if err := txRepo.ReplaceMenus(roleTenantA, role.ID, []uint{1, 2}); err != nil {
			return err
		}
		return errTxAbort
	})

	if !errors.Is(err, errTxAbort) {
		t.Fatalf("应原样返回回调的错误，实际 %v", err)
	}
	if got := countRows(t, &model.SysRole{}); got != 0 {
		t.Errorf("回滚后不应残留角色，实际 %d 条", got)
	}
	if got := countRows(t, &model.SysRoleMenu{}); got != 0 {
		t.Errorf("回滚后不应残留菜单关联，实际 %d 条", got)
	}
}

func TestConfigRepositoryTransaction(t *testing.T) {
	repo := newConfigRepoWithDB(t)

	err := repo.Transaction(func(txRepo ConfigRepository) error {
		if err := txRepo.UpsertByKey(&model.SysConfig{ConfigKey: "pay.app_id", Value: "A", Type: 1}); err != nil {
			return err
		}
		if err := txRepo.UpsertByKey(&model.SysConfig{ConfigKey: "pay.mch_id", Value: "B", Type: 1}); err != nil {
			return err
		}
		return errTxAbort
	})

	if !errors.Is(err, errTxAbort) {
		t.Fatalf("应原样返回回调的错误，实际 %v", err)
	}
	if got := countRows(t, &model.SysConfig{}); got != 0 {
		t.Errorf("整批应回滚，实际残留 %d 条配置", got)
	}
}
