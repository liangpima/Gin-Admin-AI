package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go-admin/config"
	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
	captchaService "go-admin/internal/module/captcha/service"
	"go-admin/internal/module/system/repository"
	"go-admin/internal/module/system/vo"
	"go-admin/pkg/auth"
	"go-admin/pkg/utils"

	"gorm.io/gorm"
)

type AuthService interface {
	// Login 完成「人机校验 → 限频判定 → 身份认证 → 写登录日志」整条链路。
	// lc 携带 HTTP 侧的 IP/UA，由 Controller 采集（Service 不依赖 gin）。
	Login(req *dto.LoginRequest, lc *dto.LoginContext) (*vo.LoginResponse, error)
	RefreshToken(req *dto.RefreshTokenRequest) (*vo.LoginResponse, error)
	// Logout 吊销指定 refresh token。
	// refreshToken 为空时退化为吊销该用户的全部 refresh token
	// （无法判断来源设备，宁可多吊销，也不留下可继续换发 access token 的凭据）。
	Logout(userID uint, refreshToken string) error
	// LogoutByToken 处理一次完整登出：拉黑 access token + 吊销 refresh token。
	// accessToken 为空时退化为「只吊销 refresh token」（旧客户端不带头）。
	LogoutByToken(accessToken, refreshToken string) error
	GetUserInfo(userID uint) (*vo.UserInfoResponse, error)
}

type authService struct {
	userRepo    repository.UserRepository
	roleService RoleService
	menuService MenuService
	logService  LogService
}

func NewAuthService() AuthService {
	return &authService{
		userRepo:    repository.NewUserRepository(),
		roleService: NewRoleService(),
		menuService: NewMenuService(),
		logService:  NewLogService(),
	}
}

// Login 是登录的对外入口，串联整条链路。
//
// 顺序有意固定为「人机校验 → 限频 → 认证 → 记录」：
//   - 人机校验放最前：它是挡自动化撞库的第一道闸，成本最低
//   - 限频在认证之前：否则攻击者可以靠不断尝试把账号锁死（DoS），
//     而且失败计数必须在真正比对密码前就查
//   - 无论成功失败都写登录日志：审计价值一半在失败记录里
func (s *authService) Login(req *dto.LoginRequest, lc *dto.LoginContext) (*vo.LoginResponse, error) {
	// 人机校验：凭证由 /captcha/verify 校验通过后签发，一次性。
	// 早前的写法在第一次 ShouldBindJSON 之后又绑定一次请求体取坐标 ——
	// 请求体已被读尽，二次绑定必然失败且错误被丢弃，于是整个校验分支从未执行过。
	if !captchaService.ConsumeVerifiedToken(req.CaptchaToken) {
		return nil, common.NewBizError("验证码无效或已失效，请重新验证")
	}

	ctx := context.Background()
	ip := ""
	if lc != nil {
		ip = lc.IP
	}
	keys := loginRateLimitKeys(ip, req.Username)

	locked, err := checkLoginRateLimit(ctx, keys...)
	if err != nil {
		// 限频设施不可用（fail-closed）：这是系统错误，对外由 FailWith 统一
		// 归为 500 + 通用文案，不泄漏 Redis 拓扑；日志侧已记录真实原因。
		s.saveLoginLog(0, req.Username, 0, "登录限频服务不可用", lc)
		return nil, err
	}
	if locked {
		s.saveLoginLog(0, req.Username, 0, "登录频率过高", lc)
		return nil, loginLockedError()
	}

	resp, err := s.authenticate(req)
	if err != nil {
		recordLoginFailure(ctx, keys...)
		s.saveLoginLog(0, req.Username, 0, err.Error(), lc)
		return nil, err
	}

	clearLoginFailure(ctx, keys...)

	// 从签发的 token 解析租户，使登录日志归属到正确租户
	tenantID := uint(0)
	if claims, parseErr := auth.ParseToken(resp.AccessToken); parseErr == nil {
		tenantID = claims.TenantID
	}
	s.saveLoginLog(tenantID, req.Username, 1, "登录成功", lc)

	return resp, nil
}

// authenticate 只做「凭据校验 + 签发 token」，不含限频与日志。
// 对外入口是 Login，它把限频、日志等横切逻辑串起来。
func (s *authService) authenticate(req *dto.LoginRequest) (*vo.LoginResponse, error) {
	user, err := s.userRepo.FindByUsernameForAuth(req.Username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 与密码错误返回相同信息，避免通过错误提示枚举用户名
			return nil, common.NewBizError("用户名或密码错误")
		}
		return nil, err
	}

	// 先校验密码，再检查账号状态，避免未通过验证即暴露账号状态
	if !utils.CheckPassword(req.Password, user.Password) {
		return nil, common.NewBizError("用户名或密码错误")
	}

	if user.Status == common.StatusDisabled {
		return nil, common.NewBizError("用户已被禁用")
	}

	accessToken, err := auth.GenerateAccessToken(user.ID, user.Username, user.TenantID, user.DeptID)
	if err != nil {
		return nil, err
	}

	refreshToken, err := auth.GenerateRefreshToken(user.ID, user.Username, user.TenantID, user.DeptID)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	refreshTTL := time.Duration(config.Cfg.JWT.RefreshExpire) * time.Second
	if err := cache.Set(ctx, cache.RefreshTokenKey(refreshToken), user.ID, refreshTTL); err != nil {
		return nil, fmt.Errorf("存储refresh token失败: %w", err)
	}
	if err := registerRefreshToken(ctx, user.ID, refreshToken, refreshTTL); err != nil {
		return nil, err
	}

	// 只写 login_time，不要用 Update(user) 整行回写。
	//
	// 早前这里是 `user.LoginTime = &now; _ = s.userRepo.Update(user)`，两个问题：
	// Update 的 Select 列表里没有 login_time，所以登录时间其实一次都没写进去；
	// 而列表里有 password，会把登录时读到的旧哈希一起写回，
	// 若期间管理员重置过该用户密码，重置会被静默回滚。
	if err := s.userRepo.UpdateLoginTime(user.TenantID, user.ID, time.Now()); err != nil {
		// 登录本身已成功（token 已签发），登录时间写入失败不应让登录失败
		logger.Log.Warnf("更新登录时间失败, userID=%d: %v", user.ID, err)
	}

	return &vo.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    config.Cfg.JWT.AccessExpire,
		TokenType:    "Bearer",
	}, nil
}

func (s *authService) RefreshToken(req *dto.RefreshTokenRequest) (*vo.LoginResponse, error) {
	claims, err := auth.ParseRefreshToken(req.RefreshToken)
	if err != nil {
		return nil, common.NewUnauthorizedError("refresh token无效")
	}

	ctx := context.Background()

	// 用户维度已被吊销（改密/禁用）时拒绝续期。
	// revokeUserTokens 会同时清掉集合里的 refresh token，这里是第二道防线：
	// 万一清理有遗漏，也不至于让一个已被停用的账号重新换出 access token。
	//
	// 查询失败必须拒绝（fail-closed）：Redis 抖动时把「查不了」当成「未吊销」，
	// 会让已停用账号继续换发新 access token。
	revoked, revokedErr := cache.Exists(ctx, fmt.Sprintf("user:token_revoked:%d", claims.UserID))
	if revokedErr != nil {
		return nil, fmt.Errorf("查询token吊销状态失败: %w", revokedErr)
	}
	if revoked {
		return nil, common.NewUnauthorizedError("token已失效，请重新登录")
	}

	exists, existsErr := cache.Exists(ctx, cache.RefreshTokenKey(req.RefreshToken))
	if existsErr != nil {
		return nil, fmt.Errorf("查询refresh token失败: %w", existsErr)
	}
	if !exists {
		return nil, common.NewUnauthorizedError("refresh token已过期")
	}

	// 轮换：旧 token 立即作废并从用户集合中移除。
	//
	// Del 失败必须**中断本次刷新**，不能只记日志继续：旧 token 仍然有效
	// 就等于轮换保证被打破 —— 被窃取的 refresh token 可以继续使用。
	// 此处尚未写入任何新 token，中断是干净的，用户重新登录即可。
	// 与下面 Logout 的处理保持一致（那边也是直接返回错误）。
	if err := cache.Del(ctx, cache.RefreshTokenKey(req.RefreshToken)); err != nil {
		logger.Log.Errorf("[auth] 旧 refresh token 作废失败，已中断本次刷新: err=%v", err)
		return nil, fmt.Errorf("刷新凭证失败，请重新登录: %w", err)
	}
	if claims.UserID > 0 {
		// 集合里残留一个已删除的 token 只影响「一键下线」的清理范围，
		// 不影响安全性，记录告警即可，不必打断用户的正常刷新。
		if err := cache.SRem(ctx, cache.RefreshTokenSetKey(claims.UserID), req.RefreshToken); err != nil {
			logger.Log.Warnf("[auth] 从用户 refresh token 集合中移除失败: userID=%d err=%v", claims.UserID, err)
		}
	}

	accessToken, err := auth.GenerateAccessToken(claims.UserID, claims.Username, claims.TenantID, claims.DeptID)
	if err != nil {
		return nil, err
	}

	refreshToken, err := auth.GenerateRefreshToken(claims.UserID, claims.Username, claims.TenantID, claims.DeptID)
	if err != nil {
		return nil, err
	}

	refreshTTL := time.Duration(config.Cfg.JWT.RefreshExpire) * time.Second
	if err := cache.Set(ctx, cache.RefreshTokenKey(refreshToken), claims.UserID, refreshTTL); err != nil {
		return nil, fmt.Errorf("存储refresh token失败: %w", err)
	}
	if err := registerRefreshToken(ctx, claims.UserID, refreshToken, refreshTTL); err != nil {
		return nil, err
	}

	return &vo.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    config.Cfg.JWT.AccessExpire,
		TokenType:    "Bearer",
	}, nil
}

// LogoutByToken 处理一次完整登出。
//
// 原先这段逻辑写在 Controller 的 Logout 里（解析 header、算 TTL、拉黑、吊销），
// 属于业务规则，已按规则 1 下沉。
//
// 两类失败的严重性不同，处理也不同：
//   - access token 拉黑失败：窗口有限（access token 本就短命），记日志即可
//   - refresh token 吊销失败：它是长期凭据，失败意味着「用户以为已登出、
//     实际仍能换发新 access token」，必须让调用方知道
func (s *authService) LogoutByToken(accessToken, refreshToken string) error {
	var userID uint
	if accessToken != "" {
		if claims, err := auth.ParseToken(accessToken); err == nil {
			userID = claims.UserID
			if claims.ExpiresAt != nil {
				if ttl := time.Until(claims.ExpiresAt.Time); ttl > 0 {
					if err := cache.RevokeToken(context.Background(), accessToken, ttl); err != nil {
						logger.Log.Warnf("[auth] access token 加入黑名单失败（影响窗口有限）: %v", err)
					}
				}
			}
		}
	}

	if err := s.Logout(userID, refreshToken); err != nil {
		return fmt.Errorf("吊销登录凭据失败: %w", err)
	}
	return nil
}

func (s *authService) Logout(userID uint, refreshToken string) error {
	ctx := context.Background()

	if refreshToken != "" {
		if err := cache.Del(ctx, cache.RefreshTokenKey(refreshToken)); err != nil {
			return err
		}
		if userID > 0 {
			// token 本体已删除，集合里残留条目只影响「一键下线」的清理范围，
			// 不影响安全性，记录告警即可
			if err := cache.SRem(ctx, cache.RefreshTokenSetKey(userID), refreshToken); err != nil {
				logger.Log.Warnf("[auth] 退出登录时从 refresh token 集合移除失败: userID=%d err=%v", userID, err)
			}
		}
		return nil
	}

	// 未携带 refreshToken（旧客户端）：无法定位具体会话，
	// 退化为吊销该用户全部 refresh token
	if userID == 0 {
		return nil
	}
	tokens, err := cache.SMembers(ctx, cache.RefreshTokenSetKey(userID))
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(tokens)+1)
	for _, t := range tokens {
		keys = append(keys, cache.RefreshTokenKey(t))
	}
	keys = append(keys, cache.RefreshTokenSetKey(userID))
	return cache.Del(ctx, keys...)
}

// registerRefreshToken 把 refresh token 登记到用户维度的集合中。
//
// 没有这个索引就无法按用户批量吊销：token 本身是随机串，改密或禁用用户时
// 无从得知该用户签发过哪些 token。
func registerRefreshToken(ctx context.Context, userID uint, token string, ttl time.Duration) error {
	setKey := cache.RefreshTokenSetKey(userID)
	if err := cache.SAdd(ctx, setKey, token); err != nil {
		return fmt.Errorf("登记refresh token失败: %w", err)
	}
	// 集合本身不单独续期：略微放宽，确保晚签发的 token 不会因集合过期而漏吊销
	return cache.Expire(ctx, setKey, ttl+24*time.Hour)
}

func (s *authService) GetUserInfo(userID uint) (*vo.UserInfoResponse, error) {
	user, err := s.userRepo.FindByID(0, userID)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "用户不存在")
	}

	roleIDs, err := s.userRepo.FindRoleIDsByUserID(userID)
	if err != nil {
		return nil, err
	}

	roles := make([]vo.RoleInfo, 0, len(roleIDs))
	if len(roleIDs) > 0 {
		userRoles, err := s.roleService.FindByIDs(0, roleIDs)
		if err == nil {
			for _, r := range userRoles {
				roles = append(roles, vo.RoleInfo{ID: r.ID, Name: r.Name, Code: r.Code})
			}
		}
	}

	buttons := make([]string, 0)
	menuInfos := make([]vo.MenuInfo, 0)

	if len(roleIDs) > 0 {
		menus, err := s.menuService.FindMenusByRoleIDs(roleIDs)
		if err == nil {
			for _, m := range menus {
				if m.Type == common.MenuTypeButton && m.Permission != "" {
					buttons = append(buttons, m.Permission)
				}
			}
			menuTree := buildMenuTree(menus, 0)
			menuInfos = convertToMenuInfo(menuTree)
		}
	}

	return &vo.UserInfoResponse{
		ID:       user.ID,
		Username: user.Username,
		Nickname: user.Nickname,
		Avatar:   user.Avatar,
		Email:    user.Email,
		Phone:    user.Phone,
		Roles:    roles,
		Buttons:  buttons,
		Menus:    menuInfos,
	}, nil
}

// buildMenuTree 把扁平菜单列表组装成树。
//
// 实现已抽到 common.BuildTree（O(n) 的 map 索引版本）——
// 原先每次递归都重扫整个切片，是 O(n²)。
func buildMenuTree(menus []model.SysMenu, parentID uint) []model.SysMenu {
	return common.BuildTree(menus, parentID,
		func(m model.SysMenu) uint { return m.ID },
		func(m model.SysMenu) uint { return m.ParentID },
		func(m *model.SysMenu, children []model.SysMenu) { m.Children = children },
	)
}

func convertToMenuInfo(menus []model.SysMenu) []vo.MenuInfo {
	result := make([]vo.MenuInfo, 0, len(menus))
	for _, m := range menus {
		info := vo.MenuInfo{
			ID:        m.ID,
			ParentID:  m.ParentID,
			Name:      m.Name,
			Path:      m.Path,
			Component: m.Component,
			Redirect:  m.Redirect,
			Icon:      m.Icon,
			Title:     m.Title,
			Type:      m.Type,
			Sort:      m.Sort,
			IsCache:   m.IsCache,
			Visible:   m.Visible,
			Children:  convertToMenuInfo(m.Children),
		}
		result = append(result, info)
	}
	return result
}
