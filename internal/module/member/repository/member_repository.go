package repository

import (
	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/member/model"

	"gorm.io/gorm"
)

type MemberRepository interface {
	Create(member *model.Member) error
	Update(member *model.Member) error
	Delete(tenantID, id uint) error
	FindByID(tenantID, id uint) (*model.Member, error)
	FindByPhone(tenantID uint, phone string) (*model.Member, error)
	FindByWechatOpenid(tenantID uint, openid string) (*model.Member, error)
	FindList(tenantID uint, phone, nickname string, levelID uint, status int8, page, pageSize int) ([]model.Member, int64, error)
	UpdateStatus(tenantID, id uint, status int8) error
	ReplaceTags(tenantID, memberID uint, tagIDs []uint) error
	FindTagIDsByMemberID(tenantID, memberID uint) ([]uint, error)
	UpdatePoints(tenantID, memberID uint, points int64) error
	FindMaxMemberNo() (string, error)
}

type memberRepository struct{}

func NewMemberRepository() MemberRepository {
	return &memberRepository{}
}

func (r *memberRepository) Create(member *model.Member) error {
	return database.DB.Create(member).Error
}

func (r *memberRepository) Update(member *model.Member) error {
	return database.DB.Save(member).Error
}

// Delete 软删除会员。删除前改写 phone / member_no 释放唯一索引占用，
// 否则同一手机号或会员编号将无法再次创建。
func (r *memberRepository) Delete(tenantID, id uint) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		var member model.Member
		if err := common.TenantScope(tx, tenantID).First(&member, id).Error; err != nil {
			return err
		}

		updates := make(map[string]interface{}, 2)
		if member.Phone != "" {
			updates["phone"] = common.FreedUniqueValue(member.Phone, member.ID, 20)
		}
		if member.MemberNo != "" {
			updates["member_no"] = common.FreedUniqueValue(member.MemberNo, member.ID, 32)
		}
		if len(updates) > 0 {
			if err := tx.Model(&model.Member{}).Where("id = ?", member.ID).Updates(updates).Error; err != nil {
				return err
			}
		}

		// 清理关联表，避免留下孤儿记录
		if err := tx.Where("member_id = ?", member.ID).Delete(&model.MemberTagRel{}).Error; err != nil {
			return err
		}

		return common.TenantScope(tx, tenantID).Delete(&model.Member{}, id).Error
	})
}

func (r *memberRepository) FindByID(tenantID, id uint) (*model.Member, error) {
	var member model.Member
	err := common.TenantScope(database.DB, tenantID).First(&member, id).Error
	return &member, err
}

func (r *memberRepository) FindByPhone(tenantID uint, phone string) (*model.Member, error) {
	var member model.Member
	err := common.TenantScope(database.DB, tenantID).Where("phone = ?", phone).First(&member).Error
	return &member, err
}

func (r *memberRepository) FindByWechatOpenid(tenantID uint, openid string) (*model.Member, error) {
	var member model.Member
	err := common.TenantScope(database.DB, tenantID).Where("wechat_openid = ?", openid).First(&member).Error
	return &member, err
}

func (r *memberRepository) FindList(tenantID uint, phone, nickname string, levelID uint, status int8, page, pageSize int) ([]model.Member, int64, error) {
	var members []model.Member
	var total int64

	query := common.TenantScope(database.DB.Model(&model.Member{}), tenantID)

	if phone != "" {
		query = query.Where("phone LIKE ?", "%"+common.EscapeLike(phone)+"%")
	}
	if nickname != "" {
		query = query.Where("nickname LIKE ?", "%"+common.EscapeLike(nickname)+"%")
	}
	if levelID > 0 {
		query = query.Where("level_id = ?", levelID)
	}
	if status >= 0 {
		query = query.Where("status = ?", status)
	}

	query.Count(&total)
	err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&members).Error
	return members, total, err
}

func (r *memberRepository) UpdateStatus(tenantID, id uint, status int8) error {
	return common.TenantScope(database.DB, tenantID).Model(&model.Member{}).Where("id = ?", id).Update("status", status).Error
}

// ReplaceTags 重写会员的标签关联。
//
// pay_member_tag_rel 是纯关联表（只有 member_id / tag_id，**没有 tenant_id 列**），
// 所以绝不能对它套 common.TenantScope —— 那会生成 `WHERE tenant_id = ?`，
// MySQL 直接报 1054 Unknown column，标签功能整体失效。
//
// 租户隔离改为「先校验会员归属、再操作关联」：
// 会员查不到就中止，避免用其他租户的 memberID 改写关联记录。
// 这与 user_repository.Delete 的处理方式一致。
func (r *memberRepository) ReplaceTags(tenantID, memberID uint, tagIDs []uint) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		var member model.Member
		if err := common.TenantScope(tx, tenantID).First(&member, memberID).Error; err != nil {
			return err
		}

		if err := tx.Where("member_id = ?", memberID).Delete(&model.MemberTagRel{}).Error; err != nil {
			return err
		}
		if len(tagIDs) == 0 {
			return nil
		}
		rels := make([]model.MemberTagRel, 0, len(tagIDs))
		for _, tagID := range tagIDs {
			rels = append(rels, model.MemberTagRel{MemberID: memberID, TagID: tagID})
		}
		return tx.Create(&rels).Error
	})
}

// FindTagIDsByMemberID 查询会员的标签 ID 列表。
//
// 同样不能对 pay_member_tag_rel 套 TenantScope（该表无 tenant_id 列），
// 改为按「member_id 属于本租户」的子查询施加约束（与 FindMenuIDsByRoleID 同构）。
//
// 调用方（memberService）确实已按租户过滤过会员，这里是**第二道**防线：
// 参数写了 tenantID 却不用它，等于邀请后来者直接传任意 memberID 读别人数据，
// 而签名本身还宣称支持租户隔离。宁可多查一次子查询，也不留这个陷阱。
func (r *memberRepository) FindTagIDsByMemberID(tenantID, memberID uint) ([]uint, error) {
	var tagIDs []uint
	query := database.DB.Model(&model.MemberTagRel{}).Where("member_id = ?", memberID)
	if tenantID > 0 {
		// 限定会员必须属于当前租户：memberID 是可枚举的主键，
		// 不加过滤则任意租户都能读出其他租户会员的标签关联。
		// 关联表 pay_member_tag_rel 只有 member_id/tag_id，无 tenant_id，
		// 只能通过子查询回连 pay_member 施加约束（与 FindMenuIDsByRoleID 同构）。
		query = query.Where("member_id IN (?)",
			database.DB.Model(&model.Member{}).Select("id").Where("tenant_id = ?", tenantID))
	}
	err := query.Pluck("tag_id", &tagIDs).Error
	return tagIDs, err
}

func (r *memberRepository) UpdatePoints(tenantID, memberID uint, points int64) error {
	return common.TenantScope(database.DB, tenantID).Model(&model.Member{}).Where("id = ?", memberID).UpdateColumn("points", points).Error
}

// FindMaxMemberNo 取**全平台**最大的会员编号，刻意不做租户过滤。
//
// 与 CountByUsername 是同一个取舍：**约束是全局的，推导就必须是全局的**。
//
// pay_member 的 `uk_member_no` 是全局唯一索引（`sql/init.sql`），同表的
// `uk_phone` 也一样 ——「一个手机号全平台只能注册一次」说明会员本身被当作
// 平台级实体。早前这里按租户取最大值，于是每个租户在空库上都从 100001 起号，
// 第二个租户建第一个会员就撞 `uk_member_no`，对外是 1062 唯一键冲突：
// **多租户部署下除首个租户外完全无法创建会员**（单租户部署不暴露，所以长期没被发现）。
//
// 因此这里没有 tenantID 参数是**有意**的，不要"补"上 ——
// 补上就会退回「每个租户各自从 100001 开始」的冲突状态。
func (r *memberRepository) FindMaxMemberNo() (string, error) {
	var memberNo string
	err := database.DB.Model(&model.Member{}).
		Select("member_no").
		Where("member_no != ''").
		Order("member_no DESC").
		Limit(1).
		Pluck("member_no", &memberNo).Error
	return memberNo, err
}
