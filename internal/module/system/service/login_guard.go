package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-admin/config"
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

// loginFailureLookup 读取某维度的失败计数。
//
// 做成变量是为了可测：Redis 未初始化时 cache.GetString 只会返回 ErrNotReady，
// 拿不到「键不存在」这一分支 —— 而它恰恰是最常见、也最容易写错的分支。
var loginFailureLookup = cache.GetString

// checkLoginRateLimit 判断任一维度的失败次数是否已达上限。
//
// 返回 (locked, err)：
//   - locked=true  ：失败次数已达上限，应拒绝本次登录
//   - err != nil   ：限频设施不可用且策略为 fail-closed，应拒绝本次登录（系统错误）
//   - 两者均为零值 ：放行
//
// 为什么把「不可用」也纳入拒绝：限频是登录链路上唯一的暴力破解闸门，
// 它依赖的 Redis 一旦抖动就放行，等于把「缓存故障」放大成「可无限撞库」，
// 而攻击者完全可以主动制造或等待这个窗口。因此默认 fail-closed，
// 与 middleware/auth.go 的 token 吊销检查（同样 fail-closed）保持一致。
//
// 可用性优先的部署可以显式配 security.login_fail_closed: false 退回 fail-open，
// 此时仍留 Error 级日志，避免「限频静默失效」长期无人察觉。
//
// ⚠️ 必须用 cache.GetString 而不是 cache.Get：后者的 err 里混着
// redis.Nil（键不存在）。而「键不存在」对失败计数来说是最常见的正常状态
// （还没失败过），把它当成故障会让 fail-closed 拒掉每一次干净登录。
func checkLoginRateLimit(ctx context.Context, keys ...string) (bool, error) {
	failClosed := config.Cfg.Security.IsLoginFailClosed()

	for _, key := range keys {
		v, found, err := loginFailureLookup(ctx, key)
		if err != nil {
			if failClosed {
				logger.Log.Errorf("[auth] 登录限频查询失败，按 fail-closed 拒绝本次登录: key=%s err=%v", key, err)
				return false, fmt.Errorf("登录限频服务不可用: %w", err)
			}
			logger.Log.Warnf("[auth] 登录限频查询失败，本次不做限制（fail-open，已显式配置）: key=%s err=%v", key, err)
			continue
		}
		if !found {
			// 该维度还没有失败记录，计数视为 0，直接放行这一维度
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			// 计数被写坏（如人为改动、类型冲突）时不能当作「没有失败」：
			// 这属于数据异常，按已达上限处理更安全，且错误计数本身极少出现。
			logger.Log.Warnf("[auth] 登录失败计数不是整数，按已达上限处理: key=%s value=%q", key, v)
			return true, nil
		}
		if n >= maxLoginAttempts {
			return true, nil
		}
	}
	return false, nil
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
