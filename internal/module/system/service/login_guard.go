package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-admin/internal/cache"
	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/internal/module/system/dto"
	"go-admin/internal/module/system/model"
)

// 本文件收纳「登录限频 + 登录日志」相关的业务规则。
//
// 这些逻辑原先写在 auth_controller 里（约 80 行），既违反规则 1
// （Controller 只做取参与返回），也让登录这条最核心的链路难以单测 ——
// Controller 里没法脱离 gin 与 HTTP 请求构造用例。
// 下沉后：Controller 采集 IP/UA 传入 dto.LoginContext，其余全在 Service。

const (
	// maxLoginAttempts 单维度允许的连续失败次数，超过即锁定
	maxLoginAttempts = 5
	// loginLockDuration 失败计数的窗口长度
	loginLockDuration = 15 * time.Minute
)

// loginRateLimitKeys 返回该次登录的限频键。
//
// 双维度是必须的：
//   - 只按 IP 计数，攻击者换 IP 即可绕过
//   - 只按账号计数，则可用大量账号撞库而不触发限制
//
// 任一维度超限即拒绝，见 loginLocked。
func loginRateLimitKeys(ip, username string) []string {
	return []string{
		"login:fail:ip:" + ip,
		"login:fail:account:" + username,
	}
}

// loginLocked 判断任一维度的失败次数是否已达上限。
// Redis 不可用（读取报错）时不做限制，避免缓存故障导致正常用户无法登录。
//
// 这个 fail-open 是刻意的可用性取舍，但**必须留下痕迹**：
// 否则「Redis 抖动期间限频静默失效」会长期无人察觉，
// 而这段时间恰恰是暴力破解成本最低的窗口。
func loginLocked(ctx context.Context, keys ...string) bool {
	for _, key := range keys {
		v, err := cache.Get(ctx, key)
		if err != nil {
			logger.Log.Warnf("[auth] 登录限频查询失败，本次不做限制（fail-open）: key=%s err=%v", key, err)
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			logger.Log.Warnf("[auth] 登录失败计数不是整数，按 0 处理: key=%s value=%q", key, v)
			continue
		}
		if n >= maxLoginAttempts {
			return true
		}
	}
	return false
}

// recordLoginFailure 记录一次登录失败。
//
// 用 SETNX 带 TTL 建键，而不是「INCR 之后再 EXPIRE」：
// 后者是两次独立往返，若 INCR 成功而 EXPIRE 失败（网络抖动、Redis 主从切换），
// 该 key 就**永远不会过期**，这个 IP 或账号会被永久锁死，只能人工清 Redis 才能恢复。
// SETNX 把 TTL 与建键合成一次原子操作；键已存在时不做任何事，原有 TTL 不受影响，
// 因此窗口语义仍是「首次失败起算的固定 15 分钟」。
func recordLoginFailure(ctx context.Context, keys ...string) {
	for _, key := range keys {
		if _, err := cache.SetNX(ctx, key, 0, loginLockDuration); err != nil {
			// 记不上就等于攻击者的失败次数不增长，限频形同虚设 —— 必须可见
			logger.Log.Errorf("[auth] 登录失败计数写入失败，该维度限频将失效: key=%s err=%v", key, err)
			continue
		}
		if _, err := cache.Incr(ctx, key); err != nil {
			logger.Log.Errorf("[auth] 登录失败计数自增失败，该维度限频将失效: key=%s err=%v", key, err)
			continue
		}
	}
}

// clearLoginFailure 登录成功后清零失败计数。
//
// 清不掉会让用户顶着旧计数，下一次失败就可能被误锁，因此留告警而不是静默忽略。
func clearLoginFailure(ctx context.Context, keys ...string) {
	if err := cache.Del(ctx, keys...); err != nil {
		logger.Log.Warnf("[auth] 登录成功后清除失败计数失败: keys=%v err=%v", keys, err)
	}
}

// saveLoginLog 记录登录日志；tenantID 为登录用户的租户，未识别时传 0。
//
// 日志写失败不影响登录结果（否则会把「审计系统故障」升级成「登录不可用」），
// 但必须留痕。
func (s *authService) saveLoginLog(tenantID uint, username string, status int8, msg string, lc *dto.LoginContext) {
	ua := ""
	if lc != nil {
		ua = lc.UserAgent
	}

	log := &model.SysLoginLog{
		TenantID:  tenantID,
		Username:  username,
		IP:        loginIP(lc),
		Browser:   parseUA(ua, []string{"Chrome", "Firefox", "Safari", "Edge", "Opera"}),
		OS:        parseUA(ua, []string{"Windows", "Mac OS X", "Linux", "Android", "iOS"}),
		Status:    status,
		Msg:       msg,
		LoginTime: time.Now(),
	}
	if err := s.logService.CreateLoginLog(log); err != nil {
		logger.Log.Warnf("[auth] 记录登录日志失败: username=%s err=%v", username, err)
	}
}

// loginIP 空指针安全地取 IP，避免调用方到处判空。
// （dto.LoginContext 是别的包的类型，不能在其上定义方法。）
func loginIP(lc *dto.LoginContext) string {
	if lc == nil {
		return ""
	}
	return lc.IP
}

// parseUA 从 User-Agent 里提取关键字，命中即返回；都不命中返回 Unknown。
func parseUA(ua string, keywords []string) string {
	lower := strings.ToLower(ua)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return kw
		}
	}
	return "Unknown"
}

// loginLockedError 锁定时的对外提示（含窗口时长，便于用户知道等多久）
func loginLockedError() error {
	return common.NewBizError(
		fmt.Sprintf("登录失败次数过多，请%d分钟后再试", int(loginLockDuration.Minutes())))
}
