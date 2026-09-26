package controller

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"go-admin/internal/common"
	"go-admin/internal/logger"
	"go-admin/internal/module/payment/service"

	"github.com/gin-gonic/gin"
)

// genOrderNo 生成订单号 / 退款单号。
//
// 不能用「毫秒时间戳 + 纳秒末四位」（原实现）：同一毫秒内的并发请求有约 1/10000 概率撞号，
// 而 pay_order.order_no 带唯一索引；更糟的是 CreateOrder 撞号时会直接返回已存在的订单，
// 调用方拿到的是**别人的单子**，随后却用新单号去调渠道，语义完全错乱。
//
// 改为「时间戳 + 加密随机数」：时间戳保证大致有序便于排查，随机部分让碰撞概率可忽略。
func genOrderNo(prefix string) string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		// 随机源异常属极端情况，退回时间戳 + 微秒，至少不 panic
		return fmt.Sprintf("%s%d%06d", prefix, time.Now().UnixMilli(), time.Now().Nanosecond()/1000)
	}
	return fmt.Sprintf("%s%d%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(b))
}

type PaymentController struct {
	paymentService *service.PaymentService
}

func NewPaymentController() *PaymentController {
	return &PaymentController{paymentService: service.NewPaymentService()}
}

// @Summary 创建支付订单
// @Tags 支付订单
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param subject body string true "订单标题"
// @Param body_ body string false "订单描述"
// @Param amount body int true "金额（单位：分）"
// @Param channel body string true "支付渠道：wechat / alipay"
// @Param openId body string false "微信 openid（JSAPI 支付需要）"
// @Param extra body string false "附加数据，原样回传"
// @Success 200 {object} common.Response
// @Router /system/pay/order [post]
func (ctl *PaymentController) CreateOrder(c *gin.Context) {
	var req struct {
		Subject  string `json:"subject" binding:"required"`
		Body     string `json:"body"`
		Amount   int64  `json:"amount" binding:"required"`
		Channel  string `json:"channel" binding:"required"`
		OpenID   string `json:"openId"`
		Extra    string `json:"extra"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	if req.Amount <= 0 {
		common.Error(c, common.CodeBadRequest, "金额必须大于0")
		return
	}

	if req.Channel != "wechat" && req.Channel != "alipay" {
		common.Error(c, common.CodeBadRequest, "不支持的支付渠道")
		return
	}

	tenantID := common.GetTenantID(c)
	orderNo := genOrderNo("PAY")

	result, err := ctl.paymentService.CreateOrderWithPayInfo(
		tenantID, orderNo, req.Subject, req.Body,
		req.Amount, req.Channel, req.OpenID, req.Extra,
	)
	if err != nil {
		logger.Log.Infof("[payment] create order failed: %v", err)
		common.FailWith(c, err)
		return
	}

	if result.PayError != nil {
		// 不回传渠道原始错误：里面可能带证书路径、商户配置、上游报文等实现细节。
		// 真实错误进日志供排查，对外只给可理解的提示。
		// （前端并未消费 payError，去掉不影响交互。）
		logger.Log.Errorf("[payment] 发起支付失败: channel=%s amount=%d err=%v",
			req.Channel, req.Amount, result.PayError)
		result.PayInfo["payError"] = "发起支付失败，请稍后重试或联系管理员"
	}

	common.Success(c, result.PayInfo)
}

// @Summary 按订单号查订单
// @Tags 支付订单
// @Produce json
// @Security BearerAuth
// @Param orderNo query string true "订单号"
// @Success 200 {object} common.Response
// @Router /system/pay/order [get]
func (ctl *PaymentController) GetOrder(c *gin.Context) {
	orderNo := c.Query("orderNo")
	if orderNo == "" {
		common.Error(c, common.CodeBadRequest, "订单号不能为空")
		return
	}

	tenantID := common.GetTenantID(c)
	order, err := ctl.paymentService.GetOrder(tenantID, orderNo)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, order)
}

// @Summary 关闭订单
// @Description 仅未支付的订单可关闭；状态冲突返回 400
// @Tags 支付订单
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param orderNo body string true "订单号"
// @Success 200 {object} common.Response
// @Router /system/pay/order/close [post]
func (ctl *PaymentController) CloseOrder(c *gin.Context) {
	var req struct {
		OrderNo string `json:"orderNo" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	tenantID := common.GetTenantID(c)
	// 关闭失败多半是业务状态冲突（已支付/已关闭），应回 400 而不是 500
	if err := ctl.paymentService.CloseOrder(tenantID, req.OrderNo); err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, nil)
}

// @Summary 支付订单列表
// @Tags 支付订单
// @Produce json
// @Security BearerAuth
// @Param subject query string false "订单标题（模糊）"
// @Param channel query string false "支付渠道"
// @Param status query int false "订单状态（非法值按不过滤处理）"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} common.Response
// @Router /system/pay/order/list [get]
func (ctl *PaymentController) FindList(c *gin.Context) {
	subject := c.Query("subject")
	channel := c.Query("channel")
	status := -1
	if s := c.Query("status"); s != "" {
		// 解析失败时保持 -1（不过滤）：
		// 若沿用 Sscanf 部分写入的零值，传个非法 status 会变成「只看待支付」，
		// 用户以为筛掉了数据，实际是参数写错 —— 比直接报错更难排查。
		if _, err := fmt.Sscanf(s, "%d", &status); err != nil {
			status = -1
		}
	}
	page, pageSize := common.GetPageInfo(c)
	tenantID := common.GetTenantID(c)

	list, total, err := ctl.paymentService.FindList(tenantID, subject, int8(status), channel, page, pageSize)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.SuccessWithPage(c, list, total, page, pageSize)
}

// @Summary 微信支付回调
// @Description 由微信支付平台发起，**自行验签、不做登录鉴权**；请求体是渠道原始报文。
// @Description 响应为 `{"code":"SUCCESS"}` 或 `{"code":"FAIL","message":"处理失败"}`，
// @Description 不是统一 Response 结构（渠道侧只认它自己的约定）。
// @Tags 支付回调
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Router /pay/notify/wechat [post]
func (ctl *PaymentController) WechatNotify(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Log.Infof("[pay-notify] wechat read body failed: %v", err)
		c.JSON(200, gin.H{"code": "FAIL", "message": "read body failed"})
		return
	}
	defer func() { _ = c.Request.Body.Close() }()

	logger.Log.Infof("[pay-notify] wechat received body length: %d", len(body))

	cfg := service.LoadWechatPayConfig()
	gw := service.NewWechatPayGateway(*cfg)

	result, err := gw.ParseNotify(body, c.Request.Header)
	if err != nil {
		// 回调接口是公开的（无鉴权），任何人都能 POST 过来 ——
		// 回传 err.Error() 等于把证书/商户配置等内部细节交出去。
		// 渠道侧只需要知道「失败、请重试」，细节留在日志里。
		logger.Log.Errorf("[pay-notify] wechat parse failed: %v", err)
		c.JSON(200, gin.H{"code": "FAIL", "message": "处理失败"})
		return
	}

	logger.Log.Infof("[pay-notify] wechat order_no=%s trade_no=%s status=%s", result.OrderNo, result.TradeNo, result.Status)

	if err := ctl.paymentService.HandleNotify("wechat", result); err != nil {
		logger.Log.Errorf("[pay-notify] wechat handle failed: %v", err)
		c.JSON(200, gin.H{"code": "FAIL", "message": "处理失败"})
		return
	}

	c.JSON(200, gin.H{"code": "SUCCESS", "message": "成功"})
}

// @Summary 支付宝支付回调
// @Description 由支付宝平台发起，**自行验签、不做登录鉴权**；请求体是渠道原始报文。
// @Description 响应为纯文本 `success` 或 `fail`，不是统一 Response 结构。
// @Tags 支付回调
// @Accept x-www-form-urlencoded
// @Produce text/plain
// @Success 200 {string} string "success 或 fail"
// @Router /pay/notify/alipay [post]
func (ctl *PaymentController) AlipayNotify(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Log.Infof("[pay-notify] alipay read body failed: %v", err)
		c.String(200, "fail")
		return
	}
	defer func() { _ = c.Request.Body.Close() }()

	logger.Log.Infof("[pay-notify] alipay received body length: %d", len(body))

	cfg := service.LoadAlipayConfig()
	gw := service.NewAlipayGateway(*cfg)

	result, err := gw.ParseNotify(body)
	if err != nil {
		logger.Log.Infof("[pay-notify] alipay parse failed: %v", err)
		c.String(200, "fail")
		return
	}

	logger.Log.Infof("[pay-notify] alipay order_no=%s trade_no=%s status=%s", result.OrderNo, result.TradeNo, result.Status)

	if err := ctl.paymentService.HandleNotify("alipay", result); err != nil {
		logger.Log.Infof("[pay-notify] alipay handle failed: %v", err)
		c.String(200, "fail")
		return
	}

	c.String(200, "success")
}

// @Summary 查询订单状态（轻量）
// @Description 只回 orderNo / status / paidAt 三个字段，供前端轮询支付结果
// @Tags 支付订单
// @Produce json
// @Security BearerAuth
// @Param orderNo query string true "订单号"
// @Success 200 {object} common.Response
// @Router /system/pay/order/query [get]
func (ctl *PaymentController) QueryOrder(c *gin.Context) {
	orderNo := c.Query("orderNo")
	if orderNo == "" {
		common.Error(c, common.CodeBadRequest, "订单号不能为空")
		return
	}

	tenantID := common.GetTenantID(c)
	order, err := ctl.paymentService.GetOrder(tenantID, orderNo)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	common.Success(c, gin.H{
		"orderNo": order.OrderNo,
		"status":  order.Status,
		"paidAt":  order.PaidAt,
	})
}

// @Summary 订单退款
// @Tags 支付订单
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param orderNo body string true "订单号"
// @Param refundAmt body int true "退款金额（单位：分）"
// @Param refundNo body string false "退款单号，不传则自动生成"
// @Success 200 {object} common.Response
// @Router /system/pay/order/refund [post]
func (ctl *PaymentController) RefundOrder(c *gin.Context) {
	var req struct {
		OrderNo   string `json:"orderNo" binding:"required"`
		RefundAmt int64  `json:"refundAmt" binding:"required"`
		RefundNo  string `json:"refundNo"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeBadRequest, err.Error())
		return
	}

	if req.RefundAmt <= 0 {
		common.Error(c, common.CodeBadRequest, "退款金额必须大于0")
		return
	}

	if req.RefundNo == "" {
		req.RefundNo = genOrderNo("REF")
	}

	tenantID := common.GetTenantID(c)
	result, err := ctl.paymentService.RefundOrderWithPayInfo(tenantID, req.OrderNo, req.RefundNo, req.RefundAmt)
	if err != nil {
		common.FailWith(c, err)
		return
	}

	if result.Error != nil {
		logger.Log.Infof("[payment] refund failed: %v", result.Error)
		common.FailWith(c, result.Error)
		return
	}

	common.Success(c, gin.H{
		"refundNo": result.RefundNo,
		"status":   result.Status,
	})
}
