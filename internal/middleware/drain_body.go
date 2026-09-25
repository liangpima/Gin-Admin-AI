package middleware

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxDrainBytes 补读请求体的上限，与 net/http 的 maxPostHandlerReadBytes 同值。
//
// 取同一个值不是巧合，而是刻意的（见 DrainBody 的注释）：本中间件做的事
// 与 net/http 对 keep-alive 请求在 handler 返回后做的事完全相同，
// 上游注释里那句 "approximately what a typical machine's TCP buffer size is anyway"
// 同样适用于这里。
//
// 有上限也是必要的：这个函数运行在**尚未通过鉴权**的请求上，
// 无上限地读完等于让匿名调用方用一个 Content-Length 就能让我们替他把数据收完。
// 超过上限时放弃补读，连接仍会被重置（退回修复前的行为）——
// 比「为拒绝一个请求而先收下 1GB」划算，且真正会被拒绝的合法请求
// （JSON 写接口、≤10MB 的上传）都远小于这个值。
const maxDrainBytes = 256 << 10

// DrainBody 在请求结束前把**未被读完**的请求体读掉并丢弃。
//
// # 它修的是什么
//
// 一个「响应已经写出来了、调用方却收不到」的传输层缺陷。成因链：
//
//  1. 请求带 body，且被**提前拒绝**（401 未登录 / 403 无权限 / 403 CSRF / 429 限频）——
//     这些路径都在读 body 之前就返回了；
//  2. 调用方使用 `Connection: close`（HTTP/1.0 客户端、urllib 等脚本，
//     以及本项目 nginx 配置下的上游连接）；
//  3. net/http 在这种情况下**不会**替我们把剩余 body 读掉。`(*http.body).Close()`
//     的 switch 是有顺序的（net/http/transfer.go）：
//
//        case b.sawEOF:                     // 已读到 EOF → 空操作
//        case b.hdr == nil && b.closing:    // ← Connection: close → 直接跳过，不读
//        case b.doEarlyClose:               // ← 读掉最多 maxPostHandlerReadBytes
//        default:                           // ← 全部读完
//
//     也就是说：**keep-alive 的请求，handler 返回后 net/http 本来就会补读**
//     （`doEarlyClose` 对每个服务端请求都置为 true，见 server.go），
//     只有 `closing` 这一条分支被显式跳过 —— 注释理由是
//     "no point in reading to EOF"。它假设的是「马上要关连接，读不读无所谓」，
//     但漏了一点：**连接关闭的方式会被这个决定改变**。
//  4. 于是 `close()` 时接收缓冲区里还压着未读数据（或数据在关闭后才到达），
//     内核发出的不是 FIN 而是 **RST**；
//  5. RST 会让对端内核**丢弃已收到但尚未被应用读走的数据** ——
//     包括我们刚写出去的那个 401/403。
//
// 现象因此是：调用方拿到 `connection reset`，而服务端日志里那条 403 好好地记着。
// 对浏览器（keep-alive）不会发生，所以本地开发与手工测试都发现不了。
//
// 本中间件就是补上那条被跳过的分支：让 `Connection: close` 的请求
// 也享受 keep-alive 请求本来就有的待遇。**不是发明新行为，而是消除两条路径的差异。**
//
// # 为什么必须修，而不是当成客户端的毛病
//
// 本项目自己的部署配置就会触发它：`deploy/nginx/default.conf` 设了
// `proxy_http_version 1.1` 但**没有** `proxy_set_header Connection ""`，
// nginx 因此对上游发送 `Connection: close`（这正是「要开 upstream keepalive
// 就必须显式清空 Connection 头」那条经典配置的由来）。
// 后果直接落在两个已经做完的改造上：
//
//   - B4 的「401 → 续期 → 重放」：nginx 把连接重置报成 502，前端拿不到 401，
//     续期流程根本不会触发，用户直接被登出页接走；
//   - B3 的 CSRF 403：前端只会看到网络错误，而不是「请刷新页面后重试」。
//
// 触发门槛也不高：只要**请求体比响应晚到**就行 —— 慢速链路上传一个稍大的 body、
// 声明了 Content-Length 却还没发完，服务端已经把 401 写出去并关掉了连接。
// 实测（`runtime/smoke/probe_csrf_reset.py`）：urllib 的默认形态下
// 12 次里只有 3~4 次能收到那个 403。
//
// # 为什么挂在最外层（第一个注册）
//
// 补读只要发生在「连接被关闭之前」就有效，而最外层中间件的 `c.Next()` 之后
// 恰好是链上最靠后的位置（下游全部处理完、响应已写完，但 net/http 的
// `finishRequest()` 还没把连接关掉）。挂在这里有两个好处：
//
//   - 覆盖**所有**拒绝来源：Auth / Casbin / CSRF / 限频 / 任意 handler 的提前返回，
//     不需要在每个 `c.Abort()` 旁边各写一遍（那种写法迟早会漏，而漏掉的那一处
//     只会表现为「偶发网络错误」，没人会联想到请求体）；
//   - 顺序语义是显式的：gin 里先注册的中间件后执行其 `c.Next()` 之后的部分，
//     所以「第一个注册 = 最后收尾」，正好贴着 socket 关闭那一刻。
//
// 注意这里**不判断 `c.IsAborted()`**：请求体是否被读完是传输层的事实，
// 不该取决于某个中间件是否记得调用 Abort。handler 提前返回却没 Abort
// （例如绑定失败后直接 return）同样会留下未读 body，一并覆盖掉。
//
// # 代价（两条，都是有意接受的）
//
//  1. **正常请求的代价可忽略**：body 早已被 `ShouldBindJSON` / 表单解析读完，
//     此时 `io.Copy` 只会做一次立即返回 EOF 的 Read。
//  2. **拒绝响应可能被推迟**：若客户端声明了 body 却迟迟不发，补读会等到
//     body 到达或读超时（`server.read_timeout`，本项目 60s）为止，响应因此延后。
//     这与 net/http 对 keep-alive 请求本就会做的事一致（同样的 256KB 上限、
//     同样受 read_timeout 约束），因此**没有引入新的资源占用形态** ——
//     只是把 `Connection: close` 对齐到 keep-alive 已有的行为。
//     刻意不加一个更短的读超时：那会让「body 比响应晚到」的慢速客户端
//     重新落回 RST，也就是把本中间件要修的场景又修坏。
func DrainBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		body := c.Request.Body
		if body == nil || body == http.NoBody {
			return
		}

		// 只补读、不报错：客户端中途挂断（写一半就不发了）是常态，
		// 那种情况下读到的错误没有处理价值，也不该污染日志。
		_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrainBytes))
	}
}
