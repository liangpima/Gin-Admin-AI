package repository

import (
	"errors"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"gorm.io/gorm"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。
//
// sys_dict_type / sys_dict_data 是**全局表**（无 tenant_id），因此不测租户隔离。
// 重点测三处静默故障：唯一键识别、软删除释放唯一值、以及「归属类型不可变」。

func newDictRepoWithDB(t *testing.T) DictRepository {
	t.Helper()
	// 先建库（注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.SysDictType{}, &model.SysDictData{})
	return NewDictRepository()
}

func seedDictType(t *testing.T, repo DictRepository, name, typ string, status int8) *model.SysDictType {
	t.Helper()
	dt := &model.SysDictType{Name: name, Type: typ, Status: status}
	if err := repo.CreateType(dt); err != nil {
		t.Fatalf("创建字典类型失败: %v", err)
	}
	return dt
}

func seedDictData(t *testing.T, repo DictRepository, typ, label, value string, sort int, status int8) *model.SysDictData {
	t.Helper()
	dd := &model.SysDictData{DictType: typ, Label: label, Value: value, Sort: sort, Status: status}
	if err := repo.CreateData(dd); err != nil {
		t.Fatalf("创建字典数据失败: %v", err)
	}
	return dd
}

// TestDictRepositoryTypeCRUD 字典类型的基本读写
func TestDictRepositoryTypeCRUD(t *testing.T) {
	repo := newDictRepoWithDB(t)
	created := seedDictType(t, repo, "用户状态", "sys_user_status", 1)

	t.Run("按 ID 读回", func(t *testing.T) {
		got, err := repo.FindTypeByID(created.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.Type != "sys_user_status" || got.Name != "用户状态" {
			t.Errorf("读回的数据不一致: %+v", got)
		}
	})

	t.Run("按类型编码读回", func(t *testing.T) {
		// 这是业务页面取选项的实际入口（useDict('sys_user_status')）
		got, err := repo.FindTypeByType("sys_user_status")
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.ID != created.ID {
			t.Errorf("查到了错误的记录: %+v", got)
		}
	})

	t.Run("UpdateType 生效", func(t *testing.T) {
		created.Name = "用户状态（改）"
		created.Status = 0
		if err := repo.UpdateType(created); err != nil {
			t.Fatalf("更新失败: %v", err)
		}

		got, err := repo.FindTypeByID(created.ID)
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Name != "用户状态（改）" || got.Status != 0 {
			t.Errorf("更新未落库: %+v", got)
		}
	})
}

// TestDictRepositoryCreateTypeDuplicate 类型编码重复必须被识别成业务错误
func TestDictRepositoryCreateTypeDuplicate(t *testing.T) {
	repo := newDictRepoWithDB(t)
	seedDictType(t, repo, "用户状态", "sys_user_status", 1)

	err := repo.CreateType(&model.SysDictType{Name: "另一个", Type: "sys_user_status"})
	if err == nil {
		t.Fatal("重复类型编码应报错")
	}
	if !errors.Is(err, common.ErrDuplicateKey) {
		t.Errorf("应包装成 common.ErrDuplicateKey，实际 %v", err)
	}
}

// TestDictRepositoryFindTypeList 类型列表的过滤与分页
func TestDictRepositoryFindTypeList(t *testing.T) {
	repo := newDictRepoWithDB(t)
	seedDictType(t, repo, "用户状态", "sys_user_status", 1)
	seedDictType(t, repo, "用户性别", "sys_user_gender", 1)
	seedDictType(t, repo, "会员等级", "pay_member_level", 1)

	list, total, err := repo.FindTypeList("用户", 1, 100)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Errorf("应命中 2 条，实际 total=%d list=%+v", total, list)
	}

	_, total, err = repo.FindTypeList("", 1, 2)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 3 {
		t.Errorf("total 应为过滤后的总数 3，实际 %d", total)
	}
}

// TestDictRepositoryDeleteTypeReleasesType 软删除类型必须释放 type 唯一索引。
//
// 不释放则「删掉再建同编码」必失败。而字典类型编码是**代码里的字面量**
// （前端 useDict('xxx')、后端按固定编码取选项），一旦重建不了，
// 整个功能就永久不可用 —— 只能去库里手改，属于最难恢复的一类问题。
func TestDictRepositoryDeleteTypeReleasesType(t *testing.T) {
	repo := newDictRepoWithDB(t)
	dt := seedDictType(t, repo, "用户状态", "sys_user_status", 1)

	if err := repo.DeleteType(dt.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := repo.FindTypeByType("sys_user_status"); err == nil {
		t.Error("已删除的类型不应还能按原编码查到")
	}

	again := &model.SysDictType{Name: "用户状态", Type: "sys_user_status", Status: 1}
	if err := repo.CreateType(again); err != nil {
		t.Fatalf("同编码无法重建（软删除未释放唯一值）: %v", err)
	}
}

// TestDictRepositoryDeleteTypeNotFound 删除不存在的类型应返回 ErrRecordNotFound
func TestDictRepositoryDeleteTypeNotFound(t *testing.T) {
	repo := newDictRepoWithDB(t)

	if err := repo.DeleteType(999999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("应返回 ErrRecordNotFound，实际 %v", err)
	}
}

// TestDictRepositoryDataCRUD 字典数据的基本读写
func TestDictRepositoryDataCRUD(t *testing.T) {
	repo := newDictRepoWithDB(t)
	created := seedDictData(t, repo, "sys_user_status", "正常", "1", 1, 1)

	t.Run("按 ID 读回", func(t *testing.T) {
		got, err := repo.FindDataByID(created.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.Label != "正常" || got.Value != "1" {
			t.Errorf("读回的数据不一致: %+v", got)
		}
	})

	t.Run("UpdateData 生效但不改归属类型", func(t *testing.T) {
		created.Label = "启用"
		created.Sort = 9
		// 刻意改归属类型：实现里的 Select 白名单不含 DictType，必须改不动
		created.DictType = "sys_user_gender"
		if err := repo.UpdateData(created); err != nil {
			t.Fatalf("更新失败: %v", err)
		}

		got, err := repo.FindDataByID(created.ID)
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Label != "启用" || got.Sort != 9 {
			t.Errorf("更新未落库: %+v", got)
		}
		if got.DictType != "sys_user_status" {
			t.Errorf("归属类型被改掉了（该选项会从原类型下消失）: %q", got.DictType)
		}
	})
}

// TestDictRepositoryFindDataByType 按类型取启用中的选项（业务页面的实际入口）。
//
// 只返回 status=1 是刻意的：停用的选项若也返回，前端下拉里就会出现
// 运营以为已经下线的值，选中后落库的是一个「无效状态」。
func TestDictRepositoryFindDataByType(t *testing.T) {
	repo := newDictRepoWithDB(t)
	seedDictData(t, repo, "sys_user_status", "正常", "1", 2, 1)
	seedDictData(t, repo, "sys_user_status", "停用", "0", 1, 1)
	seedDictData(t, repo, "sys_user_status", "已删除", "9", 3, 0)
	seedDictData(t, repo, "sys_user_gender", "男", "1", 1, 1)

	got, err := repo.FindDataByType("sys_user_status")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应只返回 2 条启用中的选项，实际 %+v", got)
	}
	// 排序：sort ASC, id ASC —— 停用(sort=1) 在 正常(sort=2) 之前
	if got[0].Label != "停用" || got[1].Label != "正常" {
		t.Errorf("应按 sort ASC 排序，实际 %+v", got)
	}
	for _, d := range got {
		if d.DictType != "sys_user_status" {
			t.Errorf("混入了其他类型的选项: %+v", d)
		}
	}
}

// TestDictRepositoryFindDataList 数据列表的过滤与分页
func TestDictRepositoryFindDataList(t *testing.T) {
	repo := newDictRepoWithDB(t)
	seedDictData(t, repo, "sys_user_status", "正常", "1", 1, 1)
	seedDictData(t, repo, "sys_user_status", "停用", "0", 2, 1)
	seedDictData(t, repo, "sys_user_gender", "男", "1", 1, 1)

	t.Run("按类型过滤", func(t *testing.T) {
		list, total, err := repo.FindDataList("sys_user_status", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 || len(list) != 2 {
			t.Errorf("应命中 2 条，实际 total=%d list=%+v", total, list)
		}
	})

	t.Run("空类型不过滤", func(t *testing.T) {
		_, total, err := repo.FindDataList("", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 3 {
			t.Errorf("应返回全部 3 条，实际 %d", total)
		}
	})

	t.Run("分页", func(t *testing.T) {
		first, total, err := repo.FindDataList("sys_user_status", 1, 1)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 {
			t.Errorf("total 应为 2，实际 %d", total)
		}
		if len(first) != 1 || first[0].Label != "正常" {
			t.Fatalf("第一页应为 sort 最小的「正常」，实际 %+v", first)
		}

		second, _, err := repo.FindDataList("sys_user_status", 2, 1)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(second) != 1 || second[0].Label != "停用" {
			t.Errorf("第二页应为「停用」，实际 %+v", second)
		}
	})
}

// TestDictRepositoryCountDataByValue 键值重复校验（含更新时排除自身）。
//
// excludeID 的语义极易写错：更新时若不排除自身，任何一次「不改键值只改标签」
// 的保存都会被判成重复，用户会发现改个标签名都保存不了。
func TestDictRepositoryCountDataByValue(t *testing.T) {
	repo := newDictRepoWithDB(t)
	target := seedDictData(t, repo, "sys_user_status", "正常", "1", 1, 1)

	t.Run("同类型同键值计为 1", func(t *testing.T) {
		n, err := repo.CountDataByValue("sys_user_status", "1", 0)
		if err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if n != 1 {
			t.Errorf("应统计到 1 条，实际 %d", n)
		}
	})

	t.Run("更新场景排除自身后为 0", func(t *testing.T) {
		n, err := repo.CountDataByValue("sys_user_status", "1", target.ID)
		if err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if n != 0 {
			t.Errorf("排除自身后应为 0（否则改标签名会被误判为重复），实际 %d", n)
		}
	})

	t.Run("其他类型下同键值不算重复", func(t *testing.T) {
		seedDictData(t, repo, "sys_user_gender", "男", "1", 1, 1)

		n, err := repo.CountDataByValue("sys_user_gender", "1", 0)
		if err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if n != 1 {
			t.Errorf("不同类型是各自独立的命名空间，应只统计到本类型的 1 条，实际 %d", n)
		}
	})
}

// TestDictRepositoryCountDataByType 删除类型前校验子级条数。
//
// 这个计数是「类型下还有数据就不许删」的依据：数错了要么删不掉（偏大），
// 要么把有数据的类型删掉、留下一批孤儿字典数据（偏小）。
func TestDictRepositoryCountDataByType(t *testing.T) {
	repo := newDictRepoWithDB(t)
	seedDictData(t, repo, "sys_user_status", "正常", "1", 1, 1)
	seedDictData(t, repo, "sys_user_status", "停用", "0", 2, 0)
	seedDictData(t, repo, "sys_user_gender", "男", "1", 1, 1)

	// 注意：统计不过滤 status —— 停用的子项同样要算进去，
	// 否则「删类型 → 连带删掉停用子项」的静默数据丢失就会发生
	n, err := repo.CountDataByType("sys_user_status")
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 2 {
		t.Errorf("应统计到 2 条（含停用），实际 %d", n)
	}

	if n, err := repo.CountDataByType("not_exists"); err != nil || n != 0 {
		t.Errorf("不存在的类型应返回 0 且不报错，实际 n=%d err=%v", n, err)
	}
}

// TestDictRepositoryCreateDataDuplicate 同类型下键值重复必须被识别成业务错误。
//
// 前置的 CountDataByValue 校验存在时间窗口（并发下两个请求都能通过校验），
// 因此 CreateData 里的唯一索引兜底是最后一道防线 —— 识别不出来就变成 500。
func TestDictRepositoryCreateDataDuplicate(t *testing.T) {
	repo := newDictRepoWithDB(t)
	seedDictData(t, repo, "sys_user_status", "正常", "1", 1, 1)

	err := repo.CreateData(&model.SysDictData{DictType: "sys_user_status", Label: "另一个正常", Value: "1"})
	if err == nil {
		t.Fatal("同类型下重复键值应报错")
	}
	if !errors.Is(err, common.ErrDuplicateKey) {
		t.Errorf("应包装成 common.ErrDuplicateKey，实际 %v", err)
	}

	// 不同类型下同键值应允许
	if err := repo.CreateData(&model.SysDictData{DictType: "sys_user_gender", Label: "男", Value: "1"}); err != nil {
		t.Errorf("不同类型下同键值不应冲突: %v", err)
	}
}

// TestDictRepositoryDeleteDataReleasesValue 软删除数据必须释放 value 唯一索引
func TestDictRepositoryDeleteDataReleasesValue(t *testing.T) {
	repo := newDictRepoWithDB(t)
	dd := seedDictData(t, repo, "sys_user_status", "正常", "1", 1, 1)

	if err := repo.DeleteData(dd.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	// 同类型同键值重建必须成功
	again := &model.SysDictData{DictType: "sys_user_status", Label: "正常", Value: "1", Sort: 1, Status: 1}
	if err := repo.CreateData(again); err != nil {
		t.Fatalf("同键值无法重建（软删除未释放唯一值）: %v", err)
	}

	// 计数只应看到重建后的那一条
	n, err := repo.CountDataByValue("sys_user_status", "1", 0)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 1 {
		t.Errorf("应只剩 1 条，实际 %d", n)
	}
}

// TestDictRepositoryDeleteDataNotFound 删除不存在的记录应返回 ErrRecordNotFound
func TestDictRepositoryDeleteDataNotFound(t *testing.T) {
	repo := newDictRepoWithDB(t)

	if err := repo.DeleteData(999999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("应返回 ErrRecordNotFound，实际 %v", err)
	}
}
