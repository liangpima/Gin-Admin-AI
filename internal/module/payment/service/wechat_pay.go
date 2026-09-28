package service

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-admin/pkg/httpx"

	"github.com/golang-jwt/jwt/v5"
)

type WechatPayConfig struct {
	AppID string
	MchID string
	// Key 商户 API 私钥（PEM 格式），用于请求签名
	Key string
	// APIv3Key 微信支付 APIv3 密钥（32 位字符串），用于回调报文解密。
	// 它与上面的商户私钥是两个完全不同的凭据，不可互相替代。
	APIv3Key  string
	SerialNo  string
	NotifyURL string
}

// WechatPayGateway 微信支付网关。
//
// 出网统一走 pkg/httpx：Service 层不直接依赖 net/http（分层约定）。
// 超时由 httpx 统一给（默认 10s），不在这里重复设。
type WechatPayGateway struct {
	config WechatPayConfig
	client *httpx.Client

	// fetchCerts 拉取平台证书。为 nil 时走真实出网（fetchPlatformPublicKeys）。
	//
	// 留这个接缝**不是**为了在单测里 mock 掉网络层，而是因为「未知 serial 的
	// 限速与单飞」这段逻辑的全部价值就在「到底有没有出网」上 ——
	// 不能观察出网次数，就等于没测到它（与 middleware 那次的教训同源：
	// 接线类缺陷只有真请求或真实调用计数能发现）。
	// 生产路径永远为 nil，行为与没有它时完全一致。
	fetchCerts func() (map[string]*rsa.PublicKey, error)
}

func NewWechatPayGateway(cfg WechatPayConfig) *WechatPayGateway {
	return &WechatPayGateway{
		config: cfg,
		client: httpx.NewClient(httpx.DefaultTimeout),
	}
}

// HeaderGetter 只声明本包用到的读取能力，不直接依赖 http.Header。
// http.Header 本身就有 Get(string) string，天然满足此接口，
// 所以 controller 侧传 c.Request.Header 无需任何改动。
type HeaderGetter interface {
	Get(key string) string
}

func (g *WechatPayGateway) Prepay(ctx context.Context, orderNo, subject, body string, amount int64, openID string) (map[string]interface{}, error) {
	order := map[string]interface{}{
		"appid":        g.config.AppID,
		"mchid":        g.config.MchID,
		"description":  subject,
		"out_trade_no": orderNo,
		"notify_url":   g.config.NotifyURL,
		"amount": map[string]interface{}{
			"total":    amount,
			"currency": "CNY",
		},
	}

	apiURL := "https://api.mch.weixin.qq.com/v3/pay/transactions/native"
	if openID != "" {
		order["payer"] = map[string]interface{}{"openid": openID}
		apiURL = "https://api.mch.weixin.qq.com/v3/pay/transactions/jsapi"
	}

	bodyBytes, _ := json.Marshal(order)
	resp, err := g.doRequest(ctx, "POST", apiURL, bodyBytes)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	if openID == "" {
		// 微信在 HTTP 200 下也可能不带 code_url（参数或商户配置问题），
		// 直接取值会得到 nil，前端拿到一个空的支付链接却无从判断失败原因
		codeURL, ok := result["code_url"].(string)
		if !ok || codeURL == "" {
			return nil, fmt.Errorf("微信下单响应缺少 code_url")
		}
		return map[string]interface{}{"code_url": codeURL}, nil
	}

	// 这里**不能**直接写 result["prepay_id"].(string)：
	// 字段缺失时该断言会 panic，被 Recovery 兜成 500，下单接口莫名失败。
	prepayID, ok := result["prepay_id"].(string)
	if !ok || prepayID == "" {
		return nil, fmt.Errorf("微信下单响应缺少 prepay_id")
	}
	return g.generateJSAPIPayInfo(prepayID)
}

func (g *WechatPayGateway) generateJSAPIPayInfo(prepayID string) (map[string]interface{}, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonceStr, err := generateNonceStr()
	if err != nil {
		return nil, err
	}
	packageStr := "prepay_id=" + prepayID

	message := fmt.Sprintf("%s\n%s\n%s\n%s\n", g.config.AppID, timestamp, nonceStr, packageStr)

	pk, err := parsePrivateKey(g.config.Key)
	if err != nil {
		return nil, err
	}

	hash := sha256.Sum256([]byte(message))
	sign, err := rsa.SignPKCS1v15(rand.Reader, pk, crypto.SHA256, hash[:])
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"appId":     g.config.AppID,
		"timeStamp": timestamp,
		"nonceStr":  nonceStr,
		"package":   packageStr,
		"signType":  "RSA",
		"paySign":   base64.StdEncoding.EncodeToString(sign),
	}, nil
}

func (g *WechatPayGateway) ParseNotify(body []byte, headers HeaderGetter) (*PayNotifyResult, error) {
	result := &PayNotifyResult{
		Status:  "fail",
		RawData: string(body),
	}

	// 1. Verify signature
	timestamp := headers.Get("Wechatpay-Timestamp")
	nonce := headers.Get("Wechatpay-Nonce")
	signature := headers.Get("Wechatpay-Signature")
	serial := headers.Get("Wechatpay-Serial")

	if err := g.verifySignature(timestamp, nonce, string(body), signature, serial); err != nil {
		return nil, fmt.Errorf("wechatpay signature verify failed: %w", err)
	}

	// 2. Parse notification body
	var notifyBody struct {
		ID           string `json:"id"`
		CreateTime   string `json:"create_time"`
		ResourceType string `json:"resource_type"`
		EventType    string `json:"event_type"`
		Summary      string `json:"summary"`
		Resource     struct {
			Algorithm      string `json:"algorithm"`
			Ciphertext     string `json:"ciphertext"`
			AssociatedData string `json:"associated_data"`
			Nonce          string `json:"nonce"`
			OriginalType   string `json:"original_type"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &notifyBody); err != nil {
		return nil, fmt.Errorf("parse notify body failed: %w", err)
	}

	if notifyBody.Resource.Ciphertext == "" {
		return nil, fmt.Errorf("ciphertext is empty")
	}

	// 3. Decrypt resource
	plaintext, err := g.decryptResource(
		notifyBody.Resource.Ciphertext,
		notifyBody.Resource.Nonce,
		notifyBody.Resource.AssociatedData,
	)
	if err != nil {
		return nil, fmt.Errorf("decrypt resource failed: %w", err)
	}

	// 4. Parse decrypted data
	var decrypted struct {
		OutTradeNo    string `json:"out_trade_no"`
		TransactionID string `json:"transaction_id"`
		TradeState    string `json:"trade_state"`
		SuccessTime   string `json:"success_time"`
		MchID         string `json:"mchid"`
		AppID         string `json:"appid"`
		Payer         struct {
			OpenID string `json:"openid"`
		} `json:"payer"`
		Amount struct {
			Total    int64  `json:"total"`
			PayerTotal int64 `json:"payer_total"`
			Currency string `json:"currency"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(plaintext, &decrypted); err != nil {
		return nil, fmt.Errorf("parse decrypted data failed: %w", err)
	}

	// 校验回调归属，这一步不能省。
	//
	// 验签（verifySignature）只能证明「报文确实来自微信支付平台」，
	// 但微信的平台证书是**所有商户共用**的 —— 别家商户的支付通知，
	// 用同一套平台证书同样能验签通过。
	//
	// 后果是：攻击者在自己的商户号下下一笔单，把 out_trade_no 填成
	// 我们系统里某个待支付订单的订单号，就能拿到一条微信签发的、
	// 「合法签名 + 指向我们订单」的支付成功通知，从而空手套走商品。
	// 金额校验是后面一道防线，但订单金额恰好为 1 分时同样会被穿过。
	//
	// 因此必须比对 mchid / appid，确认这笔交易确实发生在本商户本应用。
	if decrypted.MchID != g.config.MchID {
		return nil, fmt.Errorf("wechatpay notify mchid mismatch: got %q", decrypted.MchID)
	}
	if decrypted.AppID != g.config.AppID {
		return nil, fmt.Errorf("wechatpay notify appid mismatch: got %q", decrypted.AppID)
	}

	result.OrderNo = decrypted.OutTradeNo
	result.TradeNo = decrypted.TransactionID
	result.Amount = decrypted.Amount.Total

	if decrypted.TradeState == "SUCCESS" {
		result.Status = "success"
		if t, err := time.Parse("2006-01-02T15:04:05-07:00", decrypted.SuccessTime); err == nil {
			result.PaidAt = &t
		}
	}

	return result, nil
}

func (g *WechatPayGateway) verifySignature(timestamp, nonce, body, signature, serial string) error {
	// Build message for verification
	message := fmt.Sprintf("%s\n%s\n%s\n", timestamp, nonce, body)

	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("base64 decode signature failed: %w", err)
	}

	// Get platform public key (from cache or fetch)
	pubKey, err := g.getPlatformPublicKey(serial)
	if err != nil {
		return fmt.Errorf("get platform public key failed: %w", err)
	}

	hash := sha256.Sum256([]byte(message))
	return rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], sigBytes)
}

// 平台证书缓存。
//
// 回调验签每次都要用平台公钥，若每次都现拉 /v3/certificates，
// 等于每笔回调多一次带签名的 HTTPS 往返 —— 既拖慢回调，
// 也会把「获取证书失败」放大成「全部回调验签失败」。
var (
	certCacheMu   sync.RWMutex
	certCache     = make(map[string]*rsa.PublicKey)
	certCacheTime time.Time
)

// certCacheTTL 平台证书缓存时长。微信平台证书轮换周期远长于此，10 分钟是安全与性能的折中
const certCacheTTL = 10 * time.Minute

// certFetchTimeout 单次拉取平台证书的出网上限。
//
// 不用裸的 context.Background()：这条路径由**外部请求**触发，
// 没有上限就意味着一个卡住的出网连接能一直占着 goroutine。
const certFetchTimeout = 10 * time.Second

// certUnknownRefetchInterval 「未知 serial」触发的重新拉取的最小间隔。
//
// 为什么需要它：待验签的 serial 直接来自请求头 `Wechatpay-Serial`，
// 而支付回调端点**不需要鉴权**（渠道侧发起、靠验签自证）。
// 若「缓存里没有这个 serial」就无条件重新拉取，匿名调用方只要每次换一个
// 随机 serial，就能让每个请求换来一次带签名的出网调用 —— 既放大出网/CPU，
// 也可能触发微信侧的频率限制，反过来影响**正常**回调的验签。
//
// 30 秒是「证书轮换能被及时发现」与「出网频率可控」之间的折中：
// 轮换后新 serial 最多晚 30 秒被认出来，而期间的续期由渠道重试覆盖
// （微信回调会重试多次），不会丢单。
const certUnknownRefetchInterval = 30 * time.Second

// unknownSerialThrottle 固定间隔限速器：把「因未知 serial 而出网」限制成
// 全局每 interval 一次（不是每 serial 一次 —— 那样攻击者换 serial 就能绕过）。
//
// 做成带 `now` 注入的小类型而不是直接操作包级时间变量，是为了能单测：
// 真实实现用 time.Now，用例用可控时钟，不依赖 sleep（见 wechat_pay_test.go）。
type unknownSerialThrottle struct {
	mu       sync.Mutex
	last     time.Time
	interval time.Duration
	now      func() time.Time
}

// allow 尝试占用一次出网额度，返回是否获准。
func (t *unknownSerialThrottle) allow() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	nowFn := t.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	if !t.last.IsZero() && now.Sub(t.last) < t.interval {
		return false
	}
	t.last = now
	return true
}

// certUnknownThrottle 进程级的未知 serial 限速器。
// 单副本部署下这是准确的上限；多副本时每个副本各自限速（仍远优于不限速）。
var certUnknownThrottle = &unknownSerialThrottle{interval: certUnknownRefetchInterval, now: time.Now}

// certCacheFresh 缓存是否仍在有效期内。
func certCacheFresh() bool {
	certCacheMu.RLock()
	defer certCacheMu.RUnlock()
	return !certCacheTime.IsZero() && time.Since(certCacheTime) <= certCacheTTL
}

func (g *WechatPayGateway) getPlatformPublicKey(serial string) (*rsa.PublicKey, error) {
	if pub := cachedPlatformPublicKey(serial); pub != nil {
		return pub, nil
	}

	// 单飞：并发回调同时遇到缓存未命中时，只让一个真正出网，其余等它。
	// 没有这层时，N 个并发请求就是 N 次带签名的 HTTPS 往返。
	certFetchMu.Lock()
	defer certFetchMu.Unlock()

	// 双检：等锁期间可能已经有人刷新过缓存了
	if pub := cachedPlatformPublicKey(serial); pub != nil {
		return pub, nil
	}

	// 走到这里有两种可能，处置方式不同：
	//   · 缓存**已过期** → 正常的刷新时机，不限速；
	//   · 缓存**仍新鲜**但没这个 serial → 它大概率是伪造的（serial 来自
	//     未鉴权的请求头）。但仍要放行一次重新拉取，因为证书轮换后确实会
	//     出现「缓存里没有」的新 serial；只是必须限速。
	//
	// ⚠️ 限速判断必须放在**锁内、双检之后**：
	//   若放在锁外，一次过期刷新之后所有排队等锁的请求都会各自再拉一遍 ——
	//   单飞就白做了（N 个并发请求 = N 次串行的出网调用）。
	//   放在这里时，第二个及以后的请求看到的是刚刷新好的新鲜缓存，
	//   于是被限速拦下，只产生 1 次出网。
	if certCacheFresh() && !certUnknownThrottle.allow() {
		return nil, fmt.Errorf(
			"wechatpay platform certificate with serial %q not found (refetch throttled)", serial)
	}

	fetch := g.fetchCerts
	if fetch == nil {
		fetch = g.fetchPlatformPublicKeys
	}
	keys, err := fetch()
	if err != nil {
		// 拉取失败时退回旧缓存：宁可用可能过期的证书，也不要让回调整体中断
		if pub := cachedPlatformPublicKey(serial, true); pub != nil {
			return pub, nil
		}
		return nil, err
	}

	certCacheMu.Lock()
	certCache = keys
	certCacheTime = time.Now()
	certCacheMu.Unlock()

	pub, ok := keys[serial]
	if !ok {
		return nil, fmt.Errorf("certificate with serial %s not found", serial)
	}
	return pub, nil
}

// certFetchMu 让「拉取平台证书」同一时刻只有一个在飞（单飞 / single-flight）。
//
// 注意它必须在 certCacheMu **之外**持有：拉取期间其它 goroutine 仍要能读缓存
// （命中时直接返回，不受慢出网阻塞）。
var certFetchMu sync.Mutex

// cachedPlatformPublicKey 读取缓存的平台公钥。
// allowStale 为 true 时忽略过期判断（用于拉取失败时降级）
func cachedPlatformPublicKey(serial string, allowStale ...bool) *rsa.PublicKey {
	certCacheMu.RLock()
	defer certCacheMu.RUnlock()

	stale := len(allowStale) > 0 && allowStale[0]
	if !stale && (certCacheTime.IsZero() || time.Since(certCacheTime) > certCacheTTL) {
		return nil
	}
	return certCache[serial]
}

func (g *WechatPayGateway) fetchPlatformPublicKeys() (map[string]*rsa.PublicKey, error) {
	// Fetch platform certificates from WeChat Pay API
	certsURL := "https://api.mch.weixin.qq.com/v3/certificates"
	ctx, cancel := context.WithTimeout(context.Background(), certFetchTimeout)
	defer cancel()
	resp, err := g.doRequest(ctx, "GET", certsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch platform certificates failed: %w", err)
	}

	var certsResp struct {
		Data []struct {
			SerialNo string `json:"serial_no"`
			EncryptCertificate struct {
				Algorithm  string `json:"algorithm"`
				Ciphertext string `json:"ciphertext"`
				Nonce      string `json:"nonce"`
				AssociatedData string `json:"associated_data"`
			} `json:"encrypt_certificate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &certsResp); err != nil {
		return nil, fmt.Errorf("parse certificates response failed: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(certsResp.Data))
	for _, cert := range certsResp.Data {
		// Decrypt certificate using APIv3 key
		plaintext, err := g.decryptResource(
			cert.EncryptCertificate.Ciphertext,
			cert.EncryptCertificate.Nonce,
			cert.EncryptCertificate.AssociatedData,
		)
		if err != nil {
			// 单张证书解密失败（如新增了未知算法）不应中断整体，跳过即可
			continue
		}

		block, _ := pem.Decode(plaintext)
		if block == nil {
			continue
		}

		certObj, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}

		pubKey, ok := certObj.PublicKey.(*rsa.PublicKey)
		if !ok {
			continue
		}
		keys[cert.SerialNo] = pubKey
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no usable platform certificate")
	}
	return keys, nil
}

func (g *WechatPayGateway) decryptResource(ciphertext, nonce, associatedData string) ([]byte, error) {
	// 回调解密用的是「APIv3 密钥」——商户在微信支付平台单独设置的 32 位字符串，
	// 而不是请求签名所用的商户私钥（config.Key）。二者混用会导致解密必然失败。
	keyBytes := []byte(g.config.APIv3Key)
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("APIv3 密钥长度必须为 32 字节，当前 %d 字节", len(keyBytes))
	}

	ciphertextBytes, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("base64 decode ciphertext failed: %w", err)
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// APIv3 报文的 nonce 为 12 字节，与 GCM 标准 nonce 长度一致
	if len(nonce) != aesGCM.NonceSize() {
		return nil, fmt.Errorf("nonce 长度必须为 %d 字节，当前 %d 字节", aesGCM.NonceSize(), len(nonce))
	}

	plaintext, err := aesGCM.Open(nil, []byte(nonce), ciphertextBytes, []byte(associatedData))
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// doRequest 发起一次微信支付 API 请求（自动附加 APIv3 签名头）。
//
// ctx 由调用方传入并透传给底层 http 请求，这样上层设的超时/取消
// 才能真正中断这次出网调用。早前这里写死 context.Background()，
// 而调用方一律传 nil，导致网关方法签名上的 ctx 参数形同虚设。
//
// 入口的 nil ctx 防御保留在这里而不是下沉到 httpx：上层传 nil 是
// 「没有取消能力」的信号，网关应显式降级而不是让底层 panic。
// （资金链路宁可退化也不能崩。）
func (g *WechatPayGateway) doRequest(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	// 签名必须在请求发出前算好：APIv3 的签名串里含 body 摘要，
	// 顺序错了会导致网关侧验签失败。
	authorization, err := g.generateAuthorization(method, url, string(body))
	if err != nil {
		return nil, fmt.Errorf("生成支付请求签名失败: %w", err)
	}

	header := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "application/json",
		"Authorization": authorization,
	}
	// 非 2xx 也要读 body：微信把错误码放在响应体里，丢掉就没法定位问题
	respBody, status, err := g.client.Do(ctx, method, url, body, header)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("wechatpay request failed(%d): %s", status, string(respBody))
	}
	return respBody, nil
}

func (g *WechatPayGateway) generateAuthorization(method, url, body string) (string, error) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonceStr, err := generateNonceStr()
	if err != nil {
		return "", err
	}
	message := buildSignatureMessage(method, url, timestamp, nonceStr, body)

	pk, err := parsePrivateKey(g.config.Key)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256([]byte(message))
	sign, err := rsa.SignPKCS1v15(rand.Reader, pk, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`,
		g.config.MchID, nonceStr, base64.StdEncoding.EncodeToString(sign), timestamp, g.config.SerialNo), nil
}

// buildSignatureMessage 构造微信支付 APIv3 的待签名串。
//
// 格式（每行末尾都要有 \n，含最后一行）：
//
//	HTTP方法\nURL路径(含query)\n时间戳\n随机串\n报文主体\n
//
// 两个易错点：URL 必须是去掉域名的路径；报文主体必须参与签名 ——
// 漏掉 body 会让所有带请求体的调用（下单/退款）签名校验失败。
func buildSignatureMessage(method, url, timestamp, nonce, body string) string {
	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n", method, canonicalURL(url), timestamp, nonce, body)
}

// canonicalURL 取参与签名的 URL：即绝对地址去掉协议与域名后的「路径 + query」。
//
// 微信 APIv3 要求签名串里的 URL 不含域名（/v3/pay/transactions/native 而非完整 https:// 地址），
// 传入完整地址会导致签名比对失败，表现为所有主动请求（下单/退款/查询）返回 401。
// 解析失败时退回原值，不至于因为格式问题让请求彻底发不出去。
func canonicalURL(rawURL string) string {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if u.RequestURI() == "" {
		return rawURL
	}
	return u.RequestURI()
}

func parsePrivateKey(key string) (*rsa.PrivateKey, error) {
	key = strings.ReplaceAll(key, "-----BEGIN PRIVATE KEY-----", "")
	key = strings.ReplaceAll(key, "-----END PRIVATE KEY-----", "")
	key = strings.ReplaceAll(key, "\n", "")
	key = strings.TrimSpace(key)

	return jwt.ParseRSAPrivateKeyFromPEM([]byte("-----BEGIN PRIVATE KEY-----\n" + key + "\n-----END PRIVATE KEY-----"))
}

// 微信 V3 退款单状态。
//
// 关键前提：微信 V3 的退款接口在**业务失败时同样返回 HTTP 200**，
// 真正的结果在响应体的 status 字段里。只看 HTTP 状态码会把
// ABNORMAL（余额不足、账户异常等）与 PROCESSING 误判成「退款成功」。
const (
	wechatRefundSuccess    = "SUCCESS"
	wechatRefundProcessing = "PROCESSING"
	wechatRefundClosed     = "CLOSED"
	wechatRefundAbnormal   = "ABNORMAL"
)

// WechatRefund applies refund via WeChat Pay V3 API
func (g *WechatPayGateway) Refund(ctx context.Context, orderNo, refundNo string, amount, refundAmount int64) error {
	body := map[string]interface{}{
		"out_trade_no":   orderNo,
		"out_request_no": refundNo,
		"notify_url":     g.config.NotifyURL,
		"amount": map[string]interface{}{
			"refund":   refundAmount,
			"total":    amount,
			"currency": "CNY",
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("序列化退款请求失败: %w", err)
	}

	resp, err := g.doRequest(ctx, "POST", "https://api.mch.weixin.qq.com/v3/refund/domestic/refunds", bodyBytes)
	if err != nil {
		return err
	}

	// 必须解析响应体：HTTP 200 只代表「请求被受理」，不代表退款成功。
	// 早前这里直接丢弃响应体（`_, err := g.doRequest(...)`），于是
	// ABNORMAL / PROCESSING 都被当成成功 → 订单落定「已退款」而钱没退出去，
	// 且状态机锁死后连重试都被 validateRefund 拒绝，只能人工改库。
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("解析退款响应失败: %w", err)
	}

	switch result.Status {
	case wechatRefundSuccess:
		return nil
	case wechatRefundProcessing:
		// 已受理、结果未定。不能当失败处理（回滚退款权会导致重复退款），
		// 也不能当成功处理（钱可能还没退）。交由调用方保留「退款中」。
		return fmt.Errorf("%w（微信退款单 %s）", ErrRefundPending, refundNo)
	case wechatRefundClosed, wechatRefundAbnormal:
		// 确定失败，钱未退出，回滚退款权让用户重试是安全的
		return fmt.Errorf("微信退款失败，渠道状态 %s（退款单 %s）", result.Status, refundNo)
	default:
		// 未知状态一律按「未确认成功」处理，同样不允许回滚退款权
		return fmt.Errorf("%w（微信返回未知状态 %q，退款单 %s）", ErrRefundPending, result.Status, refundNo)
	}
}

// WechatQueryRefund queries refund status
func (g *WechatPayGateway) WechatQueryRefund(ctx context.Context, refundNo string) (map[string]interface{}, error) {
	url := fmt.Sprintf("https://api.mch.weixin.qq.com/v3/refund/domestic/refunds/%s", refundNo)
	resp, err := g.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// WechatQueryOrder queries order status from WeChat
func (g *WechatPayGateway) WechatQueryOrder(ctx context.Context, orderNo string) (map[string]interface{}, error) {
	url := fmt.Sprintf("https://api.mch.weixin.qq.com/v3/pay/transactions/out-trade-no/%s?mchid=%s", orderNo, g.config.MchID)
	resp, err := g.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}
	return result, nil
}
