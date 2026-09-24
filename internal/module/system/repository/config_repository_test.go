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
// sys_config 是**全局表**（无 tenant_id），因此这里不测租户隔离，
// 而是重点测唯一键、软删除释放、以及 Upsert 的语义 —— 这三处出错都是静默的。

func newConfigRepoWithDB(t *testing.T) ConfigRepository {
	t.Helper()
	// 先建库（注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.SysConfig{})
	return NewConfigRepository()
}

func seedConfig(t *testing.T, repo ConfigRepository, name, key, value string) *model.SysConfig {
	t.Helper()
	cfg := &model.SysConfig{Name: name, ConfigKey: key, Value: value, Type: 1}
	if err := repo.Create(cfg); err != nil {
		t.Fatalf("创建配置失败: %v", err)
	}
	return cfg
}

// TestConfigRepositoryCreateAndFind 基本读写
func TestConfigRepositoryCreateAndFind(t *testing.T) {
	repo := newConfigRepoWithDB(t)
	created := seedConfig(t, repo, "站点名称", "site_name", "Gin-Admin")

	t.Run("按 ID 读回", func(t *testing.T) {
		got, err := repo.FindByID(created.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.Name != "站点名称" || got.ConfigKey != "site_name" || got.Value != "Gin-Admin" {
			t.Errorf("读回的数据不一致: %+v", got)
		}
	})

	t.Run("按键名读回", func(t *testing.T) {
		got, err := repo.FindByKey("site_name")
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.ID != created.ID {
			t.Errorf("查到了错误的记录: %+v", got)
		}
	})

	t.Run("不存在的键名返回 ErrRecordNotFound", func(t *testing.T) {
		// Service 依赖这个语义决定「用默认值」还是「报错」，退回 nil 会让调用方 panic
		if _, err := repo.FindByKey("not_exists"); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("应返回 ErrRecordNotFound，实际 %v", err)
		}
	})
}

// TestConfigRepositoryCreateDuplicateKey 键名重复必须被识别成业务错误。
//
// uk_config_key 是全局唯一索引。识别不出来时，Service 无法给出
// 「参数键名已存在」的提示，用户只会看到一句 500。
func TestConfigRepositoryCreateDuplicateKey(t *testing.T) {
	repo := newConfigRepoWithDB(t)
	seedConfig(t, repo, "站点名称", "site_name", "Gin-Admin")

	err := repo.Create(&model.SysConfig{Name: "另一个", ConfigKey: "site_name", Value: "x"})
	if err == nil {
		t.Fatal("重复键名应报错")
	}
	if !errors.Is(err, common.ErrDuplicateKey) {
		t.Errorf("应包装成 common.ErrDuplicateKey（Service 据此返回 400 而非 500），实际 %v", err)
	}
}

// TestConfigRepositoryUpdate 更新只应改白名单字段
func TestConfigRepositoryUpdate(t *testing.T) {
	repo := newConfigRepoWithDB(t)
	cfg := seedConfig(t, repo, "站点名称", "site_name", "Gin-Admin")

	cfg.Name = "平台名称"
	cfg.Value = "新值"
	if err := repo.Update(cfg); err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	got, err := repo.FindByID(cfg.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.Name != "平台名称" || got.Value != "新值" {
		t.Errorf("更新未落库: %+v", got)
	}
}

// TestConfigRepositoryFindList 列表过滤与分页
func TestConfigRepositoryFindList(t *testing.T) {
	repo := newConfigRepoWithDB(t)
	seedConfig(t, repo, "站点名称", "site_name", "a")
	seedConfig(t, repo, "站点logo", "site_logo", "b")
	seedConfig(t, repo, "上传限制", "upload_limit", "c")

	t.Run("名称模糊匹配", func(t *testing.T) {
		list, total, err := repo.FindList("站点", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 || len(list) != 2 {
			t.Errorf("应命中 2 条，实际 total=%d list=%+v", total, list)
		}
	})

	t.Run("空名称不过滤", func(t *testing.T) {
		_, total, err := repo.FindList("", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 3 {
			t.Errorf("应返回全部 3 条，实际 %d", total)
		}
	})

	t.Run("分页不重不漏", func(t *testing.T) {
		first, total, err := repo.FindList("", 1, 2)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 3 {
			t.Errorf("total 应为 3，实际 %d", total)
		}
		if len(first) != 2 {
			t.Fatalf("第一页应有 2 条，实际 %d", len(first))
		}

		second, _, err := repo.FindList("", 2, 2)
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

// TestConfigRepositoryFindByKeyPrefix 前缀查询只应命中真正以该前缀开头的键。
//
// 调用场景是「按前缀批量取一组配置」（如全部 sms_* 配置）。
// 若退化成「包含」语义，会把 site_sms_flag 这类中间命中的项一起返回，
// 上层按 key 前缀分组时就会拿到不属于该组的配置。
//
// 关于通配符：这里刻意只用**不含** `%` `_` 的前缀。前缀里的 `_` 会被
// common.EscapeLike 转义成 `\_`，而 SQLite 不认反斜杠转义（`\%` 被当成
// 字面量 `\` + 通配），转义后反而**少匹配**。生产（MySQL）下是精确的
// 字面量匹配，两种引擎下「不会多匹配」这一点都成立 ——
// 与 like_escape_test.go 采用同一约定：只断言失败方向安全的那一侧。
func TestConfigRepositoryFindByKeyPrefix(t *testing.T) {
	repo := newConfigRepoWithDB(t)
	seedConfig(t, repo, "短信appid", "sms_appid", "1")
	seedConfig(t, repo, "短信密钥", "sms_secret", "2")
	seedConfig(t, repo, "站点名称", "site_name", "3")
	seedConfig(t, repo, "含sms但不在开头", "site_sms_flag", "4")

	t.Run("只返回以该前缀开头的项", func(t *testing.T) {
		got, err := repo.FindByKeyPrefix("sms")
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("应只返回以 sms 开头的 2 条，实际 %+v", got)
		}
		for _, c := range got {
			if c.ConfigKey != "sms_appid" && c.ConfigKey != "sms_secret" {
				t.Errorf("返回了非该前缀的配置: %q（前缀查询被写成了包含查询）", c.ConfigKey)
			}
		}
	})

	t.Run("通配符前缀不会命中任何记录", func(t *testing.T) {
		// `%` 未转义时模式会变成 `%%`，等价于「返回全部配置」
		for _, input := range []string{"%", "_", "%a"} {
			got, err := repo.FindByKeyPrefix(input)
			if err != nil {
				t.Fatalf("查询 %q 失败: %v", input, err)
			}
			if len(got) != 0 {
				t.Errorf("通配符前缀 %q 应被当作字面量、匹配不到任何配置，实际 %+v", input, got)
			}
		}
	})
}

// TestConfigRepositoryDeleteReleasesKey 软删除必须释放 config_key 唯一索引。
//
// 不释放的话，删除后重建同一个键名会撞 uk_config_key：
// 界面上「删掉再建」是最常见的操作路径，失败信息却只是一句数据库错误。
func TestConfigRepositoryDeleteReleasesKey(t *testing.T) {
	repo := newConfigRepoWithDB(t)
	cfg := seedConfig(t, repo, "站点名称", "site_name", "old")

	if err := repo.Delete(cfg.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := repo.FindByKey("site_name"); err == nil {
		t.Error("已删除的配置不应还能按原键名查到")
	}

	// 同键名重建必须成功
	again := &model.SysConfig{Name: "站点名称", ConfigKey: "site_name", Value: "new"}
	if err := repo.Create(again); err != nil {
		t.Fatalf("同键名无法重建（软删除未释放唯一值）: %v", err)
	}
}

// TestConfigRepositoryDeleteNotFound 删除不存在的记录应返回 ErrRecordNotFound。
//
// Service 据此转 404；若这里静默返回 nil，用户会看到「删除成功」但数据其实没变。
func TestConfigRepositoryDeleteNotFound(t *testing.T) {
	repo := newConfigRepoWithDB(t)

	if err := repo.Delete(999999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("应返回 ErrRecordNotFound，实际 %v", err)
	}
}

// TestConfigRepositoryUpsertByKey 键不存在则插入、存在则只改值与更新人。
//
// 这个语义是「批量保存配置」的基础：不存在要能建（首次部署）、
// 存在要能改（日常修改），且**不能**顺手把名称/内置标记也覆盖掉 ——
// 那些字段由初始化脚本定义，被前端表单的零值覆盖会破坏「系统内置」的语义。
func TestConfigRepositoryUpsertByKey(t *testing.T) {
	repo := newConfigRepoWithDB(t)

	t.Run("不存在时插入", func(t *testing.T) {
		cfg := &model.SysConfig{
			BaseModel: common.BaseModel{UpdateBy: 7},
			Name:      "新配置",
			ConfigKey: "brand_new",
			Value:     "v1",
			Type:      1,
		}
		if err := repo.UpsertByKey(cfg); err != nil {
			t.Fatalf("Upsert 失败: %v", err)
		}

		got, err := repo.FindByKey("brand_new")
		if err != nil {
			t.Fatalf("插入后应能查到: %v", err)
		}
		if got.Value != "v1" || got.Name != "新配置" {
			t.Errorf("插入的内容不对: %+v", got)
		}
	})

	t.Run("存在时只更新值与更新人", func(t *testing.T) {
		seedConfig(t, repo, "原始名称", "existing_key", "old")

		err := repo.UpsertByKey(&model.SysConfig{
			BaseModel: common.BaseModel{UpdateBy: 9},
			Name:      "试图改名",
			ConfigKey: "existing_key",
			Value:     "new",
			Type:      0,
		})
		if err != nil {
			t.Fatalf("Upsert 失败: %v", err)
		}

		got, err := repo.FindByKey("existing_key")
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Value != "new" {
			t.Errorf("值应被更新，实际 %q", got.Value)
		}
		if got.UpdateBy != 9 {
			t.Errorf("UpdateBy 应被更新，实际 %d", got.UpdateBy)
		}
		if got.Name != "原始名称" {
			t.Errorf("名称不应被 Upsert 覆盖（会破坏初始化脚本定义的元信息），实际 %q", got.Name)
		}

		// 不能产生第二条同键名记录
		list, total, err := repo.FindList("", 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 {
			t.Errorf("Upsert 不应新增记录，实际总数 %d (%+v)", total, list)
		}
	})
}
