package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"
	"go-admin/internal/module/member/repository"
	systemModel "go-admin/internal/module/system/model"
	systemRepository "go-admin/internal/module/system/repository"
	systemService "go-admin/internal/module/system/service"
	"go-admin/internal/testsupport"
)

// 本文件补齐 member_service.go 中仍为 0% 的部分：构造器、编号生成、
// 以及 Delete / FindByID / FindList / UpdateStatus / UpdateLastVisit 这些
// 只做透传的短方法。
//
// 短方法的价值恰恰在透传本身 —— tenantID 传丢会静默退化为全表查询
// （TenantScope(db, 0) 是「不过滤」），所以每条断言都落在「另一个租户
// 能不能看到 / 改到」上，而不是「函数返回了 nil error」。
//
// ⚠️ 会改写包级 database.DB，不能 t.Parallel。

// seedConfig 写入一条系统配置（会员编号位数等）。
func seedConfig(t *testing.T, key, value string) {
	t.Helper()
	repo := systemRepository.NewConfigRepository()
	if err := repo.UpsertByKey(&systemModel.SysConfig{ConfigKey: key, Value: value}); err != nil {
		t.Fatalf("写入配置 %s 失败: %v", key, err)
	}
}

// ─────────────────────────── 构造器 ───────────────────────────

// TestNewMemberServiceWiring 构造器必须把四个依赖都接上。
//
// 用一次真实读写来验证：构造器里漏注入某个仓储，第一次调用就会 nil panic，
// 而这类错误在「只断言 NewXxxService() != nil」的测试里是发现不了的。
func TestNewMemberServiceWiring(t *testing.T) {
	newMemberDB(t)
	svc := NewMemberService()

	req := baseCreateReq("13100000001")
	if err := svc.Create(req, 1, tenantA); err != nil {
		t.Fatalf("创建会员失败（构造器依赖未接齐？）: %v", err)
	}

	list, total, err := svc.FindList(tenantA, &dto.MemberListRequest{})
	if err != nil {
		t.Fatalf("查询会员失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("应查到 1 条会员，实际 total=%d len=%d", total, len(list))
	}
}

// ─────────────────────── 会员编号位数配置 ───────────────────────

// TestMemberNoDigits 位数配置的取值与兜底。
//
// 这段逻辑原先写在 Controller 里，下沉到 Service 时带了三条兜底规则
// （读不到、空值、非法值、低于下限都退回默认 6 位）。兜底之所以重要：
// 位数直接决定编号的格式与长度，配错一个字符就会发出一批格式不一致的编号。
func TestMemberNoDigits(t *testing.T) {
	cases := []struct {
		name  string
		seed  bool
		value string
		want  int
	}{
		{"未配置时退回默认值", false, "", defaultMemberNoDigits},
		{"配置为空串时退回默认值", true, "", defaultMemberNoDigits},
		{"非数字时退回默认值", true, "abc", defaultMemberNoDigits},
		{"带空格的非数字同样退回默认值", true, " 8", defaultMemberNoDigits},
		{"低于下限 4 时退回默认值", true, "3", defaultMemberNoDigits},
		{"恰好等于下限时生效", true, "4", 4},
		{"合法值生效", true, "8", 8},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 每个子用例独立建库，避免配置互相干扰
			s := newTestMemberService(t)
			if c.seed {
				seedConfig(t, "site.memberIdDigits", c.value)
			}

			if got := s.memberNoDigits(); got != c.want {
				t.Errorf("位数不符: 配置 %q 期望 %d 实际 %d", c.value, c.want, got)
			}
		})
	}
}

// TestMemberNoDigitsRejectsUnexpectedType 配置仓储返回了非 *SysConfig 时的兜底。
//
// 属于防御性分支：FindByKey 目前恒返回 *SysConfig，但类型断言的 !ok 分支
// 一旦被将来重构触发（例如改成返回 map），没有兜底就会 panic。
func TestMemberNoDigitsRejectsUnexpectedType(t *testing.T) {
	newMemberDB(t)
	s := &memberService{configService: wrongTypeConfigService{}}

	if got := s.memberNoDigits(); got != defaultMemberNoDigits {
		t.Errorf("类型不符时应退回默认值 %d，实际 %d", defaultMemberNoDigits, got)
	}
}

// TestMemberNoDigitsAppliedToNewMember 位数配置最终体现在新会员的编号上。
func TestMemberNoDigitsAppliedToNewMember(t *testing.T) {
	s := newTestMemberService(t)
	seedConfig(t, "site.memberIdDigits", "8")

	req := baseCreateReq("13100000002")
	if err := s.Create(req, 1, tenantA); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}
	m, _ := s.memberRepo.FindByPhone(tenantA, req.Phone)
	if m == nil {
		t.Fatal("会员未创建")
	}
	if len(m.MemberNo) != 8 {
		t.Errorf("配置 8 位后编号应为 8 位，实际 %q", m.MemberNo)
	}
}

// ─────────────────────── 会员编号推导 ───────────────────────

// TestMemberNoFromDB 按库内最大值推导下一个可用编号。
//
// 三条规则：起始值取 10^(digits-1)+1 保证位数；库内已有更大编号时以库内为准
// （否则发号会回退到已占用区间，撞唯一索引）；解析不出数字的编号不参与推导。
func TestMemberNoFromDB(t *testing.T) {
	cases := []struct {
		name   string
		seedNo string
		want   int
	}{
		{"空库取位数下限", "", 100001},
		{"库内编号更大时以库内为准", "900000", 900001},
		{"库内编号更小时仍取位数下限", "1000", 100001},
		{"非数字编号不参与推导", "ABC123", 100001},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestMemberService(t)
			if c.seedNo != "" {
				seedMember(t, tenantA, "13000000001", c.seedNo)
			}

			got, err := s.memberNoFromDB(6)
			if err != nil {
				t.Fatalf("推导编号失败: %v", err)
			}
			if got != c.want {
				t.Errorf("推导值不符: 期望 %d 实际 %d", c.want, got)
			}
		})
	}
}

// TestMemberNoFromDBIsGlobal 最大值必须**跨租户**取（P0-5 回归护栏）。
//
// 断言方向与直觉相反，但这是正确的一侧：`uk_member_no` 是全局唯一索引，
// 若按租户推导，每个租户在空库上都会从 100001 起号 ——
// 第二个租户建会员必然撞唯一索引，多租户部署下除首个租户外完全无法创建会员。
// 所以「乙租户的 900000 抬高了甲租户的起始值」不是串号，而是必须的行为。
func TestMemberNoFromDBIsGlobal(t *testing.T) {
	s := newTestMemberService(t)
	seedMember(t, tenantB, "13000000002", "900000")

	got, err := s.memberNoFromDB(6)
	if err != nil {
		t.Fatalf("推导编号失败: %v", err)
	}
	if got != 900001 {
		t.Errorf("必须取全平台最大值 +1（否则会发到乙租户已占用的号段）: 期望 900001 实际 %d", got)
	}
}

// TestMemberNoFromDBPropagatesError 仓储故障必须上抛。
func TestMemberNoFromDBPropagatesError(t *testing.T) {
	s := newTestMemberService(t)
	s.memberRepo = &stubMemberRepo{findMaxNoErr: errors.New("db down")}

	if _, err := s.memberNoFromDB(6); err == nil {
		t.Fatal("仓储故障时必须上抛，不能臆测一个起始值")
	}
}

// TestGenerateMemberNoWithoutRedis Redis 不可用时降级为按库内最大值推导。
func TestGenerateMemberNoWithoutRedis(t *testing.T) {
	s := newTestMemberService(t)
	testsupport.WithNilRedis(t)

	t.Run("空库发出位数下限", func(t *testing.T) {
		no, err := s.generateMemberNo(6)
		if err != nil {
			t.Fatalf("生成编号失败: %v", err)
		}
		if no != "100001" {
			t.Errorf("期望 100001 实际 %q", no)
		}
	})

	t.Run("库内已有更大编号时顺延", func(t *testing.T) {
		seedMember(t, tenantA, "13000000003", "900000")

		no, err := s.generateMemberNo(6)
		if err != nil {
			t.Fatalf("生成编号失败: %v", err)
		}
		if no != "900001" {
			t.Errorf("期望 900001 实际 %q", no)
		}
	})

	t.Run("Redis 与数据库都不可用时拒绝发号", func(t *testing.T) {
		broken := newTestMemberService(t)
		testsupport.WithNilRedis(t)
		broken.memberRepo = &stubMemberRepo{findMaxNoErr: errors.New("db down")}

		if _, err := broken.generateMemberNo(6); err == nil {
			t.Fatal("两处都不可用时必须报错：用臆测的起始值发号会撞唯一索引，变成难懂的 500")
		}
	})
}

// TestGenerateMemberNoWithRedis Redis 可用时的计数器语义。
//
// 这条用例是「首次初始化对齐」那段逻辑的回归护栏：首次发号要按库内最大值
// 对齐计数器，之后才走纯 INCR。对齐值写错一位，就会发出一个与历史编号
// 冲突（或凭空跳号）的编号，而且只在 Redis 键过期的第一个请求上复现。
func TestGenerateMemberNoWithRedis(t *testing.T) {
	s := newTestMemberService(t)
	testsupport.WithTestRedis(t)

	// 计数器是**全平台共用一个键**（见 generateMemberNo 注释）。
	// 必须先清掉，否则起点取决于上一次运行留下的值。
	ctx := context.Background()
	key := "member:no"
	if err := cache.Del(ctx, key); err != nil {
		t.Fatalf("清理计数器失败: %v", err)
	}
	t.Cleanup(func() { _ = cache.Del(ctx, key) })

	t.Run("首次发号按位数下限对齐", func(t *testing.T) {
		no, err := s.generateMemberNo(6)
		if err != nil {
			t.Fatalf("生成编号失败: %v", err)
		}
		if no != "100001" {
			t.Errorf("期望 100001 实际 %q", no)
		}
	})

	t.Run("后续发号连续不跳号", func(t *testing.T) {
		// 关键断言：对齐后计数器应停在**刚发出的**编号上，
		// 这样下一次 INCR 恰好是下一个编号。
		// 若把计数器对齐成 startNum+1，这里会拿到 100003 —— 编号凭空跳一位，
		// 用户会以为有会员数据丢失。
		no, err := s.generateMemberNo(6)
		if err != nil {
			t.Fatalf("生成编号失败: %v", err)
		}
		if no != "100002" {
			t.Errorf("编号应连续：期望 100002 实际 %q", no)
		}
	})

	t.Run("库内已有更大编号时对齐到库内最大值", func(t *testing.T) {
		// 删掉计数器以强制走「首次初始化」分支
		if err := cache.Del(ctx, key); err != nil {
			t.Fatalf("清理计数器失败: %v", err)
		}
		seedMember(t, tenantA, "13000000009", "700000")

		no, err := s.generateMemberNo(6)
		if err != nil {
			t.Fatalf("生成编号失败: %v", err)
		}
		if no != "700001" {
			t.Errorf("期望 700001（库内最大值 +1）实际 %q", no)
		}
	})

	t.Run("计数器写在全平台共用的键上", func(t *testing.T) {
		// 直接钉住键名。按租户分键（`member:no:<tenantID>`）是 P0-5 的一半成因：
		// 每个租户的计数器各自从 1 开始，各自去推起始号。
		if err := cache.Del(ctx, key); err != nil {
			t.Fatalf("清理计数器失败: %v", err)
		}
		if _, err := s.generateMemberNo(6); err != nil {
			t.Fatalf("生成编号失败: %v", err)
		}
		exists, err := cache.Exists(ctx, key)
		if err != nil {
			t.Fatalf("查询计数器失败: %v", err)
		}
		if !exists {
			t.Errorf("计数器应写在全平台共用键 %q 上", key)
		}
	})

	t.Run("两个租户共用同一个序列", func(t *testing.T) {
		if err := cache.Del(ctx, key); err != nil {
			t.Fatalf("清理计数器失败: %v", err)
		}
		if err := s.Create(baseCreateReq("13922220001"), 1, tenantA); err != nil {
			t.Fatalf("甲租户建会员失败: %v", err)
		}
		if err := s.Create(baseCreateReq("13922220002"), 1, tenantB); err != nil {
			t.Fatalf("乙租户建会员失败: %v", err)
		}

		a, _ := s.memberRepo.FindByPhone(tenantA, "13922220001")
		b, _ := s.memberRepo.FindByPhone(tenantB, "13922220002")
		if a == nil || b == nil {
			t.Fatal("两个租户的会员都应创建成功")
		}
		if a.MemberNo == b.MemberNo {
			t.Errorf("两个租户拿到了同一个编号: %q", a.MemberNo)
		}
	})
}

// TestCreateMemberAcrossTenantsDoesNotCollide P0-5 的直接回归用例。
//
// 这是本次修复暴露出来的缺陷：`uk_member_no` 是全局唯一索引，
// 而发号曾按租户（Redis 键 `member:no:<tenantID>` + `FindMaxMemberNo(tenantID)`）——
// 两个租户在各自空库上都推出 100001，第二个租户建第一个会员就 1062 冲突。
// 多租户部署下除首个租户外**完全无法创建会员**，且单租户部署不会暴露。
//
// 用例必须让两个租户在**同一个库**里各建一个会员：只建一个租户是测不出来的。
func TestCreateMemberAcrossTenantsDoesNotCollide(t *testing.T) {
	s := newTestMemberService(t)

	if err := s.Create(baseCreateReq("13911110001"), 1, tenantA); err != nil {
		t.Fatalf("甲租户建会员失败: %v", err)
	}
	// 修复前这里必然失败：Duplicate entry '100001' for key 'uk_member_no'
	if err := s.Create(baseCreateReq("13911110002"), 1, tenantB); err != nil {
		t.Fatalf("乙租户建会员失败（编号发到了甲租户已占用的号段）: %v", err)
	}

	a, _ := s.memberRepo.FindByPhone(tenantA, "13911110001")
	b, _ := s.memberRepo.FindByPhone(tenantB, "13911110002")
	if a == nil || b == nil {
		t.Fatal("两个租户的会员都应创建成功")
	}
	if a.MemberNo == b.MemberNo {
		t.Errorf("两个租户的会员编号重复: %q", a.MemberNo)
	}
	// 编号必须连续（同一个全平台序列），顺带钉住「不是各租户从 100001 起」
	if a.MemberNo != "100001" || b.MemberNo != "100002" {
		t.Errorf("编号应来自同一个全平台序列: 甲=%q 乙=%q", a.MemberNo, b.MemberNo)
	}
}

// ─────────────────────────── 只读与透传 ───────────────────────────

// TestMemberServiceFindByIDNotFound 单条查询必须给出 404 语义。
//
// GORM 查不到返回 gorm.ErrRecordNotFound，不转换就会变成 500 ——
// 用户传错 ID 时看到「服务器内部错误」，完全不知道是自己传错了。
func TestMemberServiceFindByIDNotFound(t *testing.T) {
	s := newTestMemberService(t)

	if _, err := s.FindByID(tenantA, 999999); err != nil {
		assertBizCode(t, err, common.CodeNotFound)
	} else {
		t.Fatal("查询不存在的会员应返回错误")
	}
}

// TestMemberServiceFindByIDIsTenantScoped 跨租户按 ID 直查必须查不到。
func TestMemberServiceFindByIDIsTenantScoped(t *testing.T) {
	s := newTestMemberService(t)
	m := seedMember(t, tenantB, "13000000011", "100011")

	if _, err := s.FindByID(tenantA, m.ID); err == nil {
		t.Error("不应能按 ID 读到其他租户的会员")
	}
}

// TestMemberServiceFindList 列表查询：状态过滤、标签装配、分页归一化、租户隔离。
func TestMemberServiceFindList(t *testing.T) {
	s := newTestMemberService(t)
	tag := seedTag(t, tenantA, "高价值")

	// 启用中且带标签
	withTag := baseCreateReq("13000000021")
	withTag.TagIds = []uint{tag.ID}
	if err := s.Create(withTag, 1, tenantA); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}
	// 已停用
	disabled := baseCreateReq("13000000022")
	disabled.Status = 0
	if err := s.Create(disabled, 1, tenantA); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}
	// 其他租户：走真实 Create 路径。
	// 这里曾经必须用 seedMember 绕开，因为发号按租户、索引却全局，
	// 第二个租户建会员必然撞 uk_member_no（P0-5）。修复后已恢复正常调用 ——
	// 换句话说，这个用例现在同时守着「FindList 按租户过滤」和「跨租户建会员不冲突」。
	if err := s.Create(baseCreateReq("13000000023"), 1, tenantB); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}

	t.Run("status 缺省时不过滤状态", func(t *testing.T) {
		// 缺省用 -1 表示「不按状态过滤」。若被写成 `status = 0`，
		// 列表会只剩停用会员 —— 这类默认值写错在界面上表现为「数据没了」。
		_, total, err := s.FindList(tenantA, &dto.MemberListRequest{})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 2 {
			t.Errorf("应返回本租户全部 2 条会员，实际 %d", total)
		}
	})

	t.Run("显式 status 过滤", func(t *testing.T) {
		enabled := int8(1)
		list, total, err := s.FindList(tenantA, &dto.MemberListRequest{Status: &enabled})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if total != 1 {
			t.Fatalf("启用中的会员应有 1 条，实际 %d", total)
		}
		// 用 JSON 断言而不是反射：memberWithTag 是 FindList 内部的私有类型，
		// 而 JSON 恰好就是前端真正消费的契约（字段必须叫 tags）。
		if tags := tagsOf(t, list[0]); len(tags) != 1 || tags[0].Name != "高价值" {
			t.Errorf("标签未装配进列表结果: %+v", tags)
		}
	})

	t.Run("分页参数归一化", func(t *testing.T) {
		req := &dto.MemberListRequest{}
		req.Page, req.PageSize = 0, 100000
		if _, _, err := s.FindList(tenantA, req); err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if req.Page != 1 || req.PageSize != common.DefaultPageSize {
			t.Errorf("分页未归一化: 实际 (%d,%d)", req.Page, req.PageSize)
		}
	})
}

// tagsOf 从列表项里取出标签数组。
//
// FindList 返回 []interface{}，元素是函数内定义的私有结构体，
// 测试无法直接做类型断言；但它是响应体的一部分，走 JSON 正好验证了对外契约。
func tagsOf(t *testing.T, item interface{}) []model.MemberTag {
	t.Helper()
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("序列化列表项失败: %v", err)
	}
	var decoded struct {
		Tags []model.MemberTag `json:"tags"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("反序列化列表项失败: %v", err)
	}
	return decoded.Tags
}

// TestMemberServiceUpdateStatus 状态变更与跨租户隔离。
func TestMemberServiceUpdateStatus(t *testing.T) {
	s := newTestMemberService(t)
	req := baseCreateReq("13000000031")
	if err := s.Create(req, 1, tenantA); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}
	m, _ := s.memberRepo.FindByPhone(tenantA, req.Phone)

	if err := s.UpdateStatus(tenantA, &dto.UpdateMemberStatusRequest{ID: m.ID, Status: 0}); err != nil {
		t.Fatalf("停用会员失败: %v", err)
	}
	if got, _ := s.FindByID(tenantA, m.ID); got.Status != 0 {
		t.Errorf("停用未生效: %d", got.Status)
	}

	// 跨租户：GORM 的 0 行受影响不报错，所以只能靠「值有没有变」来判定
	if err := s.UpdateStatus(tenantB, &dto.UpdateMemberStatusRequest{ID: m.ID, Status: 1}); err != nil {
		t.Fatalf("跨租户更新不应返回错误（0 行受影响即 nil）: %v", err)
	}
	if got, _ := s.FindByID(tenantA, m.ID); got.Status != 0 {
		t.Error("跨租户改状态竟然生效了")
	}
}

// TestMemberServiceUpdateLastVisit 最后访问时间。
func TestMemberServiceUpdateLastVisit(t *testing.T) {
	s := newTestMemberService(t)
	m := seedMember(t, tenantA, "13000000041", "100041")
	if m.LastVisitTime != nil {
		t.Fatal("准备数据失败：新会员不应有最后访问时间")
	}

	if err := s.UpdateLastVisit(tenantA, m.ID); err != nil {
		t.Fatalf("更新最后访问时间失败: %v", err)
	}
	got, _ := s.FindByID(tenantA, m.ID)
	if got.LastVisitTime == nil {
		t.Error("最后访问时间未写入")
	}

	// 会员不存在 → 404 而不是 500
	err := s.UpdateLastVisit(tenantA, 999999)
	assertBizCode(t, err, common.CodeNotFound)
}

// TestMemberServiceUpdateTagsMemberNotFound 改标签时会员不存在 → 404。
//
// 顺序很重要：会员归属校验必须发生在动关联表之前，否则用别人的会员 ID
// 就能改写（清空）对方的标签。
func TestMemberServiceUpdateTagsMemberNotFound(t *testing.T) {
	s := newTestMemberService(t)
	tag := seedTag(t, tenantA, "标签")

	err := s.UpdateTags(tenantA, &dto.UpdateMemberTagsRequest{ID: 999999, TagIds: []uint{tag.ID}})
	assertBizCode(t, err, common.CodeNotFound)
}

// TestMemberServiceUpdateRejectsCrossTenantLevel 更新时的等级归属校验。
//
// Create 有这条校验，Update 也必须有 —— 否则「先建一个无等级会员，
// 再用 update 把 levelId 指向别人租户的等级」就是一条提权旁路。
func TestMemberServiceUpdateRejectsCrossTenantLevel(t *testing.T) {
	s := newTestMemberService(t)
	foreign := seedLevel(t, tenantB, "别人的等级")
	m := newMemberWithProfile(t, s, "13000000051", 0)

	err := s.Update(&dto.UpdateMemberRequest{ID: m.ID, LevelID: &foreign.ID}, 1, tenantA)
	assertBizCode(t, err, common.CodeBadRequest)

	if got, _ := s.FindByID(tenantA, m.ID); got.LevelID != 0 {
		t.Errorf("越权等级竟然写进去了: %d", got.LevelID)
	}
}

// ─────────────────────────── 删除 ───────────────────────────

// TestMemberServiceDeleteFreesUniqueValues 软删除必须释放手机号与会员编号。
//
// 这是本项目软删除的既定约定（见 common.FreedUniqueValue）：不改写唯一键的话，
// 删掉的手机号就再也注册不了了 —— 用户侧表现为「这个号明明没人用，却提示已注册」，
// 而且没有任何报错线索。
func TestMemberServiceDeleteFreesUniqueValues(t *testing.T) {
	s := newTestMemberService(t)
	req := baseCreateReq("13000000061")
	if err := s.Create(req, 1, tenantA); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}
	m, _ := s.memberRepo.FindByPhone(tenantA, req.Phone)

	if err := s.Delete(tenantA, m.ID); err != nil {
		t.Fatalf("删除会员失败: %v", err)
	}

	// 已删除：查不到（404 语义）
	if _, err := s.FindByID(tenantA, m.ID); err == nil {
		t.Error("已删除的会员不应还能查到")
	}

	// 同一手机号可以再次创建
	again := baseCreateReq("13000000061")
	if err := s.Create(again, 1, tenantA); err != nil {
		t.Fatalf("软删除后同一手机号应能再次注册（唯一索引未释放）: %v", err)
	}
}

// TestMemberServiceDeleteIsTenantScoped 跨租户删除必须失败且不动数据。
//
// 与等级/标签不同，这里的仓储会先按租户查出会员，查不到直接返回
// gorm.ErrRecordNotFound —— 所以是**报错**，而不是静默 0 行。
func TestMemberServiceDeleteIsTenantScoped(t *testing.T) {
	s := newTestMemberService(t)
	m := seedMember(t, tenantB, "13000000071", "100071")

	if err := s.Delete(tenantA, m.ID); err == nil {
		t.Error("跨租户删除应失败")
	}
	if got, err := s.FindByID(tenantB, m.ID); err != nil || got == nil {
		t.Errorf("跨租户删除把别人的会员删掉了: %v", err)
	}
}

// TestMemberServiceCreateFailsWhenMemberNoUnavailable Create 在发不出编号时必须失败。
//
// 覆盖 Create 里的编号错误分支：静默用臆测的起始值发号，会在唯一索引上
// 撞出一条难以定位的 500，而不是一个能看懂的失败。
func TestMemberServiceCreateFailsWhenMemberNoUnavailable(t *testing.T) {
	s := newTestMemberService(t)
	testsupport.WithNilRedis(t)
	s.memberRepo = &stubMemberRepo{findMaxNoErr: errors.New("db down")}

	if err := s.Create(baseCreateReq("13000000081"), 1, tenantA); err == nil {
		t.Fatal("编号生成失败时 Create 必须返回错误")
	}
}

// TestMemberServiceFindListSurvivesTagLookupFailure 标签查询失败不能拖垮整个列表接口。
//
// 这段代码此前用 `_` 吞掉了错误，而当时标签查询因为 SQL 引用了不存在的列而
// **恒定失败** —— 表现是「会员列表的标签恒为空」，长期无人察觉。
// 现在改成记录 Warn。这里锁住的取舍是：标签属于附加信息，
// 查不到就退化为空数组，不能因此让整个会员列表 500。
func TestMemberServiceFindListSurvivesTagLookupFailure(t *testing.T) {
	s := newTestMemberService(t)
	tag := seedTag(t, tenantA, "高价值")

	req := baseCreateReq("13000000091")
	req.TagIds = []uint{tag.ID}
	if err := s.Create(req, 1, tenantA); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}

	// 只让标签查询失败，会员查询仍走真实仓储
	s.tagRepo = &stubTagRepo{findByIDsErr: errors.New("db down")}

	list, total, err := s.FindList(tenantA, &dto.MemberListRequest{})
	if err != nil {
		t.Fatalf("标签查询失败不应让整个列表接口失败: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("会员列表本身应正常返回，实际 total=%d len=%d", total, len(list))
	}
	if tags := tagsOf(t, list[0]); len(tags) != 0 {
		t.Errorf("标签查询失败时应退化为空数组，实际 %+v", tags)
	}
}

// TestUpdateMemberInvalidBirthdayKeepsOld 非法日期不得把已有生日清掉。
//
// DTO 上只写了 `binding:"omitempty"`，没有日期格式校验，所以格式非法的字符串
// 会一路走到 Service。此时正确做法是**保持原值**（解析失败就不动），
// 而不是把生日置空 —— 后者会让用户「改个昵称」顺手丢掉生日。
func TestUpdateMemberInvalidBirthdayKeepsOld(t *testing.T) {
	s := newTestMemberService(t)
	m := newMemberWithProfile(t, s, "13000000101", 0)
	if m.Birthday == nil {
		t.Fatal("准备数据失败：生日应为已设置状态")
	}

	bad := "not-a-date"
	if err := s.Update(&dto.UpdateMemberRequest{ID: m.ID, Birthday: &bad}, 1, tenantA); err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	got, _ := s.FindByID(tenantA, m.ID)
	if got.Birthday == nil {
		t.Error("非法日期不应把已有生日清掉")
	}
}

// stubTagRepo 只实现标签查询路径，用于制造「标签查询失败」。
// 未实现的方法会 panic，正好暴露测试路径与预期不符。
type stubTagRepo struct {
	repository.MemberTagRepository
	findByIDsErr error
}

func (s *stubTagRepo) FindByIDs(uint, []uint) ([]model.MemberTag, error) {
	return nil, s.findByIDsErr
}

// wrongTypeConfigService 让 FindByKey 返回一个非 *SysConfig 的值，
// 用于覆盖 memberNoDigits 的类型断言兜底分支。
type wrongTypeConfigService struct {
	systemService.ConfigService
}

func (wrongTypeConfigService) FindByKey(string) (interface{}, error) {
	return "不是 *SysConfig", nil
}
