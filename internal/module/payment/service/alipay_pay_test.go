package service

import (
	"fmt"
	"strings"
	"testing"
)

// TestYuanToFen 校验「元 → 分」的转换不使用浮点。
//
// 用 int64(amt*100) 会因浮点表示误差少 1 分（19.99 → 1998），
// 而回调会用该值与订单金额比对，少 1 分即判为金额不匹配，
// 结果是用户已付款但订单不入账。
func TestYuanToFen(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"0.01", 1},
		{"19.99", 1999}, // 浮点法会算成 1998
		{"0.29", 29},    // 浮点法会算成 28
		{"1.10", 110},
		{"100", 10000},
		{"8", 800},
		{"0.1", 10},
		{"12.05", 1205},
		{"1.999", 199}, // 超出分的部分直接截断
		{" 7.50 ", 750},
		{"", 0},
		{"abc", 0},
		{"-3.50", -350},
	}

	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := yuanToFen(c.in); got != c.want {
				t.Errorf("yuanToFen(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// TestYuanToFenNoFloatError 逐一对齐浮点写法会出错的金额，
// 覆盖 0.01~99.99 全量，确保没有任何一分钱被吞掉。
func TestYuanToFenNoFloatError(t *testing.T) {
	for i := 1; i < 10000; i++ {
		s := fmtAmount(i)
		want := int64(i)

		var f float64
		_, _ = fmt.Sscanf(s, "%f", &f)
		if floatFen := int64(f * 100); floatFen != want {
			// 这个金额正是浮点法会算错的用例，验证新实现算对了
			if got := yuanToFen(s); got != want {
				t.Errorf("yuanToFen(%q) = %d, want %d（浮点法得 %d）", s, got, want, floatFen)
			}
		} else if got := yuanToFen(s); got != want {
			t.Errorf("yuanToFen(%q) = %d, want %d", s, got, want)
		}
	}
}

func fmtAmount(fen int) string {
	return fmtInt(fen/100) + "." + pad2(fen%100)
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + fmtInt(n)
	}
	return fmtInt(n)
}

// TestCheckAlipayResponse 回归保护：HTTP 200 不等于业务成功。
//
// 支付宝网关在业务失败时同样返回 HTTP 200，真正的结果在响应体的 code 字段。
// 早前退款只判断传输层错误，于是「余额不足」「订单不可退」这类失败会被判成
// 退款成功 —— 系统把订单落定为「已退款」，钱却没退出去，且状态机锁死无法重试。
func TestCheckAlipayResponse(t *testing.T) {
	t.Run("成功码 10000 不报错", func(t *testing.T) {
		resp := map[string]interface{}{
			"alipay_trade_refund_response": map[string]interface{}{
				"code":     "10000",
				"msg":      "Success",
				"trade_no": "2024010122001",
			},
		}
		if err := checkAlipayResponse("alipay.trade.refund", resp); err != nil {
			t.Fatalf("成功码不应报错, got: %v", err)
		}
	})

	t.Run("业务失败码必须报错", func(t *testing.T) {
		// 余额不足/系统异常是典型的「HTTP 200 + 业务失败」
		resp := map[string]interface{}{
			"alipay_trade_refund_response": map[string]interface{}{
				"code":     "40004",
				"msg":      "Business Failed",
				"sub_code": "ACQ.SYSTEM_ERROR",
				"sub_msg":  "系统异常，请稍后重试",
			},
		}
		err := checkAlipayResponse("alipay.trade.refund", resp)
		if err == nil {
			t.Fatal("业务失败码必须返回错误，否则退款失败会被记成成功")
		}
		for _, want := range []string{"40004", "ACQ.SYSTEM_ERROR"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("错误信息应包含 %q 便于定位, got: %v", want, err)
			}
		}
	})

	t.Run("缺少响应字段时报错", func(t *testing.T) {
		resp := map[string]interface{}{"unexpected_key": "x"}
		if err := checkAlipayResponse("alipay.trade.refund", resp); err == nil {
			t.Fatal("缺少响应字段应报错，否则会把异常响应当成功")
		}
	})

	t.Run("响应字段类型异常时报错", func(t *testing.T) {
		resp := map[string]interface{}{
			"alipay_trade_refund_response": "not-a-map",
		}
		if err := checkAlipayResponse("alipay.trade.refund", resp); err == nil {
			t.Fatal("响应字段类型异常应报错")
		}
	})

	t.Run("code 缺失或为空时报错", func(t *testing.T) {
		for _, body := range []map[string]interface{}{
			{"msg": "no code"},
			{"code": ""},
			{"code": nil},
		} {
			resp := map[string]interface{}{"alipay_trade_refund_response": body}
			if err := checkAlipayResponse("alipay.trade.refund", resp); err == nil {
				t.Errorf("code 为空必须报错, body=%v", body)
			}
		}
	})

	t.Run("响应字段名按 method 推导", func(t *testing.T) {
		resp := map[string]interface{}{
			"alipay_trade_query_response": map[string]interface{}{
				"code": "10000",
			},
		}
		if err := checkAlipayResponse("alipay.trade.query", resp); err != nil {
			t.Errorf("alipay.trade.query 应查找 alipay_trade_query_response, got: %v", err)
		}
		// 同一份响应用错误的 method 去取，必须取不到而报错
		if err := checkAlipayResponse("alipay.trade.refund", resp); err == nil {
			t.Error("method 不匹配时应报错")
		}
	})
}

// TestFenToYuan 校验「分 → 元」同样不使用浮点。
//
// 与 yuanToFen 是对称问题：float64(amount)/100 无法精确表示 0.1 这类小数，
// 大额金额会出现 12345.67 → 12345.669999... 的误差，格式化后与订单金额
// 差 1 分，支付宝会判「金额不一致」拒单。
func TestFenToYuan(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{1, "0.01"},
		{10, "0.10"},
		{100, "1.00"},
		{1999, "19.99"},
		{29, "0.29"},
		{1205, "12.05"},
		{800, "8.00"},
		{10000, "100.00"},
		{1234567, "12345.67"}, // 大额：浮点法在此量级开始出现误差
		{99999999, "999999.99"},
		{0, "0.00"},
		{-350, "-3.50"},
	}

	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			if got := fenToYuan(c.in); got != c.want {
				t.Errorf("fenToYuan(%d) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestFenYuanRoundTrip 分 → 元 → 分 必须回到原值，一分不差。
//
// 这是资金正确性的底线：任何一分钱的漂移都会导致「已付款但订单不入账」。
func TestFenYuanRoundTrip(t *testing.T) {
	for i := int64(0); i < 200000; i++ {
		s := fenToYuan(i)
		if got := yuanToFen(s); got != i {
			t.Fatalf("往返不一致: %d 分 → %q → %d 分", i, s, got)
		}
	}
}
