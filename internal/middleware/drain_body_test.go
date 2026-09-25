package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-admin/internal/common"

	"github.com/gin-gonic/gin"
)

// countingBody 记录被读走的字节数，用来断言「请求体到底有没有被读完」。
//
// 断言对象刻意选「读走多少字节」而不是「handler 有没有跑」：
// 这个中间件的全部作用就是移动字节，只有字节数能区分
// 「补读了」「没补读」「读了但读少了」三种实现。
type countingBody struct {
	reader io.Reader
	read   int
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (b *countingBody) Close() error { return nil }

// runDrainBody 让一个请求穿过 DrainBody 与下游 handler，返回响应与读字节数。
func runDrainBody(t *testing.T, payload string, downstream gin.HandlerFunc) (*httptest.ResponseRecorder, *countingBody) {
	t.Helper()

	r := gin.New()
	r.Use(DrainBody())
	r.POST("/x", downstream)

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	body := &countingBody{reader: strings.NewReader(payload)}
	req.Body = body
	req.ContentLength = int64(len(payload))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, body
}

// TestDrainBodyReadsUnconsumedBody 被提前拒绝时，未读的请求体必须被补读掉。
//
// 这是本中间件存在的唯一理由。补读本身不做任何有用的事 —— 它的价值全在
// 「读完之后连接可以正常关闭，而不是被内核用 RST 关掉」。
// 若这条断言失败（读走 0 字节），就意味着所有「带 body 的请求被提前拒绝」
// 都会退回「调用方收不到响应」的状态。
func TestDrainBodyReadsUnconsumedBody(t *testing.T) {
	const payload = `{"username":"admin","password":"admin123"}`

	w, body := runDrainBody(t, payload, func(c *gin.Context) {
		// 真实拒绝路径的形态：先写响应并中止，**一个字节都不读**
		common.Unauthorized(c, "未登录")
		c.Abort()
	})

	if w.Code != http.StatusUnauthorized {
		t.Errorf("拒绝响应不应被 DrainBody 影响，状态码应为 401，实际 %d", w.Code)
	}
	if body.read != len(payload) {
		t.Errorf("未读的请求体应被补读完（%d 字节），实际只读了 %d 字节 —— "+
			"此时连接会带着未读数据关闭，内核发 RST，调用方拿不到上面那个 401",
			len(payload), body.read)
	}
}

// TestDrainBodyReadsPartiallyConsumedBody 下游只读了一部分时，剩下那部分也要补掉。
//
// 「读了一半」是最容易被忽略的形态：例如 handler 先 `io.ReadAll` 拿到前若干字节
// 做类型判断，然后直接返回错误。它看起来「读过 body 了」，但缓冲区里仍有剩余 ——
// 只要还有剩余，RST 就会发生，与完全没读的效果一样。
func TestDrainBodyReadsPartiallyConsumedBody(t *testing.T) {
	const payload = `{"a":"0123456789"}`

	_, body := runDrainBody(t, payload, func(c *gin.Context) {
		buf := make([]byte, 5)
		_, _ = io.ReadFull(c.Request.Body, buf) // 只读前 5 字节
		c.Status(http.StatusBadRequest)
	})

	if body.read != len(payload) {
		t.Errorf("剩余 %d 字节应被补读，实际总共读了 %d 字节",
			len(payload)-5, body.read)
	}
}

// TestDrainBodyIsNoopForConsumedBody 正常请求（body 已被读完）不受影响。
//
// 这一条守的是「代价可以忽略」这个前提：若实现改成无条件重新读取，
// 正常的写接口会被多读一遍 body —— 轻则浪费，重则在已关闭的 body 上读到错误。
func TestDrainBodyIsNoopForConsumedBody(t *testing.T) {
	const payload = `{"name":"x"}`

	w, body := runDrainBody(t, payload, func(c *gin.Context) {
		got, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Errorf("handler 读取请求体失败: %v", err)
		}
		if string(got) != payload {
			t.Errorf("handler 应拿到完整请求体，实际 %q", got)
		}
		c.Status(http.StatusOK)
	})

	if w.Code != http.StatusOK {
		t.Errorf("正常请求不应被影响，状态码 %d", w.Code)
	}
	if body.read != len(payload) {
		t.Errorf("body 已被读完时不应再读，实际读走 %d 字节（payload %d 字节）",
			body.read, len(payload))
	}
}

// TestDrainBodyStopsAtCap 补读必须有上限。
//
// 这个函数运行在**尚未通过鉴权**的请求上，因此它的读取量是一个攻击面：
// 没有上限的话，匿名调用方声明一个 1GB 的 Content-Length 就能让服务端
// 替他把这 1GB 收完。上限之外的连接仍会被重置 —— 那是修复前的行为，
// 比「为拒绝一个请求而先收下 1GB」划算。
func TestDrainBodyStopsAtCap(t *testing.T) {
	payload := strings.Repeat("a", maxDrainBytes+4096)

	_, body := runDrainBody(t, payload, func(c *gin.Context) {
		c.Status(http.StatusRequestEntityTooLarge)
	})

	if body.read != maxDrainBytes {
		t.Errorf("补读应在上限 %d 字节处停止，实际读走 %d 字节", maxDrainBytes, body.read)
	}
}

// TestDrainBodyHandlesEmptyBody 没有请求体时不得 panic，也不得多读。
//
// GET / DELETE 这类请求会走到这里（gin 对空 body 的表示不止一种：
// nil、http.NoBody、以及一个长度为 0 的 reader）。它们占了流量的多数，
// 因此这里既要求不 panic，也要求不产生读取。
func TestDrainBodyHandlesEmptyBody(t *testing.T) {
	cases := map[string]io.ReadCloser{
		"nil":        nil,
		"http.NoBody": http.NoBody,
		"空 reader":   io.NopCloser(bytes.NewReader(nil)),
	}

	for name, rc := range cases {
		t.Run(name, func(t *testing.T) {
			r := gin.New()
			r.Use(DrainBody())
			r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Body = rc

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("状态码应为 200，实际 %d", w.Code)
			}
		})
	}
}

// TestDrainBodyKeepsRejectionVisibleAfterServerCloses 真连接上验证「拒绝响应确实能收到」。
//
// 上面几条用例断言的是「字节被读走了」这个**机制**；这一条断言的是**结果**：
// 客户端用 `Connection: close`（urllib、HTTP/1.0 客户端，以及本项目 nginx 配置下
// 的上游连接都是这个形态）发一个带 body 的请求，被提前拒绝后必须真的收到 401。
//
// # 请求体为什么分两次写、中间还加一次延迟
//
// 这个缺陷的触发条件是「**请求体比响应晚到服务端**」：
// 服务端不读 body 就把 401 写出去并关掉连接，随后到达的 body 让内核回一个 RST，
// 把客户端缓冲区里那个还没被读走的 401 一起丢掉。
//
// 如果像初版那样把请求头与 body 一次性写出去，body 会和请求头落在同一个 TCP 段里，
// 被 net/http 读请求头时的 bufio 一并吞进用户态 —— 关闭时内核接收缓冲区是空的，
// 发的是 FIN 而不是 RST。**初版就是这么写成了一条永远绿的假用例**：
// 在「把补读整个删掉」的变异版本上照样 10/10 通过。
// （同类现象：用 http.Client 也测不出来，它一发出请求就立刻读响应，
// 会把 RST 与响应之间的竞速掩盖掉。）
//
// 所以这里显式制造「body 晚到」：先只写请求头 → 等 50ms 让服务端把 401 写完并关闭
// → 再写 body → 再等 300ms 让 RST 确定到达 → 最后才去读。
//   · 修复前：连接已被 RST，缓冲区里的 401 被丢弃，ReadAll 返回 connection reset；
//   · 修复后：服务端此时正阻塞在补读上等 body，body 一到就补读完、正常响应并 FIN 关闭，
//     ReadAll 稳定拿到 401。
// 两次延迟把竞速变成了确定性，因此这条用例能稳定区分「已修复 / 有缺陷」。
func TestDrainBodyKeepsRejectionVisibleAfterServerCloses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(DrainBody())
	r.Use(func(c *gin.Context) {
		common.Unauthorized(c, "未登录")
		c.Abort()
	})
	r.POST("/api/v1/x", func(c *gin.Context) {
		t.Error("鉴权拒绝后 handler 不应执行")
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	addr := srv.Listener.Addr().String()
	const payload = `{"a":1}`

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("连接测试服务失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// 第一步：只发请求头，声明一个 Content-Length 但**不发 body**
	headers := fmt.Sprintf(
		"POST /api/v1/x HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\n"+
			"Connection: close\r\nContent-Length: %d\r\n\r\n",
		addr, len(payload),
	)
	if _, err := conn.Write([]byte(headers)); err != nil {
		t.Fatalf("发送请求头失败: %v", err)
	}

	// 等服务端处理完这个「没有 body」的请求（拒绝 → 写响应 → 关连接）
	time.Sleep(50 * time.Millisecond)

	// 第二步：body 现在才发。修复前服务端已经关了连接，这些字节会换回一个 RST
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("发送请求体失败: %v", err)
	}

	// 再等一会，确保 RST（如果有）已经到达并丢弃了缓冲区里的响应
	time.Sleep(300 * time.Millisecond)

	raw, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("读取响应失败（连接被重置，401 响应已被丢弃）: %v\n"+
			"这说明请求体没有被读完 —— 连接带着未读数据关闭时内核发的是 RST 而不是 FIN", err)
	}

	if !strings.Contains(string(raw), "401") {
		t.Errorf("应收到 401 响应，实际读到:\n%s", raw)
	}
}
