package repository

import (
	"context"
	"testing"
	"time"

	"go-admin/internal/database"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。
//
// sys_operation_log / sys_login_log 是租户内表，且**没有** DeletedAt —— 日志是
// 硬删除。因此这里额外关注两类风险：
//  1. 清理时漏了租户条件 → 删掉全平台日志（且不可恢复）
//  2. 按时间清理时算错边界 → 要么删不干净（表无限增长），要么把保留期内的也删了

const (
	logTenantA uint = 1
	logTenantB uint = 2
)

func newLogRepoWithDB(t *testing.T) LogRepository {
	t.Helper()
	// 先建库（注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.SysOperationLog{}, &model.SysLoginLog{})
	return NewLogRepository()
}

func seedOpLog(t *testing.T, repo LogRepository, tenantID uint, title, action string, status int8) *model.SysOperationLog {
	t.Helper()
	l := &model.SysOperationLog{
		TenantID: tenantID,
		Title:    title,
		Action:   action,
		Status:   status,
	}
	if err := repo.CreateOperationLog(l); err != nil {
		t.Fatalf("创建操作日志失败: %v", err)
	}
	return l
}

func seedLoginLog(t *testing.T, repo LogRepository, tenantID uint, username string, status int8, loginTime time.Time) *model.SysLoginLog {
	t.Helper()
	l := &model.SysLoginLog{
		TenantID:  tenantID,
		Username:  username,
		Status:    status,
		LoginTime: loginTime,
	}
	if err := repo.CreateLoginLog(l); err != nil {
		t.Fatalf("创建登录日志失败: %v", err)
	}
	return l
}

// TestLogRepositoryCreate 日志写入不应被额外条件干扰
func TestLogRepositoryCreate(t *testing.T) {
	repo := newLogRepoWithDB(t)

	op := seedOpLog(t, repo, logTenantA, "用户管理", "创建", 1)
	if op.ID == 0 {
		t.Error("操作日志应回填自增 ID")
	}

	lg := seedLoginLog(t, repo, logTenantA, "admin", 1, time.Now())
	if lg.ID == 0 {
		t.Error("登录日志应回填自增 ID")
	}
}

// TestLogRepositoryFindOperationLogList 操作日志列表的过滤与排序
func TestLogRepositoryFindOperationLogList(t *testing.T) {
	repo := newLogRepoWithDB(t)

	seedOpLog(t, repo, logTenantA, "用户管理", "创建", 1)
	seedOpLog(t, repo, logTenantA, "角色管理", "删除", 0)
	seedOpLog(t, repo, logTenantB, "别家的用户管理", "创建", 1)

	t.Run("按租户过滤", func(t *testing.T) {
		list, total, err := repo.FindOperationLogList(logTenantA, "", nil, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 || len(list) != 2 {
			t.Fatalf("租户A 应看到 2 条，实际 total=%d list=%+v", total, list)
		}
		for _, l := range list {
			if l.TenantID != logTenantA {
				t.Errorf("看到了其他租户的日志: %+v", l)
			}
		}
	})

	t.Run("按模块标题过滤", func(t *testing.T) {
		// 标题在写入时已被翻译成中文，这里按中文模糊查
		list, total, err := repo.FindOperationLogList(logTenantA, "用户", nil, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].Title != "用户管理" {
			t.Errorf("标题过滤未生效: total=%d list=%+v", total, list)
		}
	})

	t.Run("status 为 nil 时不过滤", func(t *testing.T) {
		_, total, err := repo.FindOperationLogList(logTenantA, "", nil, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 {
			t.Errorf("nil 应表示不过滤，实际 %d", total)
		}
	})

	t.Run("status 指针过滤（0 与 1 都要生效）", func(t *testing.T) {
		// 关键点：0 是有效取值（失败），不能用「零值即不过滤」的写法，
		// 因此签名用的是 *int8。若实现里写成 `if status != nil && *status != 0`，
		// 「只看失败日志」会退化成「返回全部」。
		fail := int8(0)
		list, total, err := repo.FindOperationLogList(logTenantA, "", &fail, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].Action != "删除" {
			t.Fatalf("status=0 应只命中失败的 1 条，实际 total=%d list=%+v", total, list)
		}

		ok := int8(1)
		list, total, err = repo.FindOperationLogList(logTenantA, "", &ok, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].Action != "创建" {
			t.Errorf("status=1 应只命中成功的 1 条，实际 total=%d list=%+v", total, list)
		}
	})

	t.Run("按 id 倒序", func(t *testing.T) {
		list, _, err := repo.FindOperationLogList(logTenantA, "", nil, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		for i := 1; i < len(list); i++ {
			if list[i-1].ID <= list[i].ID {
				t.Fatalf("日志应按 id DESC（最新在前），实际 %+v", list)
			}
		}
	})

	t.Run("分页", func(t *testing.T) {
		first, total, err := repo.FindOperationLogList(logTenantA, "", nil, 1, 1)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 {
			t.Errorf("total 应为 2，实际 %d", total)
		}
		if len(first) != 1 {
			t.Fatalf("第一页应有 1 条，实际 %d", len(first))
		}

		second, _, err := repo.FindOperationLogList(logTenantA, "", nil, 2, 1)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(second) != 1 {
			t.Fatalf("第二页应有 1 条，实际 %d", len(second))
		}
		if first[0].ID == second[0].ID {
			t.Error("分页出现重复记录（Offset 算错）")
		}
	})
}

// TestLogRepositoryFindLoginLogList 登录日志列表的过滤
func TestLogRepositoryFindLoginLogList(t *testing.T) {
	repo := newLogRepoWithDB(t)
	now := time.Now()

	seedLoginLog(t, repo, logTenantA, "admin", 1, now)
	seedLoginLog(t, repo, logTenantA, "alice", 0, now)
	seedLoginLog(t, repo, logTenantB, "bob", 1, now)

	t.Run("按租户过滤", func(t *testing.T) {
		list, total, err := repo.FindLoginLogList(logTenantA, "", nil, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 || len(list) != 2 {
			t.Fatalf("租户A 应看到 2 条，实际 total=%d list=%+v", total, list)
		}
		for _, l := range list {
			if l.TenantID != logTenantA {
				t.Errorf("看到了其他租户的登录日志: %+v", l)
			}
		}
	})

	t.Run("按用户名过滤", func(t *testing.T) {
		list, total, err := repo.FindLoginLogList(logTenantA, "ali", nil, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].Username != "alice" {
			t.Errorf("用户名过滤未生效: total=%d list=%+v", total, list)
		}
	})

	t.Run("status 指针过滤", func(t *testing.T) {
		fail := int8(0)
		list, total, err := repo.FindLoginLogList(logTenantA, "", &fail, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(list) != 1 || list[0].Username != "alice" {
			t.Errorf("status=0 应只命中失败的 1 条，实际 total=%d list=%+v", total, list)
		}
	})
}

// TestLogRepositoryClearRejectsMissingTenant 缺少租户上下文时必须拒绝清空。
//
// 这是日志清理最关键的一条护栏：tenantID 为 0 时 common.TenantScope 会
// **不过滤**，于是 Delete 会作用于整张表 —— 一次「清空我的日志」变成
// 「清空全平台的日志」，而且日志是硬删除，没有任何回滚余地。
func TestLogRepositoryClearRejectsMissingTenant(t *testing.T) {
	repo := newLogRepoWithDB(t)
	seedOpLog(t, repo, logTenantA, "用户管理", "创建", 1)
	seedOpLog(t, repo, logTenantB, "角色管理", "创建", 1)
	seedLoginLog(t, repo, logTenantA, "admin", 1, time.Now())
	seedLoginLog(t, repo, logTenantB, "bob", 1, time.Now())

	t.Run("操作日志：tenantID=0 拒绝且不删任何数据", func(t *testing.T) {
		if err := repo.ClearOperationLogs(0); err == nil {
			t.Fatal("tenantID=0 必须报错")
		}

		var left int64
		if err := database.DB.Model(&model.SysOperationLog{}).Count(&left).Error; err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if left != 2 {
			t.Errorf("拒绝后不应删除任何数据，实际剩余 %d 条", left)
		}
	})

	t.Run("登录日志：tenantID=0 拒绝且不删任何数据", func(t *testing.T) {
		if err := repo.ClearLoginLogs(0); err == nil {
			t.Fatal("tenantID=0 必须报错")
		}

		var left int64
		if err := database.DB.Model(&model.SysLoginLog{}).Count(&left).Error; err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if left != 2 {
			t.Errorf("拒绝后不应删除任何数据，实际剩余 %d 条", left)
		}
	})
}

// TestLogRepositoryClearScopedToTenant 清空只影响本租户
func TestLogRepositoryClearScopedToTenant(t *testing.T) {
	repo := newLogRepoWithDB(t)
	seedOpLog(t, repo, logTenantA, "用户管理", "创建", 1)
	seedOpLog(t, repo, logTenantB, "角色管理", "创建", 1)
	seedLoginLog(t, repo, logTenantA, "admin", 1, time.Now())
	seedLoginLog(t, repo, logTenantB, "bob", 1, time.Now())

	if err := repo.ClearOperationLogs(logTenantA); err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if err := repo.ClearLoginLogs(logTenantA); err != nil {
		t.Fatalf("清空失败: %v", err)
	}

	// 租户A 清空
	if _, total, err := repo.FindOperationLogList(logTenantA, "", nil, 1, 100); err != nil || total != 0 {
		t.Errorf("租户A 的操作日志应已清空，实际 total=%d err=%v", total, err)
	}
	if _, total, err := repo.FindLoginLogList(logTenantA, "", nil, 1, 100); err != nil || total != 0 {
		t.Errorf("租户A 的登录日志应已清空，实际 total=%d err=%v", total, err)
	}

	// 租户B 不受影响
	if _, total, err := repo.FindOperationLogList(logTenantB, "", nil, 1, 100); err != nil || total != 1 {
		t.Errorf("租户B 的操作日志不应被清掉，实际 total=%d err=%v", total, err)
	}
	if _, total, err := repo.FindLoginLogList(logTenantB, "", nil, 1, 100); err != nil || total != 1 {
		t.Errorf("租户B 的登录日志不应被清掉，实际 total=%d err=%v", total, err)
	}
}

// TestLogRepositoryDeleteBeforeCutsAtBoundary 按时间清理的边界。
//
// 这里守的是「保留期」的语义：早于 cutoff 的删掉、晚于的必须留下。
// 边界写错（比如用 <= 或比较方向反了）会变成「删掉保留期内的日志」
// 或「一条都删不掉、表无限增长」，两者都不报错。
func TestLogRepositoryDeleteBeforeCutsAtBoundary(t *testing.T) {
	repo := newLogRepoWithDB(t)
	ctx := context.Background()
	now := time.Now()

	oldOp := seedOpLog(t, repo, logTenantA, "旧操作", "创建", 1)
	newOp := seedOpLog(t, repo, logTenantB, "新操作", "创建", 1)
	oldLogin := seedLoginLog(t, repo, logTenantA, "old", 1, now.Add(-100*time.Hour))
	newLogin := seedLoginLog(t, repo, logTenantB, "new", 1, now)

	// created_at 由 GORM 自动写入，测试里直接改成「很久以前」来构造历史数据
	cutoff := now.Add(-24 * time.Hour)
	if err := database.DB.Model(&model.SysOperationLog{}).Where("id = ?", oldOp.ID).
		Update("created_at", now.Add(-48*time.Hour)).Error; err != nil {
		t.Fatalf("构造历史操作日志失败: %v", err)
	}
	_ = newOp

	t.Run("操作日志：只删 cutoff 之前的，且跨租户生效", func(t *testing.T) {
		// 定时清理是平台级任务（不区分租户），因此 oldOp 属于租户A 也应被清掉
		n, err := repo.DeleteOperationLogsBefore(ctx, cutoff)
		if err != nil {
			t.Fatalf("清理失败: %v", err)
		}
		if n != 1 {
			t.Errorf("应删除 1 条（且返回真实影响行数），实际 %d", n)
		}

		var left int64
		if err := database.DB.Model(&model.SysOperationLog{}).Count(&left).Error; err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if left != 1 {
			t.Errorf("保留期内的日志必须留下，实际剩余 %d 条", left)
		}
	})

	t.Run("登录日志：按 login_time 而不是创建时间", func(t *testing.T) {
		n, err := repo.DeleteLoginLogsBefore(ctx, cutoff)
		if err != nil {
			t.Fatalf("清理失败: %v", err)
		}
		if n != 1 {
			t.Errorf("应删除 1 条，实际 %d", n)
		}

		var left int64
		if err := database.DB.Model(&model.SysLoginLog{}).Count(&left).Error; err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if left != 1 {
			t.Errorf("保留期内的登录日志必须留下，实际剩余 %d 条", left)
		}

		// 确认留下的是新登录（老的那条已被删）
		list, _, err := repo.FindLoginLogList(logTenantB, "", nil, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(list) != 1 || list[0].Username != "new" {
			t.Errorf("留下的应是新登录，实际 %+v", list)
		}
		_ = oldLogin
		_ = newLogin
	})
}

// TestLogRepositoryDeleteBeforeWithFutureCutoff 传未来时间点应清空全部匹配记录，
// 且返回的条数与实际删除一致（供定时任务打印「本次清理 N 条」）。
func TestLogRepositoryDeleteBeforeWithFutureCutoff(t *testing.T) {
	repo := newLogRepoWithDB(t)
	ctx := context.Background()

	seedOpLog(t, repo, logTenantA, "操作1", "创建", 1)
	seedOpLog(t, repo, logTenantB, "操作2", "创建", 1)
	seedLoginLog(t, repo, logTenantA, "admin", 1, time.Now().Add(-time.Hour))

	future := time.Now().Add(24 * time.Hour)

	n, err := repo.DeleteOperationLogsBefore(ctx, future)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if n != 2 {
		t.Errorf("应删除 2 条，实际 %d", n)
	}

	n, err = repo.DeleteLoginLogsBefore(ctx, future)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if n != 1 {
		t.Errorf("应删除 1 条，实际 %d", n)
	}

	// 再清一次应为 0（幂等，定时任务重复执行不会报错）
	if n, err := repo.DeleteOperationLogsBefore(ctx, future); err != nil || n != 0 {
		t.Errorf("重复清理应返回 0 且不报错，实际 n=%d err=%v", n, err)
	}
}
