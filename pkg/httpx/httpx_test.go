package httpx

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestClientDoReturnsBodyAndStatus 正常请求：拿到 body、状态码、头部透传。
func TestClientDoReturnsBodyAndStatus(t *testing.T) {
	var gotMethod, gotContentType, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("resp:" + string(body)))
	}))
	defer srv.Close()

	c := NewClient(time.Second)
	body, status, err := c.Do(context.Background(), "POST", srv.URL,
		[]byte("payload"),
		map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer x",
		})
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if status != http.StatusCreated {
		t.Errorf("状态码应为 201，实际 %d", status)
	}
	if string(body) != "resp:payload" {
		t.Errorf("响应体不对: %q", body)
	}
	if gotMethod != "POST" || gotContentType != "application/json" || gotAuth != "Bearer x" {
		t.Errorf("请求头/方法未透传: method=%s ct=%s auth=%s", gotMethod, gotContentType, gotAuth)
	}
}

// TestClientDoNon2xxStillReturnsBody 支付网关需要读 4xx 的响应体里的错误码，
// 所以非 2xx 必须把 body 交出去，而不是在这里当成失败吞掉。
func TestClientDoNon2xxStillReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"ORDER_FAIL","message":"mock"}`))
	}))
	defer srv.Close()

	c := NewClient(time.Second)
	body, status, err := c.Do(context.Background(), "POST", srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("非 2xx 不应返回传输层错误: %v", err)
	}
	if status != http.StatusBadRequest {
		t.Errorf("状态码应为 400，实际 %d", status)
	}
	if !strings.Contains(string(body), "ORDER_FAIL") {
		t.Errorf("必须读到错误响应体，实际 %q", body)
	}
}

// TestClientDoNilCtx 传 nil ctx 不得 panic —— 上层可能没有取消能力，
// 降级为不可取消，而不是把资金链路炸掉。
func TestClientDoNilCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewClient(time.Second)
	//nolint:staticcheck // 故意传 nil，验证防御逻辑
	body, status, err := c.Do(nil, "GET", srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("nil ctx 应降级而不是报错: %v", err)
	}
	if status != 200 || string(body) != "ok" {
		t.Errorf("结果不对: status=%d body=%q", status, body)
	}
}

// TestClientDoJSON 便捷方法带上 JSON 头。
func TestClientDoJSON(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(time.Second)
	body, err := c.DoJSON(context.Background(), "GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if gotAccept != "application/json" || !strings.Contains(string(body), "ok") {
		t.Errorf("Accept 头或响应不对: accept=%q body=%q", gotAccept, body)
	}
}

// TestClientDoForm 表单便捷方法带上表单头。
func TestClientDoForm(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewClient(time.Second)
	if _, err := c.DoForm(context.Background(), "POST", srv.URL, "a=1&b=2"); err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if !strings.HasPrefix(gotCT, "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type 不对: %q", gotCT)
	}
}

// TestNewClientDefaultTimeout 超时不能是「零值表示不超时」——
// 那会让漏传超时的人毫无感知地失去保护（连接池被撑爆）。
func TestNewClientDefaultTimeout(t *testing.T) {
	if got := NewClient(0).http.Timeout; got != DefaultTimeout {
		t.Errorf("传 0 应回落默认超时，实际 %v", got)
	}
	if got := NewClient(-5 * time.Second).http.Timeout; got != DefaultTimeout {
		t.Errorf("传负数应回落默认超时，实际 %v", got)
	}
	if got := NewClient(3 * time.Second).http.Timeout; got != 3*time.Second {
		t.Errorf("自定义超时应生效，实际 %v", got)
	}
}
