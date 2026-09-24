package repository

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/member/model"
	"go-admin/internal/testsupport"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。

const (
	memberTenantA uint = 1
	memberTenantB uint = 2
)

func newMemberRepoWithDB(t *testing.T) MemberRepository {
	t.Helper()
	// 先建库（会注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.Member{}, &model.MemberTagRel{}, &model.MemberTag{})
	return NewMemberRepository()
}

// seedMemberForTest 建一个会员。phone/memberNo 的唯一索引是全局的，两租户用不同值。
func seedMemberForTest(t *testing.T, tenantID uint, phone, memberNo, nickname string) *model.Member {
	t.Helper()
	m := &model.Member{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		MemberNo:        memberNo,
		Phone:           phone,
		Nickname:        nickname,
		Status:          1,
	}
	if err := NewMemberRepository().Create(m); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}
	return m
}

// TestMemberRepositoryTenantIsolation 会员数据的租户隔离。
//
// 会员是敏感个人信息（手机号/生日/消费积分），泄露即事故。
// 典型漏点是「漏传 tenantID → TenantScope 不过滤 → 全表查询」。
func TestMemberRepositoryTenantIsolation(t *testing.T) {
	repo := newMemberRepoWithDB(t)
	seedMemberForTest(t, memberTenantA, "13800000011", "000001", "甲租户会员")
	seedMemberForTest(t, memberTenantB, "13800000022", "000002", "乙租户会员")

	t.Run("列表按租户过滤", func(t *testing.T) {
		members, total, err := repo.FindList(memberTenantA, "", "", 0, -1, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 || len(members) != 1 {
			t.Fatalf("租户A 应只看到 1 条，实际 total=%d len=%d", total, len(members))
		}
		if members[0].Phone != "13800000011" {
			t.Errorf("看到了其他租户的会员: %s", members[0].Phone)
		}
	})

	t.Run("跨租户按 ID 查不到", func(t *testing.T) {
		list, _, err := repo.FindList(memberTenantB, "", "", 0, -1, 1, 100)
		if err != nil || len(list) != 1 {
			t.Fatalf("准备数据异常: %v", err)
		}
		if _, err := repo.FindByID(memberTenantA, list[0].ID); err == nil {
			t.Error("跨租户按 ID 应查不到")
		}
	})

	t.Run("按手机号查询受租户过滤", func(t *testing.T) {
		// 手机号唯一索引是全局的，但业务上查询入口都在租户上下文里
		if _, err := repo.FindByPhone(memberTenantA, "13800000022"); err == nil {
			t.Error("跨租户按手机号应查不到")
		}
		if _, err := repo.FindByPhone(memberTenantA, "13800000011"); err != nil {
			t.Errorf("本租户按手机号应查到: %v", err)
		}
	})

	t.Run("跨租户 UpdateStatus 落不到数据", func(t *testing.T) {
		others, _, _ := repo.FindList(memberTenantB, "", "", 0, -1, 1, 100)
		if err := repo.UpdateStatus(memberTenantA, others[0].ID, 0); err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		// GORM 的 Update 不返回影响行数，回读确认对方数据未被动过
		got, err := repo.FindByID(memberTenantB, others[0].ID)
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Status == 0 {
			t.Error("跨租户 UpdateStatus 居然生效了")
		}
	})

	t.Run("跨租户 UpdatePoints 归零不生效", func(t *testing.T) {
		others, _, _ := repo.FindList(memberTenantB, "", "", 0, -1, 1, 100)
		if err := repo.UpdatePoints(memberTenantA, others[0].ID, 99999); err != nil {
			t.Fatalf("更新失败: %v", err)
		}
		got, _ := repo.FindByID(memberTenantB, others[0].ID)
		if got.Points == 99999 {
			t.Error("跨租户积分修改竟然生效了（可被用来刷分）")
		}
	})

	t.Run("跨租户 Delete 删不掉", func(t *testing.T) {
		others, _, _ := repo.FindList(memberTenantB, "", "", 0, -1, 1, 100)
		if err := repo.Delete(memberTenantA, others[0].ID); err == nil {
			t.Error("跨租户删除应失败")
		}
		if _, err := repo.FindByID(memberTenantB, others[0].ID); err != nil {
			t.Error("跨租户删除失败时不应影响数据")
		}
	})

	t.Run("FindMaxMemberNo 只看本租户", func(t *testing.T) {
		// 发号器按库内最大值推导起始号，跨租户串号会让编号失序，
		// 更糟的是对不上号段后可能发出重复编号（撞 uk_member_no）
		got, err := repo.FindMaxMemberNo(memberTenantA)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got != "000001" {
			t.Errorf("应只看本租户的编号，实际 %q", got)
		}
	})
}

// TestMemberRepositoryFindTagIDsScoped 标签关联查询的租户约束。
//
// 回归背景：该方法签名带 tenantID 却一度没用它，靠调用方先查会员兜底。
// 补上子查询过滤后，直接传别的租户的 memberID 必须读到空列表。
func TestMemberRepositoryFindTagIDsScoped(t *testing.T) {
	repo := newMemberRepoWithDB(t)
	seedMemberForTest(t, memberTenantA, "13800000033", "000003", "本人")
	foreign := seedMemberForTest(t, memberTenantB, "13800000044", "000004", "别人")

	if err := repo.ReplaceTags(memberTenantB, foreign.ID, []uint{7}); err != nil {
		t.Fatalf("准备标签失败: %v", err)
	}

	got, err := repo.FindTagIDsByMemberID(memberTenantB, foreign.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("本租户应读到 1 个标签，实际 %v", got)
	}

	// 拿着别人的 memberID + 自己的 tenantID：必须读空
	leaked, err := repo.FindTagIDsByMemberID(memberTenantA, foreign.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(leaked) != 0 {
		t.Errorf("跨租户读到了标签关联 %v（= 隐私泄漏）", leaked)
	}
}

// TestMemberRepositorySoftDeleteReleasesUnique 软删除释放 phone / member_no 唯一索引。
//
// uk_phone、uk_member_no 都是全局唯一索引，不改写就再也建不出同号会员，
// 而且失败发生在 INSERT 阶段，前端只看得到一句数据库错误。
func TestMemberRepositorySoftDeleteReleasesUnique(t *testing.T) {
	repo := newMemberRepoWithDB(t)
	m := seedMemberForTest(t, memberTenantA, "13800000055", "000005", "要删的")

	// 先挂一个标签：删除应顺带清掉它，验证「不留孤儿关联」
	if err := repo.ReplaceTags(memberTenantA, m.ID, []uint{1}); err != nil {
		t.Fatalf("准备标签失败: %v", err)
	}
	got, _ := repo.FindTagIDsByMemberID(memberTenantA, m.ID)
	if len(got) != 1 {
		t.Fatalf("准备数据异常: %v", got)
	}

	if err := repo.Delete(memberTenantA, m.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := repo.FindByID(memberTenantA, m.ID); err == nil {
		t.Error("已删除的会员不应还能查到")
	}
	again := &model.Member{
		TenantBaseModel: common.TenantBaseModel{TenantID: memberTenantA},
		MemberNo:        "000005",
		Phone:           "13800000055",
		Nickname:        "重建",
		Status:          1,
	}
	if err := repo.Create(again); err != nil {
		t.Fatalf("同手机号会员无法重建（软删除未释放唯一值）: %v", err)
	}

	// 会员已被软删除 → 子查询不返回它 → 关联读出来必然是空
	rels, err := repo.FindTagIDsByMemberID(memberTenantA, m.ID)
	if err != nil {
		t.Fatalf("查询关联失败: %v", err)
	}
	if len(rels) != 0 {
		t.Errorf("删除后应清掉标签关联，实际残留 %v（孤儿记录）", rels)
	}
}

// TestMemberRepositoryUpdate 会员整行保存。
//
// 注意 Update 的实现是 `Save(member)`，**不带租户条件** —— 归属校验由
// Service 层承担（memberService.Update 先 FindByID(tenantID, req.ID)，
// 查不到直接 404）。这里把这个分工记下来：若将来有人在 Controller 里
// 直接调 Repository.Update，就等于开了一条「拿别家会员 ID 改写整行」的旁路，
// 必须先在 Service 里补归属校验。
func TestMemberRepositoryUpdate(t *testing.T) {
	repo := newMemberRepoWithDB(t)
	m := seedMemberForTest(t, memberTenantA, "13800000066", "000006", "原名")

	m.Nickname = "改名了"
	m.Points = 500
	m.Gender = 2
	if err := repo.Update(m); err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	got, err := repo.FindByID(memberTenantA, m.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.Nickname != "改名了" || got.Points != 500 || got.Gender != 2 {
		t.Errorf("更新未完整落库: %+v", got)
	}
	// 手机号/会员号没动过，不能被 Save 清空（Save 是整行覆盖，字段值取错就会丢）
	if got.Phone != "13800000066" || got.MemberNo != "000006" {
		t.Errorf("未修改的唯一字段被覆盖了: phone=%q memberNo=%q", got.Phone, got.MemberNo)
	}
}

// TestMemberRepositoryFindByWechatOpenid 按微信 openid 查会员（小程序登录入口）。
//
// 这条查询是小程序侧「静默登录」的入口：命中就复用会员、未命中就注册新会员。
// 若租户过滤失效，A 租户的小程序用户会被认成 B 租户的会员 ——
// 直接登录进别人的账号，属于最严重的一类问题。
func TestMemberRepositoryFindByWechatOpenid(t *testing.T) {
	repo := newMemberRepoWithDB(t)

	mine := seedMemberForTest(t, memberTenantA, "13800000077", "000007", "甲租户")
	mine.WechatOpenid = "openid-of-tenant-a"
	if err := repo.Update(mine); err != nil {
		t.Fatalf("写入 openid 失败: %v", err)
	}

	theirs := seedMemberForTest(t, memberTenantB, "13800000088", "000008", "乙租户")
	theirs.WechatOpenid = "openid-of-tenant-b"
	if err := repo.Update(theirs); err != nil {
		t.Fatalf("写入 openid 失败: %v", err)
	}

	t.Run("本租户能查到", func(t *testing.T) {
		got, err := repo.FindByWechatOpenid(memberTenantA, "openid-of-tenant-a")
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.ID != mine.ID {
			t.Errorf("查到了错误的会员: %+v", got)
		}
	})

	t.Run("跨租户查不到", func(t *testing.T) {
		if _, err := repo.FindByWechatOpenid(memberTenantA, "openid-of-tenant-b"); err == nil {
			t.Error("跨租户按 openid 查到了会员 —— 小程序登录会串号")
		}
	})

	t.Run("不存在的 openid 查不到", func(t *testing.T) {
		if _, err := repo.FindByWechatOpenid(memberTenantA, "openid-not-exists"); err == nil {
			t.Error("不存在的 openid 不应查到记录")
		}
		if _, err := repo.FindByWechatOpenid(memberTenantA, ""); err == nil {
			t.Error("空 openid 不应查到记录（否则未登录用户会被认成第一个空 openid 会员）")
		}
	})
}

// TestMemberRepositoryFindListFilters 会员列表的多条件过滤与「不传即不过滤」语义。
//
// 这里最容易出错的是 status：它是 int8，约定用 **-1 表示不过滤**（而不是 0），
// 因为 0 本身是「停用」这个有效取值。若写成 `status != 0` 才过滤，
// 「筛选停用会员」会退化成「返回全部」—— 界面看着有结果，其实筛选没生效。
func TestMemberRepositoryFindListFilters(t *testing.T) {
	repo := newMemberRepoWithDB(t)

	normal := seedMemberForTest(t, memberTenantA, "13900000001", "100001", "张三")
	normal.LevelID, normal.Status = 1, 1
	if err := repo.Update(normal); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}

	disabled := seedMemberForTest(t, memberTenantA, "13900000002", "100002", "李四")
	disabled.LevelID, disabled.Status = 2, 0
	if err := repo.Update(disabled); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}

	other := seedMemberForTest(t, memberTenantB, "13900000003", "100003", "王五")
	other.LevelID, other.Status = 1, 1
	if err := repo.Update(other); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}

	// 逐条件跑一遍：每个 case 都断言「只命中预期的那一条」
	cases := []struct {
		name     string
		phone    string
		nickname string
		levelID  uint
		status   int8
		wantID   uint
	}{
		{"手机号模糊匹配", "13900000002", "", 0, -1, disabled.ID},
		{"手机号前缀匹配", "139000000", "", 0, -1, 0}, // 0 = 命中多条，只校验条数
		{"昵称模糊匹配", "", "李", 0, -1, disabled.ID},
		{"按等级过滤", "", "", 1, -1, normal.ID},
		{"筛选停用（status=0 必须真的过滤）", "", "", 0, 0, disabled.ID},
		{"筛选正常（status=1）", "", "", 0, 1, normal.ID},
		{"status=-1 表示不过滤", "", "", 0, -1, 0},
		{"等级+状态组合", "", "", 2, 0, disabled.ID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list, total, err := repo.FindList(memberTenantA, tc.phone, tc.nickname, tc.levelID, tc.status, 1, 100)
			if err != nil {
				t.Fatalf("查询失败: %v", err)
			}
			// 所有 case 都必须只看到本租户的数据（租户B 的王五不该出现）
			for _, m := range list {
				if m.Nickname == "王五" {
					t.Fatalf("过滤条件越租户了: %+v", m)
				}
			}

			if tc.wantID == 0 {
				// 期望命中多条（或不过滤）：只校验条数与 total 一致
				if int(total) != len(list) {
					t.Errorf("total(%d) 与返回条数(%d) 不一致", total, len(list))
				}
				if len(list) == 0 {
					t.Error("应命中至少一条，实际为空")
				}
				return
			}

			if total != 1 || len(list) != 1 {
				t.Fatalf("应只命中 1 条，实际 total=%d list=%+v", total, list)
			}
			if list[0].ID != tc.wantID {
				t.Errorf("命中了错误的记录: %+v", list[0])
			}
		})
	}
}

// TestMemberRepositoryReplaceTagsClear 传空标签列表表示「清空标签」。
//
// 语义与「传 nil 表示不修改」相邻且容易混淆：清空是有效业务操作
// （把会员从所有标签里摘掉），必须真的删掉关联，而不是提前 return 什么都不做。
func TestMemberRepositoryReplaceTagsClear(t *testing.T) {
	repo := newMemberRepoWithDB(t)
	m := seedMemberForTest(t, memberTenantA, "13900000009", "100009", "要摘标签的")

	if err := repo.ReplaceTags(memberTenantA, m.ID, []uint{1, 2}); err != nil {
		t.Fatalf("准备标签失败: %v", err)
	}
	if got, _ := repo.FindTagIDsByMemberID(memberTenantA, m.ID); len(got) != 2 {
		t.Fatalf("准备数据异常: %v", got)
	}

	if err := repo.ReplaceTags(memberTenantA, m.ID, nil); err != nil {
		t.Fatalf("清空标签失败: %v", err)
	}
	if got, _ := repo.FindTagIDsByMemberID(memberTenantA, m.ID); len(got) != 0 {
		t.Errorf("清空后不应残留标签关联: %v", got)
	}

	// 再绑一次，确认清空后还能正常写入（不是把会员本身搞坏了）
	if err := repo.ReplaceTags(memberTenantA, m.ID, []uint{3}); err != nil {
		t.Fatalf("重新绑定标签失败: %v", err)
	}
	if got, _ := repo.FindTagIDsByMemberID(memberTenantA, m.ID); len(got) != 1 || got[0] != 3 {
		t.Errorf("重新绑定后应为 [3]，实际 %v", got)
	}
}

// TestMemberRepositoryReplaceTagsRejectsForeignMember 拿别家会员 ID 写标签必须失败。
//
// pay_member_tag_rel 没有 tenant_id 列，租户隔离只能靠「先校验会员归属」。
// 少了这一步，攻击者就能用别家的 memberID 改写对方的标签关联
// （例如把「黑名单」标签摘掉）。
func TestMemberRepositoryReplaceTagsRejectsForeignMember(t *testing.T) {
	repo := newMemberRepoWithDB(t)
	foreign := seedMemberForTest(t, memberTenantB, "13900000010", "100010", "别人的会员")

	if err := repo.ReplaceTags(memberTenantA, foreign.ID, []uint{1}); err == nil {
		t.Error("用别家会员 ID 写标签应失败")
	}
	if got, _ := repo.FindTagIDsByMemberID(memberTenantB, foreign.ID); len(got) != 0 {
		t.Errorf("跨租户写标签竟然生效了: %v", got)
	}
}
