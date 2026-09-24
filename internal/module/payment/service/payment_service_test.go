package service

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go-admin/internal/common"
	"go-admin/internal/module/payment/model"
)

type mockOrderRepo struct {
	orders   map[string]*model.PayOrder
	nextID   uint
	createFn func(order *model.PayOrder) error
	// findHook 在 FindByOrderNo 内执行，用于把「并发写入恰好落在读与写之间」
	// 这一时序窗口变成可确定性复现的场景。
	findHook func()
	// claimMu 保护状态流转，模拟数据库条件更新的原子性。
	// 没有它，并发测试就失去意义（mock 的读改写不是原子的）。
	claimMu sync.Mutex
}

func newMockRepo() *mockOrderRepo {
	return &mockOrderRepo{
		orders: make(map[string]*model.PayOrder),
		nextID: 1,
	}
}

func (m *mockOrderRepo) Create(order *model.PayOrder) error {
	if m.createFn != nil {
		return m.createFn(order)
	}
	order.ID = m.nextID
	m.nextID++
	m.orders[order.OrderNo] = order
	return nil
}

// CloseIfPending 模拟数据库条件更新：仅当订单仍为待支付（status=0）时才关闭。
// 与真实实现一致地保证「判断 + 修改」的原子性（真实实现依赖 UPDATE ... WHERE 的行锁）。
func (m *mockOrderRepo) CloseIfPending(tenantID uint, orderNo string) (bool, error) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()

	o, ok := m.orders[orderNo]
	if !ok || o.Status != model.StatusPending {
		return false, nil
	}
	o.Status = model.StatusClosed
	return true, nil
}

// FindByOrderNo 返回订单的**副本**，与 GORM 的真实行为一致（每次查询返回新结构体）。
//
// 这一点对竞态回归测试有决定意义：若返回共享指针，调用方持有的对象会被并发写入
// 直接改掉，测试就观察不到「读到旧快照 → 写回覆盖新状态」这个真实缺陷。
//
// 先取快照再触发 findHook：hook 代表「读之后、写之前」落库的并发写入，
// 它不应影响本次已读到的快照 —— 这正是真实数据库的语义。
func (m *mockOrderRepo) FindByOrderNo(tenantID uint, orderNo string) (*model.PayOrder, error) {
	o, ok := m.orders[orderNo]
	if !ok {
		return nil, fmt.Errorf("record not found")
	}
	cp := *o
	if m.findHook != nil {
		m.findHook()
	}
	return &cp, nil
}

func (m *mockOrderRepo) FindByTradeNo(tenantID uint, tradeNo string) (*model.PayOrder, error) {
	for _, o := range m.orders {
		if o.TradeNo == tradeNo {
			return o, nil
		}
	}
	return nil, fmt.Errorf("record not found")
}

func (m *mockOrderRepo) FindByID(tenantID, id uint) (*model.PayOrder, error) {
	for _, o := range m.orders {
		if o.ID == id {
			return o, nil
		}
	}
	return nil, fmt.Errorf("record not found")
}

func (m *mockOrderRepo) FindByOrderNoForNotify(orderNo string) (*model.PayOrder, error) {
	if o, ok := m.orders[orderNo]; ok {
		return o, nil
	}
	return nil, fmt.Errorf("record not found")
}

func (m *mockOrderRepo) FindList(tenantID uint, subject string, status int8, channel string, page, pageSize int) ([]model.PayOrder, int64, error) {
	var result []model.PayOrder
	for _, o := range m.orders {
		if subject != "" && o.Subject != subject {
			continue
		}
		if status >= 0 && o.Status != status {
			continue
		}
		if channel != "" && o.Channel != channel {
			continue
		}
		result = append(result, *o)
	}
	return result, int64(len(result)), nil
}

// MarkPaidIfPending 模拟数据库条件更新：仅当订单仍为待支付（status=0）时才更新，
// 返回是否由本次调用完成状态流转
func (m *mockOrderRepo) MarkPaidIfPending(orderNo, tradeNo string, paidAt *time.Time, rawNotify string) (bool, error) {
	o, ok := m.orders[orderNo]
	if !ok {
		return false, nil
	}
	if o.Status != 0 {
		return false, nil
	}
	o.Status = 1
	o.TradeNo = tradeNo
	o.PaidAt = paidAt
	o.RawNotify = rawNotify
	return true, nil
}

// ClaimRefund 模拟数据库条件更新：仅当订单为「已支付」时才置为「退款中」，
// 并保证「判断 + 修改」的原子性（真实实现依赖 UPDATE ... WHERE status=1 的行锁）
func (m *mockOrderRepo) ClaimRefund(orderNo string) (bool, error) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()

	o, ok := m.orders[orderNo]
	if !ok || o.Status != model.StatusPaid {
		return false, nil
	}
	o.Status = model.StatusRefunding
	return true, nil
}

// UpdateRefund 模拟真实实现的语义：条件更新（仅「退款中」可落定）+
// **累加**已退金额，终态由调用方传入。
//
// 累加与「退满才置为已退款」这两点必须在 mock 里如实还原，
// 否则部分退款的回归测试会失去意义 —— 旧实现正是「覆盖金额 + 恒置已退款」，
// 那会导致第二次部分退款抹掉第一次的金额，且剩余额度再也退不了。
func (m *mockOrderRepo) UpdateRefund(orderNo string, refundAmt int64, refundAt time.Time, status int8) (bool, error) {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()

	o, ok := m.orders[orderNo]
	if !ok || o.Status != model.StatusRefunding {
		return false, nil
	}
	o.Status = status
	o.RefundAmt += refundAmt
	o.RefundAt = &refundAt
	return true, nil
}

func (m *mockOrderRepo) ReleaseRefundClaim(orderNo string) error {
	m.claimMu.Lock()
	defer m.claimMu.Unlock()

	if o, ok := m.orders[orderNo]; ok && o.Status == model.StatusRefunding {
		o.Status = model.StatusPaid
	}
	return nil
}

func newTestService(repo *mockOrderRepo) *PaymentService {
	return &PaymentService{orderRepo: repo}
}

const testTenantID uint = 1

func TestCreateOrder(t *testing.T) {
	t.Run("正常创建订单", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		order, err := svc.CreateOrder(testTenantID, "ORDER001", "测试商品", "描述", 100, "wechat", "", "https://notify.url", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if order.ID == 0 {
			t.Fatal("expected order ID to be set")
		}
		if order.OrderNo != "ORDER001" {
			t.Errorf("expected OrderNo ORDER001, got %s", order.OrderNo)
		}
		if order.Amount != 100 {
			t.Errorf("expected Amount 100, got %d", order.Amount)
		}
		if order.Status != 0 {
			t.Errorf("expected Status 0, got %d", order.Status)
		}
		if order.Currency != "CNY" {
			t.Errorf("expected Currency CNY, got %s", order.Currency)
		}
	})

	t.Run("同单号同参数视为幂等重放", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		first, err := svc.CreateOrder(testTenantID, "ORDER001", "商品1", "", 100, "wechat", "", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		second, err := svc.CreateOrder(testTenantID, "ORDER001", "商品1", "", 100, "wechat", "", "", "")
		if err != nil {
			t.Fatalf("同参数重放应复用原单，实际报错: %v", err)
		}
		if first.ID != second.ID {
			t.Error("同单号同参数应返回同一订单")
		}
	})

	t.Run("同单号不同参数必须拒绝而不是返回他人订单", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		// 先用 ORDER001 建一单，再用同一单号提交完全不同的商品/金额/渠道。
		// 旧实现会直接把前一单返回给调用方 —— 调用方拿到别人的订单，
		// 却用新参数去调支付渠道，订单与支付参数彻底错配。
		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品1", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		second, err := svc.CreateOrder(testTenantID, "ORDER001", "商品2", "", 200, "alipay", "", "", "")
		if err == nil {
			t.Fatal("同单号不同参数应报错，而不是返回已存在的订单")
		}
		if second != nil {
			t.Errorf("报错时不应返回订单对象，实际: %+v", second)
		}
		if !common.IsBizError(err) {
			t.Errorf("应返回业务错误(400)，实际: %T", err)
		}
	})

	t.Run("创建失败时返回错误", func(t *testing.T) {
		repo := newMockRepo()
		repo.createFn = func(order *model.PayOrder) error {
			return fmt.Errorf("db error")
		}
		svc := newTestService(repo)

		_, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestCloseOrder(t *testing.T) {
	t.Run("关闭待支付订单", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}
		err := svc.CloseOrder(testTenantID, "ORDER001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		order, _ := svc.GetOrder(testTenantID, "ORDER001")
		if order.Status != 2 {
			t.Errorf("expected status 2, got %d", order.Status)
		}
	})

	t.Run("已支付订单不能关闭", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}
		repo.orders["ORDER001"].Status = 1

		err := svc.CloseOrder(testTenantID, "ORDER001")
		if err == nil {
			t.Fatal("expected error for paid order")
		}
	})

	t.Run("已关闭订单不能再关闭", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}
		repo.orders["ORDER001"].Status = 2

		err := svc.CloseOrder(testTenantID, "ORDER001")
		if err == nil {
			t.Fatal("expected error for already closed order")
		}
	})

	t.Run("不存在的订单返回错误", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		err := svc.CloseOrder(testTenantID, "NOT_EXIST")
		if err == nil {
			t.Fatal("expected error for non-existent order")
		}
	})
}

// TestCloseOrderDoesNotOverwritePaidOrder 竞态回归保护。
//
// 关单流程是「先查状态判断 → 再写回」，而支付回调可能正好插在这两步之间完成
// 状态流转。早前实现用 Save(order) 无条件全字段写入，会用**读时的旧快照**
// 把这一行整个覆盖：status 倒退回已关闭、trade_no 与 paid_at 被清零。
// 后果是用户已付款、订单显示已关闭、交易号丢失，账目无法核对。
//
// 这里用 findHook 把「并发写入落在读与写之间」变成确定性场景。
func TestCloseOrderDoesNotOverwritePaidOrder(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
		t.Fatalf("create order: %v", err)
	}

	// 关单读取订单之后、条件更新之前，支付回调抢先完成流转
	injected := false
	repo.findHook = func() {
		if injected {
			return
		}
		injected = true
		paidAt := time.Now()
		if _, err := repo.MarkPaidIfPending("ORDER001", "WX_TRADE_001", &paidAt, `{"raw":"data"}`); err != nil {
			t.Fatalf("mark paid: %v", err)
		}
	}

	if err := svc.CloseOrder(testTenantID, "ORDER001"); err == nil {
		t.Fatal("订单已被并发支付，关单必须失败，而不是覆盖状态")
	}

	order := repo.orders["ORDER001"]
	if order.Status != model.StatusPaid {
		t.Errorf("已支付订单被覆盖：expected status %d, got %d", model.StatusPaid, order.Status)
	}
	if order.TradeNo != "WX_TRADE_001" {
		t.Errorf("交易号被清空：got %q", order.TradeNo)
	}
	if order.PaidAt == nil {
		t.Error("支付时间被清空")
	}
}

func TestHandleNotify(t *testing.T) {
	t.Run("正常回调更新订单为已支付", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}
		paidAt := time.Now()
		err := svc.HandleNotify("wechat", &PayNotifyResult{
			OrderNo: "ORDER001",
			TradeNo: "WX_TRADE_001",
			Status:  "success",
			// 金额必须与订单一致：严格校验后，缺失金额（0）同样会被拒绝。
			// 本用例此前没设 Amount，是靠旧实现的 fail-open 才通过的。
			Amount:  100,
			PaidAt:  &paidAt,
			RawData: `{"raw":"data"}`,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		order, _ := svc.GetOrder(testTenantID, "ORDER001")
		if order.Status != 1 {
			t.Errorf("expected status 1, got %d", order.Status)
		}
		if order.TradeNo != "WX_TRADE_001" {
			t.Errorf("expected TradeNo WX_TRADE_001, got %s", order.TradeNo)
		}
	})

	t.Run("金额缺失的回调必须被拒绝", func(t *testing.T) {
		// fail-open 回归用例。
		// 旧实现是 `result.Amount > 0 && result.Amount != order.Amount`，
		// 那个 `> 0` 让「金额为 0」直接跳过整个校验 —— 只要回调载荷里
		// 没有金额字段（或解析失败），任意订单都能被标记为已支付。
		repo := newMockRepo()
		svc := newTestService(repo)
		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}

		err := svc.HandleNotify("wechat", &PayNotifyResult{
			OrderNo: "ORDER001",
			TradeNo: "WX_TRADE_001",
			Status:  "success",
		})
		if err == nil {
			t.Fatal("金额缺失的回调必须被拒绝，而不是跳过校验")
		}
		if got := repo.orders["ORDER001"].Status; got != model.StatusPending {
			t.Errorf("订单不应被置为已支付：got status %d", got)
		}
	})

	t.Run("金额不匹配的回调必须被拒绝", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)
		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}

		err := svc.HandleNotify("wechat", &PayNotifyResult{
			OrderNo: "ORDER001",
			Status:  "success",
			Amount:  1, // 只付 1 分
		})
		if err == nil {
			t.Fatal("金额不匹配必须被拒绝")
		}
		if got := repo.orders["ORDER001"].Status; got != model.StatusPending {
			t.Errorf("订单不应被置为已支付：got status %d", got)
		}
	})

	t.Run("回调渠道与订单渠道不一致必须被拒绝", func(t *testing.T) {
		// 订单渠道是 wechat，回调却声称来自 alipay —— 即使订单号相同也不放行。
		// 属于防御纵深：某个渠道的验签万一被绕过，也不能借此跨渠道改单。
		repo := newMockRepo()
		svc := newTestService(repo)
		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}

		err := svc.HandleNotify("alipay", &PayNotifyResult{
			OrderNo: "ORDER001",
			Status:  "success",
			Amount:  100,
		})
		if err == nil {
			t.Fatal("渠道不一致必须被拒绝")
		}
		if got := repo.orders["ORDER001"].Status; got != model.StatusPending {
			t.Errorf("订单不应被置为已支付：got status %d", got)
		}
	})

	t.Run("已支付订单重复回调不报错", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}
		repo.orders["ORDER001"].Status = 1

		err := svc.HandleNotify("wechat", &PayNotifyResult{
			OrderNo: "ORDER001",
			Status:  "success",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil回调返回错误", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		err := svc.HandleNotify("wechat", nil)
		if err == nil {
			t.Fatal("expected error for nil result")
		}
	})

	t.Run("空订单号返回错误", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		err := svc.HandleNotify("wechat", &PayNotifyResult{OrderNo: ""})
		if err == nil {
			t.Fatal("expected error for empty orderNo")
		}
	})

	t.Run("不存在的订单返回错误", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		err := svc.HandleNotify("wechat", &PayNotifyResult{
			OrderNo: "NOT_EXIST",
			Status:  "success",
		})
		if err == nil {
			t.Fatal("expected error for non-existent order")
		}
	})

	t.Run("非success状态不更新订单", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}
		err := svc.HandleNotify("wechat", &PayNotifyResult{
			OrderNo: "ORDER001",
			Status:  "pending",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		order, _ := svc.GetOrder(testTenantID, "ORDER001")
		if order.Status != 0 {
			t.Errorf("expected status unchanged (0), got %d", order.Status)
		}
	})

	t.Run("金额不匹配拒绝回调", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}
		paidAt := time.Now()
		err := svc.HandleNotify("wechat", &PayNotifyResult{
			OrderNo: "ORDER001",
			Status:  "success",
			Amount:  999,
			PaidAt:  &paidAt,
		})
		if err == nil {
			t.Fatal("expected error for amount mismatch")
		}
	})
}

// TestValidateRefund 覆盖退款前的全部纯校验分支。
//
// 这些规则原先是内联在 RefundOrder 里的，抽成 validateRefund 后可直接单测，
// 无需构造支付渠道。校验必须在抢占状态之前完成，否则非法请求会把订单卡在「退款中」。
func TestValidateRefund(t *testing.T) {
	paid := &model.PayOrder{Amount: 1000, Status: model.StatusPaid}
	// 已部分退款 300 的订单：剩余可退 700
	partiallyRefunded := &model.PayOrder{Amount: 1000, Status: model.StatusPaid, RefundAmt: 300}

	cases := []struct {
		name      string
		order     *model.PayOrder
		refundAmt int64
		wantErr   bool
	}{
		{"正常退款", paid, 500, false},
		{"全额退款", paid, 1000, false},
		{"未支付订单", &model.PayOrder{Amount: 1000, Status: model.StatusPending}, 500, true},
		{"已关闭订单", &model.PayOrder{Amount: 1000, Status: model.StatusClosed}, 500, true},
		{"已退款订单", &model.PayOrder{Amount: 1000, Status: model.StatusRefunded}, 500, true},
		{"退款中的订单", &model.PayOrder{Amount: 1000, Status: model.StatusRefunding}, 500, true},
		{"金额为0", paid, 0, true},
		{"金额为负", paid, -100, true},
		{"金额超过订单", paid, 2000, true},
		// 部分退款后按「剩余额度」而非订单总额判断，
		// 否则已退过的部分会被重复退一次
		{"部分退款后可退剩余额度", partiallyRefunded, 700, false},
		{"部分退款后不可超剩余额度", partiallyRefunded, 701, true},
		{"部分退款后不可再退全额", partiallyRefunded, 1000, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRefund(c.order, c.refundAmt)
			if c.wantErr && err == nil {
				t.Fatal("期望返回错误，实际为 nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("期望无错误，实际: %v", err)
			}
			// 业务校验失败必须是 BizError（400），不能退化成系统错误（500）
			if c.wantErr && err != nil && !common.IsBizError(err) {
				t.Errorf("校验失败应为业务错误，实际: %T", err)
			}
		})
	}
}

// TestPartialRefund 部分退款：累加已退金额，未退满时不把订单标为「已退款」。
//
// 这两点分别对应旧实现的两个缺陷（本用例就是它们的回归保护）：
//  1. refund_amt 是覆盖写 —— 第二次部分退款会把第一次的金额抹掉；
//  2. 状态恒置为「已退款」—— 部分退款后剩余额度再也退不了。
func TestPartialRefund(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	var gatewayCalls int
	svc.gatewayRefund = func(order *model.PayOrder, refundNo string, refundAmt int64) error {
		gatewayCalls++
		return nil
	}

	if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 1000, "wechat", "", "", ""); err != nil {
		t.Fatalf("准备订单失败: %v", err)
	}
	repo.orders["ORDER001"].Status = model.StatusPaid

	t.Run("第一次部分退款后订单保持已支付", func(t *testing.T) {
		result, err := svc.RefundOrderWithPayInfo(testTenantID, "ORDER001", "R1", 300)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Error != nil {
			t.Fatalf("退款失败: %v", result.Error)
		}

		order := repo.orders["ORDER001"]
		if order.RefundAmt != 300 {
			t.Errorf("已退金额应为 300，got %d", order.RefundAmt)
		}
		if order.Status != model.StatusPaid {
			t.Errorf("部分退款后应保持已支付（以便继续退剩余），got status %d", order.Status)
		}
	})

	t.Run("第二次部分退款金额累加而非覆盖", func(t *testing.T) {
		if _, err := svc.RefundOrderWithPayInfo(testTenantID, "ORDER001", "R2", 200); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		order := repo.orders["ORDER001"]
		if order.RefundAmt != 500 {
			t.Errorf("已退金额应累加为 500（旧实现会被覆盖成 200），got %d", order.RefundAmt)
		}
		if order.Status != model.StatusPaid {
			t.Errorf("未退满时应仍为已支付，got status %d", order.Status)
		}
	})

	t.Run("退满后转为已退款且不能再退", func(t *testing.T) {
		if _, err := svc.RefundOrderWithPayInfo(testTenantID, "ORDER001", "R3", 500); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		order := repo.orders["ORDER001"]
		if order.RefundAmt != 1000 {
			t.Errorf("已退金额应为 1000，got %d", order.RefundAmt)
		}
		if order.Status != model.StatusRefunded {
			t.Errorf("退满后应为已退款，got status %d", order.Status)
		}

		if _, err := svc.RefundOrderWithPayInfo(testTenantID, "ORDER001", "R4", 1); err == nil {
			t.Error("已退款订单再次退款必须被拒绝")
		}
	})

	t.Run("网关调用次数等于实际退款笔数", func(t *testing.T) {
		// 多一次都意味着重复退款（资金侧不可逆），因此这里必须精确相等
		if gatewayCalls != 3 {
			t.Errorf("网关调用次数应为 3，got %d", gatewayCalls)
		}
	})
}

func TestGetOrder(t *testing.T) {
	t.Run("正常获取订单", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}
		order, err := svc.GetOrder(testTenantID, "ORDER001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if order.OrderNo != "ORDER001" {
			t.Errorf("expected OrderNo ORDER001, got %s", order.OrderNo)
		}
	})

	t.Run("不存在的订单返回错误", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		_, err := svc.GetOrder(testTenantID, "NOT_EXIST")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestGetOrderByID(t *testing.T) {
	t.Run("正常获取订单", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		if _, err := svc.CreateOrder(testTenantID, "ORDER001", "商品", "", 100, "wechat", "", "", ""); err != nil {
			t.Fatalf("准备订单失败: %v", err)
		}
		order, err := svc.GetOrderByID(testTenantID, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if order.ID != 1 {
			t.Errorf("expected ID 1, got %d", order.ID)
		}
	})

	t.Run("不存在的ID返回错误", func(t *testing.T) {
		repo := newMockRepo()
		svc := newTestService(repo)

		_, err := svc.GetOrderByID(testTenantID, 999)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFindList(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	if _, err := svc.CreateOrder(testTenantID, "O1", "商品A", "", 100, "wechat", "", "", ""); err != nil {
		t.Fatalf("准备订单失败: %v", err)
	}
	if _, err := svc.CreateOrder(testTenantID, "O2", "商品B", "", 200, "alipay", "", "", ""); err != nil {
		t.Fatalf("准备订单失败: %v", err)
	}
	if _, err := svc.CreateOrder(testTenantID, "O3", "商品A", "", 300, "wechat", "", "", ""); err != nil {
		t.Fatalf("准备订单失败: %v", err)
	}
	repo.orders["O2"].Status = 1

	t.Run("按标题搜索", func(t *testing.T) {
		list, total, err := svc.FindList(testTenantID, "商品A", -1, "", 1, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 2 {
			t.Errorf("expected total 2, got %d", total)
		}
		if len(list) != 2 {
			t.Errorf("expected 2 items, got %d", len(list))
		}
	})

	t.Run("按状态筛选", func(t *testing.T) {
		list, total, err := svc.FindList(testTenantID, "", 1, "", 1, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 1 {
			t.Errorf("expected total 1, got %d", total)
		}
		if len(list) != 1 {
			t.Errorf("expected 1 item, got %d", len(list))
		}
	})

	t.Run("按渠道筛选", func(t *testing.T) {
		list, total, err := svc.FindList(testTenantID, "", -1, "alipay", 1, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 1 {
			t.Errorf("expected total 1, got %d", total)
		}
		if len(list) != 1 {
			t.Errorf("expected 1 item, got %d", len(list))
		}
	})

	t.Run("无筛选条件返回全部", func(t *testing.T) {
		_, total, err := svc.FindList(testTenantID, "", -1, "", 1, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 3 {
			t.Errorf("expected total 3, got %d", total)
		}
	})
}

func TestConcurrentCreateOrder(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	const goroutines = 50
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := svc.CreateOrder(testTenantID, "SAME_ORDER", "商品", "", 100, "wechat", "", "", "")
			errs <- err
		}()
	}

	for i := 0; i < goroutines; i++ {
		<-errs
	}

	if len(repo.orders) != 1 {
		t.Errorf("expected 1 order (dedup), got %d", len(repo.orders))
	}

	order, _ := svc.GetOrder(testTenantID, "SAME_ORDER")
	if order == nil {
		t.Fatal("order not found")
	}
}

// TestRefundConcurrentOnlyOneReachesGateway 并发退款只能有一次真正打到支付渠道。
//
// 这是 P0 回归测试。修复前的实现是「查状态 → 调网关 → 改状态」，
// 两个并发请求会同时通过状态检查，于是向渠道真实退款两次 —— 钱多退一份。
// 修复后改为「先条件更新抢占退款中，抢到的人才调网关」。
//
// 断言渠道被调用次数 == 1：这是唯一能区分「修复了」与「没修」的判据。
func TestRefundConcurrentOnlyOneReachesGateway(t *testing.T) {
	repo := newMockRepo()
	repo.orders["ORDER_RACE"] = &model.PayOrder{
		BaseModel: common.BaseModel{ID: 1},
		OrderNo:   "ORDER_RACE",
		TenantID:  testTenantID,
		Amount:    10000,
		Channel:   "wechat",
		Status:    model.StatusPaid,
	}
	svc := newTestService(repo)

	var gatewayCalls int32
	svc.gatewayRefund = func(order *model.PayOrder, refundNo string, refundAmt int64) error {
		atomic.AddInt32(&gatewayCalls, 1)
		// 拉长窗口，让并发请求尽可能重叠
		time.Sleep(30 * time.Millisecond)
		return nil
	}

	const goroutines = 8
	var wg sync.WaitGroup
	var successCount int32
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.RefundOrderWithPayInfo(testTenantID, "ORDER_RACE", "REF001", 10000)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&gatewayCalls); got != 1 {
		t.Errorf("支付渠道被调用了 %d 次，期望恰好 1 次（并发重复退款）", got)
	}
	if got := atomic.LoadInt32(&successCount); got != 1 {
		t.Errorf("成功退款 %d 次，期望恰好 1 次", got)
	}
	if got := repo.orders["ORDER_RACE"].Status; got != model.StatusRefunded {
		t.Errorf("订单最终状态应为已退款(%d)，实际 %d", model.StatusRefunded, got)
	}
}

// TestRefundGatewayFailureReleasesClaim 渠道退款失败时必须回滚状态，否则订单永久卡在「退款中」
func TestRefundGatewayFailureReleasesClaim(t *testing.T) {
	repo := newMockRepo()
	repo.orders["ORDER_FAIL"] = &model.PayOrder{
		BaseModel: common.BaseModel{ID: 1},
		OrderNo:   "ORDER_FAIL",
		TenantID:  testTenantID,
		Amount:    5000,
		Channel:   "alipay",
		Status:    model.StatusPaid,
	}
	svc := newTestService(repo)
	svc.gatewayRefund = func(order *model.PayOrder, refundNo string, refundAmt int64) error {
		return fmt.Errorf("渠道超时")
	}

	result, err := svc.RefundOrderWithPayInfo(testTenantID, "ORDER_FAIL", "REF002", 5000)
	if err != nil {
		t.Fatalf("渠道失败应通过 result.Error 返回，而非直接 error: %v", err)
	}
	if result.Error == nil {
		t.Fatal("期望 result.Error 非空")
	}
	if got := repo.orders["ORDER_FAIL"].Status; got != model.StatusPaid {
		t.Errorf("渠道失败后状态应回滚为已支付(%d)，实际 %d", model.StatusPaid, got)
	}
}

// TestRefundRejectsAlreadyRefunded 已退款订单再次退款应被拒绝
func TestRefundRejectsAlreadyRefunded(t *testing.T) {
	repo := newMockRepo()
	repo.orders["ORDER_DONE"] = &model.PayOrder{
		BaseModel: common.BaseModel{ID: 1},
		OrderNo:   "ORDER_DONE",
		TenantID:  testTenantID,
		Amount:    5000,
		Channel:   "wechat",
		Status:    model.StatusRefunded,
	}
	svc := newTestService(repo)
	svc.gatewayRefund = func(order *model.PayOrder, refundNo string, refundAmt int64) error {
		t.Fatal("已退款订单不应再调用支付渠道")
		return nil
	}

	_, err := svc.RefundOrderWithPayInfo(testTenantID, "ORDER_DONE", "REF003", 5000)
	if err == nil {
		t.Fatal("已退款订单应被拒绝")
	}
}

// TestRefundRejectsUnsupportedChannel 未知渠道不应留下卡在「退款中」的订单
func TestRefundRejectsUnsupportedChannel(t *testing.T) {
	repo := newMockRepo()
	repo.orders["ORDER_CH"] = &model.PayOrder{
		BaseModel: common.BaseModel{ID: 1},
		OrderNo:   "ORDER_CH",
		TenantID:  testTenantID,
		Amount:    5000,
		Channel:   "unionpay",
		Status:    model.StatusPaid,
	}
	svc := newTestService(repo)

	if _, err := svc.RefundOrderWithPayInfo(testTenantID, "ORDER_CH", "REF004", 5000); err == nil {
		t.Fatal("不支持的渠道应被拒绝")
	}
	if got := repo.orders["ORDER_CH"].Status; got != model.StatusPaid {
		t.Errorf("不支持渠道时状态不应被改动，实际 %d", got)
	}
}
