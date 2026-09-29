package router

import (
	"context"
	"errors"
	"net/http"
	"time"

	"go-admin/config"
	"go-admin/internal/cache"
	"go-admin/internal/database"
	"go-admin/internal/logger"
	"go-admin/internal/metrics"
	"go-admin/internal/middleware"
	captchaController "go-admin/internal/module/captcha/controller"
	memberController "go-admin/internal/module/member/controller"
	paymentController "go-admin/internal/module/payment/controller"
	"go-admin/internal/module/system/controller"
	// gen:imports —— cmd/gen 在**本行之后**追加新模块的 controller 导入。不要删除该注释。

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// 权限码与 sys_menu.permission 一一对应。
// 「角色 -> 菜单」的授权关系由 sys_role_menu 维护，中间件据此自动生成 Casbin 策略，
// 因此新增接口只需在注册时声明权限码（或传空串表示仅需登录态），
// 无需再手工维护 casbin_rule 表。
const (
	permUserList   = "system:user:list"
	permUserAdd    = "system:user:add"
	permUserEdit   = "system:user:edit"
	permUserDelete = "system:user:delete"
	permUserExport = "system:user:export"

	permRoleList   = "system:role:list"
	permRoleAdd    = "system:role:add"
	permRoleEdit   = "system:role:edit"
	permRoleDelete = "system:role:delete"

	permMenuList   = "system:menu:list"
	permMenuAdd    = "system:menu:add"
	permMenuEdit   = "system:menu:edit"
	permMenuDelete = "system:menu:delete"

	permDeptList   = "system:dept:list"
	permDeptAdd    = "system:dept:add"
	permDeptEdit   = "system:dept:edit"
	permDeptDelete = "system:dept:delete"

	permPostList   = "system:post:list"
	permPostAdd    = "system:post:add"
	permPostEdit   = "system:post:edit"
	permPostDelete = "system:post:delete"

	permDictList   = "system:dict:list"
	permDictAdd    = "system:dict:add"
	permDictEdit   = "system:dict:edit"
	permDictDelete = "system:dict:delete"

	permConfigList   = "system:config:list"
	permConfigAdd    = "system:config:add"
	permConfigEdit   = "system:config:edit"
	permConfigDelete = "system:config:delete"

	permLogList   = "system:log:list"
	permLogDelete = "system:log:delete"

	permFileList   = "system:file:list"
	permFileUpload = "system:file:upload"
	permFileDelete = "system:file:delete"

	permAgreementList   = "system:agreement:list"
	permAgreementAdd    = "system:agreement:add"
	permAgreementEdit   = "system:agreement:edit"
	permAgreementDelete = "system:agreement:delete"

	permMemberList   = "member:list"
	permMemberAdd    = "member:add"
	permMemberEdit   = "member:edit"
	permMemberDelete = "member:delete"
	permMemberExport = "member:export"
	permMemberErase  = "member:erase"

	permMemberLevelList   = "member:level:list"
	permMemberLevelAdd    = "member:level:add"
	permMemberLevelEdit   = "member:level:edit"
	permMemberLevelDelete = "member:level:delete"

	permMemberTagList   = "member:tag:list"
	permMemberTagAdd    = "member:tag:add"
	permMemberTagEdit   = "member:tag:edit"
	permMemberTagDelete = "member:tag:delete"

	permMemberPointsList = "member:points:list"

	permPayOrderList   = "payment:order:list"
	permPayOrderCreate = "payment:order:create"
	permPayOrderRefund = "payment:order:refund"
	permPayOrderClose  = "payment:order:close"
	// gen:perms —— cmd/gen 在**本行之后**追加新模块的权限码。不要删除该注释。
)

// protected 注册一条受保护路由，并登记其所需权限码。
// code 为空表示仅要求登录态（自助接口，如 userInfo / changePwd）。
// 组级鉴权中间件会依据登记表校验；未登记的路由会被拒绝。
func protected(g *gin.RouterGroup, method, path, code string, h gin.HandlerFunc) {
	middleware.RegisterPermission(method, g.BasePath()+path, code)
	g.Handle(method, path, h)
}

// protectedAdmin 与 protected 相同，但额外要求操作者持有 admin 角色。
//
// 用于**全局表**的写接口（sys_config 保存 OSS / 支付密钥、字典维护等）：
// 这些表没有 tenant_id，任何持有对应权限码的租户管理员都能改写，
// 而改动会作用到所有租户。Casbin 只能表达「有没有某个权限码」，
// 表达不了「必须是平台级角色」，因此这类接口在权限码之外再加一道角色判定（AND）。
func protectedAdmin(g *gin.RouterGroup, method, path, code string, h gin.HandlerFunc) {
	middleware.RegisterPermission(method, g.BasePath()+path, code)
	g.Handle(method, path, middleware.RequireAdminRole(), h)
}

func Setup(mode string) *gin.Engine {
	gin.SetMode(mode)

	r := gin.New()

	// 代理信任列表必须在注册任何路由之前设定。
	//
	// gin 默认信任所有代理头（X-Forwarded-For / X-Real-IP），而 c.ClientIP()
	// 是登录失败限频、登录日志、操作日志的 IP 来源。默认值下客户端只要每次
	// 伪造一个新的 X-Forwarded-For，就能把「单 IP 5 次失败即锁定」变成无限次尝试。
	// 列表为空（默认）表示不信任任何代理头，ClientIP() 直接取连接对端地址。
	//
	// 校验已在 config.Validate 完成（含拒绝 0.0.0.0/0），此处失败属配置绕过校验，
	// 但仍不能静默忽略：gin 解析失败会保留旧的「信任一切」，那正是要避免的状态。
	if err := r.SetTrustedProxies(config.Cfg.Server.TrustedProxies); err != nil {
		logger.Log.Errorf("[router] 设置可信代理失败，IP 限频可能可被 X-Forwarded-For 绕过: %v", err)
	}

	// 必须是**第一个**注册的：gin 里先注册的中间件后执行其 c.Next() 之后的部分，
	// 所以只有挂在这里，补读请求体才会发生在连接关闭之前、且晚于所有下游拒绝。
	// 详见 middleware.DrainBody 的注释 —— 漏了它，被提前拒绝的写请求
	// 在 Connection: close 的调用方（含本项目 nginx 配置）眼里会变成连接重置。
	//
	// 用 DrainBodyFromConfig 而不是传一个写死的上限：上限必须 ≥ server.max_body_size，
	// 否则大 body 的上传请求被 401 拒绝时补读会中途停止，RST 照旧发生
	// （曾经写死 256KB < upload.max_size 10MB，这条路径一直是坏的）。
	r.Use(middleware.DrainBodyFromConfig())
	// 请求体上限必须早于**任何读取 body 的代码**（操作日志中间件、上传、
	// 支付回调都会整份读入），因此紧跟 DrainBody 注册。
	// DrainBody 要留在最外层（它的收尾要贴着 socket 关闭那一刻），
	// 而本中间件只需在所有业务中间件之前，所以顺序是这两者的唯一解。
	// 上限取自 server.max_body_size，Validate 已强制它大于 upload.max_size。
	r.Use(middleware.BodyLimitFromConfig())
	// 指标中间件必须覆盖**全部**请求（含 413 / panic 兜成的 500），
	// 因此紧跟 BodyLimit、先于 Recovery —— Recovery 把 panic 改写成 500，
	// 那次响应也必须被计数。
	r.Use(metrics.Middleware())
	r.Use(middleware.Recovery())
	r.Use(middleware.Logger())
	r.Use(middleware.Cors())
	r.Use(middleware.Tenant())

	fileController := controller.NewFileController()
	payController := paymentController.NewPaymentController()
	memberCtrl := memberController.NewMemberController()
	memberLevelCtrl := memberController.NewMemberLevelController()
	memberTagCtrl := memberController.NewMemberTagController()
	pointsLogCtrl := memberController.NewPointsLogController()
	// gen:controllers —— cmd/gen 在**本行之后**实例化新模块的 controller。不要删除该注释。

	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"name":    "Gin-Admin API",
			"version": "1.0.0",
			"docs":    "/swagger/index.html",
			"health":  "/health",
		})
	})

	// ---------- 健康检查 ----------
	//
	// liveness 与 readiness 必须分开，否则会互相拖累：
	//
	//   /health      liveness —— 只表示"进程与 HTTP 服务存活"，**刻意不探测依赖**。
	//                依赖抖动时若让 liveness 失败，容器编排会不断重启实例，
	//                把本可自愈的故障放大成雪崩。
	//   /health/ready readiness —— 探测 MySQL 与 Redis，任一不可用返回 503。
	//                编排据此决定是否把流量导入本实例：依赖没就绪时不该接流量。
	//
	// 两个接口都不需要认证（编排的探针不带 token），因此**只返回 ok/down**，
	// 不返回具体错误 —— 原始错误会带出内网地址、库名等拓扑信息。
	// 排查所需的细节在服务端日志里。
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/health/ready", func(c *gin.Context) {
		checks := gin.H{"mysql": "ok", "redis": "ok"}
		ready := true

		if err := pingMySQL(c.Request.Context()); err != nil {
			checks["mysql"] = "down"
			ready = false
			logger.Log.Errorf("[health] MySQL 不可用: %v", err)
		}
		if err := cache.RDB.Ping(c.Request.Context()).Err(); err != nil {
			checks["redis"] = "down"
			ready = false
			logger.Log.Errorf("[health] Redis 不可用: %v", err)
		}

		if !ready {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "checks": checks})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready", "checks": checks})
	})

	api := r.Group("/api/v1")

	authController := controller.NewAuthController()
	userController := controller.NewUserController()
	roleController := controller.NewRoleController()
	menuController := controller.NewMenuController()
	deptController := controller.NewDeptController()
	dashboardController := controller.NewDashboardController()
	postController := controller.NewPostController()
	configController := controller.NewConfigController()
	dictController := controller.NewDictController()
	logController := controller.NewLogController()
	agreementController := controller.NewAgreementController()
	captchaCtrl := captchaController.NewCaptchaController()

	// 公开接口：无需登录
	//
	// 公开端点全部挂限流：登录防爆破（fail-closed，与登录失败限频同一开关
	// security.login_fail_closed），验证码防刷，refresh 防高频换发。
	// 维度是来源 IP（Auth 之前拿不到用户身份），依赖上方 SetTrustedProxies
	// 已正确收紧，否则伪造 X-Forwarded-For 可换 IP 绕过。
	loginRL := middleware.RateLimit(middleware.RateLimitOptions{
		Name:       "login",
		Limit:      10,
		Window:     time.Minute,
		FailClosed: config.Cfg.Security.IsLoginFailClosed(),
	})
	refreshRL := middleware.RateLimit(middleware.RateLimitOptions{
		Name: "refresh", Limit: 30, Window: time.Minute,
	})
	auth := api.Group("/auth")
	{
		auth.POST("/login", loginRL, authController.Login)
		auth.POST("/refresh", refreshRL, authController.RefreshToken)
	}

	api.GET("/site/info", configController.SiteInfo)

	captchaRL := middleware.RateLimit(middleware.RateLimitOptions{
		Name: "captcha", Limit: 30, Window: time.Minute,
	})
	api.GET("/captcha/generate", captchaRL, captchaCtrl.Generate)
	api.POST("/captcha/verify", captchaRL, captchaCtrl.Verify)

	authorized := api.Group("")
	authorized.Use(middleware.Auth())
	// CSRF 必须紧跟在 Auth 之后：它的豁免规则依赖 Auth 写进上下文的
	// 「凭据来自头还是 cookie」（见 middleware/csrf.go）。
	// 放在 CasbinAuth / OperationLog 之前，是让被 CSRF 拦下的请求
	// 不再消耗一次鉴权查询与一次操作日志写库 —— 那两步对伪造请求没有价值，
	// 反而给了攻击者一个「用无效请求刷库」的入口。
	authorized.Use(middleware.CSRF())
	authorized.Use(middleware.CasbinAuth())
	authorized.Use(middleware.OperationLog())
	{
		// 自助接口：仅要求登录态（权限码传空串）
		// gen:routes —— cmd/gen 在**本行之后**插入新模块的路由组。不要删除该注释。
		protected(authorized, http.MethodPost, "/auth/logout", "", authController.Logout)
		protected(authorized, http.MethodGet, "/auth/userInfo", "", authController.GetUserInfo)
		protected(authorized, http.MethodGet, "/dashboard/stats", "", dashboardController.GetStats)
		protected(authorized, http.MethodPut, "/system/user/changePwd", "", userController.ChangePassword)

		system := authorized.Group("/system")
		{
			protected(system, http.MethodPost, "/user", permUserAdd, userController.Create)
			protected(system, http.MethodPut, "/user", permUserEdit, userController.Update)
			protected(system, http.MethodDelete, "/user/:id", permUserDelete, userController.Delete)
			protected(system, http.MethodGet, "/user/:id", permUserList, userController.FindByID)
			protected(system, http.MethodGet, "/user/list", permUserList, userController.FindList)
			// 导出与 /user/:id 同级共存：gin 的静态段优先于参数段（既有 /user/list 已印证）
			protected(system, http.MethodGet, "/user/export", permUserExport, userController.Export)
			protected(system, http.MethodPut, "/user/status", permUserEdit, userController.UpdateStatus)
			protected(system, http.MethodPut, "/user/roles", permUserEdit, userController.UpdateRoles)
			protected(system, http.MethodPut, "/user/dept", permUserEdit, userController.UpdateDept)
			protected(system, http.MethodPut, "/user/resetPwd", permUserEdit, userController.ResetPassword)

			protected(system, http.MethodPost, "/role", permRoleAdd, roleController.Create)
			protected(system, http.MethodPut, "/role", permRoleEdit, roleController.Update)
			protected(system, http.MethodDelete, "/role/:id", permRoleDelete, roleController.Delete)
			protected(system, http.MethodGet, "/role/:id", permRoleList, roleController.FindByID)
			protected(system, http.MethodGet, "/role/list", permRoleList, roleController.FindList)
			protected(system, http.MethodPut, "/role/status", permRoleEdit, roleController.UpdateStatus)
			protected(system, http.MethodGet, "/role/all", permRoleList, roleController.FindAll)

			protected(system, http.MethodPost, "/menu", permMenuAdd, menuController.Create)
			protected(system, http.MethodPut, "/menu", permMenuEdit, menuController.Update)
			protected(system, http.MethodDelete, "/menu/:id", permMenuDelete, menuController.Delete)
			protected(system, http.MethodGet, "/menu/:id", permMenuList, menuController.FindByID)
			protected(system, http.MethodGet, "/menu/tree", permMenuList, menuController.FindTree)
			protected(system, http.MethodGet, "/menu/all", permMenuList, menuController.FindAll)

			protected(system, http.MethodPost, "/dept", permDeptAdd, deptController.Create)
			protected(system, http.MethodPut, "/dept", permDeptEdit, deptController.Update)
			protected(system, http.MethodDelete, "/dept/:id", permDeptDelete, deptController.Delete)
			protected(system, http.MethodGet, "/dept/:id", permDeptList, deptController.FindByID)
			protected(system, http.MethodGet, "/dept/tree", permDeptList, deptController.FindTree)

			protected(system, http.MethodPost, "/post", permPostAdd, postController.Create)
			protected(system, http.MethodPut, "/post", permPostEdit, postController.Update)
			protected(system, http.MethodDelete, "/post/:id", permPostDelete, postController.Delete)
			protected(system, http.MethodGet, "/post/list", permPostList, postController.FindList)

			// sys_config 是**全局表**（无 tenant_id），承载 OSS / 支付 / 短信等
			// 平台级凭据。因此写接口用 protectedAdmin：权限码之外再要求 admin 角色，
			// 否则任何拿到 config:edit 的租户管理员都能把平台共用的凭据换成自己的，
			// 影响范围是**所有租户**（见 middleware.RequireAdminRole）。
			// 读接口不限制：值里的敏感项已在 Service 层打码（maskConfig）。
			protectedAdmin(system, http.MethodPost, "/config", permConfigAdd, configController.Create)
			protectedAdmin(system, http.MethodPut, "/config", permConfigEdit, configController.Update)
			protectedAdmin(system, http.MethodDelete, "/config/:id", permConfigDelete, configController.Delete)
			protected(system, http.MethodGet, "/config/list", permConfigList, configController.FindList)
			protected(system, http.MethodGet, "/config/prefix", permConfigList, configController.FindByPrefix)
			protectedAdmin(system, http.MethodPut, "/config/batch", permConfigEdit, configController.BatchSave)
			protectedAdmin(system, http.MethodPost, "/config/upload", permConfigEdit, configController.UploadCert)

			protected(system, http.MethodPost, "/dict/type", permDictAdd, dictController.CreateType)
			protected(system, http.MethodPut, "/dict/type/:id", permDictEdit, dictController.UpdateType)
			protected(system, http.MethodDelete, "/dict/type/:id", permDictDelete, dictController.DeleteType)
			protected(system, http.MethodGet, "/dict/type/list", permDictList, dictController.FindTypeList)
			protected(system, http.MethodPost, "/dict/data", permDictAdd, dictController.CreateData)
			protected(system, http.MethodPut, "/dict/data/:id", permDictEdit, dictController.UpdateData)
			protected(system, http.MethodDelete, "/dict/data/:id", permDictDelete, dictController.DeleteData)
			protected(system, http.MethodGet, "/dict/data/list", permDictList, dictController.FindDataList)
			// 仅要求登录态：这是业务页面渲染下拉/标签用的引用数据，
			// 卡 dict:list 会让没有字典管理权限的操作员看到空下拉（详见 handler 注释）
			protected(system, http.MethodGet, "/dict/data/type/:type", "", dictController.FindDataByType)

			protected(system, http.MethodGet, "/log/operation", permLogList, logController.FindOperationLogList)
			protected(system, http.MethodGet, "/log/login", permLogList, logController.FindLoginLogList)
			protected(system, http.MethodDelete, "/log/operation", permLogDelete, logController.ClearOperationLogs)
			protected(system, http.MethodDelete, "/log/login", permLogDelete, logController.ClearLoginLogs)

			protected(system, http.MethodPost, "/agreement", permAgreementAdd, agreementController.Create)
			protected(system, http.MethodPut, "/agreement", permAgreementEdit, agreementController.Update)
			protected(system, http.MethodDelete, "/agreement/:id", permAgreementDelete, agreementController.Delete)
			protected(system, http.MethodGet, "/agreement/list", permAgreementList, agreementController.FindList)
			protected(system, http.MethodGet, "/agreement/type/:type", permAgreementList, agreementController.FindByType)

			// 上传用独立的 system:file:upload，不再复用查看权限 ——
			// 复用会让「只被授予附件查看」的低权角色也能往服务器写文件
			protected(system, http.MethodPost, "/file/upload", permFileUpload, fileController.Upload)
			protected(system, http.MethodGet, "/file/list", permFileList, fileController.FindList)
			protected(system, http.MethodGet, "/file/:id", permFileList, fileController.FindByID)
			protected(system, http.MethodDelete, "/file/:id", permFileDelete, fileController.Delete)

			// 下单是"创建类"接口的幂等样板：客户端带 Idempotency-Key 头，
			// 重试（双击/网络超时重发）会重放首次响应而不是再建一笔订单。
			// 不带头则完全透传，前端可渐进接入（见 middleware.Idempotency 注释）。
			protected(system, http.MethodPost, "/pay/order", permPayOrderCreate, middleware.Idempotency(payController.CreateOrder))
			protected(system, http.MethodGet, "/pay/order", permPayOrderList, payController.GetOrder)
			protected(system, http.MethodPost, "/pay/order/close", permPayOrderClose, payController.CloseOrder)
			protected(system, http.MethodPost, "/pay/order/refund", permPayOrderRefund, payController.RefundOrder)
			protected(system, http.MethodGet, "/pay/order/list", permPayOrderList, payController.FindList)
			protected(system, http.MethodGet, "/pay/order/query", permPayOrderList, payController.QueryOrder)

			member := authorized.Group("/member")
			{
				protected(member, http.MethodPost, "", permMemberAdd, memberCtrl.Create)
				protected(member, http.MethodPut, "", permMemberEdit, memberCtrl.Update)
				protected(member, http.MethodDelete, "/:id", permMemberDelete, memberCtrl.Delete)
				protected(member, http.MethodGet, "/:id", permMemberList, memberCtrl.FindByID)
				protected(member, http.MethodGet, "/list", permMemberList, memberCtrl.FindList)
				protected(member, http.MethodPut, "/status", permMemberEdit, memberCtrl.UpdateStatus)
				protected(member, http.MethodPut, "/tags", permMemberEdit, memberCtrl.UpdateTags)
				protected(member, http.MethodPut, "/visit", permMemberEdit, memberCtrl.UpdateLastVisit)
				// P2-5 个人信息合规：导出（查询权/可携带权）与匿名化注销（删除权）
				protected(member, http.MethodGet, "/:id/export", permMemberExport, memberCtrl.ExportMemberData)
				protected(member, http.MethodPost, "/:id/erase", permMemberErase, memberCtrl.EraseMemberData)
				protected(member, http.MethodGet, "/level/all", permMemberLevelList, memberCtrl.FindAllLevels)
				protected(member, http.MethodGet, "/tag/all", permMemberTagList, memberCtrl.FindAllTags)

				protected(member, http.MethodPost, "/level", permMemberLevelAdd, memberLevelCtrl.Create)
				protected(member, http.MethodPut, "/level", permMemberLevelEdit, memberLevelCtrl.Update)
				protected(member, http.MethodDelete, "/level/:id", permMemberLevelDelete, memberLevelCtrl.Delete)
				protected(member, http.MethodGet, "/level/list", permMemberLevelList, memberLevelCtrl.FindList)

				protected(member, http.MethodPost, "/tag", permMemberTagAdd, memberTagCtrl.Create)
				protected(member, http.MethodPut, "/tag", permMemberTagEdit, memberTagCtrl.Update)
				protected(member, http.MethodDelete, "/tag/:id", permMemberTagDelete, memberTagCtrl.Delete)
				protected(member, http.MethodGet, "/tag/list", permMemberTagList, memberTagCtrl.FindList)

				protected(member, http.MethodGet, "/points/list", permMemberPointsList, pointsLogCtrl.FindList)
			}
		}
	}

	// 支付回调：由支付平台发起，自行验签，不做登录鉴权
	// 回调端点公开可达，按 IP 限流兜住伪造流量打满连接池的突刺；
	// fail-open——回调真身来自微信/支付宝，误拒的代价（丢单）大于限流失效。
	notifyRL := middleware.RateLimit(middleware.RateLimitOptions{
		Name: "pay-notify", Limit: 120, Window: time.Minute,
	})
	r.POST("/api/v1/pay/notify/wechat", notifyRL, payController.WechatNotify)
	r.POST("/api/v1/pay/notify/alipay", notifyRL, payController.AlipayNotify)

	// Swagger 文档仅在开发环境暴露
	if mode != "release" {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	// 静态文件服务 - 上传文件。
	// 该目录对匿名访问开放（前端需直链引用），因此补一层安全响应头，
	// 防止 .svg 等同源可解析文件被当作文档打开而触发存储型 XSS。
	uploads := r.Group("/uploads", middleware.UploadSecurity())
	uploads.Static("/", "uploads")

	return r
}

// pingMySQL 探测数据库连接是否可用。
//
// 带 2s 超时：健康检查必须快速返回，否则探针会被慢查询拖住并误判为超时失败。
func pingMySQL(ctx context.Context) error {
	if database.DB == nil {
		return errors.New("数据库未初始化")
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return sqlDB.PingContext(ctx)
}
