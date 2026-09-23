package service

import (
	"errors"
	"strings"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"
	"go-admin/internal/module/member/repository"
	systemModel "go-admin/internal/module/system/model"
	systemService "go-admin/internal/module/system/service"
	"go-admin/internal/testsupport"
)

// ⚠️ 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。
const (
	tenantA uint = 1
	tenantB uint = 2
)

func newTestMemberService(t *testing.T) *memberService {
	t.Helper()
	// 先建库（会注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	// SysConfig 也要建：Create 会读取「会员编号位数」配置
	testsupport.NewDB(t,
		&model.Member{}, &model.MemberLevel{}, &model.MemberTag{}, &model.MemberTagRel{},
		&systemModel.SysConfig{})
	return &memberService{
		memberRepo:    repository.NewMemberRepository(),
		tagRepo:       repository.NewMemberTagRepository(),
		levelRepo:     repository.NewMemberLevelRepository(),
		configService: systemService.NewConfigService(),
	}
}

func seedTag(t *testing.T, tenantID uint, name string) *model.MemberTag {
	t.Helper()
	tag := &model.MemberTag{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Name:            name,
	}
	if err := repository.NewMemberTagRepository().Create(tag); err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}
	return tag
}

func seedLevel(t *testing.T, tenantID uint, name string) *model.MemberLevel {
	t.Helper()
	level := &model.MemberLevel{
		TenantBaseModel: common.TenantBaseModel{TenantID: tenantID},
		Name:            name,
	}
	if err := repository.NewMemberLevelRepository().Create(level); err != nil {
		t.Fatalf("创建等级失败: %v", err)
	}
	return level
}

func baseCreateReq(phone string) *dto.CreateMemberRequest {
	return &dto.CreateMemberRequest{
		Username: "u_" + phone,
		Nickname: "测试会员",
		Phone:    phone,
		Gender:   0,
		Status:   1,
	}
}

// TestDedupeNonZeroIDs 去重并剔除 0。
//
// 0 不是合法主键，前端下拉框未选择时常传 0；写进关联表会造出一条
// 指向不存在标签的脏记录。
func TestDedupeNonZeroIDs(t *testing.T) {
	cases := []struct {
		name string
		in   []uint
		want []uint
	}{
		{"空输入", nil, nil},
		{"全为 0", []uint{0, 0}, nil},
		{"去重", []uint{3, 1, 3, 2, 1}, []uint{3, 1, 2}},
		{"剔除 0 并去重", []uint{0, 5, 0, 5, 7}, []uint{5, 7}},
		{"保持首次出现顺序", []uint{9, 4, 9}, []uint{9, 4}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dedupeNonZeroIDs(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("长度不符: got %v, want %v", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("顺序/内容不符: got %v, want %v", got, c.want)
				}
			}
		})
	}
}

// TestNormalizeTagIDsRejectsOtherTenant 跨租户标签必须被拒绝。
//
// pay_member_tag_rel 是纯关联表（没有 tenant_id），租户隔离无法靠 TenantScope
// 完成。不做校验时，租户 A 的管理员枚举 tag_id 就能把租户 B 的标签绑到
// 自己会员上，破坏隔离与数据一致性。
func TestNormalizeTagIDsRejectsOtherTenant(t *testing.T) {
	s := newTestMemberService(t)
	foreign := seedTag(t, tenantB, "别的租户的标签")
	own := seedTag(t, tenantA, "自己的标签")

	// 混合：合法 + 越权，必须整体拒绝（不能只挑合法的用）
	_, err := s.normalizeTagIDs(tenantA, []uint{own.ID, foreign.ID})
	if err == nil {
		t.Fatal("包含其他租户的标签时应报错")
	}
	var bizErr *common.BizError
	if !errors.As(err, &bizErr) {
		t.Fatalf("应是业务错误（400 语义），实际: %T %v", err, err)
	}

	// 错误文案不能点名是哪个 ID 不合法，否则可被用来探测其他租户的 ID
	if msg := err.Error(); strings.Contains(msg, foreign.Name) {
		t.Errorf("错误信息泄露了其他租户的标签名: %s", msg)
	}
}

// TestNormalizeTagIDsAcceptsOwnTenant 本租户标签正常通过，并完成去重。
func TestNormalizeTagIDsAcceptsOwnTenant(t *testing.T) {
	s := newTestMemberService(t)
	t1 := seedTag(t, tenantA, "标签一")
	t2 := seedTag(t, tenantA, "标签二")

	got, err := s.normalizeTagIDs(tenantA, []uint{t1.ID, t2.ID, t1.ID, 0})
	if err != nil {
		t.Fatalf("本租户标签不应报错: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应去重为 2 个，实际 %v", got)
	}

	// 空输入直接返回 nil，不触发查询
	if got, err := s.normalizeTagIDs(tenantA, nil); err != nil || got != nil {
		t.Errorf("空标签应返回 (nil, nil)，实际 (%v, %v)", got, err)
	}
}

// TestNormalizeLevelID 等级归属校验。
func TestNormalizeLevelID(t *testing.T) {
	s := newTestMemberService(t)
	own := seedLevel(t, tenantA, "本租户等级")
	foreign := seedLevel(t, tenantB, "别的租户等级")

	// 0 表示不设等级，直接放行且不查询
	if got, err := s.normalizeLevelID(tenantA, 0); err != nil || got != 0 {
		t.Errorf("levelID=0 应放行，实际 (%d, %v)", got, err)
	}

	if got, err := s.normalizeLevelID(tenantA, own.ID); err != nil || got != own.ID {
		t.Errorf("本租户等级应通过，实际 (%d, %v)", got, err)
	}

	if _, err := s.normalizeLevelID(tenantA, foreign.ID); err == nil {
		t.Error("其他租户的等级应被拒绝")
	}
}

// TestCreateRejectsCrossTenantTagWithoutWritingMember 校验必须发生在落库之前。
//
// 这是本模块最关键的行为断言：若先建会员再校验标签，越权请求虽然返回失败，
// 库里却留下了一条没有标签的会员 —— 调用方只看到失败，不会知道已经写了数据。
func TestCreateRejectsCrossTenantTagWithoutWritingMember(t *testing.T) {
	s := newTestMemberService(t)
	foreign := seedTag(t, tenantB, "别的租户的标签")

	req := baseCreateReq("13800000001")
	req.TagIds = []uint{foreign.ID}

	if err := s.Create(req, 1, tenantA); err == nil {
		t.Fatal("使用其他租户的标签建会员应报错")
	}

	// 会员表里不能留下任何记录
	if m, _ := s.memberRepo.FindByPhone(tenantA, req.Phone); m != nil && m.ID > 0 {
		t.Fatalf("校验失败却写入了会员（半成品数据）: id=%d", m.ID)
	}
}

// TestCreateRejectsCrossTenantLevel 越权等级同样必须拒绝且不落库。
func TestCreateRejectsCrossTenantLevel(t *testing.T) {
	s := newTestMemberService(t)
	foreign := seedLevel(t, tenantB, "别的租户的等级")

	req := baseCreateReq("13800000002")
	req.LevelID = foreign.ID

	if err := s.Create(req, 1, tenantA); err == nil {
		t.Fatal("使用其他租户的等级建会员应报错")
	}
	if m, _ := s.memberRepo.FindByPhone(tenantA, req.Phone); m != nil && m.ID > 0 {
		t.Fatalf("校验失败却写入了会员: id=%d", m.ID)
	}
}

// TestCreateAcceptsOwnTenantAssociations 本租户的等级与标签应正常绑定。
func TestCreateAcceptsOwnTenantAssociations(t *testing.T) {
	s := newTestMemberService(t)
	level := seedLevel(t, tenantA, "黄金会员")
	tag1 := seedTag(t, tenantA, "标签一")
	tag2 := seedTag(t, tenantA, "标签二")

	req := baseCreateReq("13800000003")
	req.LevelID = level.ID
	req.TagIds = []uint{tag1.ID, tag2.ID}

	if err := s.Create(req, 1, tenantA); err != nil {
		t.Fatalf("本租户的等级与标签应能创建成功: %v", err)
	}

	m, err := s.memberRepo.FindByPhone(tenantA, req.Phone)
	if err != nil || m == nil {
		t.Fatalf("会员未创建成功: %v", err)
	}
	if m.LevelID != level.ID {
		t.Errorf("等级未绑定，期望 %d 实际 %d", level.ID, m.LevelID)
	}
	if len(m.MemberNo) != 6 {
		t.Errorf("会员编号位数应为 6，实际 %q", m.MemberNo)
	}

	tagIDs, err := s.memberRepo.FindTagIDsByMemberID(tenantA, m.ID)
	if err != nil {
		t.Fatalf("查询会员标签失败: %v", err)
	}
	if len(tagIDs) != 2 {
		t.Errorf("应绑定 2 个标签，实际 %d 个: %v", len(tagIDs), tagIDs)
	}
}

// TestUpdateTagsRejectsCrossTenant 单独的改标签接口同样要挡住越权。
func TestUpdateTagsRejectsCrossTenant(t *testing.T) {
	s := newTestMemberService(t)
	own := seedTag(t, tenantA, "自己的标签")
	foreign := seedTag(t, tenantB, "别的租户的标签")

	req := baseCreateReq("13800000004")
	req.TagIds = []uint{own.ID}
	if err := s.Create(req, 1, tenantA); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	m, _ := s.memberRepo.FindByPhone(tenantA, req.Phone)

	err := s.UpdateTags(tenantA, &dto.UpdateMemberTagsRequest{ID: m.ID, TagIds: []uint{foreign.ID}})
	if err == nil {
		t.Fatal("改用其他租户的标签应报错")
	}

	// 原有标签不能被清掉：校验必须发生在删除旧关联之前
	tagIDs, _ := s.memberRepo.FindTagIDsByMemberID(tenantA, m.ID)
	if len(tagIDs) != 1 || tagIDs[0] != own.ID {
		t.Errorf("校验失败后原有标签被破坏: %v", tagIDs)
	}
}
