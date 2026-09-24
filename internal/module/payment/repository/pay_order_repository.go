package repository

import (
	"time"

	"go-admin/internal/common"
	"go-admin/internal/database"
	"go-admin/internal/module/payment/model"

	"gorm.io/gorm"
)

type PayOrderRepository interface {
	Create(order *model.PayOrder) error
	FindByOrderNo(tenantID uint, orderNo string) (*model.PayOrder, error)
	FindByTradeNo(tenantID uint, tradeNo string) (*model.PayOrder, error)
	FindByID(tenantID, id uint) (*model.PayOrder, error)
	FindList(tenantID uint, subject string, status int8, channel string, page, pageSize int) ([]model.PayOrder, int64, error)
	// 用于支付回调，不带 tenant 过滤（回调无法获取 tenant_id）
	FindByOrderNoForNotify(orderNo string) (*model.PayOrder, error)
	// MarkPaidIfPending 原子地将"待支付"订单标记为"已支付"，
	// 返回 affected=true 表示由本次调用完成状态流转（即首次处理）。
	// 重复/并发回调只会有一个成功，从而实现幂等。
	MarkPaidIfPending(orderNo, tradeNo string, paidAt *time.Time, rawNotify string) (bool, error)

	// CloseIfPending 原子地把"待支付"订单关闭。
	// 返回 affected=true 表示由本次调用完成状态流转。
	// 必须用条件更新而非「读出来改字段再整行 Save」：后者会用读时的旧快照
	// 覆盖掉这期间支付回调写入的 status/trade_no/paid_at，
	// 造成「用户已付款、订单却显示已关闭且交易号丢失」。
	CloseIfPending(tenantID uint, orderNo string) (bool, error)

	// ClaimRefund 原子地把"已支付"订单抢占为"退款中"。
	// 返回 affected=true 表示本次调用抢到了退款权，**只有抢到的请求才允许调支付网关**。
	// 这是防止并发重复退款的关键：网关调用是不可逆的资金操作，必须先抢占再调用。
	ClaimRefund(orderNo string) (bool, error)

	// UpdateRefund 落定一次退款结果：**累加**已退金额，并写入调用方算好的目标状态。
	//
	// 与旧接口 MarkRefunded 的差别（那正是缺陷根源）：
	//   - 旧实现写的是 refund_amt = refundAmt（覆盖），第二次部分退款会抹掉第一次的金额；
	//   - 旧实现状态一律置为「已退款」，于是部分退款后剩余金额再也退不了。
	// 现在改为累加 + 由 Service 依据「累计是否退满」决定终态：
	// 退满才是「已退款」，否则回到「已支付」，以便继续退剩余部分。
	//
	// 条件 WHERE status = 退款中 保证只有抢到退款权的那个流程能落定，
	// 因此同一订单不会出现两个流程并发累加同一笔金额。
	UpdateRefund(orderNo string, refundAmt int64, refundAt time.Time, status int8) (bool, error)

	// ReleaseRefundClaim 退款失败时把"退款中"回滚为"已支付"，让用户可以重试
	ReleaseRefundClaim(orderNo string) error
}

type payOrderRepository struct{}

func NewPayOrderRepository() PayOrderRepository {
	return &payOrderRepository{}
}

func (r *payOrderRepository) Create(order *model.PayOrder) error {
	return database.DB.Create(order).Error
}

func (r *payOrderRepository) FindByOrderNo(tenantID uint, orderNo string) (*model.PayOrder, error) {
	var order model.PayOrder
	err := common.TenantScope(database.DB, tenantID).Where("order_no = ?", orderNo).First(&order).Error
	return &order, err
}

func (r *payOrderRepository) FindByTradeNo(tenantID uint, tradeNo string) (*model.PayOrder, error) {
	var order model.PayOrder
	err := common.TenantScope(database.DB, tenantID).Where("trade_no = ?", tradeNo).First(&order).Error
	return &order, err
}

func (r *payOrderRepository) FindByID(tenantID, id uint) (*model.PayOrder, error) {
	var order model.PayOrder
	err := common.TenantScope(database.DB, tenantID).First(&order, id).Error
	return &order, err
}

func (r *payOrderRepository) FindByOrderNoForNotify(orderNo string) (*model.PayOrder, error) {
	var order model.PayOrder
	err := database.DB.Where("order_no = ?", orderNo).First(&order).Error
	return &order, err
}

func (r *payOrderRepository) FindList(tenantID uint, subject string, status int8, channel string, page, pageSize int) ([]model.PayOrder, int64, error) {
	var orders []model.PayOrder
	var total int64

	query := common.TenantScope(database.DB.Model(&model.PayOrder{}), tenantID)

	if subject != "" {
		query = query.Where("subject LIKE ?", "%"+common.EscapeLike(subject)+"%")
	}
	if status >= 0 {
		query = query.Where("status = ?", status)
	}
	if channel != "" {
		query = query.Where("channel = ?", channel)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&orders).Error
	return orders, total, err
}

// CloseIfPending 以「条件更新 + 影响行数」关闭订单：只有仍处于待支付（status=0）
// 的订单才会被置为已关闭（status=2）。
//
// 为什么不能用 `Save(order)`：那是无条件全字段写入。关单流程必然是
// 「先查出来判断状态 → 再写回去」，而支付回调可能正好插在这两步之间，
// 把 status 改成已支付并写入 trade_no/paid_at。此时 Save 会用**读时的旧快照**
// 把这一行整个覆盖回去：status 倒退回已关闭、trade_no 与 paid_at 被清零。
// 结果是用户付了钱、订单显示已关闭、交易号丢失，连对账都无从查起。
//
// 条件更新把「判断」和「写入」合成一条 SQL，由数据库行锁保证原子性，
// 并通过 RowsAffected 告诉调用方是否真的由本次完成流转。
func (r *payOrderRepository) CloseIfPending(tenantID uint, orderNo string) (bool, error) {
	result := common.TenantScope(database.DB, tenantID).Model(&model.PayOrder{}).
		Where("order_no = ? AND status = ?", orderNo, model.StatusPending).
		Update("status", model.StatusClosed)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// MarkPaidIfPending 以「条件更新 + 影响行数」实现回调幂等：
// 只有仍处于待支付（status=0）的订单才会被更新，重复回调不会重复发货。
func (r *payOrderRepository) MarkPaidIfPending(orderNo, tradeNo string, paidAt *time.Time, rawNotify string) (bool, error) {
	result := database.DB.Model(&model.PayOrder{}).
		Where("order_no = ? AND status = ?", orderNo, model.StatusPending).
		Updates(map[string]interface{}{
			"status":     1,
			"trade_no":   tradeNo,
			"paid_at":    paidAt,
			"raw_notify": rawNotify,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ClaimRefund 以「条件更新 + 影响行数」抢占退款权：只有仍处于已支付（status=1）
// 的订单才能被置为退款中（status=4）。
//
// 为什么必须这样做：退款要调用支付网关，那是**不可逆的资金操作**。
// 若先查状态、再调网关、最后改状态，两个并发请求会同时通过状态检查，
// 于是向渠道真实退款两次 —— 数据库状态最终一致，钱却多退了一份。
// 把状态流转提前到网关调用之前，就只有一个请求能拿到退款权。
func (r *payOrderRepository) ClaimRefund(orderNo string) (bool, error) {
	result := database.DB.Model(&model.PayOrder{}).
		Where("order_no = ? AND status = ?", orderNo, model.StatusPaid).
		Update("status", model.StatusRefunding)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// UpdateRefund 落定退款结果：累加已退金额，并写入调用方给定的终态。
//
// 两个刻意的设计：
//
//  1. refund_amt 在 SQL 侧累加（refund_amt + ?），而不是应用层直接赋值 ——
//     赋值会把之前几次部分退款的金额一起抹掉。
//  2. 终态由 Service 判断后传入，而不是在这里用 CASE 计算。
//     原因是 MySQL 的 UPDATE 里，同一条 SET 子句中后写的表达式会读到
//     前面刚更新过的值（与多数数据库相反），要把「累加」和「按累计金额判状态」
//     塞进同一条 SQL 就得依赖这个反直觉的顺序行为，太容易改错。
//     而并发安全并不依赖它：能走到这里的流程必然已持有退款权
//     （WHERE status = 退款中），同一订单不会有第二个流程并发写 refund_amt。
func (r *payOrderRepository) UpdateRefund(orderNo string, refundAmt int64, refundAt time.Time, status int8) (bool, error) {
	result := database.DB.Model(&model.PayOrder{}).
		Where("order_no = ? AND status = ?", orderNo, model.StatusRefunding).
		Updates(map[string]interface{}{
			"status":     status,
			"refund_amt": gorm.Expr("refund_amt + ?", refundAmt),
			"refund_at":  refundAt,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ReleaseRefundClaim 网关退款失败时回滚状态，让订单回到可再次退款的状态
func (r *payOrderRepository) ReleaseRefundClaim(orderNo string) error {
	return database.DB.Model(&model.PayOrder{}).
		Where("order_no = ? AND status = ?", orderNo, model.StatusRefunding).
		Update("status", model.StatusPaid).Error
}
