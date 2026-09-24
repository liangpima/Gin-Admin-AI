package service

import (
	"context"
	"errors"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/internal/middleware"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/internal/module/system/repository"

	"gorm.io/gorm"
)

type RoleService interface {
	Create(req *dto.CreateRoleRequest, operatorID, tenantID uint) error
	Update(req *dto.UpdateRoleRequest, operatorID, tenantID uint) error
	Delete(tenantID, id uint) error
	FindByID(tenantID, id uint) (interface{}, error)
	FindByIDs(tenantID uint, ids []uint) ([]model.SysRole, error)
	FindList(tenantID uint, req *dto.RoleListRequest) ([]interface{}, int64, error)
	UpdateStatus(tenantID uint, req *dto.StatusRequest) error
	FindAll(tenantID uint) ([]model.SysRole, error)
	// EnsureRolesGrantable 校验操作者是否有权把这些角色授予他人。
	//
	// 供用户模块在绑定角色前调用：把角色绑到用户上等价于把该角色的全部权限
	// 授予该用户，因此「我能不能授予这个角色」必须和「我能不能给它配这些菜单」
	// 用同一套标准判定，否则会出现「建角色时受限、绑角色时不受限」的旁路。
	EnsureRolesGrantable(tenantID, operatorID uint, roles []model.SysRole) error
}

type roleService struct {
	roleRepo repository.RoleRepository
	menuRepo repository.MenuRepository
}

func NewRoleService() RoleService {
	return &roleService{
		roleRepo: repository.NewRoleRepository(),
		menuRepo: repository.NewMenuRepository(),
	}
}

// assertGrantable 授权收敛的核心判定：操作者必须**已持有**这些权限码。
//
// 为什么需要它：Casbin 策略是按「角色-菜单」关系生成的，给角色挂上菜单就等于
// 授予对应权限码。若不校验操作者自身是否持有，一个只被授予
// `system:role:add/edit` 的租户管理员就能建一个挂满全量菜单的角色并绑给自己，
// 从而垂直提权到超管 —— 权限校验只挡住了「这一步」，挡不住「这一步的效果」。
//
// 三条放行口径（都是有意的）：
//   - permissions 为空：本次授权不涉及任何权限码（只勾了目录型菜单），无需拦截
//   - 操作者持有 admin：admin 在 SyncPoliciesFromRoleMenus 中被写入通配策略
//     （*/*），它本就拥有全部权限，收敛判定对它恒真
//   - 目标权限码是操作者已有权限的子集：这正是「授权只向上收敛」的合法情形
func (s *roleService) assertGrantable(tenantID, operatorID uint, permissions []string, msg string) error {
	if len(permissions) == 0 {
		return nil
	}
	held, err := middleware.OperatorHoldsPermissions(tenantID, operatorID, permissions)
	if err != nil {
		// 判定失败属系统错误（如鉴权设施未就绪）：原样返回 → 500，
		// 绝不能因为「算不出来」就放行
		return err
	}
	if !held {
		return common.NewForbiddenError(msg)
	}
	return nil
}

// checkMenusGrantable 校验本次提交的菜单不超出操作者自身的权限范围
func (s *roleService) checkMenusGrantable(tenantID, operatorID uint, menuIDs []uint) error {
	if len(menuIDs) == 0 {
		return nil
	}
	perms, err := s.menuRepo.FindPermissionsByIDs(menuIDs)
	if err != nil {
		return err
	}
	return s.assertGrantable(tenantID, operatorID, perms,
		"不能授予超出自身权限范围的菜单，请联系超级管理员")
}

// checkReservedCode 保留角色编码（admin）不得被非 admin 操作者使用或改动。
//
// 为什么单独拦这一条：admin 编码在同步策略时被无条件写入通配策略，
// 与它绑定了哪些菜单无关。因此只要能拿到这个编码，就等于拿到全部权限 ——
// 无论是把已有角色改名成 admin，还是改掉 admin 角色的编码（通配策略会跟着
// 新编码走），都是绕过菜单维度校验的直接提权路径。
func (s *roleService) checkReservedCode(tenantID, operatorID uint, codes ...string) error {
	need := false
	for _, code := range codes {
		if code == middleware.AdminRoleCode {
			need = true
			break
		}
	}
	if !need {
		return nil
	}

	operatorRoles, err := middleware.RoleCodesFor(tenantID, operatorID)
	if err != nil {
		return err
	}
	if middleware.HasAdminRole(operatorRoles) {
		return nil
	}
	return common.NewForbiddenError("超级管理员角色仅限超级管理员创建或修改")
}

// EnsureRolesGrantable 见接口注释
func (s *roleService) EnsureRolesGrantable(tenantID, operatorID uint, roles []model.SysRole) error {
	if len(roles) == 0 {
		return nil
	}

	// admin 角色必须先于权限比对被拦下：它持有的权限码只等于「挂在它名下的菜单」，
	// 而它的真实权限是通配的 *，两者不一致 —— 若只比对菜单，一个碰巧持有
	// admin 全部菜单权限的低权管理员就能把 admin 角色授予他人。
	for _, r := range roles {
		if r.Code == middleware.AdminRoleCode {
			if err := s.checkReservedCode(tenantID, operatorID, r.Code); err != nil {
				return err
			}
		}
	}

	roleIDs := make([]uint, 0, len(roles))
	for _, r := range roles {
		roleIDs = append(roleIDs, r.ID)
	}
	perms, err := s.roleRepo.FindPermissionsByRoleIDs(roleIDs)
	if err != nil {
		return err
	}
	return s.assertGrantable(tenantID, operatorID, perms,
		"不能授予超出自身权限范围的角色，请联系超级管理员")
}

func (s *roleService) Create(req *dto.CreateRoleRequest, operatorID, tenantID uint) error {
	// 角色 code 必须**全局**唯一：Casbin 策略的主体是角色 code，域是常量 "default"，
	// 两个租户用同一 code 会导致策略合并、互相继承权限（跨租户泄漏）。
	// 所以重名校验也必须按全局来，否则会出现「校验通过、插入撞唯一索引」→ 500
	if count, err := s.roleRepo.CountByCode(req.Code, 0); err != nil {
		return err
	} else if count > 0 {
		return common.NewBizError("角色编码已存在")
	}

	// 授权收敛校验一律放在**落库之前**：否则校验失败会留下一个已建好、
	// 但没有权限（或权限被拒）的半成品角色，用户还得手工清理。
	if err := s.checkReservedCode(tenantID, operatorID, req.Code); err != nil {
		return err
	}
	if err := s.checkMenusGrantable(tenantID, operatorID, req.MenuIds); err != nil {
		return err
	}

	role := &model.SysRole{
		TenantBaseModel: common.TenantBaseModel{
			BaseModel: common.BaseModel{
				CreateBy: operatorID,
				UpdateBy: operatorID,
			},
			TenantID: tenantID,
		},
		Name:      req.Name,
		Code:      req.Code,
		Sort:      req.Sort,
		Status:    req.Status,
		DataScope: req.DataScope,
	}
	role.Remark = req.Remark

	if err := s.roleRepo.Create(role); err != nil {
		// 上面 Count 校验有时间窗口，并发下靠唯一索引兜底
		if errors.Is(err, common.ErrDuplicateKey) {
			return common.NewBizError("角色编码已存在")
		}
		return err
	}

	if len(req.MenuIds) > 0 {
		if err := s.roleRepo.ReplaceMenus(tenantID, role.ID, req.MenuIds); err != nil {
			return err
		}
		s.syncPolicies()
	}

	return nil
}

func (s *roleService) Update(req *dto.UpdateRoleRequest, operatorID, tenantID uint) error {
	// 同 Create：角色 code 全局唯一，改编码时也按全局校验。
	// 只在请求提供了 code 时校验 —— 部分更新（如只保存权限）不带 code，
	// 拿空串去查既无意义，也会掩盖真实意图。
	if req.Code != "" {
		if count, err := s.roleRepo.CountByCode(req.Code, req.ID); err != nil {
			return err
		} else if count > 0 {
			return common.NewBizError("角色编码已存在")
		}
	}

	role, err := s.roleRepo.FindByID(tenantID, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFoundError("角色不存在")
		}
		return err
	}

	// 授权收敛：既要拦住「改名为 admin」，也要拦住「改动 admin 角色本身」。
	// 后者常被忽略 —— admin 的通配策略不依赖菜单，但**依赖角色编码**，
	// 把 admin 改名成别的编码，等于把通配权限交给了那个编码。
	if err := s.checkReservedCode(tenantID, operatorID, role.Code, req.Code); err != nil {
		return err
	}
	// MenuIds 为 nil 表示本次不涉及授权（如只改名），此时不该触发校验
	if req.MenuIds != nil {
		if err := s.checkMenusGrantable(tenantID, operatorID, req.MenuIds); err != nil {
			return err
		}
	}

	// 逐字段判断「是否提供」再赋值，实现真正的部分更新。
	// 无条件赋值会把未提供的字段清成零值（历史上就是靠校验拦住了，
	// 代价是权限分配功能直接不可用）。
	if req.Name != "" {
		role.Name = req.Name
	}
	if req.Code != "" {
		role.Code = req.Code
	}
	if req.Sort != nil {
		role.Sort = *req.Sort
	}
	if req.Status != nil {
		role.Status = *req.Status
	}
	if req.DataScope != nil {
		role.DataScope = *req.DataScope
	}
	if req.Remark != nil {
		role.Remark = *req.Remark
	}
	role.UpdateBy = operatorID

	if err := s.roleRepo.Update(tenantID, role); err != nil {
		if errors.Is(err, common.ErrDuplicateKey) {
			return common.NewBizError("角色编码已存在")
		}
		return err
	}

	if req.MenuIds != nil {
		if err := s.roleRepo.ReplaceMenus(tenantID, role.ID, req.MenuIds); err != nil {
			return err
		}
	}

	// 角色编码/状态/授权变化都会影响策略，必须同步（不能只在菜单变更时同步）
	s.syncPolicies()

	return nil
}

func (s *roleService) Delete(tenantID, id uint) error {
	if err := s.roleRepo.Delete(tenantID, id); err != nil {
		return err
	}
	s.syncPolicies()
	return nil
}

// syncPolicies 角色授权变更后重建 Casbin 策略并清理角色缓存，使权限立即生效。
// 失败仅记录日志：授权数据已落库，下次启动会重新同步。
func (s *roleService) syncPolicies() {
	if err := middleware.SyncPoliciesFromRoleMenus(); err != nil {
		logger.Log.Errorf("同步权限策略失败: %v", err)
	}
	// 角色编码/状态变更后，用户角色缓存需失效，否则最长 60s 内仍按旧角色鉴权
	if err := cache.DelByPrefix(context.Background(), "rbac:roles:"); err != nil {
		logger.Log.Warnf("清理角色缓存失败: %v", err)
	}
}

func (s *roleService) FindByID(tenantID, id uint) (interface{}, error) {
	role, err := s.roleRepo.FindByID(tenantID, id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "角色不存在")
	}
	return role, nil
}

func (s *roleService) FindByIDs(tenantID uint, ids []uint) ([]model.SysRole, error) {
	return s.roleRepo.FindByIDs(tenantID, ids)
}

func (s *roleService) FindList(tenantID uint, req *dto.RoleListRequest) ([]interface{}, int64, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	req.Page, req.PageSize = common.NormalizePageParams(req.Page, req.PageSize)

	roles, total, err := s.roleRepo.FindList(tenantID, req.Name, req.Code, req.Status, req.Page, req.PageSize)
	if err != nil {
		return nil, 0, err
	}

	result := make([]interface{}, len(roles))
	for i, r := range roles {
		result[i] = r
	}
	return result, total, nil
}

func (s *roleService) UpdateStatus(tenantID uint, req *dto.StatusRequest) error {
	return s.roleRepo.UpdateStatus(tenantID, req.ID, req.Status)
}

func (s *roleService) FindAll(tenantID uint) ([]model.SysRole, error) {
	roles, _, err := s.roleRepo.FindList(tenantID, "", "", nil, 1, 1000)
	return roles, err
}
