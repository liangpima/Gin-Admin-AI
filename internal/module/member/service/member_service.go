package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/logger"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"
	"go-admin/internal/module/member/repository"
	systemModel "go-admin/internal/module/system/model"
	systemService "go-admin/internal/module/system/service"

	"gorm.io/gorm"
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
	unique := common.UniqueNonZeroIDs(tagIDs)
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

// dedupeNonZeroIDs 已上移到 common.UniqueNonZeroIDs（system 模块另有一份，一并收敛）。

// phoneConflictOrErr 把仓储返回的**唯一键冲突**翻译成业务错误。
//
// 为什么需要它：手机号查重是「先查再写」，两步之间并发请求可以插进来
// （另一人同时建会员或改号），所以预检查一定会漏 —— 唯一索引才是最终裁判。
// 但索引返回的是驱动原始错误（MySQL 1062 / SQLite 2067），直接交给
// `common.FailWith` 会被归一成 500「服务器内部错误」，
// 用户看到的提示与真实原因（手机号冲突）毫无关系。
//
// ⚠️ **只翻译唯一键冲突，其余错误原样返回**：把 DB 故障也说成「手机号已注册」
// 会让监控失去按 5xx 告警的能力（违反规则 5）。判定用
// `database.IsDuplicateKey`（按错误码，不匹配错误文本）。
func phoneConflictOrErr(err error) error {
	if database.IsDuplicateKey(err) {
		return common.NewBizError("手机号已注册")
	}
	return err
}

func (s *memberService) Create(req *dto.CreateMemberRequest, operatorID, tenantID uint) error {
	// 编号位数来自系统配置（原先由 Controller 读取后传入）
	digits := s.memberNoDigits()

	if req.Phone != "" {
		// 查重范围是**租户内**，与 pay_member 的
		// `uk_tenant_phone`(tenant_id, phone) 索引范围一致（见 model.Member 的说明）。
		// 两边范围必须一致：不一致时会出现「校验通过却插入报 1062」，
		// 对外表现为 500「服务器内部错误」，与真实原因毫无关系。
		//
		// 判定依据只能是 err，且**不能吞掉它**：
		//   · FindByPhone 无论查没查到都返回非 nil 指针（&member, err），
		//     所以 `existing, _ := ...; if existing != nil` 恒为真；
		//   · 吞掉 err 会把「数据库故障」误判成「手机号没被注册」，
		//     重名校验被静默跳过（与 CountByUsername / CountByCode 同一取舍）。
		existing, err := s.memberRepo.FindByPhone(tenantID, req.Phone)
		switch {
		case err == nil:
			if existing != nil && existing.ID > 0 {
				return common.NewBizError("手机号已注册")
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			// 未注册，可以继续创建
		default:
			return err
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

	memberNo, err := s.generateMemberNo(digits)
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
		// 预检查与写入之间有并发窗口，唯一索引是最终裁判 —— 见 phoneConflictOrErr
		return phoneConflictOrErr(err)
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
		// 只把「记录不存在」转成 404：无条件转 404 会把数据库故障
		// 说成「会员不存在」，监控按 5xx 告警的能力随之失效（违反规则 5）
		return common.NotFoundOrErr(err, "会员不存在")
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
	if req.Phone != "" && req.Phone != member.Phone {
		// 改手机号必须**显式查重**（排除自己）。
		//
		// 不查的后果：一路撞到唯一索引 → 1062 → common.FailWith 归一成
		// 500「服务器内部错误」，提示与真实原因毫无关系，用户只会以为系统坏了。
		// 与 Create 的查重同一套判定（依据只能是 err，不能吞）。
		//
		// 查重范围是**租户内**，与 uk_tenant_phone(tenant_id, phone) 一致：
		// 两个租户可以各自拥有同一手机号。别把这里改成「全平台唯一」——
		// 那是 H13 之前的设计，已按产品口径改掉。
		existing, err := s.memberRepo.FindByPhone(tenantID, req.Phone)
		switch {
		case err == nil:
			if existing != nil && existing.ID > 0 && existing.ID != member.ID {
				return common.NewBizError("手机号已注册")
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			// 未被占用，可以继续
		default:
			return err
		}
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
		// 预检查与写入之间有并发窗口，唯一索引是最终裁判 —— 见 phoneConflictOrErr
		return phoneConflictOrErr(err)
	}

	if req.TagIds != nil {
		if err := s.memberRepo.ReplaceTags(tenantID, member.ID, tagIDs); err != nil {
			return err
		}
	}

	return nil
}

func (s *memberService) Delete(tenantID, id uint) error {
	// 仓储在「不存在或不属于本租户」时返回 gorm.ErrRecordNotFound，
	// 不转换就会被当成系统错误回 500（与 post/tag 的 Delete 保持一致）。
	return common.NotFoundOrErr(s.memberRepo.Delete(tenantID, id), "会员不存在")
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

	// 标签关联改成两次批量查询，而不是「每行各查两次」。
	//
	// 原实现是循环里对每个会员查一次关联表 + 一次标签表，pageSize 上限 100
	// 时一次列表请求最多 201 次查询 —— 页码越靠后越慢，且随页大小线性放大。
	// 现在无论多少行都只有 2 次查询。
	//
	// 出错处理刻意保持「记日志 + 留空标签」而不是让整个列表失败：
	// 标签只是列表的附属信息，为它牺牲整页数据不划算（会员列表是核心页面）。
	// 但**不能静默**—— 此前标签查询因 SQL 引用不存在的列而恒定失败，
	// 错误被 `_` 吞掉，表现为「会员列表标签恒为空」且长期无人察觉。
	memberIDs := make([]uint, len(members))
	for i, m := range members {
		memberIDs[i] = m.ID
	}

	tagIDsByMember, err := s.memberRepo.FindTagIDsByMemberIDs(tenantID, memberIDs)
	if err != nil {
		logger.Log.Warnf("批量查询会员标签关联失败, tenant=%d, members=%d: %v", tenantID, len(memberIDs), err)
		tagIDsByMember = map[uint][]uint{}
	}

	// 把整页用到的 tagID 去重后一次性取回详情，再在内存里做映射，
	// 避免同一标签被多个会员共用时重复查询。
	allTagIDs := make([]uint, 0, len(memberIDs))
	seen := make(map[uint]struct{}, len(memberIDs))
	for _, tagIDs := range tagIDsByMember {
		for _, id := range tagIDs {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			allTagIDs = append(allTagIDs, id)
		}
	}

	tagByID := make(map[uint]model.MemberTag, len(allTagIDs))
	if len(allTagIDs) > 0 {
		tags, err := s.tagRepo.FindByIDs(tenantID, allTagIDs)
		if err != nil {
			logger.Log.Warnf("批量查询标签详情失败, tenant=%d, tags=%d: %v", tenantID, len(allTagIDs), err)
		}
		for _, tag := range tags {
			tagByID[tag.ID] = tag
		}
	}

	result := make([]interface{}, len(members))
	for i, m := range members {
		tags := make([]model.MemberTag, 0, len(tagIDsByMember[m.ID]))
		for _, id := range tagIDsByMember[m.ID] {
			// 标签被删除（或跨租户）时详情查不到，跳过即可 ——
			// 直接追加零值会渲染出一个 ID 为 0 的空标签。
			if tag, ok := tagByID[id]; ok {
				tags = append(tags, tag)
			}
		}
		result[i] = memberWithTag{Member: m, Tags: tags}
	}
	return result, total, nil
}

func (s *memberService) UpdateStatus(tenantID uint, req *dto.UpdateMemberStatusRequest) error {
	// 仓储会先确认目标属于本租户（不属于则返回 ErrRecordNotFound），
	// 这里把「不存在」转成 404，DB 故障仍走 500
	return common.NotFoundOrErr(s.memberRepo.UpdateStatus(tenantID, req.ID, req.Status), "会员不存在")
}

func (s *memberService) generateMemberNo(digits int) (string, error) {
	ctx := context.Background()
	// 计数器必须是**全平台**的，不能带 tenantID：
	// pay_member 的 uk_member_no 是全局唯一索引，按租户发号会让每个租户
	// 都从 100001 起号，第二个租户建会员必然撞索引。
	// 详见 memberRepository.FindMaxMemberNo 的注释。
	key := "member:no"
	format := fmt.Sprintf("%%0%dd", digits)

	// 先确认计数器是否已初始化，再取号。
	//
	// 为什么不能沿用「先 INCR，看到 seq==1 再对齐」：并发首次发号时，
	// 多个调用者分别拿到 seq = 1、2、3…，而只有 seq==1 的那个走对齐分支，
	// 其余直接按 2、3 发号 —— 发出去的正是 000002 这种与历史编号冲突的畸形值。
	// 判定「是否已初始化」只能看键存不存在：INCR 的返回值区分不出
	// 「计数器刚被创建」与「计数器恰好等于 2」，EXISTS 可以。
	initialized, existsErr := cache.Exists(ctx, key)
	if existsErr != nil {
		// Redis 不可用时回退到数据库查询（有竞态风险，但可接受降级）。
		// 数据库也查不到时必须让调用方知道 —— 用臆测的起始值发号，
		// 撞上唯一索引只会变成一条难以理解的 500。
		startNum, dbErr := s.memberNoFromDB(digits)
		if dbErr != nil {
			logger.Log.Errorf("[member] Redis 与数据库均不可用，无法生成会员编号: err=%v", dbErr)
			return "", fmt.Errorf("生成会员编号失败: %w", dbErr)
		}
		logger.Log.Warnf("[member] Redis 不可用，会员编号降级为按库内最大值推导")
		return fmt.Sprintf(format, startNum), nil
	}

	if !initialized {
		startNum, dbErr := s.memberNoFromDB(digits)
		if dbErr != nil {
			// 无法确认库内最大值时不能发号：可能与已有编号冲突
			logger.Log.Errorf("[member] 读取会员编号最大值失败，拒绝发号: err=%v", dbErr)
			return "", fmt.Errorf("生成会员编号失败: %w", dbErr)
		}

		// SETNX 让「谁来初始化」有唯一赢家：
		//   - 赢家把 startNum 直接发出去。计数器此时等于 startNum
		//     （INCR 语义下表示「上一次发出的编号」），下次 INCR 恰好得 startNum+1。
		//     对齐值写成 startNum+1 会让编号凭空跳一位（100001 之后直接发 100003），
		//     用户看到跳号会以为有会员数据丢失。
		//   - 输家说明已有并发调用者完成了初始化，落到下面走正常 INCR，
		//     因此不会有两个请求发出同一个号。
		won, setErr := cache.SetNX(ctx, key, strconv.Itoa(startNum), 0)
		if setErr != nil {
			// 这个错误**不能吞**：初始化失败时键状态未知，
			// 下一次 INCR 可能从 1 开始，于是把 000002 这种与历史编号冲突的值发出去。
			// 处理方式是删掉计数器，让下次调用重新走初始化分支；
			// 本次返回的 startNum 本身是按库内最大值推导的，是正确的。
			logger.Log.Errorf("[member] 会员编号计数器初始化失败: err=%v", setErr)
			if delErr := cache.Del(ctx, key); delErr != nil {
				logger.Log.Errorf("[member] 清理失效的编号计数器也失败，下次发号可能冲突: err=%v", delErr)
			}
			return fmt.Sprintf(format, startNum), nil
		}
		if won {
			return fmt.Sprintf(format, startNum), nil
		}
	}

	// 使用 Redis INCR 原子递增，避免并发重复
	seq, err := cache.Incr(ctx, key)
	if err != nil {
		// 走到这里说明 EXISTS 时 Redis 还是好的，取号时却失败了。
		// 与上面的降级同理：按库内最大值推导，宁可可能重复也不能不发号。
		startNum, dbErr := s.memberNoFromDB(digits)
		if dbErr != nil {
			logger.Log.Errorf("[member] 取号失败且无法从数据库推导，放弃发号: err=%v", dbErr)
			return "", fmt.Errorf("生成会员编号失败: %w", dbErr)
		}
		logger.Log.Warnf("[member] 会员编号计数器取号失败，降级为按库内最大值推导: err=%v", err)
		return fmt.Sprintf(format, startNum), nil
	}

	return fmt.Sprintf(format, seq), nil
}

// memberNoFromDB 按库内最大会员编号推导下一个可用编号。
//
// 起始值取 10^(digits-1)+1（如 digits=6 → 100001），保证编号位数符合配置；
// 库内已有更大的编号时以库内为准，避免发号回退到已被占用的区间。
//
// 与 FindMaxMemberNo 一致：这里也**不按租户**推导（uk_member_no 是全局索引）。
func (s *memberService) memberNoFromDB(digits int) (int, error) {
	maxNo, err := s.memberRepo.FindMaxMemberNo()
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
		return common.NotFoundOrErr(err, "会员不存在")
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
		return common.NotFoundOrErr(err, "会员不存在")
	}
	now := time.Now()
	member.LastVisitTime = &now
	return s.memberRepo.Update(member)
}
