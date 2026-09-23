package common

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// BizError 业务错误：由调用方的输入或业务前置条件不满足导致，
// 与「数据库挂了」「签名失败」这类系统错误有本质区别。
//
// 背景：此前所有 Controller 把 service 返回的错误一律映射为 500，
// 于是「用户名已存在」「订单已关闭」这类本该是 400 的提示也变成了 500。
// 结果是前端只能靠文案区分错误类型，网关和监控也无法按状态码告警。
//
// 用法：service 层用 NewBizError / NewNotFoundError 显式标记，
// Controller 统一用 common.FailWith(c, err) 输出。
type BizError struct {
	Code int
	Msg  string
}

func (e *BizError) Error() string { return e.Msg }

// NewBizError 业务校验失败或不满足前置条件，对应 400
func NewBizError(msg string) error {
	return &BizError{Code: CodeBadRequest, Msg: msg}
}

// NewBizErrorf 带格式化的业务错误，对应 400
func NewBizErrorf(format string, args ...interface{}) error {
	return &BizError{Code: CodeBadRequest, Msg: fmt.Sprintf(format, args...)}
}

// NewBizErrorWithCode 业务失败但需要指定业务码（如 CodeFileTooLarge）。
//
// 为什么不直接用 NewBizError：不同业务码前端处理方式不同（文件过大要提示换文件，
// 参数错误只需弹一下），统一成 400 会让前端只能靠文案区分。
func NewBizErrorWithCode(code int, msg string) error {
	return &BizError{Code: code, Msg: msg}
}

// NewNotFoundError 操作的目标资源不存在，对应 404。
// 与 400 的区别：400 是「请求本身有问题」，404 是「请求没问题但引用了不存在的东西」。
func NewNotFoundError(msg string) error {
	return &BizError{Code: CodeNotFound, Msg: msg}
}

// NewUnauthorizedError 认证类业务失败，对应 401。
//
// 与 NewBizError 的区别在状态码语义：refresh token 失效这类问题，前端需要按
// 「登录态已失效」处理（清会话并跳登录页），而不是当成普通参数错误弹提示。
// 注意**登录接口本身不要用它**：登录失败返回 401 会让前端把它当作"被踢出"，
// 跳转登录页并清空表单，用户看不到「用户名或密码错误」的提示。
func NewUnauthorizedError(msg string) error {
	return &BizError{Code: CodeUnauthorized, Msg: msg}
}

// ErrDuplicateKey 唯一约束冲突的哨兵错误。
//
// 由 Repository 在捕获到数据库唯一键冲突（MySQL 1062）时包装返回，
// Service 用 errors.Is 判定后转成业务错误（400）：
//
//	if err := s.userRepo.Create(user); err != nil {
//	    if errors.Is(err, common.ErrDuplicateKey) {
//	        return common.NewBizError("用户名已存在")
//	    }
//	    return err
//	}
//
// 为什么不能只靠写入前的 Count 校验：Count 与 INSERT 之间存在时间窗口，
// 并发下两个请求会同时通过校验，随后必有一个撞上唯一索引。
// 约束才是唯一性的最终权威，前置校验只是为了让常见情形有友好提示。
var ErrDuplicateKey = errors.New("duplicate key")

// NotFoundOrErr 把「记录不存在」归一为 404 业务错误，其余错误原样返回。
//
// GORM 查询不到记录时返回的是 gorm.ErrRecordNotFound，它本身看不出是什么资源没找到，
// 直接透出会变成 500（且对外暴露的是空泛的内部错误）。各 Service 的单条查询应统一
// 用这里转成带语义的 404，Controller 侧只需 common.FailWith 即可。
func NotFoundOrErr(err error, msg string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NewNotFoundError(msg)
	}
	return err
}

// AsBizError 取出错误链上的业务错误；ok 为 false 表示这是系统错误
func AsBizError(err error) (*BizError, bool) {
	var be *BizError
	if err == nil {
		return nil, false
	}
	if !errors.As(err, &be) {
		return nil, false
	}
	return be, true
}

// IsBizError 判断是否为业务错误（供需要区分处理的地方使用）
func IsBizError(err error) bool {
	_, ok := AsBizError(err)
	return ok
}
