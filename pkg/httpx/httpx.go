// Package httpx 封装出网 HTTP 调用。
//
// 存在的理由是分层约定（见 CLAUDE.md 规则 2）：业务代码不应直接依赖
// net/http。这条约束的价值在于把「协议细节」与「业务判断」分开 ——
// 网关里真正属于业务的是参数组装、签名、响应解析，出网那一段
// （构造请求、超时、读响应体）每次写法都一样，且写错的代价很高
// （漏设超时 = 撑爆连接池；ctx 传 nil = 直接 panic；忘关 Body = 连接泄漏）。
//
// 收敛到这里，这些坑只需在一处修一次。
//
// 依赖方向：pkg/** 不得反向依赖 internal/**，因此本包只暴露
// 最小的泛型请求方法，不感知任何业务类型。
package httpx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultTimeout 单次出网调用的默认超时。
// 资金链路宁可超时失败重试，也不能长时间占着连接。
const DefaultTimeout = 10 * time.Second

// Client 是轻量的出网 HTTP 客户端。
// 零值不可用，用 NewClient 构造。
type Client struct {
	http *http.Client
}

// NewClient 返回带超时的客户端。timeout <= 0 时用 DefaultTimeout ——
// 不做「零值表示不超时」的语义，那会让漏传超时的人毫无感知地失去保护。
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{http: &http.Client{Timeout: timeout}}
}

// Do 发起一次请求并返回响应体。
//
// 行为约定（与常见封装的差异是刻意的）：
//   - 只接受 GET/POST/PUT/DELETE 之外也一视同仁，方法由调用方决定；
//   - 返回已读完的 body 与状态码，调用方不必也不应再关连接；
//   - status >= 400 也返回 body + nil error —— 支付网关需要读错误响应体
//     才能给出可诊断的报错（微信/支付宝都在非 2xx 体里放错误码）；
//     是否把非 2xx 当失败，由各业务自行判断。
//
// ctx 为 nil 时退化为 context.Background()：NewRequestWithContext 收到
// nil ctx 会 panic，而这些调用点都在资金链路上。
// header 用 map[string]string 而不是 http.Header：这样调用方（尤其是
// 被禁止依赖 net/http 的业务层）不必为了传一个头就引入 net/http。
// 头只有一个值 —— 资金网关需要的 Authorization / Content-Type 都是单值。
func (c *Client) Do(ctx context.Context, method, url string, body []byte, header map[string]string) ([]byte, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取响应失败: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

// DoJSON 是 Do 的便捷包装：自动带上 JSON 头，忽略非 2xx。
func (c *Client) DoJSON(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	header := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}
	// 支付网关要求能读到 4xx/5xx 的响应体（里面有错误码），所以这里
	// 不判断状态码，原样返回；失败判定交给调用方。
	respBody, _, err := c.Do(ctx, method, url, body, header)
	return respBody, err
}

// DoForm 是 Do 的便捷包装：表单编码头，忽略非 2xx（理由同 DoJSON）。
func (c *Client) DoForm(ctx context.Context, method, url, form string) ([]byte, error) {
	header := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	respBody, _, err := c.Do(ctx, method, url, []byte(form), header)
	return respBody, err
}
