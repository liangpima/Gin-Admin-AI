package service

import (
	"errors"
	"testing"

	"go-admin/internal/common"
	"go-admin/internal/module/member/dto"
	"go-admin/internal/module/member/model"
	"go-admin/internal/module/member/repository"

	"gorm.io/gorm"
)

// 本文件是 system/service/error_semantics_test.go 在 member 模块的对应物（P2-13 / M4·M5）。
//
// member 模块此前有 4 处无条件 `NewNotFoundError`（Update / UpdateTags /
// UpdateLastVisit / 标签 Update）与 2 处原样透传仓储错误（会员 Delete / 等级 Update），
// 方向相反、成因相同：没有区分「记录不存在」与「数据库故障」。
//
// 每条都同时断言两个方向 —— 只测一边的话，把 NotFoundOrErr 改成无条件
// NewNotFoundError（或反过来直接 return err）都能蒙混过关。
//
// 桩统一内嵌接口，只覆盖用例真正会走到的方法：
// 未被覆盖的方法一旦被调用会因 nil 接口 panic，这是有意的 ——
// 它会让「用例其实走到了别的分支」立刻暴露，而不是静默通过。

var errSemanticsDBFailure = errors.New("dial tcp: connection refused")

// assertSystemError 断言不是业务错误（会被 FailWith 转成 500）。
func assertSystemError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("应为系统错误，实际返回 nil")
	}
	if _, ok := common.AsBizError(err); ok {
		t.Errorf("数据库故障不应被包装成业务错误，实际: %v", err)
	}
}

// assertNotFound 断言是 404 业务错误。
func assertNotFound(t *testing.T, err error) {
	t.Helper()
	be, ok := common.AsBizError(err)
	if !ok {
		t.Fatalf("应为业务错误，实际: %T %v", err, err)
	}
	if be.Code != common.CodeNotFound {
		t.Errorf("业务码应为 404，实际 %d（%s）", be.Code, be.Msg)
	}
}

type stubErrSemanticsMemberRepo struct {
	repository.MemberRepository
	findByIDErr error
	deleteErr   error
}

func (s *stubErrSemanticsMemberRepo) FindByID(tenantID, id uint) (*model.Member, error) {
	return nil, s.findByIDErr
}

func (s *stubErrSemanticsMemberRepo) Delete(tenantID, id uint) error { return s.deleteErr }

type stubErrSemanticsTagRepo struct {
	repository.MemberTagRepository
	findByIDErr error
}

func (s *stubErrSemanticsTagRepo) FindByID(tenantID, id uint) (*model.MemberTag, error) {
	return nil, s.findByIDErr
}

type stubErrSemanticsLevelRepo struct {
	repository.MemberLevelRepository
	findByIDErr error
}

func (s *stubErrSemanticsLevelRepo) FindByID(tenantID, id uint) (*model.MemberLevel, error) {
	return nil, s.findByIDErr
}

func TestMemberUpdateErrorSemantics(t *testing.T) {
	t.Run("会员不存在返回 404", func(t *testing.T) {
		svc := &memberService{memberRepo: &stubErrSemanticsMemberRepo{findByIDErr: gorm.ErrRecordNotFound}}
		assertNotFound(t, svc.Update(&dto.UpdateMemberRequest{ID: 9}, 1, 1))
	})

	t.Run("数据库故障不得被说成会员不存在", func(t *testing.T) {
		svc := &memberService{memberRepo: &stubErrSemanticsMemberRepo{findByIDErr: errSemanticsDBFailure}}
		assertSystemError(t, svc.Update(&dto.UpdateMemberRequest{ID: 9}, 1, 1))
	})
}

func TestMemberUpdateTagsErrorSemantics(t *testing.T) {
	t.Run("会员不存在返回 404", func(t *testing.T) {
		svc := &memberService{memberRepo: &stubErrSemanticsMemberRepo{findByIDErr: gorm.ErrRecordNotFound}}
		assertNotFound(t, svc.UpdateTags(1, &dto.UpdateMemberTagsRequest{ID: 9}))
	})

	t.Run("数据库故障不得被说成会员不存在", func(t *testing.T) {
		svc := &memberService{memberRepo: &stubErrSemanticsMemberRepo{findByIDErr: errSemanticsDBFailure}}
		assertSystemError(t, svc.UpdateTags(1, &dto.UpdateMemberTagsRequest{ID: 9}))
	})
}

func TestMemberUpdateLastVisitErrorSemantics(t *testing.T) {
	t.Run("会员不存在返回 404", func(t *testing.T) {
		svc := &memberService{memberRepo: &stubErrSemanticsMemberRepo{findByIDErr: gorm.ErrRecordNotFound}}
		assertNotFound(t, svc.UpdateLastVisit(1, 9))
	})

	t.Run("数据库故障不得被说成会员不存在", func(t *testing.T) {
		svc := &memberService{memberRepo: &stubErrSemanticsMemberRepo{findByIDErr: errSemanticsDBFailure}}
		assertSystemError(t, svc.UpdateLastVisit(1, 9))
	})
}

func TestMemberDeleteErrorSemantics(t *testing.T) {
	t.Run("会员不存在返回 404", func(t *testing.T) {
		svc := &memberService{memberRepo: &stubErrSemanticsMemberRepo{deleteErr: gorm.ErrRecordNotFound}}
		assertNotFound(t, svc.Delete(1, 9))
	})

	t.Run("数据库故障仍走 500", func(t *testing.T) {
		svc := &memberService{memberRepo: &stubErrSemanticsMemberRepo{deleteErr: errSemanticsDBFailure}}
		assertSystemError(t, svc.Delete(1, 9))
	})
}

func TestMemberTagUpdateErrorSemantics(t *testing.T) {
	t.Run("标签不存在返回 404", func(t *testing.T) {
		svc := &memberTagService{tagRepo: &stubErrSemanticsTagRepo{findByIDErr: gorm.ErrRecordNotFound}}
		assertNotFound(t, svc.Update(&dto.UpdateMemberTagRequest{ID: 9}, 1, 1))
	})

	t.Run("数据库故障不得被说成标签不存在", func(t *testing.T) {
		svc := &memberTagService{tagRepo: &stubErrSemanticsTagRepo{findByIDErr: errSemanticsDBFailure}}
		assertSystemError(t, svc.Update(&dto.UpdateMemberTagRequest{ID: 9}, 1, 1))
	})
}

func TestMemberLevelUpdateErrorSemantics(t *testing.T) {
	t.Run("等级不存在返回 404", func(t *testing.T) {
		svc := &memberLevelService{levelRepo: &stubErrSemanticsLevelRepo{findByIDErr: gorm.ErrRecordNotFound}}
		assertNotFound(t, svc.Update(&dto.UpdateMemberLevelRequest{ID: 9}, 1, 1))
	})

	t.Run("数据库故障不得被说成等级不存在", func(t *testing.T) {
		svc := &memberLevelService{levelRepo: &stubErrSemanticsLevelRepo{findByIDErr: errSemanticsDBFailure}}
		assertSystemError(t, svc.Update(&dto.UpdateMemberLevelRequest{ID: 9}, 1, 1))
	})
}
