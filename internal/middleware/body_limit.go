package middleware

import (
	"net/http"
	"strconv"

	"go-admin/config"
	"go-admin/internal/common"

	"github.com/gin-gonic/gin"
)

// BodyLimit 限制请求体大小，超限直接拒绝，并在读取阶段兜住未声明长度的情况。
//
// # 它修的是什么
//
// Go 的 http 服务器**不限制**请求体大小：`r.Body` 是一个按需读取的流，
// 只要调用方愿意发，服务端就会一直收。本项目有三条路径会把整个 body 收进内存/磁盘：
//
//  1. `OperationLog` 中间件：对 JSON 写接口 `io.ReadAll(r.Body)`，紧接着
//     `json.Unmarshal` 再复制一份完整副本（截断到 2000 字符是在这之后才做的），
//     峰值内存约等于 body 的 2 倍；
//  2. 上传接口：`c.FormFile` 会先把整个 multipart 解析完，超过
//     `MaxMultipartMemory`(32MB) 的部分全部落到 `os.TempDir()`，
//     而 `file_service` 里的大小校验发生在**收完之后**；
//  3. 支付回调（`/pay/notify/*`）：注册在 Auth 之外，同样 `io.ReadAll`。
//
// 因此任意一个已登录用户（甚至匿名调用方，走回调路径）只要发一个超大 body，
// 就能把进程内存或临时盘吃光，影响所有租户。nginx 侧的 `client_max_body_size`
// 只在经过 nginx 时生效，而 `deploy/nginx/default.conf` 自己也写明后端端口可直达。
//
// # 两道防线，各自解决一个问题
//
//   - **Content-Length 预检**：长度已声明且超限时立即返回 413，**一个字节都不读**。
//     这是绝大多数情况，也避免了「为拒绝一个 1GB 请求而先把它收下来」。
//   - **MaxBytesReader**：对 chunked 编码（无 Content-Length）或长度造假的请求，
//     在读取阶段就地截断。到上限时 Read 返回 `*http.MaxBytesError`，
//     下游的 `ShouldBindJSON` / `FormFile` 会以错误告终，内存占用被硬性封顶。
//     这是「内存不被吃光」这个安全目标的实际保障，413 的文案质量是次要的。
//
// # 上限取值
//
// 来自 `server.max_body_size`（MB，缺省 64），`Validate()` 会强制它
// **大于** `upload.max_size` —— 否则「配了上传大小限制」会变成
// 「所有上传都被请求体上限拒绝」，而且错误来自这里、与上传配置毫无关联。
//
// # 注册位置
//
// 紧跟在 `DrainBody` 之后（即整个链的第二个）。`DrainBody` 必须是最外层
// （它的 `c.Next()` 之后要贴着 socket 关闭那一刻），而本中间件要在
// **任何读取 body 的代码之前**生效，因此排在其余中间件之前。
func BodyLimit(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limit <= 0 {
			c.Next()
			return
		}

		// 已声明长度且超限：直接拒绝，不读 body。
		// ContentLength 为 -1 表示未声明（chunked），交给下面的 MaxBytesReader。
		if c.Request.ContentLength > limit {
			common.Error(c, common.CodePayloadTooLarge,
				"请求体过大，最大允许 "+humanSize(limit))
			c.Abort()
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// BodyLimitFromConfig 按当前配置返回请求体限流中间件。
//
// 单独提供这个函数而不是让调用方自己读配置：上限的默认值与「必须大于上传上限」
// 的约束都定义在 config 包（Validate 会校验），调用方只需要一个入口，
// 不必知道 `server.max_body_size` 与 `MaxBodyBytes()` 的关系。
func BodyLimitFromConfig() gin.HandlerFunc {
	return BodyLimit(config.Cfg.Server.MaxBodyBytes())
}

// humanSize 把字节数转成便于阅读的 MB 文案。
//
// 上限本身就是以 MB 配置的，这里只做一次除法，不引入单位换算库。
func humanSize(bytes int64) string {
	mb := bytes >> 20
	if mb <= 0 {
		mb = 1
	}
	return strconv.FormatInt(mb, 10) + "MB"
}
