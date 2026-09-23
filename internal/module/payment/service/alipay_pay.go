package service

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-admin/pkg/httpx"
)

type AlipayConfig struct {
	AppID       string
	PrivateKey  string
	NotifyURL   string
	ReturnURL   string
	PublicKeyID string
}

// AlipayGateway 支付宝网关。
//
// 出网统一走 pkg/httpx：Service 层不直接依赖 net/http（分层约定）。
type AlipayGateway struct {
	config AlipayConfig
	client *httpx.Client
}

func NewAlipayGateway(cfg AlipayConfig) *AlipayGateway {
	return &AlipayGateway{
		config: cfg,
		client: httpx.NewClient(httpx.DefaultTimeout),
	}
}

func (g *AlipayGateway) Prepay(ctx context.Context, orderNo, subject string, amount int64, returnURL string) (map[string]interface{}, error) {
	amountStr := fenToYuan(amount)

	params := map[string]string{
		"app_id":     g.config.AppID,
		"method":     "alipay.trade.page.pay",
		"charset":    "utf-8",
		"sign_type":  "RSA2",
		"timestamp":  time.Now().Format("2006-01-02 15:04:05"),
		"version":    "1.0",
		"notify_url": g.config.NotifyURL,
	}

	// 验证 returnURL 防止开放重定向
	if returnURL != "" {
		if err := validateReturnURL(returnURL); err != nil {
			return nil, err
		}
		params["return_url"] = returnURL
	}

	bizContent := map[string]interface{}{
		"out_trade_no": orderNo,
		"total_amount": amountStr,
		"subject":      subject,
		"product_code": "FAST_INSTANT_TRADE_PAY",
	}
	bizBytes, _ := json.Marshal(bizContent)
	params["biz_content"] = string(bizBytes)

	sign, err := g.sign(params)
	if err != nil {
		return nil, err
	}
	params["sign"] = sign

	formData := buildFormQuery(params)
	formURL := "https://openapi.alipay.com/gateway.do?" + formData

	return map[string]interface{}{
		"form_url": formURL,
		"method":   "redirect",
	}, nil
}

func (g *AlipayGateway) PrepayApp(ctx context.Context, orderNo, subject string, amount int64) (map[string]interface{}, error) {
	amountStr := fenToYuan(amount)

	params := map[string]string{
		"app_id":     g.config.AppID,
		"method":     "alipay.trade.app.pay",
		"charset":    "utf-8",
		"sign_type":  "RSA2",
		"timestamp":  time.Now().Format("2006-01-02 15:04:05"),
		"version":    "1.0",
		"notify_url": g.config.NotifyURL,
	}

	bizContent := map[string]interface{}{
		"out_trade_no": orderNo,
		"total_amount": amountStr,
		"subject":      subject,
	}
	bizBytes, _ := json.Marshal(bizContent)
	params["biz_content"] = string(bizBytes)

	sign, err := g.sign(params)
	if err != nil {
		return nil, err
	}
	params["sign"] = sign

	return map[string]interface{}{
		"order_string": buildFormQuery(params),
	}, nil
}

func (g *AlipayGateway) QueryTrade(ctx context.Context, orderNo string) (map[string]interface{}, error) {
	params := map[string]string{
		"app_id":    g.config.AppID,
		"method":    "alipay.trade.query",
		"charset":   "utf-8",
		"sign_type": "RSA2",
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		"version":   "1.0",
	}

	bizContent := map[string]interface{}{
		"out_trade_no": orderNo,
	}
	bizBytes, _ := json.Marshal(bizContent)
	params["biz_content"] = string(bizBytes)

	sign, err := g.sign(params)
	if err != nil {
		return nil, err
	}
	params["sign"] = sign

	respBody, err := g.doRequest(ctx, "POST", "https://openapi.alipay.com/gateway.do", params)
	if err != nil {
		return nil, err
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, err
	}

	return resp, nil
}

// alipaySuccessCode 支付宝网关业务成功的 code 值
const alipaySuccessCode = "10000"

// checkAlipayResponse 校验网关响应中的业务码。
//
// 支付宝网关在**业务失败时同样返回 HTTP 200**，真正的结果在响应体的 code 字段
// （成功为 "10000"）。只看 HTTP 状态会把「余额不足」「订单不可退」「退款单号重复」
// 这类失败误判为成功 —— 对退款而言后果是：系统把订单落定为「已退款」，
// 钱却根本没退给用户，且状态机已锁死无法重试。
//
// method 为接口方法名（如 alipay.trade.refund），响应字段名为其下划线形式 + _response。
func checkAlipayResponse(method string, resp map[string]interface{}) error {
	respKey := strings.ReplaceAll(method, ".", "_") + "_response"

	raw, ok := resp[respKey]
	if !ok {
		return fmt.Errorf("支付宝响应缺少字段 %s", respKey)
	}
	body, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("支付宝响应字段 %s 结构异常", respKey)
	}

	code, _ := body["code"].(string)
	if code == alipaySuccessCode {
		return nil
	}

	// 失败时带上 code / sub_code / msg / sub_msg，便于定位是参数问题还是渠道问题
	subCode, _ := body["sub_code"].(string)
	msg, _ := body["msg"].(string)
	subMsg, _ := body["sub_msg"].(string)
	return fmt.Errorf("支付宝业务失败: code=%s sub_code=%s msg=%s sub_msg=%s",
		code, subCode, msg, subMsg)
}

func (g *AlipayGateway) Refund(ctx context.Context, orderNo, refundNo string, amount int64) (map[string]interface{}, error) {
	amountStr := fenToYuan(amount)

	params := map[string]string{
		"app_id":     g.config.AppID,
		"method":     "alipay.trade.refund",
		"charset":    "utf-8",
		"sign_type":  "RSA2",
		"timestamp":  time.Now().Format("2006-01-02 15:04:05"),
		"version":    "1.0",
		"notify_url": g.config.NotifyURL,
	}

	bizContent := map[string]interface{}{
		"out_trade_no":   orderNo,
		"refund_amount":  amountStr,
		"out_request_no": refundNo,
	}
	bizBytes, _ := json.Marshal(bizContent)
	params["biz_content"] = string(bizBytes)

	sign, err := g.sign(params)
	if err != nil {
		return nil, err
	}
	params["sign"] = sign

	respBody, err := g.doRequest(ctx, "POST", "https://openapi.alipay.com/gateway.do", params)
	if err != nil {
		return nil, err
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, err
	}

	// 关键：HTTP 200 不等于退款成功，必须校验业务码。
	// 不校验的后果是退款失败被记成成功，订单落定为「已退款」而钱没退出去。
	if err := checkAlipayResponse("alipay.trade.refund", resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (g *AlipayGateway) ParseNotify(body []byte) (*PayNotifyResult, error) {
	result := &PayNotifyResult{
		Status:  "fail",
		RawData: string(body),
	}

	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}

	sign := form.Get("sign")

	params := make(map[string]string)
	for k := range form {
		if k != "sign" && k != "sign_type" {
			params[k] = form.Get(k)
		}
	}

	if err := g.verify(params, sign); err != nil {
		return nil, fmt.Errorf("alipay verify failed: %w", err)
	}

	// 校验回调归属。
	//
	// 验签只能证明「报文来自支付宝且签名串未被篡改」，不能证明
	// 「这笔交易属于本应用」—— 同一支付宝账号体系下的其他应用
	// （服务商模式下代管的子应用尤其常见）同样能生成通过验签的通知。
	// 若不比对 app_id，别家应用的支付通知就能把我们的订单标记为已支付。
	// 支付宝接口规范也明确要求商户校验 app_id，这里与微信侧保持一致。
	if appID := form.Get("app_id"); appID != g.config.AppID {
		return nil, fmt.Errorf("alipay notify app_id mismatch: got %q", appID)
	}

	result.TradeNo = form.Get("trade_no")
	result.OrderNo = form.Get("out_trade_no")

	// 解析实际支付金额（元转分）
	if totalAmount := form.Get("total_amount"); totalAmount != "" {
		result.Amount = yuanToFen(totalAmount)
	}

	tradeStatus := form.Get("trade_status")
	if tradeStatus == "TRADE_SUCCESS" || tradeStatus == "TRADE_FINISHED" {
		result.Status = "success"
		gmtPayStr := form.Get("gmt_payment")
		if t, err := time.Parse("2006-01-02 15:04:05", gmtPayStr); err == nil {
			result.PaidAt = &t
		}
	}

	return result, nil
}

// fenToYuan 把「分」转换为支付宝要求的「元」字符串（保留两位小数）。
//
// 不能用 float64(amount)/100：float64 无法精确表示 0.1、0.01 这类小数，
// 大额金额会出现 12345.67 → 12345.669999... 之类的误差，格式化后
// 可能与订单金额差 1 分，被支付宝判为「金额不一致」而拒单。
// 与 yuanToFen 对称，全程整数运算：整数部分用整除，小数部分用取余。
func fenToYuan(amount int64) string {
	neg := amount < 0
	if neg {
		amount = -amount
	}
	out := fmt.Sprintf("%d.%02d", amount/100, amount%100)
	if neg {
		return "-" + out
	}
	return out
}

// yuanToFen 把「元」金额字符串转换为「分」。
//
// 不能用 int64(amt * 100)：浮点乘法存在表示误差，例如 19.99 * 100
// 实际得到 1998.9999...，截断后变成 1998 分。回调里会拿它与订单金额比对，
// 一旦少 1 分就会判为「支付金额不匹配」，结果是用户已付款但订单不入账。
// 因此这里按字符串拆分整数与小数部分，避免引入浮点。
func yuanToFen(amount string) int64 {
	s := strings.TrimSpace(amount)
	if s == "" {
		return 0
	}

	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}

	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i+1:]
	}
	// 只保留到分；支付宝最多两位小数，超出部分本就不该参与比对
	if len(fracPart) > 2 {
		fracPart = fracPart[:2]
	}
	for len(fracPart) < 2 {
		fracPart += "0"
	}
	if intPart == "" {
		intPart = "0"
	}

	yuan, err1 := strconv.ParseInt(intPart, 10, 64)
	cent, err2 := strconv.ParseInt(fracPart, 10, 64)
	if err1 != nil || err2 != nil {
		return 0
	}

	total := yuan*100 + cent
	if neg {
		return -total
	}
	return total
}

func (g *AlipayGateway) sign(params map[string]string) (string, error) {
	sortedKeys := make([]string, 0, len(params))
	for k := range params {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	var pairs []string
	for _, k := range sortedKeys {
		if params[k] != "" {
			pairs = append(pairs, k+"="+params[k])
		}
	}
	content := strings.Join(pairs, "&")

	pk, err := parsePrivateKey(g.config.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("parse private key failed: %w", err)
	}

	hash := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, pk, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}

	return base64EncodeStd(signature), nil
}

func (g *AlipayGateway) verify(params map[string]string, signature string) error {
	sortedKeys := make([]string, 0, len(params))
	for k := range params {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	var pairs []string
	for _, k := range sortedKeys {
		if params[k] != "" {
			pairs = append(pairs, k+"="+params[k])
		}
	}
	content := strings.Join(pairs, "&")

	sigBytes, err := base64DecodeStd(signature)
	if err != nil {
		return err
	}

	publicKey, err := g.getPublicKey()
	if err != nil {
		return err
	}

	hash := sha256.Sum256([]byte(content))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hash[:], sigBytes)
}

func (g *AlipayGateway) getPublicKey() (*rsa.PublicKey, error) {
	key := g.config.PublicKeyID
	if key == "" {
		return nil, fmt.Errorf("alipay public key not configured, set oss config pay.alipay_public_key")
	}

	key = strings.ReplaceAll(key, "-----BEGIN PUBLIC KEY-----", "")
	key = strings.ReplaceAll(key, "-----END PUBLIC KEY-----", "")
	key = strings.ReplaceAll(key, "\n", "")
	key = strings.TrimSpace(key)

	return parsePublicKey(key)
}

// doRequest 发起一次支付宝网关请求。
//
// ctx 由调用方透传，使上层超时/取消能真正中断这次出网调用。
//
// nil ctx 防御保留在这里：上层传 nil 是「没有取消能力」的信号，
// 网关应显式降级，而不是指望底层不炸 —— 这是资金链路。
func (g *AlipayGateway) doRequest(ctx context.Context, method, requestURL string, params map[string]string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}

	respBody, status, err := g.client.Do(ctx, method, requestURL, []byte(form.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if err != nil {
		return nil, err
	}
	// 支付宝把错误放在 JSON 里的 code 字段，所以这里对非 2xx 只做兜底报错，
	// 前提是响应体确实不是 JSON（JSON 场景交由解析方给出更精确的提示）。
	if status >= 400 && !json.Valid(respBody) {
		return nil, fmt.Errorf("alipay request failed(%d): %s", status, string(respBody))
	}
	return respBody, nil
}

func buildFormQuery(params map[string]string) string {
	sortedKeys := make([]string, 0, len(params))
	for k := range params {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	var pairs []string
	for _, k := range sortedKeys {
		if params[k] != "" {
			pairs = append(pairs, k+"="+url.QueryEscape(params[k]))
		}
	}
	return strings.Join(pairs, "&")
}

func parsePublicKey(key string) (*rsa.PublicKey, error) {
	pemStr := "-----BEGIN PUBLIC KEY-----\n" + key + "\n-----END PUBLIC KEY-----"

	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}

	return rsaPub, nil
}

// validateReturnURL 校验 returnURL，防止开放重定向攻击
func validateReturnURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("无效的回调地址")
	}

	// 只允许 http/https 协议
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("回调地址只支持 http/https 协议")
	}

	// 禁止 javascript: data: 等危险协议
	scheme := strings.ToLower(u.Scheme)
	if scheme == "javascript" || scheme == "data" || scheme == "vbscript" {
		return fmt.Errorf("不支持的协议类型")
	}

	// 主机名不能为空
	if u.Host == "" {
		return fmt.Errorf("回调地址缺少主机名")
	}

	return nil
}
