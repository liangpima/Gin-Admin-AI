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

	"gorm.io/gorm"
)

// ⚠️ 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。
const (
	tenantA uint = 1
	tenantB uint = 2
)

// newMemberDB 建一个包含 member 模块全部表的内存库并注入 database.DB。
//
// 单独抽出来是因为同一包内既有「构造完整 memberService」的用例，也有
// 只测某个薄封装 Service 的用例；两边共用一份建表清单，避免日后新增表时
// 只改了一处，另一处的用例因为「表不存在」而以奇怪的方式失败。
func newMemberDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 默认把 Redis 置空：会员编号生成有「Redis 计数器」与「按库内最大值推导」
	// 两条分支，不固定 Redis 状态的话，同一批用例在不同机器上（甚至同一机器
	// 连续两次运行之间）走的分支不同 —— 覆盖率会摆动，断言也不再可复现。
	// 需要真实 Redis 的用例自己调 testsupport.WithTestRedis 覆盖掉。
	testsupport.WithNilRedis(t)

	// 先建库（会注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	// SysConfig 也要建：Create 会读取「会员编号位数」配置
	return testsupport.NewDB(t,
		&model.Member{}, &model.MemberLevel{}, &model.MemberTag{}, &model.MemberTagRel{},
		&model.PointsLog{}, &systemModel.SysConfig{})
}

func newTestMemberService(t *testing.T) *memberService {
	t.Helper()
	newMemberDB(t)
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

// newMemberWithProfile 创建一个字段饱满的会员：性别、生日、备注、等级、标签都有值，
// 用于验证「部分更新不得清掉未提供的字段」。
func newMemberWithProfile(t *testing.T, s *memberService, phone string, levelID uint) *model.Member {
	t.Helper()
	birthday := "1990-05-20"
	req := baseCreateReq(phone)
	req.Gender = 2
	req.Birthday = birthday
	req.LevelID = levelID
	req.Remark = "重要会员"
	if err := s.Create(req, 1, tenantA); err != nil {
		t.Fatalf("创建会员失败: %v", err)
	}
	m, err := s.memberRepo.FindByPhone(tenantA, phone)
	if err != nil || m == nil {
		t.Fatalf("会员未创建: %v", err)
	}
	return m
}

// TestUpdateMemberLevelOnlyKeepsOtherFields 「只改等级」事故回归。
//
// 事故实测记录：前端「修改等级」只提交 {id, levelId}，旧实现无条件赋值
// 把 status 从 1 清成 0（会员被静默停用）、gender 从 2 清成 0。
// 本用例复现该请求形状，锁定「未提供的字段必须原样保留」。
// 如果实现被改回无条件赋值，这里会红。
func TestUpdateMemberLevelOnlyKeepsOtherFields(t *testing.T) {
	s := newTestMemberService(t)
	level := seedLevel(t, tenantA, "白金会员")
	m := newMemberWithProfile(t, s, "13800000021", 0)

	err := s.Update(&dto.UpdateMemberRequest{ID: m.ID, LevelID: &level.ID}, 1, tenantA)
	if err != nil {
		t.Fatalf("只提交等级应更新成功: %v", err)
	}

	got, err := s.memberRepo.FindByID(tenantA, m.ID)
	if err != nil {
		t.Fatalf("回读会员失败: %v", err)
	}
	if got.LevelID != level.ID {
		t.Errorf("等级未更新：期望 %d，实际 %d", level.ID, got.LevelID)
	}
	if got.Status != 1 {
		t.Errorf("status 被部分更新清成停用（复现了历史事故）：%d", got.Status)
	}
	if got.Gender != 2 {
		t.Errorf("gender 被部分更新清零（复现了历史事故）：%d", got.Gender)
	}
	if got.Nickname != "测试会员" {
		t.Errorf("nickname 不应变化: %q", got.Nickname)
	}
	if got.Remark != "重要会员" {
		t.Errorf("remark 不应变化: %q", got.Remark)
	}
	if got.Birthday == nil {
		t.Error("birthday 不应被清掉")
	}
	if got.MemberNo != m.MemberNo {
		t.Errorf("会员编号不应变化: %q -> %q", m.MemberNo, got.MemberNo)
	}
}

// TestUpdateMemberExplicitZeroStatus 「显式停用」必须生效。
//
// 指针方案的另一半约束：放宽对零值的校验之后，用户仍要能把状态改成 0。
// 若实现把零值当「未提供」跳过，停用操作会静默失败。
func TestUpdateMemberExplicitZeroStatus(t *testing.T) {
	s := newTestMemberService(t)
	m := newMemberWithProfile(t, s, "13800000022", 0)

	status := int8(0)
	err := s.Update(&dto.UpdateMemberRequest{ID: m.ID, Status: &status}, 1, tenantA)
	if err != nil {
		t.Fatalf("停用会员应成功: %v", err)
	}

	got, _ := s.memberRepo.FindByID(tenantA, m.ID)
	if got.Status != 0 {
		t.Errorf("显式 status=0 未生效，实际 %d", got.Status)
	}
	if got.Gender != 2 {
		t.Errorf("只改状态不应影响性别: %d", got.Gender)
	}
}

// TestUpdateMemberEmptyBirthdayClears 生日指针的三态语义：
// nil 不动、非空串设置、空串清空。
func TestUpdateMemberEmptyBirthdayClears(t *testing.T) {
	s := newTestMemberService(t)
	m := newMemberWithProfile(t, s, "13800000023", 0)
	if m.Birthday == nil {
		t.Fatal("准备数据失败：生日应为已设置状态")
	}

	empty := ""
	if err := s.Update(&dto.UpdateMemberRequest{ID: m.ID, Birthday: &empty}, 1, tenantA); err != nil {
		t.Fatalf("清空生日应成功: %v", err)
	}
	got, _ := s.memberRepo.FindByID(tenantA, m.ID)
	if got.Birthday != nil {
		t.Errorf("传空串应清空生日，实际 %v", *got.Birthday)
	}
}

// TestUpdateMemberEmptyTagIdsClears 标签的 nil / 空切片语义区分：
// TagIds 缺省（nil）不动标签；显式传 [] 才清空。
func TestUpdateMemberEmptyTagIdsClears(t *testing.T) {
	s := newTestMemberService(t)
	tag := seedTag(t, tenantA, "待清空标签")
	req := baseCreateReq("13800000024")
	req.TagIds = []uint{tag.ID}
	if err := s.Create(req, 1, tenantA); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	m, _ := s.memberRepo.FindByPhone(tenantA, req.Phone)

	// 只改昵称：标签必须原样保留
	if err := s.Update(&dto.UpdateMemberRequest{ID: m.ID, Nickname: "改名"}, 1, tenantA); err != nil {
		t.Fatalf("只改昵称应成功: %v", err)
	}
	tagIDs, _ := s.memberRepo.FindTagIDsByMemberID(tenantA, m.ID)
	if len(tagIDs) != 1 {
		t.Fatalf("未提供 tagIds 时标签不应变化: %v", tagIDs)
	}

	// 显式空切片：清空
	if err := s.Update(&dto.UpdateMemberRequest{ID: m.ID, TagIds: []uint{}}, 1, tenantA); err != nil {
		t.Fatalf("清空标签应成功: %v", err)
	}
	tagIDs, _ = s.memberRepo.FindTagIDsByMemberID(tenantA, m.ID)
	if len(tagIDs) != 0 {
		t.Errorf("传空切片应清空标签，实际 %v", tagIDs)
	}
}

// TestUpdateMemberCrossTenantRejected 拿其他租户的会员 ID 更新必须失败。
func TestUpdateMemberCrossTenantRejected(t *testing.T) {
	s := newTestMemberService(t)
	req := baseCreateReq("13800000025")
	if err := s.Create(req, 1, tenantB); err != nil {
		t.Fatalf("准备数据失败: %v", err)
	}
	m, _ := s.memberRepo.FindByPhone(tenantB, req.Phone)

	nickname := "劫持"
	err := s.Update(&dto.UpdateMemberRequest{ID: m.ID, Nickname: nickname}, 1, tenantA)
	if err == nil {
		t.Fatal("更新其他租户的会员应失败")
	}
	got, _ := s.memberRepo.FindByID(tenantB, m.ID)
	if got.Nickname == nickname {
		t.Error("跨租户更新竟然生效了")
	}
}

// TestCreatePhoneDuplicateCheck 手机号查重的两种结果必须区分开。
//
// 背景：这里原先写的是 `existing, _ := s.memberRepo.FindByPhone(...)`，
// 而 FindByPhone 无论查没查到都返回非 nil 指针（&member, err）——
// 吞掉 err 等于把「数据库故障」误判成「手机号没被注册」，查重被静默跳过。
// 这正是 dict_service 注释里点名警告过的反模式（同一坑踩过第二次）。
func TestCreatePhoneDuplicateCheck(t *testing.T) {
	t.Run("手机号已注册返回业务错误", func(t *testing.T) {
		svc := newTestMemberService(t)

		if err := svc.Create(&dto.CreateMemberRequest{Phone: "13800000001"}, 1, tenantA); err != nil {
			t.Fatalf("首个会员应创建成功: %v", err)
		}
		err := svc.Create(&dto.CreateMemberRequest{Phone: "13800000001"}, 1, tenantA)

		if err == nil {
			t.Fatal("重复手机号必须被拒绝")
		}
		if !common.IsBizError(err) {
			t.Errorf("重名属业务错误（400），实际 %T: %v", err, err)
		}
		if !strings.Contains(err.Error(), "手机号") {
			t.Errorf("提示应点明是手机号冲突，实际: %v", err)
		}
	})

	t.Run("查库失败必须上抛而不是当成未注册", func(t *testing.T) {
		// 用桩仓储精确制造「只有查重这一步失败」：
		// 直接删表是测不出来的 —— 那样后续 INSERT 也会失败，
		// 有缺陷的实现（吞掉查重错误继续往下走）与修复后的实现
		// 最终都会返回一个系统错误，断言无法区分。
		stub := &stubMemberRepo{findByPhoneErr: errors.New("db down")}
		svc := newTestMemberService(t)
		svc.memberRepo = stub

		err := svc.Create(&dto.CreateMemberRequest{Phone: "13800000002"}, 1, tenantA)
		if err == nil {
			t.Fatal("数据库故障时必须上抛，不能当成「手机号未注册」继续创建")
		}
		if common.IsBizError(err) {
			t.Errorf("数据库故障属系统错误（500），不该包装成业务错误: %v", err)
		}
		if stub.created != 0 {
			t.Error("查重失败时不应继续创建会员（否则查重形同虚设）")
		}
	})
}

// stubMemberRepo 只实现查重 / 编号推导路径用到的三个方法。
// 内嵌 repository.MemberRepository 接口来满足类型要求：
// 未实现的方法一旦被调用会 panic，这正好能暴露「测试路径与预期不符」。
type stubMemberRepo struct {
	repository.MemberRepository
	findByPhoneErr error
	findMaxNoErr   error
	maxMemberNo    string
	created        int
}

func (s *stubMemberRepo) FindByPhone(tenantID uint, phone string) (*model.Member, error) {
	return &model.Member{}, s.findByPhoneErr
}

func (s *stubMemberRepo) FindMaxMemberNo(tenantID uint) (string, error) {
	return s.maxMemberNo, s.findMaxNoErr
}

func (s *stubMemberRepo) Create(*model.Member) error {
	s.created++
	return nil
}
