package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"
	"go-admin/internal/module/member/repository"
	systemModel "go-admin/internal/module/system/model"
	systemService "go-admin/internal/module/system/service"
)

type MemberService interface {
	Create(req *dto.CreateMemberRequest, operatorID, tenantID uint) error
	Update(req *dto.UpdateMemberRequest, operatorID, tenantID uint) error
	Delete(tenantID, id uint) error
	FindByID(tenantID, id uint) (*model.Member, error)
	FindList(tenantID uint, req *dto.MemberListRequest) ([]interface{}, int64, error)
	UpdateStatus(tenantID uint, req *dto.UpdateMemberStatusRequest) error
	UpdateTags(tenantID uint, req *dto.UpdateMemberTagsRequest) error
	UpdateLastVisit(tenantID, id uint) error
}

type memberService struct {
	memberRepo repository.MemberRepository
	tagRepo    repository.MemberTagRepository
	levelRepo  repository.MemberLevelRepository

	// 跨模块取配置走 Service（AGENTS 规则 4 允许），不碰对方的 Repository
	configService systemService.ConfigService
}

func NewMemberService() MemberService {
	return &memberService{
		memberRepo:    repository.NewMemberRepository(),
		tagRepo:       repository.NewMemberTagRepository(),
		levelRepo:     repository.NewMemberLevelRepository(),
		configService: systemService.NewConfigService(),
	}
}

// defaultMemberNoDigits 会员编号默认位数：配置缺失或非法时的兜底。
const defaultMemberNoDigits = 6

// memberNoDigits 读取「会员编号位数」配置。
//
// 这段逻辑原先写在 Controller 里，还顺带在每次请求内 new 了一个 ConfigService ——
// 既是业务规则（位数的下限校验、非法值兜底），又浪费对象。
// 已按规则 1 下沉到 Service，Controller 只负责取参与返回。
func (s *memberService) memberNoDigits() int {
	val, err := s.configService.FindByKey("site.memberIdDigits")
	if err != nil {
		return defaultMemberNoDigits
	}
	cfg, ok := val.(*systemModel.SysConfig)
	if !ok || cfg.Value == "" {
		return defaultMemberNoDigits
	}
	n, err := strconv.Atoi(cfg.Value)
	if err != nil || n < 4 {
		return defaultMemberNoDigits
	}
	return n
}

// normalizeLevelID 校验会员等级属于当前租户，0 表示不设等级。
//
// 为什么必须在 Service 层做：member.LevelID 是裸 ID，仓储层的 TenantScope
// 只作用于会员表本身，拦不住「本租户的会员指向其他租户的等级」——
// 前端枚举 level_id 就能把别人的等级挂到自己会员上。
//
// 不区分「不存在」与「不属于本租户」，否则可被用来探测其他租户的 ID 是否存在。
// 写法与 system 模块 userService.normalizeRoleIDs 保持一致。
func (s *memberService) normalizeLevelID(tenantID, levelID uint) (uint, error) {
	if levelID == 0 {
		return 0, nil
	}
	if _, err := s.levelRepo.FindByID(tenantID, levelID); err != nil {
		return 0, common.NewBizError("会员等级无效，请刷新后重试")
	}
	return levelID, nil
}

// normalizeTagIDs 校验标签 ID 全部属于当前租户，并去重。
//
// pay_member_tag_rel 是纯关联表（只有 member_id / tag_id，**没有 tenant_id**），
// 租户隔离无法靠 TenantScope 完成，只能在写入前按租户查出被引用方并比对数量 ——
// 这正是 AGENTS.md 规则 7 的要求（用户模块已照此实现，会员模块此前漏了）。
func (s *memberService) normalizeTagIDs(tenantID uint, tagIDs []uint) ([]uint, error) {
	unique := dedupeNonZeroIDs(tagIDs)
	if len(unique) == 0 {
		return nil, nil
	}

	tags, err := s.tagRepo.FindByIDs(tenantID, unique)
	if err != nil {
		return nil, err
	}
	if len(tags) != len(unique) {
		return nil, common.NewBizError("包含无效的标签，请刷新后重试")
	}
	return unique, nil
}

// dedupeNonZeroIDs 去重并剔除 0。
// 0 不是合法主键，通常是前端下拉框未选择时的默认值，不应写进关联表。
func dedupeNonZeroIDs(ids []uint) []uint {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *memberService) Create(req *dto.CreateMemberRequest, operatorID, tenantID uint) error {
	// 编号位数来自系统配置（原先由 Controller 读取后传入）
	digits := s.memberNoDigits()

	if req.Phone != "" {
		existing, _ := s.memberRepo.FindByPhone(tenantID, req.Phone)
		if existing != nil && existing.ID > 0 {
			return common.NewBizError("手机号已注册")
		}
	}

	// 先校验关联的等级与标签归属，**再**落库。
	// 顺序很重要：若放在建会员之后，标签校验失败会留下「会员已建、标签没绑」的
	// 半成品数据，而调用方只看到失败，不会知道已经写了一条。
	levelID, err := s.normalizeLevelID(tenantID, req.LevelID)
	if err != nil {
		return err
	}
	tagIDs, err := s.normalizeTagIDs(tenantID, req.TagIds)
	if err != nil {
		return err
	}

	memberNo, err := s.generateMemberNo(tenantID, digits)
	if err != nil {
		return fmt.Errorf("生成会员编号失败: %v", err)
	}

	member := &model.Member{
		TenantBaseModel: common.TenantBaseModel{
			BaseModel: common.BaseModel{
				CreateBy: operatorID,
				UpdateBy: operatorID,
			},
			TenantID: tenantID,
		},
		MemberNo:     memberNo,
		Username:     req.Username,
		Nickname:     req.Nickname,
		Avatar:       req.Avatar,
		Phone:        req.Phone,
		Gender:       req.Gender,
		LevelID:      levelID,
		Status:       req.Status,
		Points:       0,
		RegisterTime: time.Now(),
	}
	member.Remark = req.Remark

	if req.Birthday != "" {
		if t, err := time.Parse("2006-01-02", req.Birthday); err == nil {
			member.Birthday = &t
		}
	}

	if err := s.memberRepo.Create(member); err != nil {
		return err
	}

	if len(tagIDs) > 0 {
		// 不再吞掉错误：标签写入失败必须让调用方知道，否则会员建好了却没标签，
		// 调用方以为成功，数据静默不一致。
		if err := s.memberRepo.ReplaceTags(tenantID, member.ID, tagIDs); err != nil {
			return err
		}
	}

	return nil
}

func (s *memberService) Update(req *dto.UpdateMemberRequest, operatorID, tenantID uint) error {
	member, err := s.memberRepo.FindByID(tenantID, req.ID)
	if err != nil {
		return common.NewNotFoundError("会员不存在")
	}

	// 同 Create：关联归属先校验再落库。
	// LevelID / TagIds 都用「未提供即不改」的语义：
	// LevelID 为 nil、TagIds 为 nil 都表示本次不涉及该关联；
	// TagIds 传空切片才表示「清空标签」。
	var levelID uint
	if req.LevelID != nil {
		levelID, err = s.normalizeLevelID(tenantID, *req.LevelID)
		if err != nil {
			return err
		}
	}
	var tagIDs []uint
	if req.TagIds != nil {
		tagIDs, err = s.normalizeTagIDs(tenantID, req.TagIds)
		if err != nil {
			return err
		}
	}

	// 逐字段判断「是否提供」再赋值，实现真正的部分更新。
	// 无条件赋值会把未提供的字段清成零值 —— 实测「只改等级」会把会员
	// 顺手改成停用（status 1→0）并重置性别，属于静默的数据损坏。
	if req.Username != "" {
		member.Username = req.Username
	}
	if req.Nickname != "" {
		member.Nickname = req.Nickname
	}
	if req.Avatar != "" {
		member.Avatar = req.Avatar
	}
	if req.Phone != "" {
		member.Phone = req.Phone
	}
	if req.Gender != nil {
		member.Gender = *req.Gender
	}
	if req.Status != nil {
		member.Status = *req.Status
	}
	if req.Remark != nil {
		member.Remark = *req.Remark
	}
	if req.LevelID != nil {
		// 等级归属校验在 normalizeLevelID 内（上面已按 req.LevelID 校验过）
		member.LevelID = levelID
	}
	if req.Birthday != nil {
		// 指针非 nil 才动生日：空串表示「清空」，非空串表示「设置」
		if *req.Birthday == "" {
			member.Birthday = nil
		} else if t, err := time.Parse("2006-01-02", *req.Birthday); err == nil {
			member.Birthday = &t
		}
	}

	if err := s.memberRepo.Update(member); err != nil {
		return err
	}

	if req.TagIds != nil {
		if err := s.memberRepo.ReplaceTags(tenantID, member.ID, tagIDs); err != nil {
			return err
		}
	}

	return nil
}

func (s *memberService) Delete(tenantID, id uint) error {
	return s.memberRepo.Delete(tenantID, id)
}

func (s *memberService) FindByID(tenantID, id uint) (*model.Member, error) {
	member, err := s.memberRepo.FindByID(tenantID, id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "会员不存在")
	}
	return member, nil
}

func (s *memberService) FindList(tenantID uint, req *dto.MemberListRequest) ([]interface{}, int64, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	req.Page, req.PageSize = common.NormalizePageParams(req.Page, req.PageSize)

	status := int8(-1)
	if req.Status != nil {
		status = *req.Status
	}

	members, total, err := s.memberRepo.FindList(tenantID, req.Phone, req.Nickname, req.LevelID, status, req.Page, req.PageSize)
	if err != nil {
		return nil, 0, err
	}

	type memberWithTag struct {
		model.Member
		Tags []model.MemberTag `json:"tags"`
	}

	result := make([]interface{}, len(members))
	for i, m := range members {
		// 不再用 `_` 丢弃错误：标签查询此前因 SQL 引用不存在的列而恒定失败，
		// 错误被吞掉，表现为「会员列表标签恒为空」且长期无人察觉。
		tagIDs, err := s.memberRepo.FindTagIDsByMemberID(tenantID, m.ID)
		if err != nil {
			logger.Log.Warnf("查询会员标签关联失败, memberID=%d: %v", m.ID, err)
		}
		tags, err := s.tagRepo.FindByIDs(tenantID, tagIDs)
		if err != nil {
			logger.Log.Warnf("查询标签详情失败, memberID=%d: %v", m.ID, err)
		}
		result[i] = memberWithTag{Member: m, Tags: tags}
	}
	return result, total, nil
}

func (s *memberService) UpdateStatus(tenantID uint, req *dto.UpdateMemberStatusRequest) error {
	return s.memberRepo.UpdateStatus(tenantID, req.ID, req.Status)
}

func (s *memberService) generateMemberNo(tenantID uint, digits int) (string, error) {
	ctx := context.Background()
	key := fmt.Sprintf("member:no:%d", tenantID)
	format := fmt.Sprintf("%%0%dd", digits)

	// 使用 Redis INCR 原子递增，避免并发重复
	seq, err := cache.Incr(ctx, key)
	if err != nil {
		// Redis 不可用时回退到数据库查询（有竞态风险，但可接受降级）。
		// 数据库也查不到时必须让调用方知道 —— 用臆测的起始值发号，
		// 撞上唯一索引只会变成一条难以理解的 500。
		startNum, dbErr := s.memberNoFromDB(tenantID, digits)
		if dbErr != nil {
			logger.Log.Errorf("[member] Redis 与数据库均不可用，无法生成会员编号: tenant=%d err=%v", tenantID, dbErr)
			return "", fmt.Errorf("生成会员编号失败: %w", dbErr)
		}
		logger.Log.Warnf("[member] Redis 不可用，会员编号降级为按库内最大值推导: tenant=%d", tenantID)
		return fmt.Sprintf(format, startNum), nil
	}

	// 首次初始化：如果序列为1，把计数器对齐到「库内最大值 + 1」
	if seq == 1 {
		startNum, dbErr := s.memberNoFromDB(tenantID, digits)
		if dbErr != nil {
			// 无法确认库内最大值时不能发号：可能与本租户已有编号冲突
			logger.Log.Errorf("[member] 读取会员编号最大值失败，拒绝发号: tenant=%d err=%v", tenantID, dbErr)
			return "", fmt.Errorf("生成会员编号失败: %w", dbErr)
		}

		if setErr := cache.Set(ctx, key, strconv.Itoa(startNum+1), 0); setErr != nil {
			// 这个错误**不能吞**：对齐失败时 key 会停在 1，下一次 Incr 返回 2，
			// 于是把 000002 这种与历史编号冲突的值发出去。
			// 处理方式是删掉计数器，让下次调用重新走初始化分支；
			// 本次返回的 startNum 本身是按库内最大值推导的，是正确的。
			logger.Log.Errorf("[member] 会员编号计数器对齐失败: tenant=%d err=%v", tenantID, setErr)
			if delErr := cache.Del(ctx, key); delErr != nil {
				logger.Log.Errorf("[member] 清理失效的编号计数器也失败，下次发号可能冲突: tenant=%d err=%v", tenantID, delErr)
			}
		}
		return fmt.Sprintf(format, startNum), nil
	}

	return fmt.Sprintf(format, seq), nil
}

// memberNoFromDB 按库内最大会员编号推导下一个可用编号。
//
// 起始值取 10^(digits-1)+1（如 digits=6 → 100001），保证编号位数符合配置；
// 库内已有更大的编号时以库内为准，避免发号回退到已被占用的区间。
func (s *memberService) memberNoFromDB(tenantID uint, digits int) (int, error) {
	maxNo, err := s.memberRepo.FindMaxMemberNo(tenantID)
	if err != nil {
		return 0, err
	}

	startNum := int(math.Pow10(digits-1)) + 1
	if maxNo != "" {
		if n, parseErr := strconv.Atoi(maxNo); parseErr == nil && n >= startNum {
			startNum = n + 1
		}
	}
	return startNum, nil
}

func (s *memberService) UpdateTags(tenantID uint, req *dto.UpdateMemberTagsRequest) error {
	_, err := s.memberRepo.FindByID(tenantID, req.ID)
	if err != nil {
		return common.NewNotFoundError("会员不存在")
	}

	// 与 Create/Update 一致：先校验标签归属再写关联表
	tagIDs, err := s.normalizeTagIDs(tenantID, req.TagIds)
	if err != nil {
		return err
	}
	return s.memberRepo.ReplaceTags(tenantID, req.ID, tagIDs)
}

// UpdateLastVisit 更新会员最后访问时间
func (s *memberService) UpdateLastVisit(tenantID, id uint) error {
	member, err := s.memberRepo.FindByID(tenantID, id)
	if err != nil {
		return common.NewNotFoundError("会员不存在")
	}
	now := time.Now()
	member.LastVisitTime = &now
	return s.memberRepo.Update(member)
}
