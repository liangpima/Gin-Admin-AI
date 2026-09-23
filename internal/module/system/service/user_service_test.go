package service

import (
	"errors"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	"go-admin/pkg/utils"
)

// stubRoleService 只实现 normalizeRoleIDs 会触达的 FindByIDs，
// 其余方法不会被调用，返回零值即可。
type stubRoleService struct {
	// roles 模拟「按 tenantID 过滤后」查到的角色
	roles []model.SysRole
	err   error
	// lastIDs 记录实际收到的 ID 列表，用于校验去重/过滤行为
	lastIDs []uint
}

func (s *stubRoleService) Create(req *dto.CreateRoleRequest, operatorID, tenantID uint) error {
	return nil
}
func (s *stubRoleService) Update(req *dto.UpdateRoleRequest, operatorID, tenantID uint) error {
	return nil
}
func (s *stubRoleService) Delete(tenantID, id uint) error { return nil }
func (s *stubRoleService) FindByID(tenantID, id uint) (interface{}, error) {
	return nil, nil
}
func (s *stubRoleService) FindByIDs(tenantID uint, ids []uint) ([]model.SysRole, error) {
	s.lastIDs = ids
	return s.roles, s.err
}
func (s *stubRoleService) FindList(tenantID uint, req *dto.RoleListRequest) ([]interface{}, int64, error) {
	return nil, 0, nil
}
func (s *stubRoleService) UpdateStatus(tenantID uint, req *dto.StatusRequest) error { return nil }
func (s *stubRoleService) FindAll(tenantID uint) ([]model.SysRole, error)          { return nil, nil }

// TestNormalizeRoleIDsRejectsForeignRole 回归保护：跨租户角色分配必须被拒绝。
//
// sys_user_role 是纯关联表（只有 user_id / role_id，没有 tenant_id 列），
// 租户隔离无法靠 TenantScope 完成。不校验的后果是租户 A 的管理员可以把用户绑到
// 租户 B 的角色 ID 上（ID 可枚举），而 Casbin 策略是按角色 code 生成的，
// 绑上即继承对方的菜单与权限码 —— 跨租户权限提升。
func TestNormalizeRoleIDsRejectsForeignRole(t *testing.T) {
	// 请求绑 2 个角色，但按本租户过滤后只查到 1 个 → 另一个属于其他租户
	stub := &stubRoleService{roles: []model.SysRole{{Code: "editor"}}}
	svc := &userService{roleService: stub}

	_, err := svc.normalizeRoleIDs(2, []uint{1, 99})
	if err == nil {
		t.Fatal("包含非本租户的角色 ID 时必须拒绝")
	}
	if !common.IsBizError(err) {
		t.Errorf("应返回业务错误(400)，实际: %T", err)
	}
}

// TestNormalizeRoleIDsAcceptsOwnRoles 本租户角色应正常通过。
func TestNormalizeRoleIDsAcceptsOwnRoles(t *testing.T) {
	stub := &stubRoleService{roles: []model.SysRole{{Code: "editor"}, {Code: "viewer"}}}
	svc := &userService{roleService: stub}

	got, err := svc.normalizeRoleIDs(2, []uint{1, 2})
	if err != nil {
		t.Fatalf("本租户角色不应报错: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("期望 2 个角色，实际 %d", len(got))
	}
}

// TestNormalizeRoleIDsDedupesAndDropsZero 重复项与 0 应被剔除后再查询。
func TestNormalizeRoleIDsDedupesAndDropsZero(t *testing.T) {
	stub := &stubRoleService{roles: []model.SysRole{{Code: "editor"}}}
	svc := &userService{roleService: stub}

	got, err := svc.normalizeRoleIDs(1, []uint{1, 1, 0, 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("重复项与 0 应被剔除，实际返回 %v", got)
	}
	if len(stub.lastIDs) != 1 || stub.lastIDs[0] != 1 {
		t.Errorf("传给 FindByIDs 的应为去重后的 [1]，实际 %v", stub.lastIDs)
	}
}

// TestNormalizeRoleIDsEmptySkipsQuery 空输入不应触发查询。
func TestNormalizeRoleIDsEmptySkipsQuery(t *testing.T) {
	stub := &stubRoleService{err: errors.New("FindByIDs 不应被调用")}
	svc := &userService{roleService: stub}

	for _, in := range [][]uint{nil, {}, {0}} {
		got, err := svc.normalizeRoleIDs(1, in)
		if err != nil {
			t.Errorf("空输入不应报错, in=%v, err=%v", in, err)
		}
		if got != nil {
			t.Errorf("空输入应返回 nil, in=%v, got=%v", in, got)
		}
	}

	// 传空数组表示「清空角色」，调用方会走到 ReplaceRoles(userID, nil)
	got, err := svc.normalizeRoleIDs(1, []uint{})
	if err != nil || got != nil {
		t.Errorf("空数组应返回 (nil, nil) 以便清空角色, got=%v err=%v", got, err)
	}
}

// TestNormalizeRoleIDsPropagatesSystemError DB 故障必须原样上抛，
// 不能被包装成 400 业务错误 —— 否则数据库挂了会显示成「参数不合法」。
func TestNormalizeRoleIDsPropagatesSystemError(t *testing.T) {
	stub := &stubRoleService{err: errors.New("db down")}
	svc := &userService{roleService: stub}

	_, err := svc.normalizeRoleIDs(1, []uint{1})
	if err == nil {
		t.Fatal("系统错误必须上抛")
	}
	if common.IsBizError(err) {
		t.Error("系统错误不应被包装成业务错误")
	}
}

// stubPostService 与 stubRoleService 同构，只实现 normalizePostIDs 会触达的 FindByIDs。
type stubPostService struct {
	posts   []model.SysPost
	err     error
	lastIDs []uint
}

func (s *stubPostService) Create(tenantID uint, name, code string, sort int, status int8, operatorID uint) error {
	return nil
}
func (s *stubPostService) Update(tenantID, id uint, name, code string, sort int, status int8, operatorID uint) error {
	return nil
}
func (s *stubPostService) Delete(tenantID, id uint) error { return nil }
func (s *stubPostService) FindByID(tenantID, id uint) (interface{}, error) {
	return nil, nil
}
func (s *stubPostService) FindByIDs(tenantID uint, ids []uint) ([]model.SysPost, error) {
	s.lastIDs = ids
	return s.posts, s.err
}
func (s *stubPostService) FindAll(tenantID uint) ([]model.SysPost, error) { return nil, nil }
func (s *stubPostService) FindList(tenantID uint, name string, status *int8, page, pageSize int) ([]interface{}, int64, error) {
	return nil, 0, nil
}
func (s *stubPostService) UpdateStatus(tenantID, id uint, status int8) error { return nil }

// TestNormalizePostIDsRejectsForeignPost 岗位已改为租户内数据，
// 因此与角色同理：sys_user_post 是纯关联表（无 tenant_id），
// 必须确认岗位属于当前租户后再绑定，否则可拿其他租户的岗位 ID 建立跨租户绑定。
func TestNormalizePostIDsRejectsForeignPost(t *testing.T) {
	stub := &stubPostService{posts: []model.SysPost{{Code: "dev"}}}
	svc := &userService{postService: stub}

	_, err := svc.normalizePostIDs(2, []uint{1, 99})
	if err == nil {
		t.Fatal("包含非本租户的岗位 ID 时必须拒绝")
	}
	if !common.IsBizError(err) {
		t.Errorf("应返回业务错误(400)，实际: %T", err)
	}
}

func TestNormalizePostIDsAcceptsOwnPosts(t *testing.T) {
	stub := &stubPostService{posts: []model.SysPost{{Code: "dev"}, {Code: "ops"}}}
	svc := &userService{postService: stub}

	got, err := svc.normalizePostIDs(2, []uint{1, 2})
	if err != nil {
		t.Fatalf("本租户岗位不应报错: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("期望 2 个岗位，实际 %d", len(got))
	}
}

func TestNormalizePostIDsDedupesAndDropsZero(t *testing.T) {
	stub := &stubPostService{posts: []model.SysPost{{Code: "dev"}}}
	svc := &userService{postService: stub}

	got, err := svc.normalizePostIDs(1, []uint{1, 1, 0, 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("重复项与 0 应被剔除，实际返回 %v", got)
	}
	if len(stub.lastIDs) != 1 || stub.lastIDs[0] != 1 {
		t.Errorf("传给 FindByIDs 的应为去重后的 [1]，实际 %v", stub.lastIDs)
	}
}

func TestNormalizePostIDsEmptySkipsQuery(t *testing.T) {
	stub := &stubPostService{err: errors.New("FindByIDs 不应被调用")}
	svc := &userService{postService: stub}

	for _, in := range [][]uint{nil, {}, {0}} {
		got, err := svc.normalizePostIDs(1, in)
		if err != nil {
			t.Errorf("空输入不应报错, in=%v, err=%v", in, err)
		}
		if got != nil {
			t.Errorf("空输入应返回 nil, in=%v, got=%v", in, got)
		}
	}
}

func TestNormalizePostIDsPropagatesSystemError(t *testing.T) {
	stub := &stubPostService{err: errors.New("db down")}
	svc := &userService{postService: stub}

	_, err := svc.normalizePostIDs(1, []uint{1})
	if err == nil {
		t.Fatal("系统错误必须上抛")
	}
	if common.IsBizError(err) {
		t.Error("系统错误不应被包装成业务错误")
	}
}

const testTenantID uint = 1

// TestValidatePasswordStrength 密码强度策略。
//
// 这是唯一一道挡住弱口令的关卡（创建、重置、改密三条路径都走它），
// 策略一旦被改松，靠人工 review 很难发现，因此逐条钉住。
func TestValidatePasswordStrength(t *testing.T) {
	cases := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"大小写字母+数字", "Abc12345", false},
		{"仅大小写字母", "Abcdefgh", false},
		{"仅小写+数字", "abc12345", false},
		{"仅大写+数字", "ABC12345", false},
		{"太短（5位）", "Abc12", true},
		{"恰好6位且满足两类", "Abc123", false},
		{"只有小写字母", "abcdefgh", true},
		{"只有数字", "12345678", true},
		{"含空格", "Abc 1234", true},
		{"含制表符", "Abc\t1234", true},
		{"空密码", "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePasswordStrength(c.password)
			if c.wantErr && err == nil {
				t.Errorf("密码 %q 应被拒绝，实际通过", c.password)
			}
			if !c.wantErr && err != nil {
				t.Errorf("密码 %q 应通过，实际被拒: %v", c.password, err)
			}
			// 密码策略属于业务校验，必须是 400 而不是 500
			if err != nil && !common.IsBizError(err) {
				t.Errorf("应为业务错误，实际 %T", err)
			}
		})
	}
}

func TestUserServiceCreate(t *testing.T) {
	t.Run("用户名已存在时拒绝", func(t *testing.T) {
		repo := &mockUserRepo{
			countByUsernameFn: func(string, uint) (int64, error) { return 1, nil },
		}
		svc := newTestUserService(repo)

		err := svc.Create(testTenantID, &dto.CreateUserRequest{
			Username: "admin", Password: "Abc12345",
		}, 1)

		assertBizError(t, err, common.CodeBadRequest)
		if len(repo.createdUsers) != 0 {
			t.Error("重名时不应写入数据库")
		}
	})

	t.Run("弱密码时拒绝", func(t *testing.T) {
		repo := &mockUserRepo{}
		svc := newTestUserService(repo)

		err := svc.Create(testTenantID, &dto.CreateUserRequest{
			Username: "newbie", Password: "123456",
		}, 1)

		assertBizError(t, err, common.CodeBadRequest)
		if len(repo.createdUsers) != 0 {
			t.Error("弱密码时不应写入数据库")
		}
	})

	t.Run("成功时密码以 bcrypt 落库而非明文", func(t *testing.T) {
		repo := &mockUserRepo{}
		svc := newTestUserService(repo)

		err := svc.Create(testTenantID, &dto.CreateUserRequest{
			Username: "newbie", Password: "Abc12345",
			Nickname: "新人", DeptID: 1, Status: common.StatusEnabled,
		}, 7)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(repo.createdUsers) != 1 {
			t.Fatalf("应写入 1 个用户，实际 %d", len(repo.createdUsers))
		}
		u := repo.createdUsers[0]

		if u.Password == "Abc12345" {
			t.Fatal("密码被明文存储了")
		}
		if !utils.CheckPassword("Abc12345", u.Password) {
			t.Error("落库的密码无法通过校验")
		}
		if u.TenantID != testTenantID {
			t.Errorf("租户应绑定为 %d，实际 %d", testTenantID, u.TenantID)
		}
		if u.CreateBy != 7 || u.UpdateBy != 7 {
			t.Errorf("操作人应记录为 7，实际 createBy=%d updateBy=%d", u.CreateBy, u.UpdateBy)
		}
	})
}

func TestUserServiceChangePassword(t *testing.T) {
	hashed, err := utils.HashPassword("OldPass123")
	if err != nil {
		t.Fatalf("准备密码哈希失败: %v", err)
	}

	newSvc := func() (*userService, *mockUserRepo) {
		repo := &mockUserRepo{
			findByIDFn: func(uint, uint) (*model.SysUser, error) {
				return &model.SysUser{
					TenantBaseModel: common.TenantBaseModel{
						BaseModel: common.BaseModel{ID: 9},
						TenantID:  testTenantID,
					},
					Password: hashed,
				}, nil
			},
		}
		return newTestUserService(repo), repo
	}

	t.Run("用户不存在返回 404", func(t *testing.T) {
		repo := &mockUserRepo{findByIDFn: func(uint, uint) (*model.SysUser, error) {
			return nil, errNotFound
		}}
		svc := newTestUserService(repo)

		err := svc.ChangePassword(9, &dto.ChangePasswordRequest{
			OldPassword: "OldPass123", NewPassword: "NewPass123",
		})
		assertBizError(t, err, common.CodeNotFound)
	})

	t.Run("旧密码错误时拒绝", func(t *testing.T) {
		svc, repo := newSvc()

		err := svc.ChangePassword(9, &dto.ChangePasswordRequest{
			OldPassword: "WrongPass1", NewPassword: "NewPass123",
		})
		assertBizError(t, err, common.CodeBadRequest)
		if repo.resetPwdCalls != 0 {
			t.Error("旧密码校验失败时不应写库")
		}
	})

	t.Run("新密码强度不足时拒绝", func(t *testing.T) {
		svc, repo := newSvc()

		err := svc.ChangePassword(9, &dto.ChangePasswordRequest{
			OldPassword: "OldPass123", NewPassword: "123456",
		})
		assertBizError(t, err, common.CodeBadRequest)
		if repo.resetPwdCalls != 0 {
			t.Error("新密码强度不足时不应写库")
		}
	})

	t.Run("旧密码正确且新密码合规时写入哈希", func(t *testing.T) {
		// 注意：成功路径会走到 revokeUserTokens（依赖 Redis），
		// 因此这里只断言「校验通过后确实调用了写库」，不覆盖吊销部分。
		svc, repo := newSvc()

		_ = svc.ChangePassword(9, &dto.ChangePasswordRequest{
			OldPassword: "OldPass123", NewPassword: "NewPass123",
		})

		if repo.resetPwdCalls != 1 {
			t.Fatalf("应写入一次新密码，实际 %d 次", repo.resetPwdCalls)
		}
		if repo.lastResetPwdVal == "NewPass123" {
			t.Error("新密码被明文写入了")
		}
		if !utils.CheckPassword("NewPass123", repo.lastResetPwdVal) {
			t.Error("写入的密码无法通过校验")
		}
	})
}

func TestUserServiceUpdate(t *testing.T) {
	t.Run("用户不存在返回 404 而不是 500", func(t *testing.T) {
		repo := &mockUserRepo{findByIDFn: func(uint, uint) (*model.SysUser, error) {
			return nil, errNotFound
		}}
		svc := newTestUserService(repo)

		err := svc.Update(testTenantID, &dto.UpdateUserRequest{ID: 999}, 1)
		assertBizError(t, err, common.CodeNotFound)
	})
}

func TestUserServiceUpdateRoles(t *testing.T) {
	t.Run("用户不存在返回 404", func(t *testing.T) {
		repo := &mockUserRepo{findByIDFn: func(uint, uint) (*model.SysUser, error) {
			return nil, errNotFound
		}}
		svc := newTestUserService(repo)

		err := svc.UpdateRoles(testTenantID, &dto.UpdateUserRolesRequest{ID: 999, RoleIds: []uint{1}})
		assertBizError(t, err, common.CodeNotFound)
		if repo.replacedRoles != nil {
			t.Error("用户不存在时不应改写角色")
		}
	})
}

// TestUserUpdatePartialKeepsFields 「只改状态」的部分更新回归。
//
// 背景：UpdateUserRequest 改为指针语义后，nil 字段必须原样保留。
// 若实现被改回无条件赋值，未提供的 email/phone/deptId/remark 会被清成零值。
func TestUserUpdatePartialKeepsFields(t *testing.T) {
	full := &model.SysUser{
		Nickname: "老昵称",
		Email:    "keep@example.com",
		Phone:    "13800000001",
		Status:   1,
		DeptID:   9,
	}
	full.Remark = "保留我"
	repo := &mockUserRepo{findByIDFn: func(uint, uint) (*model.SysUser, error) {
		return full, nil
	}}
	svc := newTestUserService(repo)

	status := int8(0)
	if err := svc.Update(testTenantID, &dto.UpdateUserRequest{ID: full.ID, Status: &status}, 1); err != nil {
		t.Fatalf("只改状态应成功: %v", err)
	}
	if len(repo.updatedUsers) != 1 {
		t.Fatalf("应落库一次，实际 %d 次", len(repo.updatedUsers))
	}
	got := repo.updatedUsers[0]
	if got.Status != 0 {
		t.Errorf("显式 status=0 未生效: %d", got.Status)
	}
	if got.Email != "keep@example.com" {
		t.Errorf("email 被部分更新清掉了: %q", got.Email)
	}
	if got.Phone != "13800000001" {
		t.Errorf("phone 被部分更新清掉了: %q", got.Phone)
	}
	if got.DeptID != 9 {
		t.Errorf("deptId 被部分更新清掉了: %d", got.DeptID)
	}
	if got.Remark != "保留我" {
		t.Errorf("remark 被部分更新清掉了: %q", got.Remark)
	}
}

// TestUserUpdateEmptyRoleIdsClears RoleIds 的 nil / 空切片语义：
// 缺省不动角色，显式 [] 才清空（清空还要走 normalizeRoleIDs 后落库）。
func TestUserUpdateEmptyRoleIdsClears(t *testing.T) {
	repo := &mockUserRepo{findByIDFn: func(uint, uint) (*model.SysUser, error) {
		return &model.SysUser{Nickname: "n"}, nil
	}}
	svc := newTestUserService(repo)

	// nil：不触发 ReplaceRoles
	if err := svc.Update(testTenantID, &dto.UpdateUserRequest{ID: 1, Nickname: stringp2("改名")}, 1); err != nil {
		t.Fatalf("只改昵称应成功: %v", err)
	}
	if repo.replacedRoles != nil {
		t.Error("未提供 roleIds 时不应改写角色关联")
	}

	// 空切片：清空
	if err := svc.Update(testTenantID, &dto.UpdateUserRequest{ID: 1, RoleIds: []uint{}}, 1); err != nil {
		t.Fatalf("清空角色应成功: %v", err)
	}
	if len(repo.updatedUsers) != 2 {
		t.Fatalf("两次都应落库，实际 %d 次", len(repo.updatedUsers))
	}
}

func stringp2(v string) *string { return &v }
