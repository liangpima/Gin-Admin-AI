package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/internal/module/payment/model"
	paymentRepo "go-admin/internal/module/payment/repository"
	systemModel "go-admin/internal/module/system/model"
	systemService "go-admin/internal/module/system/service"
)

// payGatewayTimeout 单次支付网关调用的时间上限。
//
// 网关内部的 http.Client 另有 10s 超时，这里再包一层是让「出网调用必须有上限」
// 收敛到调用方统一控制，同时让网关方法签名上的 ctx 参数真正生效
// —— 此前调用方一律传 nil、网关内部也忽略该参数，属于「看起来有超时、实际没有」。
const payGatewayTimeout = 15 * time.Second

// payCtx 返回一次支付网关调用使用的上下文。
//
// 不用 context.Background() 裸传：调用链上没有更上层 ctx 可用，
// 但资金操作必须有一个明确的等待上限。
func payCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), payGatewayTimeout)
}

type PayNotifyResult struct {
	OrderNo  string
	TradeNo  string
	Status   string
	Amount   int64
	PaidAt   *time.Time
	RawData  string
}

type PaymentService struct {
	orderRepo paymentRepo.PayOrderRepository
	mu        sync.Mutex

	// gatewayRefund 实际调用支付渠道退款的函数，默认为 refundVia。
	// 留出这个接缝是因为真实渠道调用依赖线上配置与网络，无法在单测中执行，
	// 而「并发退款只允许一个请求打到渠道」正是最需要回归保护的行为。
	gatewayRefund func(order *model.PayOrder, refundNo string, refundAmt int64) error
}

func NewPaymentService() *PaymentService {
	return &PaymentService{
		orderRepo: paymentRepo.NewPayOrderRepository(),
	}
}

// refund 调用支付渠道发起退款，可通过 gatewayRefund 替换以便测试
func (s *PaymentService) refund(order *model.PayOrder, refundNo string, refundAmt int64) error {
	if s.gatewayRefund != nil {
		return s.gatewayRefund(order, refundNo, refundAmt)
	}
	return s.refundVia(order, refundNo, refundAmt)
}

// refundVia 按渠道发起真实退款请求
func (s *PaymentService) refundVia(order *model.PayOrder, refundNo string, refundAmt int64) error {
	// ctx 提到 switch 之前：两个分支共用同一个调用上限，无需各自 defer
	ctx, cancel := payCtx()
	defer cancel()

	switch order.Channel {
	case "wechat":
		cfg := LoadWechatPayConfig()
		gw := NewWechatPayGateway(*cfg)
		return gw.Refund(ctx, order.OrderNo, refundNo, order.Amount, refundAmt)
	case "alipay":
		cfg := LoadAlipayConfig()
		gw := NewAlipayGateway(*cfg)
		_, err := gw.Refund(ctx, order.OrderNo, refundNo, refundAmt)
		return err
	default:
		return common.NewBizErrorf("不支持的支付渠道: %s", order.Channel)
	}
}

func (s *PaymentService) CreateOrder(tenantID uint, orderNo, subject, body string, amount int64, channel, openID, notifyURL, extra string) (*model.PayOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 订单号重复时，只有「标题 + 金额 + 渠道」全部一致才视为幂等重放并复用原单；
	// 否则说明订单号被复用（撞号或调用方传了重复单号），
	// 此时**绝不能把已存在的订单返回给调用方** —— 那是别人的单子，
	// 调用方却会拿新参数去调渠道，造成订单与支付参数错配。
	if existing, err := s.orderRepo.FindByOrderNo(tenantID, orderNo); err == nil && existing != nil && existing.ID > 0 {
		if existing.Subject == subject && existing.Amount == amount && existing.Channel == channel {
			return existing, nil
		}
		return nil, common.NewBizError("订单号已存在，请更换订单号后重试")
	}

	order := &model.PayOrder{
		TenantID:  tenantID,
		OrderNo:   orderNo,
		Subject:   subject,
		Body:      body,
		Amount:    amount,
		Currency:  "CNY",
		Channel:   channel,
		Status:    model.StatusPending,
		OpenID:    openID,
		NotifyURL: notifyURL,
		Extra:     extra,
	}

	if err := s.orderRepo.Create(order); err != nil {
		return nil, err
	}

	return order, nil
}

func (s *PaymentService) GetOrder(tenantID uint, orderNo string) (*model.PayOrder, error) {
	order, err := s.orderRepo.FindByOrderNo(tenantID, orderNo)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "订单不存在")
	}
	return order, nil
}

func (s *PaymentService) GetOrderByID(tenantID, id uint) (*model.PayOrder, error) {
	order, err := s.orderRepo.FindByID(tenantID, id)
	if err != nil {
		return nil, common.NotFoundOrErr(err, "订单不存在")
	}
	return order, nil
}

func (s *PaymentService) CloseOrder(tenantID uint, orderNo string) error {
	// 走 GetOrder 而非直接查 repository：前者会把「记录不存在」转成 404 业务错误，
	// 否则 gorm.ErrRecordNotFound 一路透出会变成 500「服务器内部错误」
	order, err := s.GetOrder(tenantID, orderNo)
	if err != nil {
		return err
	}

	if order.Status == model.StatusPaid {
		return common.NewBizError("订单已支付，无法关闭")
	}
	if order.Status == model.StatusClosed {
		return common.NewBizError("订单已关闭")
	}
	if order.Status == model.StatusRefunding {
		return common.NewBizError("订单退款处理中，无法关闭")
	}
	if order.Status == model.StatusRefunded {
		return common.NewBizError("订单已退款，无法关闭")
	}

	// 上面的状态检查只为给出更精确的提示，**不构成并发保护** ——
	// 检查与写入之间订单仍可能被支付回调置为已支付。
	// 真正的保护是条件更新（WHERE status = 待支付）：只有仍处于待支付的订单
	// 才会被关闭，affected=false 说明状态已被并发流转，此时绝不能覆盖。
	affected, err := s.orderRepo.CloseIfPending(tenantID, orderNo)
	if err != nil {
		return err
	}
	if !affected {
		return common.NewBizError("订单状态已变更，请刷新后重试")
	}
	return nil
}

func (s *PaymentService) HandleNotify(channel string, result *PayNotifyResult) error {
	if result == nil || result.OrderNo == "" {
		return fmt.Errorf("invalid notify result")
	}

	// 回调场景：不带 tenant_id 过滤（无法从外部请求获取租户信息）
	order, err := s.orderRepo.FindByOrderNoForNotify(result.OrderNo)
	if err != nil {
		return fmt.Errorf("order not found: %s", result.OrderNo)
	}

	// 校验回调渠道与订单渠道一致。
	//
	// HandleNotify 此前完全没用 channel 参数，于是「微信的通知」可以落到
	// 「支付宝的订单」上 —— 只要订单号对得上就放行。正常情况下不会发生
	// （两个通知端点各自只解析对应渠道的报文），但这层校验成本几乎为零，
	// 能在某个渠道的验签出问题时兜住跨渠道改单，属于防御纵深。
	if channel != "" && order.Channel != channel {
		logger.Log.Errorf("[payment] 回调渠道与订单渠道不一致, 订单=%s 订单渠道=%s 回调渠道=%s",
			result.OrderNo, order.Channel, channel)
		return fmt.Errorf("回调渠道与订单渠道不一致")
	}

	if order.Status == model.StatusPaid {
		logger.Log.Infof("[payment] 订单 %s 已是已支付，跳过重复回调", result.OrderNo)
		return nil
	}

	if result.Status == "success" {
		// 金额必须严格相等。
		//
		// 早前写的是 `result.Amount > 0 && result.Amount != order.Amount`，
		// 那个 `> 0` 意味着「没解析到金额就不校验」，属于 fail-open：
		// 只要让渠道载荷中的金额字段缺失或解析失败，金额校验会被整体跳过。
		// 现在改为无条件比对，金额缺失（=0）同样拒绝。
		if result.Amount != order.Amount {
			// 金额不符属异常（可能是伪造回调或渠道串单），必须留在 Error 级便于告警
			logger.Log.Errorf("[payment] 回调金额不匹配, 订单 %s 期望 %d 实际 %d",
				result.OrderNo, order.Amount, result.Amount)
			return fmt.Errorf("支付金额不匹配")
		}

		paidAt := result.PaidAt
		if paidAt == nil {
			now := time.Now()
			paidAt = &now
		}

		// 以数据库条件更新保证幂等：并发/重复回调中只有一个能完成状态流转，
		// 避免因实例级锁不共享（每次请求新建 Service）导致的重复发货。
		affected, err := s.orderRepo.MarkPaidIfPending(result.OrderNo, result.TradeNo, paidAt, result.RawData)
		if err != nil {
			return err
		}
		if !affected {
			logger.Log.Infof("[payment] 订单 %s 已被并发回调处理，跳过", result.OrderNo)
			return nil
		}

		logger.Log.Infof("[payment] 订单 %s 支付成功, trade_no: %s", result.OrderNo, result.TradeNo)
	}

	return nil
}

// validateRefund 退款前的纯校验（不产生任何副作用），供退款流程复用
func validateRefund(order *model.PayOrder, refundAmt int64) error {
	if order.Status == model.StatusRefunding {
		return common.NewBizError("退款正在处理中，请勿重复提交")
	}
	if order.Status == model.StatusRefunded {
		return common.NewBizError("订单已退款")
	}
	if order.Status != model.StatusPaid {
		return common.NewBizError("订单未支付，无法退款")
	}
	if refundAmt <= 0 {
		return common.NewBizError("退款金额必须大于0")
	}
	// 按「剩余可退金额」判断，而不是订单总额：
	// 支持分次部分退款后，此前已退过的部分不能再退一次。
	// 若某次退款正好退满，状态会落定为「已退款」，后续请求在上面
	// 的状态检查（!= 已支付）就被拦下了，因此这里用剩余额度即可覆盖。
	remaining := order.Amount - order.RefundAmt
	if refundAmt > remaining {
		return common.NewBizErrorf("退款金额超过可退余额（最多可退 %d 分）", remaining)
	}
	return nil
}

func (s *PaymentService) FindList(tenantID uint, subject string, status int8, channel string, page, pageSize int) ([]model.PayOrder, int64, error) {
	return s.orderRepo.FindList(tenantID, subject, status, channel, page, pageSize)
}

type CreateOrderResult struct {
	Order    *model.PayOrder
	PayInfo  map[string]interface{}
	PayError error
}

func (s *PaymentService) CreateOrderWithPayInfo(tenantID uint, orderNo, subject, body string, amount int64, channel, openID, extra string) (*CreateOrderResult, error) {
	configs := LoadWechatPayConfig()
	notifyURL := configs.NotifyURL

	order, err := s.CreateOrder(tenantID, orderNo, subject, body, amount, channel, openID, notifyURL, extra)
	if err != nil {
		return nil, err
	}

	result := &CreateOrderResult{
		Order: order,
		PayInfo: map[string]interface{}{
			"orderNo": order.OrderNo,
			"amount":  order.Amount,
			"status":  order.Status,
		},
	}

	// 出网调用必须有明确的等待上限：payCtx 早就为此写好了，但调用方一直传 nil
	// （网关内部再把 nil 降级为 Background），等于「签名上有 ctx、实际没有超时」。
	ctx, cancel := payCtx()
	defer cancel()

	switch channel {
	case "wechat":
		cfg := LoadWechatPayConfig()
		gw := NewWechatPayGateway(*cfg)
		payInfo, payErr := gw.Prepay(ctx, orderNo, subject, body, amount, openID)
		if payErr != nil {
			result.PayError = payErr
		} else {
			for k, v := range payInfo {
				result.PayInfo[k] = v
			}
		}
	case "alipay":
		cfg := LoadAlipayConfig()
		gw := NewAlipayGateway(*cfg)
		payInfo, payErr := gw.Prepay(ctx, orderNo, subject, amount, cfg.ReturnURL)
		if payErr != nil {
			result.PayError = payErr
		} else {
			for k, v := range payInfo {
				result.PayInfo[k] = v
			}
		}
	}

	return result, nil
}

type RefundOrderResult struct {
	RefundNo string
	Status   string
	Error    error
}

// RefundOrderWithPayInfo 发起退款。
//
// 关键：**先抢占状态，再调用支付网关**。
//
// 早期实现是「查状态 → 调网关 → 改状态」，三者之间没有互斥。
// 两个并发请求会同时通过状态检查，于是向渠道真实退款两次 ——
// 数据库最终状态看起来正常，钱却多退了一份。
// 实例级 mutex 也救不了：它只覆盖最后那步改状态，而资金操作在锁外。
//
// 现在的顺序是：
//  1. 校验（纯读，无副作用）
//  2. ClaimRefund 用条件更新把 已支付 → 退款中，只有一个请求能成功
//  3. 抢到的人才调网关
//  4. 网关失败 → 回滚为已支付（允许重试）；成功 → 落定为已退款
func (s *PaymentService) RefundOrderWithPayInfo(tenantID uint, orderNo string, refundNo string, refundAmt int64) (*RefundOrderResult, error) {
	order, err := s.GetOrder(tenantID, orderNo)
	if err != nil {
		return nil, err
	}

	if err := validateRefund(order, refundAmt); err != nil {
		return nil, err
	}

	// 渠道校验放在抢占之前：不支持时直接拒绝，不会留下卡在「退款中」的订单
	if order.Channel != "wechat" && order.Channel != "alipay" {
		return nil, common.NewBizErrorf("不支持的支付渠道: %s", order.Channel)
	}

	// 抢占退款权。抢不到说明已有并发请求在处理，直接拒绝，绝不重复调网关。
	claimed, err := s.orderRepo.ClaimRefund(orderNo)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, common.NewBizError("退款正在处理中或已退款，请勿重复提交")
	}

	result := &RefundOrderResult{
		RefundNo: refundNo,
		Status:   "refunding",
	}

	if err := s.refund(order, refundNo, refundAmt); err != nil {
		// 渠道侧失败：回滚状态，让用户可以重新发起退款
		if releaseErr := s.orderRepo.ReleaseRefundClaim(orderNo); releaseErr != nil {
			// 订单会卡在「退款中」且用户无法重试，需要人工介入 —— 必须是 Error 级
			logger.Log.Errorf("[payment] 退款失败后回滚状态也失败, 订单=%s: %v（订单可能卡在退款中，需人工核对）",
				orderNo, releaseErr)
		}
		result.Error = err
		return result, nil
	}

	// 依据「累计退款是否退满」决定终态：
	//   退满 → 已退款；未退满（部分退款）→ 回到已支付，以便继续退剩余部分。
	//
	// order.RefundAmt 是本次抢占之前读到的值。这一列只可能被持有退款权
	// （status=退款中）的流程修改，而当前请求正是持有者，因此该读值可信。
	// 旧实现这里无条件写「已退款」，导致部分退款后剩余金额永远退不了；
	// 且 refund_amt 是覆盖写，第二次退款会把第一次的金额抹掉。
	newRefundTotal := order.RefundAmt + refundAmt
	finalStatus := model.StatusPaid
	if newRefundTotal >= order.Amount {
		finalStatus = model.StatusRefunded
	}

	affected, err := s.orderRepo.UpdateRefund(orderNo, refundAmt, time.Now(), finalStatus)
	if err != nil {
		result.Error = fmt.Errorf("更新退款状态失败: %v", err)
		return result, nil
	}
	if !affected {
		// 已抢到退款权（即钱可能已退出）却没能落定状态，属资金不一致，
		// 必须落在 Error 级以便告警与人工核对
		logger.Log.Errorf("[payment] 退款已完成但状态落定失败, 订单=%s（渠道可能已退款，需人工核对账目）", orderNo)
	} else if finalStatus == model.StatusPaid {
		// 部分退款属正常业务，但订单状态仍是「已支付」，
		// 对账时容易困惑，故记一条 Info 说明已退金额与剩余额度
		logger.Log.Infof("[payment] 订单 %s 部分退款完成，累计已退 %d/%d 分，订单保持已支付",
			orderNo, newRefundTotal, order.Amount)
	}

	return result, nil
}

func loadPayConfig() map[string]string {
	configService := systemService.NewConfigService()
	// 需要真实密钥用于签名与验签，因此读取原始值（接口侧会打码）
	results, err := configService.FindByPrefixRaw("pay.")
	if err != nil {
		// 不静默：读不到配置会让所有渠道凭据变成空串，最终以「签名失败」
		// 「商户号未配置」这类看不出根因的错误暴露出来。
		// 仍返回空 map：调用方（LoadWechatPayConfig / LoadAlipayConfig）
		// 的签名是返回配置结构，改签名会牵连上层；这里至少把根因留在日志里。
		logger.Log.Errorf("[payment] 读取 pay.* 配置失败，渠道凭据将为空: %v", err)
	}

	cfgMap := make(map[string]string)
	for _, r := range results {
		if cfg, ok := r.(systemModel.SysConfig); ok {
			key := cfg.ConfigKey
			if len(key) > 4 && key[:4] == "pay." {
				cfgMap[key[4:]] = cfg.Value
			}
		}
	}
	return cfgMap
}

func LoadWechatPayConfig() *WechatPayConfig {
	cfgMap := loadPayConfig()
	return &WechatPayConfig{
		AppID:     cfgMap["wechat_app_id"],
		MchID:     cfgMap["wechat_mch_id"],
		Key:       cfgMap["wechat_key"],
		APIv3Key:  cfgMap["wechat_apiv3_key"],
		SerialNo:  cfgMap["wechat_serial_no"],
		NotifyURL: cfgMap["notify_url"],
	}
}

func LoadAlipayConfig() *AlipayConfig {
	cfgMap := loadPayConfig()
	return &AlipayConfig{
		AppID:       cfgMap["alipay_app_id"],
		PrivateKey:  cfgMap["alipay_key"],
		NotifyURL:   cfgMap["notify_url"],
		ReturnURL:   cfgMap["return_url"],
		PublicKeyID: cfgMap["alipay_public_key"],
	}
}
