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

	"github.com/go-sql-driver/mysql"
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
	db := testsupport.NewDB(t,
		&model.Member{}, &model.MemberLevel{}, &model.MemberTag{}, &model.MemberTagRel{},
		&model.PointsLog{}, &systemModel.SysConfig{})

	// 手机号的唯一约束是 (tenant_id, phone) 复合索引，而模型无法用标签表达它
	// （TenantID 定义在嵌入的 common.TenantBaseModel 里），AutoMigrate 建不出来。
	// 测试里显式补上，让测试 schema 与 sql/init.sql 一致 ——
	// 否则「不同租户可各自使用同一手机号」这条路径根本没被约束到，
	// 用例会绿得毫无意义（与 system/repository 里 sys_post 的处理同一套路）。
	if err := db.Exec(
		"CREATE UNIQUE INDEX IF NOT EXISTS uk_tenant_phone ON pay_member (tenant_id, phone)",
	).Error; err != nil {
		t.Fatalf("补建手机号唯一索引失败: %v", err)
	}
	return db
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

// TestDedupeNonZeroIDs 已随实现上移到 internal/common/collection_test.go。
// 这里不再保留一份 —— 同一个纯函数在两个包里各测一遍，只会让
// 「两处实现已经悄悄不同」这件事更难发现。

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

// TestCreateMemberSamePhoneAcrossTenants 手机号是**租户内**唯一，不是全平台唯一。
//
// 回归的是 H13。原索引是全局的 `uk_phone(phone)`，而查重
// （`FindByPhone(tenantID, ...)`）按租户过滤 —— 两边范围不一致，现象是：
// 租户 B 用租户 A 已注册的手机号建会员 → 查重查不到（放行）→ INSERT 撞唯一索引
// → 1062 → 对外 500「服务器内部错误」，提示与真实原因毫无关系。
//
// 断言落在「两边都能建成」上，而不是只对着索引名断言 —— 索引范围是否与
// 应用层语义一致，实际发生地就是这里。
func TestCreateMemberSamePhoneAcrossTenants(t *testing.T) {
	svc := newTestMemberService(t)
	const shared = "13800008888"

	if err := svc.Create(&dto.CreateMemberRequest{Phone: shared}, 1, tenantA); err != nil {
		t.Fatalf("甲租户创建失败: %v", err)
	}
	if err := svc.Create(&dto.CreateMemberRequest{Phone: shared}, 1, tenantB); err != nil {
		t.Fatalf("乙租户使用同一手机号必须成功，实际: %v", err)
	}

	// 反向对照：同一租户内仍然必须被拒。
	// 没有这一条，上面两条断言在「唯一索引根本没建」时照样会通过。
	if err := svc.Create(&dto.CreateMemberRequest{Phone: shared}, 1, tenantA); err == nil ||
		!common.IsBizError(err) {
		t.Fatalf("同租户内重复手机号必须返回业务错误，实际 %T: %v", err, err)
	}

	// 两个租户各自都能按手机号查到自己的那一条（而不是互相查到对方）
	a, err := svc.memberRepo.FindByPhone(tenantA, shared)
	if err != nil || a.TenantID != tenantA {
		t.Fatalf("甲租户按手机号查询异常: %v (tenant=%d)", err, a.TenantID)
	}
	b, err := svc.memberRepo.FindByPhone(tenantB, shared)
	if err != nil || b.TenantID != tenantB {
		t.Fatalf("乙租户按手机号查询异常: %v (tenant=%d)", err, b.TenantID)
	}
	if a.ID == b.ID {
		t.Error("两个租户查到了同一条会员记录")
	}
}

// TestUpdateMemberPhoneDuplicateCheck 改手机号必须查重，且范围是**租户内**。
//
// 原先 `Update` 直接赋值 `member.Phone` 就落库：改成本租户内另一个会员已占用的号
// 会一路撞到唯一索引 → 1062 → `common.FailWith` 归一成 500「服务器内部错误」。
// 用户看到的提示与真实原因（手机号冲突）毫无关系，只会以为系统坏了。
func TestUpdateMemberPhoneDuplicateCheck(t *testing.T) {
	t.Run("改成同租户已占用的手机号 → 业务错误，且不落库", func(t *testing.T) {
		svc := newTestMemberService(t)
		if err := svc.Create(&dto.CreateMemberRequest{Phone: "13800000001"}, 1, tenantA); err != nil {
			t.Fatalf("准备：建甲失败: %v", err)
		}
		if err := svc.Create(&dto.CreateMemberRequest{Phone: "13800000002"}, 1, tenantA); err != nil {
			t.Fatalf("准备：建乙失败: %v", err)
		}
		target, _ := svc.memberRepo.FindByPhone(tenantA, "13800000002")

		err := svc.Update(&dto.UpdateMemberRequest{ID: target.ID, Phone: "13800000001"}, 1, tenantA)

		if err == nil {
			t.Fatal("改成同租户已占用的手机号必须被拒")
		}
		if !common.IsBizError(err) {
			t.Errorf("手机号冲突属业务错误（400），实际 %T: %v", err, err)
		}
		if !strings.Contains(err.Error(), "手机号") {
			t.Errorf("提示应点明是手机号冲突，实际: %v", err)
		}
		// 被拒的写操作必须**真的没落库**，否则「报错了但改了」更难查
		got, _ := svc.memberRepo.FindByID(tenantA, target.ID)
		if got.Phone != "13800000002" {
			t.Errorf("被拒的改号竟然生效了，当前手机号 %s", got.Phone)
		}
	})

	t.Run("改成跨租户占用的手机号 → 允许（租户内唯一的既定语义）", func(t *testing.T) {
		// 反向对照：查重**不能**改成全平台唯一。手机号是租户内的业务标识，
		// 两个租户各自拥有同一手机号是允许的（见 model.Member 的说明）。
		svc := newTestMemberService(t)
		if err := svc.Create(&dto.CreateMemberRequest{Phone: "13800000001"}, 1, tenantA); err != nil {
			t.Fatalf("准备：建甲租户会员失败: %v", err)
		}
		if err := svc.Create(&dto.CreateMemberRequest{Phone: "13800000002"}, 1, tenantB); err != nil {
			t.Fatalf("准备：建乙租户会员失败: %v", err)
		}
		target, _ := svc.memberRepo.FindByPhone(tenantB, "13800000002")

		if err := svc.Update(&dto.UpdateMemberRequest{ID: target.ID, Phone: "13800000001"}, 1, tenantB); err != nil {
			t.Fatalf("跨租户同号应当允许，实际被拒: %v", err)
		}
	})

	t.Run("提交自己原来的手机号 → 不报错", func(t *testing.T) {
		// 「未变化」不该被查重拦下：编辑弹窗会把整行回填后原样提交，
		// 若查重不排除自己，任何一次「只改昵称」都会报「手机号已注册」。
		svc := newTestMemberService(t)
		if err := svc.Create(&dto.CreateMemberRequest{Phone: "13800000001"}, 1, tenantA); err != nil {
			t.Fatalf("准备失败: %v", err)
		}
		target, _ := svc.memberRepo.FindByPhone(tenantA, "13800000001")

		if err := svc.Update(&dto.UpdateMemberRequest{ID: target.ID, Phone: "13800000001"}, 1, tenantA); err != nil {
			t.Fatalf("提交未变化的手机号不应报错: %v", err)
		}
	})

	t.Run("同租户冲突在**预检查**阶段就拦下（不进入写操作）", func(t *testing.T) {
		// 为什么必须单独钉这一条：只加「唯一键冲突兜底」也能让上面那条用例通过
		// （索引会把写入拒掉，错误一样被翻译成业务错误），但那样每次冲突都真的
		// 写了一次库、且 `pay_member` 上另一个唯一索引（uk_member_no）的冲突
		// 也会被说成「手机号已注册」—— 提示指错方向。
		// 这里断言的是「预检查命中时 Update 根本没被调用」。
		svc := newTestMemberService(t)
		stub := &stubMemberRepo{
			// 查重命中另一个会员
			existingMember: &model.Member{
				TenantBaseModel: common.TenantBaseModel{BaseModel: common.BaseModel{ID: 99}},
				Phone:           "13800000001",
			},
			member: &model.Member{
				TenantBaseModel: common.TenantBaseModel{BaseModel: common.BaseModel{ID: 7}},
				Phone:           "13800000000",
			},
		}
		svc.memberRepo = stub

		err := svc.Update(&dto.UpdateMemberRequest{ID: 7, Phone: "13800000001"}, 1, tenantA)

		if !common.IsBizError(err) {
			t.Fatalf("应返回业务错误，实际 %T: %v", err, err)
		}
		if stub.updated != 0 {
			t.Errorf("预检查命中时不应再调用 Update（实际调用 %d 次）", stub.updated)
		}
	})
}

// TestDuplicateKeyTranslatedToBizError 并发下预检查会漏，唯一索引才是最终裁判。
//
// 「先查再写」两步之间并发请求可以插进来，所以除了预检查还必须有一层兜底：
// 仓储返回的唯一键冲突要翻译成业务错误；而**其它**仓储错误必须原样上抛，
// 否则数据库故障会被说成「手机号已注册」，监控按 5xx 告警的能力随之失效。
//
// 用真实 MySQL 错误码（1062）构造，匹配 `database.IsDuplicateKey` 的判定路径。
func TestDuplicateKeyTranslatedToBizError(t *testing.T) {
	dupErr := &mysql.MySQLError{
		Number:  1062,
		Message: "Duplicate entry '1-13800000001' for key 'uk_tenant_phone'",
	}

	// 预检查放行的桩：FindByPhone 报「未找到」，把冲突留给写操作
	newStubService := func(updateErr error) (*memberService, *stubMemberRepo) {
		svc := newTestMemberService(t)
		stub := &stubMemberRepo{
			findByPhoneErr: gorm.ErrRecordNotFound,
			updateErr:      updateErr,
			member: &model.Member{
				TenantBaseModel: common.TenantBaseModel{BaseModel: common.BaseModel{ID: 7}},
				Phone:           "13800000000",
				Status:          1,
			},
		}
		svc.memberRepo = stub
		return svc, stub
	}

	t.Run("改号撞唯一索引 → 业务错误", func(t *testing.T) {
		svc, _ := newStubService(dupErr)

		err := svc.Update(&dto.UpdateMemberRequest{ID: 7, Phone: "13800000001"}, 1, tenantA)

		if err == nil {
			t.Fatal("唯一键冲突必须被翻译，不能原样透出")
		}
		if !common.IsBizError(err) {
			t.Errorf("唯一键冲突属业务错误（400），实际 %T: %v", err, err)
		}
	})

	t.Run("创建撞唯一索引 → 业务错误（同一个坑的另一条入口）", func(t *testing.T) {
		svc := newTestMemberService(t)
		svc.memberRepo = &stubMemberRepo{
			findByPhoneErr: gorm.ErrRecordNotFound,
			maxMemberNo:    "100001",
			createErr:      dupErr,
		}

		err := svc.Create(&dto.CreateMemberRequest{Phone: "13800000001"}, 1, tenantA)

		if err == nil || !common.IsBizError(err) {
			t.Fatalf("创建时的唯一键冲突也必须翻译成业务错误，实际 %T: %v", err, err)
		}
	})

	t.Run("非唯一键的仓储错误必须原样上抛", func(t *testing.T) {
		svc, _ := newStubService(errors.New("db down"))

		err := svc.Update(&dto.UpdateMemberRequest{ID: 7, Phone: "13800000001"}, 1, tenantA)

		if err == nil {
			t.Fatal("数据库故障必须上抛")
		}
		if common.IsBizError(err) {
			t.Errorf("数据库故障是系统错误（500），不该说成「手机号已注册」: %v", err)
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
	// existingMember 非 nil 时由 FindByPhone 返回它（模拟「查到了一条记录」）
	existingMember *model.Member
	// member 是 FindByID 的返回值；为 nil 表示记录不存在
	member *model.Member
	// createErr / updateErr 让用例直接制造「仓储写失败」，例如唯一键冲突
	createErr error
	updateErr error
	// updated 记录 Update 被调用的次数：用来区分「预检查拦下」与「写下去才失败」
	updated int
}

func (s *stubMemberRepo) FindByPhone(tenantID uint, phone string) (*model.Member, error) {
	if s.existingMember != nil {
		return s.existingMember, s.findByPhoneErr
	}
	return &model.Member{}, s.findByPhoneErr
}

func (s *stubMemberRepo) FindMaxMemberNo() (string, error) {
	return s.maxMemberNo, s.findMaxNoErr
}

func (s *stubMemberRepo) FindByID(tenantID, id uint) (*model.Member, error) {
	if s.member == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return s.member, nil
}

func (s *stubMemberRepo) Create(*model.Member) error {
	s.created++
	return s.createErr
}

func (s *stubMemberRepo) Update(*model.Member) error {
	s.updated++
	return s.updateErr
}
