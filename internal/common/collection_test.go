package common

import (
	"reflect"
	"testing"
)

// TestUniqueNonZeroIDs 去重 + 剔除 0 + 保持首次出现顺序。
//
// 这个函数守的是「关联表复合主键被重复 ID 撞到」这类缺陷：
// 请求体里的 `[5,5]` 完全合法，但直接逐条 Create 会撞主键 →
// 一个本该幂等的「保存权限」操作变成 500。
func TestUniqueNonZeroIDs(t *testing.T) {
	cases := []struct {
		name string
		in   []uint
		want []uint
	}{
		{"空输入", nil, nil},
		{"空切片", []uint{}, nil},
		{"全为 0", []uint{0, 0}, nil},
		{"去重", []uint{3, 1, 3, 2, 1}, []uint{3, 1, 2}},
		{"剔除 0 并去重", []uint{0, 5, 0, 5, 7}, []uint{5, 7}},
		{"保持首次出现顺序", []uint{9, 4, 9}, []uint{9, 4}},
		{"无重复时原样返回", []uint{1, 2, 3}, []uint{1, 2, 3}},
		{"单个元素", []uint{7}, []uint{7}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := UniqueNonZeroIDs(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("UniqueNonZeroIDs(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestUniqueNonZeroIDsDoesNotMutateInput 不得原地修改入参。
//
// 调用方传进来的往往是请求体里的切片，原地重排会让「请求内容」
// 在服务层内部被悄悄改写 —— 排查时看到的现象与日志里的请求体对不上。
func TestUniqueNonZeroIDsDoesNotMutateInput(t *testing.T) {
	in := []uint{3, 1, 3}
	_ = UniqueNonZeroIDs(in)

	if !reflect.DeepEqual(in, []uint{3, 1, 3}) {
		t.Fatalf("入参被修改: %v", in)
	}
}
