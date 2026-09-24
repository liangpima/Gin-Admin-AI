package repository

import (
	"errors"
	"testing"

	"go-admin/internal/database"
	"go-admin/internal/module/system/model"
	"go-admin/internal/testsupport"

	"gorm.io/gorm"
)

// 本文件的用例都会改写包级 database.DB，因此**不能** t.Parallel。
//
// sys_menu 是**全局表**（无 tenant_id），菜单树是所有租户共用的功能清单。
// 这里重点关注两类：FindMenusByRoleIDs 决定「用户能看到哪些菜单」，
// FindPermissionsByIDs 决定「这次授权涉及哪些权限码」（授权收敛校验的输入）——
// 两者算错都会直接变成权限问题。

func newMenuRepoWithDB(t *testing.T) MenuRepository {
	t.Helper()
	// 先建库（注入 database.DB），再构造仓储 —— 仓储在构造时捕获 database.DB
	testsupport.NewDB(t, &model.SysMenu{}, &model.SysRoleMenu{})
	return NewMenuRepository()
}

func seedMenu(t *testing.T, repo MenuRepository, name, title string, parentID uint, sort int, status int8, permission string) *model.SysMenu {
	t.Helper()
	m := &model.SysMenu{
		ParentID:   parentID,
		Name:       name,
		Title:      title,
		Type:       1,
		Permission: permission,
		Sort:       sort,
		Status:     status,
	}
	if err := repo.Create(m); err != nil {
		t.Fatalf("创建菜单失败: %v", err)
	}
	return m
}

func bindRoleMenus(t *testing.T, roleID uint, menuIDs ...uint) {
	t.Helper()
	rels := make([]model.SysRoleMenu, 0, len(menuIDs))
	for _, id := range menuIDs {
		rels = append(rels, model.SysRoleMenu{RoleID: roleID, MenuID: id})
	}
	if len(rels) == 0 {
		return
	}
	if err := database.DB.Create(&rels).Error; err != nil {
		t.Fatalf("绑定角色菜单失败: %v", err)
	}
}

// TestMenuRepositoryCRUD 基本读写
func TestMenuRepositoryCRUD(t *testing.T) {
	repo := newMenuRepoWithDB(t)
	created := seedMenu(t, repo, "system", "系统管理", 0, 1, 1, "")

	t.Run("按 ID 读回", func(t *testing.T) {
		got, err := repo.FindByID(created.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if got.Name != "system" || got.Title != "系统管理" {
			t.Errorf("读回的数据不一致: %+v", got)
		}
	})

	t.Run("Update 生效", func(t *testing.T) {
		created.Title = "系统设置"
		created.Sort = 9
		if err := repo.Update(created); err != nil {
			t.Fatalf("更新失败: %v", err)
		}

		got, err := repo.FindByID(created.ID)
		if err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Title != "系统设置" || got.Sort != 9 {
			t.Errorf("更新未落库: %+v", got)
		}
	})

	t.Run("不存在的 ID 返回 ErrRecordNotFound", func(t *testing.T) {
		if _, err := repo.FindByID(999999); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("应返回 ErrRecordNotFound，实际 %v", err)
		}
	})
}

// TestMenuRepositoryFindAll 与 FindAllForManage 的区别：
// 前者只给启用中的（构建用户可见的路由），后者含停用（管理页要能编辑）。
//
// 把管理页也接成 FindAll，停用的菜单就会在列表里凭空消失 ——
// 运营再也点不进去启用它。
func TestMenuRepositoryFindAll(t *testing.T) {
	repo := newMenuRepoWithDB(t)
	enabled := seedMenu(t, repo, "system", "系统管理", 0, 1, 1, "")
	disabled := seedMenu(t, repo, "monitor", "监控", 0, 2, 0, "")

	t.Run("FindAll 只给启用中的", func(t *testing.T) {
		list, err := repo.FindAll()
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(list) != 1 || list[0].ID != enabled.ID {
			t.Errorf("只应返回启用中的菜单，实际 %+v", list)
		}
	})

	t.Run("FindAllForManage 含停用", func(t *testing.T) {
		list, err := repo.FindAllForManage()
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("管理列表应含停用菜单共 2 条，实际 %+v", list)
		}
		seen := map[uint]bool{}
		for _, m := range list {
			seen[m.ID] = true
		}
		if !seen[enabled.ID] || !seen[disabled.ID] {
			t.Errorf("管理列表应同时含启用与停用，实际 %+v", list)
		}
	})

	t.Run("按 sort 升序", func(t *testing.T) {
		list, err := repo.FindAllForManage()
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		for i := 1; i < len(list); i++ {
			if list[i-1].Sort > list[i].Sort {
				t.Fatalf("应按 sort ASC 排序，实际 %+v", list)
			}
		}
	})
}

// TestMenuRepositoryFindMenusByRoleIDs 角色 → 菜单的去重查询。
//
// 这是「用户能看到哪些菜单」的唯一来源。要点：
//   - 多个角色共用一个菜单时不能重复返回（否则前端菜单树出现重复节点）
//   - 停用的菜单必须过滤（否则停用功能仍出现在侧边栏）
//   - 空角色列表不能报错（新用户还没分配角色是常见状态）
func TestMenuRepositoryFindMenusByRoleIDs(t *testing.T) {
	repo := newMenuRepoWithDB(t)
	shared := seedMenu(t, repo, "system", "系统管理", 0, 1, 1, "")
	onlyA := seedMenu(t, repo, "system-user", "用户管理", shared.ID, 1, 1, "system:user:list")
	onlyB := seedMenu(t, repo, "system-role", "角色管理", shared.ID, 2, 1, "system:role:list")
	disabled := seedMenu(t, repo, "monitor", "监控", 0, 3, 0, "")

	bindRoleMenus(t, 10, shared.ID, onlyA.ID, disabled.ID)
	bindRoleMenus(t, 20, shared.ID, onlyB.ID)

	t.Run("多角色共用菜单只返回一次", func(t *testing.T) {
		list, err := repo.FindMenusByRoleIDs([]uint{10, 20})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("应为 shared/onlyA/onlyB 共 3 条（shared 去重），实际 %d: %+v", len(list), list)
		}
		counts := map[uint]int{}
		for _, m := range list {
			counts[m.ID]++
		}
		if counts[shared.ID] != 1 {
			t.Errorf("两个角色共用的菜单被返回了 %d 次（前端会出现重复节点）", counts[shared.ID])
		}
	})

	t.Run("停用菜单被过滤", func(t *testing.T) {
		list, err := repo.FindMenusByRoleIDs([]uint{10})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		for _, m := range list {
			if m.ID == disabled.ID {
				t.Errorf("停用的菜单不应出现在用户菜单里: %+v", m)
			}
		}
	})

	t.Run("只返回被授权的菜单", func(t *testing.T) {
		list, err := repo.FindMenusByRoleIDs([]uint{20})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		for _, m := range list {
			if m.ID == onlyA.ID {
				t.Errorf("角色 20 没被授予 onlyA，不应返回: %+v", m)
			}
		}
	})

	t.Run("空角色列表不报错且返回空", func(t *testing.T) {
		// 新用户还没分配角色时会走到这里；若实现把空列表拼进 IN ()，
		// 在 MySQL 上是语法错误，用户登录直接失败
		list, err := repo.FindMenusByRoleIDs(nil)
		if err != nil {
			t.Fatalf("空角色列表不应报错: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("无角色应返回空菜单，实际 %+v", list)
		}
	})
}

// TestMenuRepositoryFindPermissionsByIDs 权限码提取。
//
// 这是「授权收敛」校验的输入：角色服务用它算出「本次授权涉及哪些权限码」，
// 再要求这些权限码必须是操作者已持有的子集。
// 因此多返回一个权限码会误伤（低权管理员建不了角色），
// 少返回一个则会放过提权（本次会话的安全基线里明确要求拦住的旁路）。
func TestMenuRepositoryFindPermissionsByIDs(t *testing.T) {
	repo := newMenuRepoWithDB(t)
	dir := seedMenu(t, repo, "system", "系统管理", 0, 1, 1, "")
	userList := seedMenu(t, repo, "system-user", "用户管理", dir.ID, 1, 1, "system:user:list")
	userAdd := seedMenu(t, repo, "system-user-add", "新增用户", dir.ID, 2, 1, "system:user:add")
	// 两个菜单声明同一个权限码，用于验证去重
	userAddDup := seedMenu(t, repo, "system-user-add2", "新增用户(副本)", dir.ID, 3, 1, "system:user:add")

	t.Run("提取权限码并去重", func(t *testing.T) {
		got, err := repo.FindPermissionsByIDs([]uint{userList.ID, userAdd.ID, userAddDup.ID})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("应为 system:user:list 与 system:user:add 共 2 个（去重后），实际 %+v", got)
		}
		seen := map[string]int{}
		for _, p := range got {
			seen[p]++
		}
		if seen["system:user:list"] != 1 || seen["system:user:add"] != 1 {
			t.Errorf("权限码去重不正确: %+v", got)
		}
	})

	t.Run("目录型菜单（无权限码）不参与", func(t *testing.T) {
		// 目录菜单 permission 为空，勾选它不构成授权。若把空串也算进来，
		// 低权管理员连建目录都会被「你无权授予空权限码」挡下
		got, err := repo.FindPermissionsByIDs([]uint{dir.ID})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("目录菜单不应贡献权限码，实际 %+v", got)
		}
	})

	t.Run("空 ID 列表返回空切片", func(t *testing.T) {
		got, err := repo.FindPermissionsByIDs(nil)
		if err != nil {
			t.Fatalf("空列表不应报错: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("应返回空切片，实际 %+v", got)
		}
	})

	t.Run("不存在的 ID 返回空", func(t *testing.T) {
		got, err := repo.FindPermissionsByIDs([]uint{999999})
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("不存在的菜单不应贡献权限码，实际 %+v", got)
		}
	})
}

// TestMenuRepositoryDeleteCleansRoleMenus 删除菜单必须清理角色-菜单关联。
//
// sys_role_menu 没有软删除，留下孤儿记录会让角色的菜单集合里含一个
// 已不存在的 menu_id：重建同 ID 菜单的概率极低，但 SyncPoliciesFromRoleMenus
// 遍历时会把孤儿当成真实菜单，算出来的权限集与界面显示不一致。
func TestMenuRepositoryDeleteCleansRoleMenus(t *testing.T) {
	repo := newMenuRepoWithDB(t)
	menu := seedMenu(t, repo, "system", "系统管理", 0, 1, 1, "")
	bindRoleMenus(t, 10, menu.ID)
	bindRoleMenus(t, 20, menu.ID)

	if err := repo.Delete(menu.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := repo.FindByID(menu.ID); err == nil {
		t.Error("已删除的菜单不应还能查到")
	}

	var left int64
	if err := database.DB.Model(&model.SysRoleMenu{}).Where("menu_id = ?", menu.ID).Count(&left).Error; err != nil {
		t.Fatalf("统计关联失败: %v", err)
	}
	if left != 0 {
		t.Errorf("删除菜单后应清掉全部角色关联，实际残留 %d 条（孤儿记录）", left)
	}
}

// TestMenuRepositoryParentHelpers 父子关系辅助方法（成环校验与删除前置检查）
func TestMenuRepositoryParentHelpers(t *testing.T) {
	repo := newMenuRepoWithDB(t)
	root := seedMenu(t, repo, "system", "系统管理", 0, 1, 1, "")
	child := seedMenu(t, repo, "system-user", "用户管理", root.ID, 1, 1, "system:user:list")
	seedMenu(t, repo, "system-user-add", "新增用户", child.ID, 1, 1, "system:user:add")

	t.Run("FindParentID 命中", func(t *testing.T) {
		parentID, ok, err := repo.FindParentID(child.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if !ok || parentID != root.ID {
			t.Errorf("应返回父节点 %d，实际 ok=%v parentID=%d", root.ID, ok, parentID)
		}
	})

	t.Run("FindParentID 根节点返回 0 但 ok=true", func(t *testing.T) {
		// 根节点存在、父 ID 就是 0。若实现用「parentID == 0」判存在，
		// 成环校验会把根节点当成「不存在」而放行，于是允许把根挂到自己下面
		parentID, ok, err := repo.FindParentID(root.ID)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if !ok {
			t.Error("根节点是存在的，ok 应为 true（用 parentID==0 判存在会漏掉它）")
		}
		if parentID != 0 {
			t.Errorf("根节点的父 ID 应为 0，实际 %d", parentID)
		}
	})

	t.Run("FindParentID 不存在时 ok=false", func(t *testing.T) {
		_, ok, err := repo.FindParentID(999999)
		if err != nil {
			t.Fatalf("不存在的 ID 不应报错（实现用 Pluck 而非 First）: %v", err)
		}
		if ok {
			t.Error("不存在的菜单 ok 应为 false")
		}
	})

	t.Run("CountByParentID 只数直接子节点", func(t *testing.T) {
		// 直接子节点为空时更深层必然也不存在，无需递归统计整棵子树
		n, err := repo.CountByParentID(root.ID)
		if err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if n != 1 {
			t.Errorf("根节点应只有 1 个直接子节点（孙节点不算），实际 %d", n)
		}

		n, err = repo.CountByParentID(child.ID)
		if err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if n != 1 {
			t.Errorf("child 应有 1 个直接子节点，实际 %d", n)
		}

		leaf := seedMenu(t, repo, "leaf", "叶子", 0, 9, 1, "")
		n, err = repo.CountByParentID(leaf.ID)
		if err != nil {
			t.Fatalf("统计失败: %v", err)
		}
		if n != 0 {
			t.Errorf("叶子节点应无子节点，实际 %d", n)
		}
	})
}
