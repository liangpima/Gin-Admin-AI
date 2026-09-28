package middleware

import (
	"sync"
)

// routePerms 路由权限表：key 为 "METHOD 完整路径模板"，value 为所需权限码。
//
// 空字符串表示该路由仅要求登录态（自助接口）。
// 未登记的路由会被默认拒绝，避免新增接口时漏配权限而被放行。
//
// 之所以用「登记表 + 组级中间件查表」而不是「每路由挂一个设置 context 的中间件」：
// gin 的组中间件在路由处理器之前执行，若把权限码写在路由级中间件里，
// 组级鉴权中间件运行时该值尚未写入，会导致校验被整体跳过。
var (
	routePermsMu sync.RWMutex
	routePerms   = make(map[string]string)
	// routePermSet 是 routePerms 的「值集合」视图，供权限码白名单校验用。
	// 单独维护而不是每次遍历 routePerms，是因为校验发生在每次菜单/角色写入时，
	// 而登记表有近百条，O(1) 查表比线性扫描更合适。
	routePermSet = make(map[string]struct{})
)

// RegisterPermission 登记路由所需权限码，在路由注册阶段调用。
// code 为空表示仅要求登录态。
func RegisterPermission(method, fullPath, code string) {
	routePermsMu.Lock()
	defer routePermsMu.Unlock()
	routePerms[method+" "+fullPath] = code
	if code != "" {
		routePermSet[code] = struct{}{}
	}
}

// IsRegisteredPermission 判断权限码是否已被任一真实路由登记。
//
// 为什么需要它：菜单的 permission 会被 SyncPoliciesFromRoleMenus 原样编译成
// Casbin 策略 `{roleCode, default, permission, "*"}`，而 model.conf 的 matcher
// 是 `(p.obj == "*" || r.obj == p.obj) && (p.act == "*" || r.act == p.act)`。
// 也就是说 **菜单上的 permission 字符串直接决定了「哪些接口放行」**：
// 把它改成 `*` 就等于给持有该菜单的角色签发了一张全接口通行证，
// 而 `roleService` 那套 OperatorHoldsPermissions 收敛模型会被整体旁路
// （拿到 `*` 之后所有「授予是否越权」的判定都恒真）。
//
// 因此写入 permission 前必须按「真实存在、且真的会被 CasbinAuth 检查」的
// 权限码白名单校验。未登记的字符串本身永远不会被匹配到（配错只是静默失效），
// 但 `*` 例外 —— 它是唯一一个「配错却能生效」的值，必须显式挡住。
func IsRegisteredPermission(code string) bool {
	routePermsMu.RLock()
	defer routePermsMu.RUnlock()
	_, ok := routePermSet[code]
	return ok
}

// RegisteredPermissionCount 返回已登记的权限码数量，供启动自检/测试使用。
func RegisteredPermissionCount() int {
	routePermsMu.RLock()
	defer routePermsMu.RUnlock()
	return len(routePermSet)
}

// routePermission 查询路由所需权限码；ok 为 false 表示该路由未登记
func routePermission(method, fullPath string) (code string, ok bool) {
	routePermsMu.RLock()
	defer routePermsMu.RUnlock()
	code, ok = routePerms[method+" "+fullPath]
	return code, ok
}
