package service

import (
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"
	"go-admin/internal/module/member/repository"
)

// 本文件覆盖 member_level / member_tag / points_log 三个「薄封装」Service。
//
// 它们此前整体 0% 覆盖，成因与 system 模块相同：老用例一律 `&xxxService{repo: stub}`
// 直接构造，绕开了 NewXxxService 构造器。而这类薄封装的**全部价值**恰好就在
// 「把 tenantID / operatorID 原样透传给仓储」上 —— 用桩替换仓储后，
// 租户过滤、审计字段、软删除释放唯一键这些真正会出事的行为全被替换掉了，
// 报表上有覆盖率，实际上什么都没验证。所以这里一律走真实仓储 + 内存库。
//
// ⚠️ 与同包其它用例一样会改写包级 database.DB，因此不能 t.Parallel。

// seedMember 直接落一条会员，供标签关联类用例使用。
//
// member_no / phone 都是唯一索引，必须显式给值：留空的话同库第二条就撞唯一键。
func seedMember(t *testing.T, tenantID uint, phone, memberNo string) *model.Member {
	t.Helper()
	m := &model.Member{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		MemberNo:        memberNo,
		Phone:           phone,
		Nickname:        "会员" + memberNo,
		Status:          1,
	}
	if err := repository.NewMemberRepository().Create(m); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}
	return m
}

// ─────────────────────────── 会员等级 ───────────────────────────

// TestMemberLevelServiceCreateAndQuery 构造器 + 创建 + 查询 + 租户隔离。
func TestMemberLevelServiceCreateAndQuery(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberLevelService()

	if err := svc.Create(&dto.CreateMemberLevelRequest{
		Name: "黄金会员", MinPoints: 100, Discount: 9.5, Icon: "gold.png", Sort: 2, Status: 1,
	}, 7, tenantA); err != nil {
		t.Fatalf("创建等级失败: %v", err)
	}
	// 另一个租户的同名等级，用于确认查询确实按租户过滤
	if err := svc.Create(&dto.CreateMemberLevelRequest{
		Name: "别人租户的等级", MinPoints: 1, Discount: 10, Sort: 1, Status: 1,
	}, 7, tenantB); err != nil {
		t.Fatalf("创建等级失败: %v", err)
	}

	list, total, err := svc.FindList(tenantA, &dto.MemberLevelListRequest{})
	if err != nil {
		t.Fatalf("查询等级失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("本租户应只看到 1 条等级，实际 total=%d len=%d", total, len(list))
	}
	got := list[0]
	if got.Name != "黄金会员" || got.MinPoints != 100 || got.Discount != 9.5 ||
		got.Icon != "gold.png" || got.Sort != 2 || got.Status != 1 {
		t.Errorf("创建时字段未完整落库: %+v", got)
	}
	// 租户与审计字段由 Service 写入。漏 TenantID 会让这条数据对所有租户可见，
	// 而且不会报错 —— 属于最难发现的一类问题。
	if got.TenantID != tenantA {
		t.Errorf("TenantID 未写入: 期望 %d 实际 %d", tenantA, got.TenantID)
	}
	if got.CreateBy != 7 || got.UpdateBy != 7 {
		t.Errorf("操作人未写入: createBy=%d updateBy=%d", got.CreateBy, got.UpdateBy)
	}
}

// TestMemberLevelServiceFindListNormalizesPaging 分页参数必须归一化后再下传。
//
// 归一化写在 Service 里（而不是仓储），所以断言点是「请求结构体被就地改写」。
// 漏掉这一步的后果是把 pageSize=100000 直接透给数据库，或 page=0 算出负 offset。
func TestMemberLevelServiceFindListNormalizesPaging(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberLevelService()

	cases := []struct {
		name                   string
		inPage, inSize         int
		wantPage, wantPageSize int
	}{
		{"零值归位到默认分页", 0, 0, 1, common.DefaultPageSize},
		{"负数页码归位", -5, 20, 1, 20},
		{"超大 pageSize 收敛到默认值", 3, 100000, 3, common.DefaultPageSize},
		{"合法值原样保留", 2, 15, 2, 15},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := &dto.MemberLevelListRequest{}
			req.Page, req.PageSize = c.inPage, c.inSize

			if _, _, err := svc.FindList(tenantA, req); err != nil {
				t.Fatalf("查询失败: %v", err)
			}
			if req.Page != c.wantPage || req.PageSize != c.wantPageSize {
				t.Errorf("分页未归一化: 传入 (%d,%d) 期望 (%d,%d) 实际 (%d,%d)",
					c.inPage, c.inSize, c.wantPage, c.wantPageSize, req.Page, req.PageSize)
			}
		})
	}
}

// TestMemberLevelServiceFindAllSkipsDisabled 下拉框数据源只应包含启用中的等级。
//
// pay_member_level.status 是「是否可选」。若 FindAll 丢掉 status 过滤，
// 已被停用的等级会重新出现在前端下拉框里，用户能把会员挂到废弃等级上。
func TestMemberLevelServiceFindAllSkipsDisabled(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberLevelService()

	if err := svc.Create(&dto.CreateMemberLevelRequest{Name: "启用中", Discount: 10, Status: 1}, 1, tenantA); err != nil {
		t.Fatalf("创建等级失败: %v", err)
	}
	if err := svc.Create(&dto.CreateMemberLevelRequest{Name: "已停用", Discount: 10, Status: 0}, 1, tenantA); err != nil {
		t.Fatalf("创建等级失败: %v", err)
	}

	all, err := svc.FindAll(tenantA)
	if err != nil {
		t.Fatalf("查询全部等级失败: %v", err)
	}
	if len(all) != 1 || all[0].Name != "启用中" {
		t.Errorf("FindAll 应只返回启用中的等级，实际 %+v", all)
	}
}

// TestMemberLevelServiceUpdatePartial 部分更新不得清掉未提供的字段。
//
// 与会员模块是同一类事故：前端「改个名字」只提交 {id, name}，其余字段是零值。
// 若实现把 `if req.MinPoints != nil` 写成无条件赋值，MinPoints/Discount/Icon/Sort
// 会被一起清成 0 —— 界面提示「保存成功」，折扣规则却被静默抹掉。
func TestMemberLevelServiceUpdatePartial(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberLevelService()

	if err := svc.Create(&dto.CreateMemberLevelRequest{
		Name: "原名称", MinPoints: 500, Discount: 8.5, Icon: "old.png", Sort: 9, Status: 1,
	}, 1, tenantA); err != nil {
		t.Fatalf("创建等级失败: %v", err)
	}
	before, _, _ := svc.FindList(tenantA, &dto.MemberLevelListRequest{})

	// 只改名称
	if err := svc.Update(&dto.UpdateMemberLevelRequest{ID: before[0].ID, Name: "新名称"}, 42, tenantA); err != nil {
		t.Fatalf("更新等级失败: %v", err)
	}

	after, _, _ := svc.FindList(tenantA, &dto.MemberLevelListRequest{})
	got := after[0]
	if got.Name != "新名称" {
		t.Errorf("名称未更新: %q", got.Name)
	}
	if got.MinPoints != 500 || got.Discount != 8.5 || got.Icon != "old.png" || got.Sort != 9 || got.Status != 1 {
		t.Errorf("部分更新清掉了未提供的字段（复现了历史事故）: %+v", got)
	}
	if got.UpdateBy != 42 {
		t.Errorf("更新人未写入: %d", got.UpdateBy)
	}
	if got.CreateBy != before[0].CreateBy {
		t.Errorf("更新不应改动创建人: %d -> %d", before[0].CreateBy, got.CreateBy)
	}
}

// TestMemberLevelServiceUpdateExplicitZero 显式传 0 必须生效。
//
// 指针方案的另一半约束：区分「未提供」之后，用户仍要能把 Sort 改成 0、
// 把 Status 改成停用。若实现把零值当「未提供」跳过，这两个操作会静默失败。
func TestMemberLevelServiceUpdateExplicitZero(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberLevelService()

	if err := svc.Create(&dto.CreateMemberLevelRequest{Name: "等级", Discount: 10, Sort: 9, Status: 1}, 1, tenantA); err != nil {
		t.Fatalf("创建等级失败: %v", err)
	}
	rows, _, _ := svc.FindList(tenantA, &dto.MemberLevelListRequest{})

	zeroSort, disabled := 0, int8(0)
	err := svc.Update(&dto.UpdateMemberLevelRequest{ID: rows[0].ID, Sort: &zeroSort, Status: &disabled}, 1, tenantA)
	if err != nil {
		t.Fatalf("更新等级失败: %v", err)
	}

	after, _, _ := svc.FindList(tenantA, &dto.MemberLevelListRequest{})
	if after[0].Sort != 0 {
		t.Errorf("显式 sort=0 未生效: %d", after[0].Sort)
	}
	if after[0].Status != 0 {
		t.Errorf("显式 status=0 未生效: %d", after[0].Status)
	}
}

// TestMemberLevelServiceUpdateErrors Update 的两种失败语义。
//
// 目标不存在 → 404（不是 500）；跨租户 → 同样 404，且**不能**改到别人的数据。
func TestMemberLevelServiceUpdateErrors(t *testing.T) {
	t.Run("不存在的等级返回 404 语义", func(t *testing.T) {
		newMemberDB(t)
		svc := NewMemberLevelService()

		err := svc.Update(&dto.UpdateMemberLevelRequest{ID: 999999, Name: "x"}, 1, tenantA)
		assertBizCode(t, err, common.CodeNotFound)
	})

	t.Run("跨租户更新被拒绝且数据不变", func(t *testing.T) {
		newMemberDB(t)
		svc := NewMemberLevelService()
		if err := svc.Create(&dto.CreateMemberLevelRequest{Name: "别人的等级", Discount: 10, Status: 1}, 1, tenantB); err != nil {
			t.Fatalf("创建等级失败: %v", err)
		}
		foreign, _, _ := svc.FindList(tenantB, &dto.MemberLevelListRequest{})

		err := svc.Update(&dto.UpdateMemberLevelRequest{ID: foreign[0].ID, Name: "劫持"}, 1, tenantA)
		assertBizCode(t, err, common.CodeNotFound)

		// 关键：目标记录必须原样保留（若仓储的归属校验被去掉，这里会变成「劫持」）
		still, _, _ := svc.FindList(tenantB, &dto.MemberLevelListRequest{})
		if still[0].Name != "别人的等级" {
			t.Errorf("跨租户更新竟然生效了: %q", still[0].Name)
		}
	})
}

// TestMemberLevelServiceDeleteScopedToTenant 删除必须限定在本租户。
//
// 仓储用的是 `TenantScope(...).Delete(&MemberLevel{}, id)`：条件不匹配时
// GORM 返回 nil（0 行受影响），所以**不会报错** —— 只能靠「记录是否还在」来验证。
// 这条用例的存在就是为了堵住「不报错 = 删掉了」的误判。
func TestMemberLevelServiceDeleteScopedToTenant(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberLevelService()
	if err := svc.Create(&dto.CreateMemberLevelRequest{Name: "本租户等级", Discount: 10, Status: 1}, 1, tenantA); err != nil {
		t.Fatalf("创建等级失败: %v", err)
	}
	mine, _, _ := svc.FindList(tenantA, &dto.MemberLevelListRequest{})

	// 别的租户来删：不报错，但也不能真的删掉
	if err := svc.Delete(tenantB, mine[0].ID); err != nil {
		t.Fatalf("跨租户删除不应返回错误（GORM 0 行受影响即 nil）: %v", err)
	}
	still, total, _ := svc.FindList(tenantA, &dto.MemberLevelListRequest{})
	if total != 1 {
		t.Fatalf("跨租户删除把别人的等级删掉了: total=%d", total)
	}
	if still[0].ID != mine[0].ID {
		t.Errorf("记录 ID 变了: %d -> %d", mine[0].ID, still[0].ID)
	}

	// 本租户删：生效
	if err := svc.Delete(tenantA, mine[0].ID); err != nil {
		t.Fatalf("本租户删除失败: %v", err)
	}
	if _, total, _ := svc.FindList(tenantA, &dto.MemberLevelListRequest{}); total != 0 {
		t.Errorf("本租户删除未生效: total=%d", total)
	}
}

// ─────────────────────────── 会员标签 ───────────────────────────

// TestMemberTagServiceCreateAndQuery 构造器 + 创建 + 查询 + 租户隔离。
func TestMemberTagServiceCreateAndQuery(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberTagService()

	if err := svc.Create(&dto.CreateMemberTagRequest{Name: "高价值", Color: "#ff0000", Sort: 3, Status: 1}, 7, tenantA); err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}
	if err := svc.Create(&dto.CreateMemberTagRequest{Name: "别人租户的标签", Color: "#000000", Sort: 1, Status: 1}, 7, tenantB); err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}

	list, total, err := svc.FindList(tenantA, &dto.MemberTagListRequest{})
	if err != nil {
		t.Fatalf("查询标签失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("本租户应只看到 1 条标签，实际 total=%d len=%d", total, len(list))
	}
	got := list[0]
	if got.Name != "高价值" || got.Color != "#ff0000" || got.Sort != 3 || got.Status != 1 {
		t.Errorf("创建时字段未完整落库: %+v", got)
	}
	if got.TenantID != tenantA {
		t.Errorf("TenantID 未写入: 期望 %d 实际 %d", tenantA, got.TenantID)
	}
	if got.CreateBy != 7 || got.UpdateBy != 7 {
		t.Errorf("操作人未写入: createBy=%d updateBy=%d", got.CreateBy, got.UpdateBy)
	}
}

// TestMemberTagServiceFindListNormalizesPaging 标签列表的分页归一化。
func TestMemberTagServiceFindListNormalizesPaging(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberTagService()

	req := &dto.MemberTagListRequest{}
	req.Page, req.PageSize = 0, 99999
	if _, _, err := svc.FindList(tenantA, req); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if req.Page != 1 || req.PageSize != common.DefaultPageSize {
		t.Errorf("分页未归一化: 实际 (%d,%d)", req.Page, req.PageSize)
	}
}

// TestMemberTagServiceFindAllSkipsDisabled 下拉框数据源只应包含启用中的标签。
func TestMemberTagServiceFindAllSkipsDisabled(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberTagService()

	if err := svc.Create(&dto.CreateMemberTagRequest{Name: "启用中", Status: 1}, 1, tenantA); err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}
	if err := svc.Create(&dto.CreateMemberTagRequest{Name: "已停用", Status: 0}, 1, tenantA); err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}

	all, err := svc.FindAll(tenantA)
	if err != nil {
		t.Fatalf("查询全部标签失败: %v", err)
	}
	if len(all) != 1 || all[0].Name != "启用中" {
		t.Errorf("FindAll 应只返回启用中的标签，实际 %+v", all)
	}
}

// TestMemberTagServiceUpdatePartial 部分更新不得清掉未提供的字段。
func TestMemberTagServiceUpdatePartial(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberTagService()

	if err := svc.Create(&dto.CreateMemberTagRequest{Name: "原名称", Color: "#123456", Sort: 8, Status: 1}, 1, tenantA); err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}
	rows, _, _ := svc.FindList(tenantA, &dto.MemberTagListRequest{})

	if err := svc.Update(&dto.UpdateMemberTagRequest{ID: rows[0].ID, Name: "新名称"}, 42, tenantA); err != nil {
		t.Fatalf("更新标签失败: %v", err)
	}

	after, _, _ := svc.FindList(tenantA, &dto.MemberTagListRequest{})
	got := after[0]
	if got.Name != "新名称" {
		t.Errorf("名称未更新: %q", got.Name)
	}
	if got.Color != "#123456" || got.Sort != 8 || got.Status != 1 {
		t.Errorf("部分更新清掉了未提供的字段: %+v", got)
	}
	if got.UpdateBy != 42 {
		t.Errorf("更新人未写入: %d", got.UpdateBy)
	}
}

// TestMemberTagServiceUpdateErrors Update 的两种失败语义（404 / 跨租户）。
func TestMemberTagServiceUpdateErrors(t *testing.T) {
	t.Run("不存在的标签返回 404 语义", func(t *testing.T) {
		newMemberDB(t)
		svc := NewMemberTagService()

		err := svc.Update(&dto.UpdateMemberTagRequest{ID: 999999, Name: "x"}, 1, tenantA)
		assertBizCode(t, err, common.CodeNotFound)
	})

	t.Run("跨租户更新被拒绝且数据不变", func(t *testing.T) {
		newMemberDB(t)
		svc := NewMemberTagService()
		if err := svc.Create(&dto.CreateMemberTagRequest{Name: "别人的标签", Status: 1}, 1, tenantB); err != nil {
			t.Fatalf("创建标签失败: %v", err)
		}
		foreign, _, _ := svc.FindList(tenantB, &dto.MemberTagListRequest{})

		err := svc.Update(&dto.UpdateMemberTagRequest{ID: foreign[0].ID, Name: "劫持"}, 1, tenantA)
		assertBizCode(t, err, common.CodeNotFound)

		still, _, _ := svc.FindList(tenantB, &dto.MemberTagListRequest{})
		if still[0].Name != "别人的标签" {
			t.Errorf("跨租户更新竟然生效了: %q", still[0].Name)
		}
	})
}

// TestMemberTagServiceDeleteRemovesRelations 删除标签必须一并清掉会员-标签关联。
//
// pay_member_tag_rel 是纯关联表，没有外键约束。标签被删而关联留着的话，
// 会员列表会按 tag_id 去查一个已软删除的标签 —— 前端表现为标签神秘消失，
// 而库里持续堆积孤儿记录。这条用例锁住仓储里的那笔事务。
func TestMemberTagServiceDeleteRemovesRelations(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberTagService()
	memberRepo := repository.NewMemberRepository()

	tag := seedTag(t, tenantA, "待删标签")
	member := seedMember(t, tenantA, "13900000001", "100001")

	if err := memberRepo.ReplaceTags(tenantA, member.ID, []uint{tag.ID}); err != nil {
		t.Fatalf("绑定标签失败: %v", err)
	}
	if ids, _ := memberRepo.FindTagIDsByMemberID(tenantA, member.ID); len(ids) != 1 {
		t.Fatalf("准备数据失败，关联未建立: %v", ids)
	}

	if err := svc.Delete(tenantA, tag.ID); err != nil {
		t.Fatalf("删除标签失败: %v", err)
	}

	if ids, _ := memberRepo.FindTagIDsByMemberID(tenantA, member.ID); len(ids) != 0 {
		t.Errorf("标签已删但关联残留（孤儿记录）: %v", ids)
	}
}

// TestMemberTagServiceDeleteScopedToTenant 跨租户删除不得生效。
func TestMemberTagServiceDeleteScopedToTenant(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberTagService()

	if err := svc.Create(&dto.CreateMemberTagRequest{Name: "本租户标签", Status: 1}, 1, tenantA); err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}
	mine, _, _ := svc.FindList(tenantA, &dto.MemberTagListRequest{})

	if err := svc.Delete(tenantB, mine[0].ID); err != nil {
		t.Fatalf("跨租户删除不应返回错误: %v", err)
	}
	if _, total, _ := svc.FindList(tenantA, &dto.MemberTagListRequest{}); total != 1 {
		t.Errorf("跨租户删除把别人的标签删掉了: total=%d", total)
	}
}

// ─────────────────────────── 积分流水 ───────────────────────────

// TestPointsLogServiceFindList 构造器 + 租户隔离 + 三个筛选维度 + 分页归一化。
//
// 积分流水是只读接口，全部价值都在筛选条件上：漏掉 tenantID 会把别人的
// 积分变动暴露出来（`TenantScope(db, 0)` 是不过滤，不会报错）；
// 漏掉 memberID/type 条件则是「筛选器点了没反应」。
func TestPointsLogServiceFindList(t *testing.T) {
	newMemberDB(t)
	svc := NewPointsLogService()
	repo := repository.NewPointsLogRepository()

	seedLog := func(tenantID, memberID uint, changeType int8, source string) {
		t.Helper()
		if err := repo.Create(tenantID, &model.PointsLog{
			MemberID: memberID, Change: 10, Type: changeType, Source: source,
		}); err != nil {
			t.Fatalf("创建积分流水失败: %v", err)
		}
	}

	seedLog(tenantA, 101, 1, "下单")
	seedLog(tenantA, 101, 2, "消费")
	seedLog(tenantA, 202, 1, "签到")
	seedLog(tenantB, 101, 1, "别人租户的流水")

	t.Run("不带筛选只返回本租户全部流水", func(t *testing.T) {
		req := &dto.PointsLogListRequest{}
		logs, total, err := svc.FindList(tenantA, req)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 3 || len(logs) != 3 {
			t.Fatalf("本租户应有 3 条流水，实际 total=%d len=%d", total, len(logs))
		}
		for _, l := range logs {
			if l.TenantID != tenantA {
				t.Errorf("返回了其他租户的流水: tenantID=%d source=%q", l.TenantID, l.Source)
			}
		}
	})

	t.Run("按会员筛选", func(t *testing.T) {
		_, total, err := svc.FindList(tenantA, &dto.PointsLogListRequest{MemberID: 101})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 {
			t.Errorf("memberID=101 应有 2 条，实际 %d", total)
		}
	})

	t.Run("按类型筛选", func(t *testing.T) {
		_, total, err := svc.FindList(tenantA, &dto.PointsLogListRequest{Type: 2})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 {
			t.Errorf("type=2 应有 1 条，实际 %d", total)
		}
	})

	t.Run("会员与类型组合筛选", func(t *testing.T) {
		_, total, err := svc.FindList(tenantA, &dto.PointsLogListRequest{MemberID: 202, Type: 1})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 {
			t.Errorf("memberID=202 且 type=1 应有 1 条，实际 %d", total)
		}
	})

	t.Run("type=0 表示不按类型过滤", func(t *testing.T) {
		// 0 不是合法类型（1 获取 / 2 消费），仓储以 `> 0` 判定「是否筛选」。
		// 若被改成 `>= 0`，前端默认值 0 会让列表恒为空。
		_, total, err := svc.FindList(tenantA, &dto.PointsLogListRequest{MemberID: 101, Type: 0})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 {
			t.Errorf("type=0 应视为不过滤（2 条），实际 %d", total)
		}
	})

	t.Run("分页参数归一化", func(t *testing.T) {
		req := &dto.PointsLogListRequest{}
		req.Page, req.PageSize = 0, 100000
		if _, _, err := svc.FindList(tenantA, req); err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if req.Page != 1 || req.PageSize != common.DefaultPageSize {
			t.Errorf("分页未归一化: 实际 (%d,%d)", req.Page, req.PageSize)
		}
	})
}

// assertBizCode 断言错误是业务错误且业务码为 want。
//
// 只断言 err != nil 是不够的：把 404 写成 500 时前端提示与监控告警都会错，
// 而这正是 common 包引入 BizError 要解决的问题。
func assertBizCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望业务错误（code=%d），实际返回 nil", want)
	}
	be, ok := common.AsBizError(err)
	if !ok {
		t.Fatalf("期望业务错误（code=%d），实际是系统错误 %T: %v", want, err, err)
	}
	if be.Code != want {
		t.Errorf("业务码不符: 期望 %d 实际 %d (%s)", want, be.Code, be.Msg)
	}
}
