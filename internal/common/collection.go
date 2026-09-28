package common

// UniqueNonZeroIDs 返回去重后的非零 ID，保持「首次出现」的顺序；无有效项时返回 nil。
//
// 剔除 0：0 不是合法主键，通常是前端下拉框未选择时的默认值，写进关联表
// 只会留下一条指向不存在记录的脏数据。
//
// 去重：关联表（`sys_role_menu` / `sys_user_role` / `sys_user_post` /
// `pay_member_tag_rel`）的主键都是 (A, B) 复合主键，同一对 ID 出现两次会
// **直接撞主键** → 数据库报错 → 接口 500。而请求体里的重复值完全合法
// （前端多选控件、手写 curl、客户端重试都可能产生 `[5,5]`），
// 把它当成「服务端故障」既误导调用方，也让「保存权限」这种本该幂等的操作
// 变成偶发失败。因此**凡是往关联表写 ID 列表，都先过这个函数**。
//
// 顺序稳定是为了让行为可预测、也便于测试断言 —— 不依赖 map 的遍历顺序。
func UniqueNonZeroIDs(ids []uint) []uint {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		// 统一返回 nil：调用方普遍用 `len(x) == 0` 判断，但 nil 比
		// 空切片更不容易在后续 append 时产生「以为是空其实有底层数组」的误解
		return nil
	}
	return out
}
