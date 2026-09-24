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
	"go-admin/internal/logger"
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

// dedupeNonZeroIDs 去重并剔除 0，保持原有顺序；无有效项时返回 nil。
func dedupeNonZeroIDs(ids []uint) []uint {
	if len(ids) == 0 {
		return nil
	}
	unique := make([]uint, 0, len(ids))
	seen := make(map[uint]bool, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil
	}
	return unique
}

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
	unique := dedupeNonZeroIDs(roleIDs)
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
	unique := dedupeNonZeroIDs(postIDs)
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

	if err := s.userRepo.Create(user); err != nil {
		// 上面的 Count 校验存在时间窗口，并发下仍可能撞唯一索引，靠这里兜底
		if errors.Is(err, common.ErrDuplicateKey) {
			return common.NewBizError("用户名已存在")
		}
		return err
	}

	if len(roleIDs) > 0 {
		if err := s.userRepo.ReplaceRoles(user.ID, roleIDs); err != nil {
			return err
		}
	}
	if len(postIDs) > 0 {
		if err := s.userRepo.ReplacePosts(user.ID, postIDs); err != nil {
			return err
		}
	}

	return nil
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

	if err := s.userRepo.Update(user); err != nil {
		return err
	}

	if req.RoleIds != nil {
		if err := s.userRepo.ReplaceRoles(user.ID, roleIDs); err != nil {
			return err
		}
	}
	if req.PostIds != nil {
		if err := s.userRepo.ReplacePosts(user.ID, postIDs); err != nil {
			return err
		}
	}

	return nil
}

func (s *userService) Delete(tenantID, id uint) error {
	return s.userRepo.Delete(tenantID, id)
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
	if err := s.userRepo.UpdateStatus(tenantID, req.ID, req.Status); err != nil {
		return err
	}
	// 禁用用户时吊销其 Token
	if req.Status == common.StatusDisabled {
		s.revokeUserTokens(req.ID)
	}
	return nil
}

func (s *userService) UpdateRoles(tenantID, operatorID uint, req *dto.UpdateUserRolesRequest) error {
	_, err := s.userRepo.FindByID(tenantID, req.ID)
	if err != nil {
		return common.NewNotFoundError("用户不存在")
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
		return common.NewNotFoundError("用户不存在")
	}

	// 与 Update 里的 deptId 校验同源：sys_user.dept_id 指向租户内表，
	// 不校验就能把用户挂到别租户的部门上（界面上表现为「没有部门」）
	deptID, err := s.normalizeDeptID(tenantID, req.DeptID)
	if err != nil {
		return err
	}
	user.DeptID = deptID
	return s.userRepo.Update(user)
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
	s.revokeUserTokens(req.ID)
	return nil
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
	s.revokeUserTokens(userID)
	return nil
}

// revokeUserTokens 吊销用户的所有 refresh token，并使其旧 access token 失效。
//
// refresh token 本身是随机串，必须依靠登录时登记的用户维度集合才能枚举出来；
// 早前直接删 refresh_token:user:<id> 是删了一个从未写入的键，等于没吊销。
func (s *userService) revokeUserTokens(userID uint) {
	ctx := context.Background()

	tokens, err := cache.SMembers(ctx, cache.RefreshTokenSetKey(userID))
	if err != nil {
		logger.Log.Warnf("读取refresh token列表失败: %v", err)
	}

	keys := make([]string, 0, len(tokens)+1)
	for _, t := range tokens {
		keys = append(keys, cache.RefreshTokenKey(t))
	}
	keys = append(keys, cache.RefreshTokenSetKey(userID))

	if err := cache.Del(ctx, keys...); err != nil {
		logger.Log.Warnf("吊销refresh token失败: %v", err)
	}

	// 同时设置一个标记，使得该用户的所有旧 access token 失效
	if err := cache.Set(ctx, fmt.Sprintf("user:token_revoked:%d", userID), "1",
		time.Duration(config.Cfg.JWT.AccessExpire)*time.Second); err != nil {
		logger.Log.Warnf("设置token吊销标记失败: %v", err)
	}
}

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
	return nil
}
