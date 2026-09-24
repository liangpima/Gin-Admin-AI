package common

import "testing"

// treeNode 是测试用的最小节点类型。
type treeNode struct {
	ID       uint
	ParentID uint
	Children []treeNode
}

func buildTestTree(nodes []treeNode, rootID uint) []treeNode {
	return BuildTree(nodes, rootID,
		func(n treeNode) uint { return n.ID },
		func(n treeNode) uint { return n.ParentID },
		func(n *treeNode, children []treeNode) { n.Children = children },
	)
}

func TestBuildTreeBasic(t *testing.T) {
	nodes := []treeNode{
		{ID: 1, ParentID: 0},
		{ID: 2, ParentID: 1},
		{ID: 3, ParentID: 1},
		{ID: 4, ParentID: 2},
		{ID: 5, ParentID: 0},
	}

	tree := buildTestTree(nodes, 0)

	if len(tree) != 2 {
		t.Fatalf("根节点数应为 2，got %d", len(tree))
	}
	if tree[0].ID != 1 || tree[1].ID != 5 {
		t.Fatalf("根节点顺序应保持输入顺序，got %d,%d", tree[0].ID, tree[1].ID)
	}
	if len(tree[0].Children) != 2 {
		t.Fatalf("节点1 应有 2 个子节点，got %d", len(tree[0].Children))
	}
	if len(tree[0].Children[0].Children) != 1 {
		t.Fatalf("节点2 应有 1 个子节点，got %d", len(tree[0].Children[0].Children))
	}
	if tree[0].Children[0].Children[0].ID != 4 {
		t.Errorf("节点2 的子节点应为 4，got %d", tree[0].Children[0].Children[0].ID)
	}
}

func TestBuildTreeEmptyReturnsEmptySlice(t *testing.T) {
	// 空结果必须是空切片而不是 nil，前端拿到的 JSON 要是 [] 而不是 null
	tree := buildTestTree(nil, 0)
	if tree == nil {
		t.Fatal("应为非 nil 的空切片")
	}
	if len(tree) != 0 {
		t.Fatalf("应为空，got %d", len(tree))
	}
}

// TestBuildTreeIgnoresCycle 是本函数最关键的回归点。
//
// 数据里若存在互指父子（1→2→1），两种实现的表现不同：
//   - 朴素递归（遍历全表找 children）：同样不会爆栈，因为环不可达；
//   - 如果在 build 里**不做可达性限制**（例如直接从任意节点起构建），就会无限递归。
//
// 本用例把「环不会导致爆栈，只是让那批节点不可达」这一行为固定下来 ——
// 一旦将来有人改成「遍历所有节点建树」，这里会以栈溢出/超时的方式失败。
func TestBuildTreeIgnoresCycle(t *testing.T) {
	nodes := []treeNode{
		{ID: 1, ParentID: 0},
		{ID: 2, ParentID: 1}, // 正常子树
		{ID: 8, ParentID: 9}, // 环：8 → 9 → 8
		{ID: 9, ParentID: 8},
	}

	tree := buildTestTree(nodes, 0)

	if len(tree) != 1 || tree[0].ID != 1 {
		t.Fatalf("只有从根可达的节点应出现在树里，got %+v", tree)
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].ID != 2 {
		t.Fatalf("节点1 下应只有节点2，got %+v", tree[0].Children)
	}
}

// TestBuildTreeSelfReference 自引用（parent_id = 自己的 id）同样不可达。
func TestBuildTreeSelfReference(t *testing.T) {
	nodes := []treeNode{
		{ID: 1, ParentID: 0},
		{ID: 7, ParentID: 7},
	}

	tree := buildTestTree(nodes, 0)

	if len(tree) != 1 || tree[0].ID != 1 {
		t.Fatalf("自引用节点不应出现在树里，got %+v", tree)
	}
}

// TestBuildTreeOrphanDropped 父节点不存在的节点会「消失」。
//
// 这不是要修的行为，而是要在测试里写明它 —— 正因为会静默消失，
// Service 层才必须在创建/修改时校验 ParentID 是否存在（见 menu/dept service）。
func TestBuildTreeOrphanDropped(t *testing.T) {
	nodes := []treeNode{
		{ID: 1, ParentID: 0},
		{ID: 5, ParentID: 999}, // 父节点不存在
	}

	tree := buildTestTree(nodes, 0)

	if len(tree) != 1 {
		t.Fatalf("孤儿节点不应出现在树里，got %d 个根节点", len(tree))
	}
}

// ---- BuildTreeForest：父节点缺失时提升为根（P1-1）----

// TestBuildTreeForestPromotesOrphans 父节点不在结果集里的节点必须仍然可见。
//
// 这是部门改为租户内数据后的必需行为：历史数据的父部门可能仍留在平台级
// （tenant_id=0），租户账号查自己的部门时父节点被过滤掉了。
// 若沿用 BuildTree，返回的会是**空树** —— 部门管理页一片空白，数据却完好。
func TestBuildTreeForestPromotesOrphans(t *testing.T) {
	type node struct {
		ID       uint
		ParentID uint
		Children []node
	}
	idOf := func(n node) uint { return n.ID }
	parentOf := func(n node) uint { return n.ParentID }
	setChildren := func(n *node, c []node) { n.Children = c }

	// 只加载了 id=2、3 两个部门，它们的父节点 1 不在结果集里（被租户过滤掉）
	nodes := []node{{ID: 2, ParentID: 1}, {ID: 3, ParentID: 1}}

	got := BuildTreeForest(nodes, 0, idOf, parentOf, setChildren)
	if len(got) != 2 {
		t.Fatalf("父节点缺失的节点应被提升为根，期望 2 个顶层节点，实际 %d", len(got))
	}

	// 对照：BuildTree 会返回空树，这正是要避免的
	if plain := BuildTree(nodes, 0, idOf, parentOf, setChildren); len(plain) != 0 {
		t.Errorf("前置条件不成立：BuildTree 本应返回空树，实际 %d 个", len(plain))
	}
}

// TestBuildTreeForestKeepsNesting 正常层级关系不受影响。
func TestBuildTreeForestKeepsNesting(t *testing.T) {
	type node struct {
		ID       uint
		ParentID uint
		Children []node
	}
	idOf := func(n node) uint { return n.ID }
	parentOf := func(n node) uint { return n.ParentID }
	setChildren := func(n *node, c []node) { n.Children = c }

	// 1(根) → 2 → 3
	nodes := []node{{ID: 1}, {ID: 2, ParentID: 1}, {ID: 3, ParentID: 2}}

	got := BuildTreeForest(nodes, 0, idOf, parentOf, setChildren)
	if len(got) != 1 {
		t.Fatalf("应只有 1 个顶层节点，实际 %d", len(got))
	}
	if len(got[0].Children) != 1 || got[0].Children[0].ID != 2 {
		t.Fatalf("层级结构不对: %+v", got[0].Children)
	}
	if len(got[0].Children[0].Children) != 1 || got[0].Children[0].Children[0].ID != 3 {
		t.Errorf("第三层不对: %+v", got[0].Children[0].Children)
	}
}

// TestBuildTreeForestStillIgnoresCycle 环仍然只是不可达，不会爆栈。
//
// 「提升为根」的规则不能把环上的节点也提升成根 —— 否则递归会绕进环里。
// 环上的节点父指针都在集合内，因此不会命中提升分支。
func TestBuildTreeForestStillIgnoresCycle(t *testing.T) {
	type node struct {
		ID       uint
		ParentID uint
		Children []node
	}
	idOf := func(n node) uint { return n.ID }
	parentOf := func(n node) uint { return n.ParentID }
	setChildren := func(n *node, c []node) { n.Children = c }

	// 1 是正常根；4 ↔ 5 互相引用成环；6 自引用
	nodes := []node{{ID: 1}, {ID: 4, ParentID: 5}, {ID: 5, ParentID: 4}, {ID: 6, ParentID: 6}}

	got := BuildTreeForest(nodes, 0, idOf, parentOf, setChildren)
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("只有 id=1 应可达（环上节点与自引用节点都不可达），实际 %+v", got)
	}
}
