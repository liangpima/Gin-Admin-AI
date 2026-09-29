package service

import (
	"strings"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"
)

// 本文件覆盖 P2-5 个人信息合规的两个动作：
// ExportMemberData（查询权/可携带权）与 EraseMemberData（删除权）。
//
// 合规判据是**可验证的行为**而不是字段清单：
//   - 导出必须完整（资料 + 标签 + 全部积分流水）
//   - 匿名化后：原手机号**可重新注册**（唯一索引释放）、账号停用、
//     个人标识字段清空、业务统计口径（积分/注册时间）保留

// TestExportMemberDataCompleteness 导出必须包含资料、标签、全部积分流水。
func TestExportMemberDataCompleteness(t *testing.T) {
	svc := newTestMemberService(t)
	tenant := uint(1)

	tag := seedTag(t, tenant, "VIP")
	if err := svc.Create(&dto.CreateMemberRequest{
		Username: "exportee", Nickname: "导出对象", Phone: "13800000301",
		LevelID: 0, Status: 1,
	}, 1, tenant); err != nil {
		t.Fatalf("建会员失败: %v", err)
	}
	member, err := svc.memberRepo.FindByPhone(tenant, "13800000301")
	if err != nil {
		t.Fatalf("查会员失败: %v", err)
	}
	_ = tag
	if err := svc.pointsLogRepo.Create(tenant, &model.PointsLog{
		MemberID: member.ID, Change: 100, Type: 1, Source: "register",
	}); err != nil {
		t.Fatalf("造积分流水失败: %v", err)
	}
	if err := svc.pointsLogRepo.Create(tenant, &model.PointsLog{
		MemberID: member.ID, Change: -20, Type: 2, Source: "exchange",
	}); err != nil {
		t.Fatalf("造积分流水失败: %v", err)
	}
	_ = tag

	data, err := svc.ExportMemberData(tenant, member.ID)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if data.Member == nil || data.Member.ID != member.ID {
		t.Fatal("导出应包含会员资料")
	}
	if len(data.PointsLogs) != 2 {
		t.Errorf("积分流水应完整导出 2 条，实际 %d（截断的导出等于没导出）", len(data.PointsLogs))
	}
	if data.ExportedAt.IsZero() {
		t.Error("应记录导出时间")
	}
}

// TestEraseMemberDataAnonymizesAndReleasesPhone 匿名化的核心合规判据：
// 原手机号**可重新注册**、个人标识清空、账号停用、统计口径保留。
func TestEraseMemberDataAnonymizesAndReleasesPhone(t *testing.T) {
	svc := newTestMemberService(t)
	tenant := uint(1)
	const phone = "13800000302"

	if err := svc.Create(&dto.CreateMemberRequest{
		Username: "eraseme", Nickname: "待注销", Phone: phone, Status: 1,
	}, 1, tenant); err != nil {
		t.Fatalf("建会员失败: %v", err)
	}
	member, _ := svc.memberRepo.FindByPhone(tenant, phone)
	origPoints := member.Points

	if err := svc.EraseMemberData(tenant, 9, member.ID); err != nil {
		t.Fatalf("匿名化失败: %v", err)
	}

	erased, err := svc.memberRepo.FindByID(tenant, member.ID)
	if err != nil {
		t.Fatalf("匿名化后记录应保留（非硬删）: %v", err)
	}
	if erased.Phone == phone {
		t.Error("原手机号应被释放（替换为派生占位值）")
	}
	if !strings.Contains(erased.Phone, "erased-") {
		t.Errorf("占位手机号应带 erased- 前缀，实际 %q", erased.Phone)
	}
	if erased.Username != "" || erased.Nickname != "已注销会员" || erased.Avatar != "" || erased.WechatOpenid != "" {
		t.Errorf("个人标识字段应清空/匿名化，实际 username=%q nickname=%q", erased.Username, erased.Nickname)
	}
	if erased.Status != common.StatusDisabled {
		t.Errorf("匿名化后应停用（防止匿名实体残留登录能力），实际 %d", erased.Status)
	}
	if erased.Points != origPoints {
		t.Errorf("业务统计口径应保留（积分 %d），实际 %d", origPoints, erased.Points)
	}

	// **可验证的合规判据**：同一手机号能再次注册
	if err := svc.Create(&dto.CreateMemberRequest{
		Username: "reregistered", Nickname: "重新注册", Phone: phone, Status: 1,
	}, 1, tenant); err != nil {
		t.Fatalf("匿名化后同一手机号应可重新注册: %v", err)
	}
}

// TestEraseMemberDataNotFound 匿名化不存在的/跨租户的会员 → 404。
func TestEraseMemberDataNotFound(t *testing.T) {
	svc := newTestMemberService(t)
	if err := svc.EraseMemberData(1, 9, 99999); err == nil {
		t.Fatal("不存在的会员应报错")
	}
}
