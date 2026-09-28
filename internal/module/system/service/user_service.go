package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"go-admin/config"
	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/middleware"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"
	"go-admin/internal/module/system/vo"
	"go-admin/pkg/utils"

	"gorm.io/gorm"
)

type UserService interface {
	Create(tenantID uint, req *dto.CreateUserRequest, operatorID uint) error
	Update(tenantID uint, req *dto.UpdateUserRequest, operatorID uint) error
	Delete(tenantID, id uint) error
	FindByID(tenantID, id uint) (interface{}, error)
	FindList(tenantID uint, req *dto.UserListRequest) ([]interface{}, int64, error)
	// ExportList 导出用列表：同样的筛选条件，但不做分页截断
	ExportList(tenantID uint, req *dto.UserListRequest) ([]interface{}, error)
	UpdateStatus(tenantID uint, req *dto.StatusRequest) error
	UpdateRoles(tenantID, operatorID uint, req *dto.UpdateUserRolesRequest) error
	UpdateDept(tenantID uint, req *dto.UpdateUserDeptRequest) error
	ResetPassword(tenantID uint, req *dto.ResetPasswordRequest) error
	ChangePassword(userID uint, req *dto.ChangePasswordRequest) error
}

// UserWithRoles 用户列表/导出返回的视图：用户本体 + 其角色。
// 提升为包级类型是为了让 controller 在导出时能做类型断言
// （早前它是 FindList 内部的匿名结构体，外部无法引用）。
type UserWithRoles struct {
	model.SysUser
	Roles []vo.RoleInfo `json:"roles"`
}

type userService struct {
	userRepo    repository.UserRepository
	roleService RoleService
	postService PostService
	deptService DeptService
}

func NewUserService() UserService {
	return &userService{
		userRepo:    repository.NewUserRepository(),
		roleService: NewRoleService(),
		postService: NewPostService(),
		deptService: NewDeptService(),
	}
}

// normalizeDeptID 校验部门属于当前租户。
//
// sys_user.dept_id 是指向租户内表 sys_dept 的引用，与角色/岗位同理：
// 不校验时租户 A 可以把用户的部门指向租户 B 的部门 ID（ID 可枚举）。
// 后果不像跨租户角色那样直接提权，但同样是数据越界 —— 而且更隐蔽：
// 用户列表按租户过滤、部门树也按租户过滤，指向别租户的部门时该用户
// 在界面上会表现为「没有部门」，不报任何错。
//
// deptID 为 0 表示不设部门，直接放行。
func (s *userService) normalizeDeptID(tenantID, deptID uint) (uint, error) {
	if deptID == 0 {
		return 0, nil
	}
	if _, err := s.deptService.FindByID(tenantID, deptID); err != nil {
		// 部门不存在 / 不属于本租户 → 业务错误（400）。
		// 不点名是「不存在」还是「不属于你」：否则可用来探测其他租户的部门 ID。
		if common.IsBizError(err) {
			return 0, common.NewBizError("部门不存在或不属于当前租户，请刷新后重试")
		}
		return 0, err
	}
	return deptID, nil
}

// dedupeNonZeroIDs 已上移到 common.UniqueNonZeroIDs（会员模块此前有一份
// 行为略有差异的副本 —— 全为 0 时一个返回 nil、一个返回空切片，
// 正是「复制粘贴后各自演化」的典型。收敛到一处后不再有这个问题）。

// normalizeRoleIDs 校验角色 ID 全部属于当前租户，并返回去重后的合法列表。
//
// 为什么必须在 Service 层做：sys_user_role 是纯关联表，只有 user_id / role_id，
// **没有 tenant_id 列**，租户隔离无法靠 TenantScope 完成。
// 不做校验的后果是租户 A 的管理员可以把用户绑到租户 B 的角色 ID 上
// （ID 可枚举），而 Casbin 策略是按角色 code 生成的
// （见 middleware.SyncPoliciesFromRoleMenus），绑上即继承对方的菜单与权限码，
// 构成跨租户权限提升。
//
// tenantID 为 0 时 FindByIDs 会退化为不过滤 —— 平台级账号可跨租户分配角色，
// 与项目既定的 TenantScope 语义保持一致。
//
// 除归属外还要做**授权收敛**校验（EnsureRolesGrantable）：把角色绑到用户上
// 等价于把该角色的全部权限授予该用户，若只校验归属，一个只有 user:edit 权限的
// 管理员就能把超管角色绑给自己或新建的账号 —— 这是比改角色菜单更短的一条提权路径。
func (s *userService) normalizeRoleIDs(tenantID, operatorID uint, roleIDs []uint) ([]uint, error) {
	unique := common.UniqueNonZeroIDs(roleIDs)
	if len(unique) == 0 {
		return nil, nil
	}

	roles, err := s.roleService.FindByIDs(tenantID, unique)
	if err != nil {
		return nil, err
	}
	if len(roles) != len(unique) {
		// 不点名是哪个 ID 不合法：否则可被用来探测其他租户的角色 ID 是否存在
		return nil, common.NewBizError("包含无效的角色，请刷新后重试")
	}
	if err := s.roleService.EnsureRolesGrantable(tenantID, operatorID, roles); err != nil {
		return nil, err
	}
	return unique, nil
}

// normalizePostIDs 与 normalizeRoleIDs 同理：sys_user_post 也是纯关联表，
// 且 sys_post 已改为租户内数据，必须确认岗位属于当前租户后再绑定。
func (s *userService) normalizePostIDs(tenantID uint, postIDs []uint) ([]uint, error) {
	unique := common.UniqueNonZeroIDs(postIDs)
	if len(unique) == 0 {
		return nil, nil
	}

	posts, err := s.postService.FindByIDs(tenantID, unique)
	if err != nil {
		return nil, err
	}
	if len(posts) != len(unique) {
		return nil, common.NewBizError("包含无效的岗位，请刷新后重试")
	}
	return unique, nil
}

func (s *userService) Create(tenantID uint, req *dto.CreateUserRequest, operatorID uint) error {
	// 用户名是**全局**唯一（登录接口不带租户字段，只能按用户名全局定位用户），
	// 所以这里也必须按全局校验，否则会出现「校验通过、插入撞唯一索引」→ 500
	if count, err := s.userRepo.CountByUsername(req.Username, 0); err != nil {
		return err
	} else if count > 0 {
		return common.NewBizError("用户名已存在")
	}

	// 先校验角色/岗位归属再落库：若放在创建之后，校验失败会留下一个
	// 已建好但没有角色/岗位的半成品用户
	roleIDs, err := s.normalizeRoleIDs(tenantID, operatorID, req.RoleIds)
	if err != nil {
		return err
	}
	postIDs, err := s.normalizePostIDs(tenantID, req.PostIds)
	if err != nil {
		return err
	}
	deptID, err := s.normalizeDeptID(tenantID, req.DeptID)
	if err != nil {
		return err
	}

	if err := validatePasswordStrength(req.Password); err != nil {
		return err
	}

	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		return err
	}

	user := &model.SysUser{
		TenantBaseModel: common.TenantBaseModel{
			BaseModel: common.BaseModel{
				CreateBy: operatorID,
				UpdateBy: operatorID,
			},
			TenantID: tenantID,
		},
		Username: req.Username,
		Password: hash,
		Nickname: req.Nickname,
		Email:    req.Email,
		Phone:    req.Phone,
		Status:   req.Status,
		DeptID:   deptID,
	}
	user.Remark = req.Remark

	// 主表与两张关联表必须在**一次提交**里完成。
	//
	// 分成三次写（原先的写法）时，用户已落库、角色写入失败会留下一个
	// 「能登录但没有任何权限」的半成品账号：调用方看到 500 以为整次操作
	// 失败了，实际那个账号已经存在且可登录，排查时现象离根因很远。
	// 反过来，ReplacePosts 失败也会留下「有角色、没岗位」的中间态。
	return s.userRepo.Transaction(func(txRepo repository.UserRepository) error {
		if err := txRepo.Create(user); err != nil {
			// 上面的 Count 校验存在时间窗口，并发下仍可能撞唯一索引，靠这里兜底
			if errors.Is(err, common.ErrDuplicateKey) {
				return common.NewBizError("用户名已存在")
			}
			return err
		}

		if len(roleIDs) > 0 {
			if err := txRepo.ReplaceRoles(user.ID, roleIDs); err != nil {
				return err
			}
		}
		if len(postIDs) > 0 {
			if err := txRepo.ReplacePosts(user.ID, postIDs); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *userService) Update(tenantID uint, req *dto.UpdateUserRequest, operatorID uint) error {
	// 更新不开放修改用户名（见 dto.UpdateUserRequest），因此无需重名校验。
	// 早前这里调用 CountByUsername(tenantID, "", req.ID) —— 传入空用户名恒为 0，
	// 「用户名已存在」是一条永远不会触发的死分支。存在性由下面的 FindByID 保证。
	user, err := s.userRepo.FindByID(tenantID, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFoundError("用户不存在")
		}
		return err
	}

	// 记下变更前的状态：停用必须吊销 Token，而「本来就是停用」不应重复吊销
	prevStatus := user.Status

	// 逐字段判断「是否提供」：指针为 nil 即本次不涉及该字段
	if req.Nickname != nil {
		user.Nickname = *req.Nickname
	}
	if req.Email != nil {
		user.Email = *req.Email
	}
	if req.Phone != nil {
		user.Phone = *req.Phone
	}
	if req.Status != nil {
		user.Status = *req.Status
	}
	if req.DeptID != nil {
		deptID, err := s.normalizeDeptID(tenantID, *req.DeptID)
		if err != nil {
			return err
		}
		user.DeptID = deptID
	}
	if req.Remark != nil {
		user.Remark = *req.Remark
	}
	user.UpdateBy = operatorID

	// 角色/岗位的归属与授权收敛校验统一提到落库之前。
	//
	// 原先它们在 userRepo.Update 之后执行，于是「资料已改、角色被拒」会留下
	// 半成品状态：用户看到 403 以为整次提交都失败了，实际昵称/邮箱已经变了。
	// 与 Create 的「先校验再落库」保持一致。
	var roleIDs []uint
	if req.RoleIds != nil {
		// 传空数组表示清空角色；传了非本租户或超出自身权限范围的角色会被拒绝
		roleIDs, err = s.normalizeRoleIDs(tenantID, operatorID, req.RoleIds)
		if err != nil {
			return err
		}
	}
	var postIDs []uint
	if req.PostIds != nil {
		// 传空数组表示清空岗位；传了非本租户的岗位 ID 会被拒绝
		postIDs, err = s.normalizePostIDs(tenantID, req.PostIds)
		if err != nil {
			return err
		}
	}

	if err := s.userRepo.Transaction(func(txRepo repository.UserRepository) error {
		if err := txRepo.Update(tenantID, user); err != nil {
			return err
		}

		if req.RoleIds != nil {
			if err := txRepo.ReplaceRoles(user.ID, roleIDs); err != nil {
				return err
			}
		}
		if req.PostIds != nil {
			if err := txRepo.ReplacePosts(user.ID, postIDs); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// 下面两步都必须放在**事务提交之后**：
	//   - 在事务内清缓存的话，并发请求可能读到提交前的旧数据并把它写回缓存，
	//     于是「清缓存」之后缓存里仍是旧角色，TTL（60s）内撤销授权不生效 ——
	//     清了等于没清；
	//   - Token 吊销依赖 Redis，不参与数据库事务、回滚也不会撤销它。
	//     放进事务内会在提交失败时产生「用户已下线但资料没改」的不一致。
	if req.RoleIds != nil {
		// 角色变更后必须失效该用户的角色缓存，否则最长 60s 内仍按旧角色鉴权。
		// UpdateRoles 接口一直是这么做的，这里漏了 —— 走「编辑用户」改角色
		// 会绕过缓存清理，撤销授权不即时生效。
		middleware.ClearRoleCache(tenantID, user.ID)
	}

	// 停用即下线：改 status 字段与调 UpdateStatus 接口必须产生同样的效果。
	// 此前只有 UpdateStatus 会吊销 Token，于是走「编辑用户」把状态改成停用
	// 就能绕过吊销 —— 而被停用的账号仍能继续访问，且 refresh token 还能
	// 换发新的 access token（RefreshToken 只查吊销标记、不查库）。
	if user.Status == common.StatusDisabled && prevStatus != common.StatusDisabled {
		if err := s.revokeUserTokens(user.ID); err != nil {
			return err
		}
	}

	return nil
}

func (s *userService) Delete(tenantID, id uint) error {
	if err := s.userRepo.Delete(tenantID, id); err != nil {
		return common.NotFoundOrErr(err, "用户不存在")
	}
	// 删除同样必须下线：软删除后 Auth 中间件并不查库，旧 token 依旧有效。
	// 吊销失败要报错，不能静默 —— 否则「已删除的账号仍可访问」无人知晓。
	return s.revokeUserTokens(id)
}

func (s *userService) FindByID(tenantID, id uint) (interface{}, error) {
	user, err := s.userRepo.FindByID(tenantID, id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "用户不存在")
	}

	type userWithRoles struct {
		model.SysUser
		Roles []vo.RoleInfo `json:"roles"`
	}

	roleIDs, err := s.userRepo.FindRoleIDsByUserID(user.ID)
	if err != nil {
		return nil, err
	}
	roleService := NewRoleService()
	roles, err := roleService.FindByIDs(tenantID, roleIDs)
	if err != nil {
		return nil, err
	}
	roleInfos := make([]vo.RoleInfo, 0, len(roles))
	for _, r := range roles {
		roleInfos = append(roleInfos, vo.RoleInfo{ID: r.ID, Name: r.Name, Code: r.Code})
	}

	return userWithRoles{SysUser: *user, Roles: roleInfos}, nil
}

// ExportMaxRows 单次导出的最大行数上限。
// 导出走的是不分页查询，必须设硬上限，否则一次导出可能把整表读进内存。
const ExportMaxRows = 10000

func (s *userService) FindList(tenantID uint, req *dto.UserListRequest) ([]interface{}, int64, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	req.Page, req.PageSize = common.NormalizePageParams(req.Page, req.PageSize)
	return s.query(tenantID, req, req.Page, req.PageSize)
}

// ExportList 导出用查询：筛选条件与列表一致，但不做分页截断（受 ExportMaxRows 限制）
func (s *userService) ExportList(tenantID uint, req *dto.UserListRequest) ([]interface{}, error) {
	rows, _, err := s.query(tenantID, req, 1, ExportMaxRows)
	return rows, err
}

// query 按条件查询用户并装配角色，供列表与导出复用，避免两处逻辑漂移
func (s *userService) query(tenantID uint, req *dto.UserListRequest, page, pageSize int) ([]interface{}, int64, error) {
	users, total, err := s.userRepo.FindList(tenantID, req.Username, req.Phone, req.Status, req.DeptID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}

	result := make([]interface{}, len(users))
	if len(users) == 0 {
		return result, total, nil
	}

	// 批量加载角色关系与角色详情，把 2N+1 次查询压缩为固定 3 次
	userIDs := make([]uint, 0, len(users))
	for _, u := range users {
		userIDs = append(userIDs, u.ID)
	}

	roleIDsByUser, err := s.userRepo.FindRoleIDsByUserIDs(userIDs)
	if err != nil {
		return nil, 0, err
	}

	roleIDSet := make(map[uint]struct{})
	for _, ids := range roleIDsByUser {
		for _, id := range ids {
			roleIDSet[id] = struct{}{}
		}
	}
	distinctRoleIDs := make([]uint, 0, len(roleIDSet))
	for id := range roleIDSet {
		distinctRoleIDs = append(distinctRoleIDs, id)
	}

	roleByID := make(map[uint]model.SysRole, len(distinctRoleIDs))
	if len(distinctRoleIDs) > 0 {
		roles, err := NewRoleService().FindByIDs(tenantID, distinctRoleIDs)
		if err != nil {
			return nil, 0, err
		}
		for _, r := range roles {
			roleByID[r.ID] = r
		}
	}

	for i, u := range users {
		ids := roleIDsByUser[u.ID]
		roleInfos := make([]vo.RoleInfo, 0, len(ids))
		for _, rid := range ids {
			if r, ok := roleByID[rid]; ok {
				roleInfos = append(roleInfos, vo.RoleInfo{ID: r.ID, Name: r.Name, Code: r.Code})
			}
		}
		result[i] = UserWithRoles{SysUser: u, Roles: roleInfos}
	}
	return result, total, nil
}

func (s *userService) UpdateStatus(tenantID uint, req *dto.StatusRequest) error {
	// 仓储会先确认目标属于本租户（不属于则返回 ErrRecordNotFound），
	// 因此下面的吊销只可能作用在本租户用户上 —— 早前不校验归属时，
	// 枚举 ID 就能强制下线其他租户的用户。
	if err := s.userRepo.UpdateStatus(tenantID, req.ID, req.Status); err != nil {
		return common.NotFoundOrErr(err, "用户不存在")
	}
	// 禁用用户时吊销其 Token
	if req.Status == common.StatusDisabled {
		return s.revokeUserTokens(req.ID)
	}
	return nil
}

func (s *userService) UpdateRoles(tenantID, operatorID uint, req *dto.UpdateUserRolesRequest) error {
	_, err := s.userRepo.FindByID(tenantID, req.ID)
	if err != nil {
		// 只把 gorm.ErrRecordNotFound 转成 404，其余（DB 故障、连接断开）
		// 必须原样透出成 500。此前这里写的是无条件 NewNotFoundError，
		// 于是数据库故障被报成「用户不存在」—— 用户按提示反复刷新，
		// 而监控里一条 5xx 都没有，故障可以静默持续（违反规则 5）。
		return common.NotFoundOrErr(err, "用户不存在")
	}

	// 校验角色归属与授权收敛：这是「更新角色」接口，也是跨租户提权
	// 与垂直提权最直接的入口
	roleIDs, err := s.normalizeRoleIDs(tenantID, operatorID, req.RoleIds)
	if err != nil {
		return err
	}
	if err := s.userRepo.ReplaceRoles(req.ID, roleIDs); err != nil {
		return err
	}
	// 角色变更后立即失效该用户的角色缓存，否则最长 60s 内仍按旧角色鉴权
	middleware.ClearRoleCache(tenantID, req.ID)
	return nil
}

func (s *userService) UpdateDept(tenantID uint, req *dto.UpdateUserDeptRequest) error {
	user, err := s.userRepo.FindByID(tenantID, req.ID)
	if err != nil {
		// 同 UpdateRoles：区分「不存在」（404）与「数据库故障」（500）
		return common.NotFoundOrErr(err, "用户不存在")
	}

	// 与 Update 里的 deptId 校验同源：sys_user.dept_id 指向租户内表，
	// 不校验就能把用户挂到别租户的部门上（界面上表现为「没有部门」）
	deptID, err := s.normalizeDeptID(tenantID, req.DeptID)
	if err != nil {
		return err
	}
	user.DeptID = deptID
	return s.userRepo.Update(tenantID, user)
}

func (s *userService) ResetPassword(tenantID uint, req *dto.ResetPasswordRequest) error {
	if err := validatePasswordStrength(req.Password); err != nil {
		return err
	}
	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		return err
	}
	if err := s.userRepo.ResetPassword(tenantID, req.ID, hash); err != nil {
		return err
	}

	// 必须连带吊销目标用户已签发的全部 Token。
	//
	// ChangePassword（用户自助改密）与 UpdateStatus（禁用账号）都会吊销，
	// 唯独这条管理员重置路径之前漏了 —— 而「密码疑似泄露、紧急重置」正是它最
	// 主要的使用场景。不吊销的话，攻击者手里的 refresh token 仍能继续换发新的
	// access token，重置密码等于没做。
	return s.revokeUserTokens(req.ID)
}

func (s *userService) ChangePassword(userID uint, req *dto.ChangePasswordRequest) error {
	user, err := s.userRepo.FindByID(0, userID)
	if err != nil {
		return common.NotFoundOrErr(err, "用户不存在")
	}

	if !utils.CheckPassword(req.OldPassword, user.Password) {
		return common.NewBizError("旧密码错误")
	}

	if err := validatePasswordStrength(req.NewPassword); err != nil {
		return err
	}

	hash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		return err
	}
	if err := s.userRepo.ResetPassword(0, userID, hash); err != nil {
		return err
	}

	// 密码修改后吊销所有 Token
	return s.revokeUserTokens(userID)
}

// revokeUserTokens 吊销用户的所有 refresh token，并使其旧 access token 失效。
//
// refresh token 本身是随机串，必须依靠登录时登记的用户维度集合才能枚举出来；
// 早前直接删 refresh_token:user:<id> 是删了一个从未写入的键，等于没吊销。
//
// 失败必须返回错误，而不是只记一条日志继续返回成功：调用方（改密、重置密码、
// 停用、删除）都是**安全动作**，它们依赖这个返回值来表达「该账号已下线」。
// 静默失败会让「已停用」只体现在数据库里 —— 账号仍能继续访问，
// 直到 access token 自然过期（2h），甚至用 refresh token 继续换发（7 天）。
func (s *userService) revokeUserTokens(userID uint) error {
	ctx := context.Background()

	tokens, err := cache.SMembers(ctx, cache.RefreshTokenSetKey(userID))
	if errors.Is(err, cache.ErrNotReady) {
		// Redis 未启用：系统本就没有可吊销的 refresh token（token 集合、
		// 吊销标记都无处存放），因此这里不是失败而是无事可做。
		//
		// 为什么可以安全返回 nil：Auth 与 RefreshToken 检查吊销标记时同样
		// 依赖 Redis，`cache.IsTokenRevoked` 在 ErrNotReady 下 fail-closed
		// （见 middleware/auth.go），也就是说没有 Redis 时请求本来就进不来。
		// 若在这里返回错误，只会让「未配置 Redis 的部署」连停用用户都做不到。
		return nil
	}
	if err != nil {
		// 读不到集合就枚举不出该用户的 refresh token。此时**不能**只删集合键
		// 就当作吊销完成 —— 真正的 refresh_token:* 会全部残留，而调用方
		// 会以为已经下线。宁可报错让调用方知道吊销没做成。
		return fmt.Errorf("读取用户 refresh token 列表失败: %w", err)
	}

	keys := make([]string, 0, len(tokens)+2)
	for _, t := range tokens {
		keys = append(keys, cache.RefreshTokenKey(t))
	}
	keys = append(keys, cache.RefreshTokenSetKey(userID))
	// 标记一并删除，避免「集合已清空、标记还在」的中间态
	keys = append(keys, cache.UserTokenRevokedKey(userID))

	if err := cache.Del(ctx, keys...); err != nil {
		return fmt.Errorf("吊销 refresh token 失败: %w", err)
	}

	// 标记的存活时间必须覆盖 **refresh token 的有效期**，而不是 access token 的。
	// 该标记同时被 Auth 与 RefreshToken 检查：TTL 只等于 AccessExpire（2h）时，
	// 2 小时后被停用的账号又能用 refresh token 换出新的 access token，
	// 「停用」等于没生效。
	if err := cache.Set(ctx, cache.UserTokenRevokedKey(userID), "1",
		time.Duration(config.Cfg.JWT.RefreshExpire)*time.Second); err != nil {
		return fmt.Errorf("设置 token 吊销标记失败: %w", err)
	}
	return nil
}

// maxPasswordBytes bcrypt 能处理的密码长度上限（字节）。
//
// 它不是我们的策略选择，而是 bcrypt 的硬限制：超过 72 字节
// GenerateFromPassword 会直接报错。放在这里是为了让「为什么是 72」
// 有一个可查的来源，而不是散落在错误文案里。
const maxPasswordBytes = 72

// validatePasswordStrength 校验密码强度：至少包含大写字母、小写字母、数字中的两种
func validatePasswordStrength(password string) error {
	if len(password) < 6 {
		return common.NewBizError("密码长度不能少于6位")
	}
	var hasUpper, hasLower, hasDigit bool
	for _, ch := range password {
		switch {
		case unicode.IsUpper(ch):
			hasUpper = true
		case unicode.IsLower(ch):
			hasLower = true
		case unicode.IsDigit(ch):
			hasDigit = true
		}
	}
	types := 0
	if hasUpper {
		types++
	}
	if hasLower {
		types++
	}
	if hasDigit {
		types++
	}
	if types < 2 {
		return common.NewBizError("密码必须包含大写字母、小写字母、数字中的至少两种")
	}
	if strings.ContainsAny(password, " \t\n\r") {
		return common.NewBizError("密码不能包含空格")
	}
	// bcrypt 的输入上限是 72 **字节**（不是字符）。
	//
	// 超过时 GenerateFromPassword 直接返回 ErrPasswordTooLong 而不是截断，
	// 于是 utils.HashPassword 报错、整条链路以 500「服务器内部错误」结束 ——
	// 用户只是密码太长，却拿不到任何可操作的提示。在业务层拦成 400。
	//
	// 必须按字节数判断：DTO 上的 `max=128` 是**字符**数，一个汉字占 3 字节，
	// 128 个汉字是 384 字节，照样超限。
	if len(password) > maxPasswordBytes {
		return common.NewBizError("密码过长，请控制在 72 个字节以内（约 72 个英文字符或 24 个汉字）")
	}
	return nil
}
