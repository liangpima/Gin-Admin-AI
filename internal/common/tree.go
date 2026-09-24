package common

// BuildTree 把扁平列表按「父指针」组装成树，时间复杂度 O(n)。
//
// 为什么需要它：菜单与部门原本各自实现了一份「递归扫描整个切片找子节点」的
// 树构建，每个节点都要重新遍历一遍全表，整体是 O(n²)。
// 这两张表当前只有几百行，平方级开销可以忽略；但它是随数据量放大的，
// 而每次菜单/部门列表请求都会走一遍 —— 没有理由留着。
//
// 关于「环」：如果数据里出现互相引用的父子关系，递归会无限展开。
// 这里不额外做访问标记，而是依赖调用方从根（rootID）出发这一点：
// 环上的节点其祖先链必定不包含 rootID（否则它就不在环上），
// 因此从根出发永远走不到环里，递归自然不会进入。
// 换句话说，**环只会让那批节点不可达（从界面上消失），不会导致爆栈**。
// 该性质由 TestBuildTreeIgnoresCycle 与 TestBuildTreeSelfReference 固定住。
//
// 类型参数 T 是节点类型（如 model.SysMenu），三个访问器负责解耦字段名差异，
// 避免为菜单和部门各写一份几乎相同的代码。
func BuildTree[T any](
	nodes []T,
	rootID uint,
	idOf func(T) uint,
	parentOf func(T) uint,
	setChildren func(*T, []T),
) []T {
	byParent := make(map[uint][]T, len(nodes))
	for _, n := range nodes {
		pid := parentOf(n)
		byParent[pid] = append(byParent[pid], n)
	}

	var build func(pid uint) []T
	build = func(pid uint) []T {
		children := byParent[pid]
		// 与原实现保持一致：返回空切片而非 nil，
		// 前端 JSON 序列化后是 []，不需要额外判空。
		out := make([]T, 0, len(children))
		for _, c := range children {
			node := c // 先复制再挂子节点：直接改 c 会写进 byParent 的元素，
			// 同一节点被多处引用时会互相串味
			setChildren(&node, build(idOf(node)))
			out = append(out, node)
		}
		return out
	}

	return build(rootID)
}

// BuildTreeForest 与 BuildTree 相同，但把「父节点不在本次结果集中」的节点
// 也当作根节点返回，避免整棵子树从界面上消失。
//
// 为什么需要它：BuildTree 从 rootID（通常是 0）出发，只认「父指针恰好等于
// rootID」的节点为根。按租户过滤后的部门列表里，**合法地**会出现父节点不在
// 结果集中的节点：
//   - 部门被改造为租户内数据后，历史数据里的子部门归属某租户，而它的父部门
//     仍留在平台级（tenant_id=0）—— 租户账号查自己的部门时，父节点不在集合里
//   - 迁移回填只覆盖了能确定归属的部门，父链上仍可能有未回填的节点
//
// 这种情况下 BuildTree 会返回**空树**（没有任何节点的父指针是 0），
// 表现为「部门管理页一片空白」，而数据其实完好。把这类节点提升为根，
// 信息量没有任何损失（层级关系该保留的仍保留），只是多出几个顶层节点。
//
// 环的处理与 BuildTree 一致：环上的节点父指针都在集合内，因此不会成为根，
// 依然只是不可达而不会爆栈。
func BuildTreeForest[T any](
	nodes []T,
	rootID uint,
	idOf func(T) uint,
	parentOf func(T) uint,
	setChildren func(*T, []T),
) []T {
	present := make(map[uint]struct{}, len(nodes))
	for _, n := range nodes {
		present[idOf(n)] = struct{}{}
	}

	byParent := make(map[uint][]T, len(nodes))
	roots := make([]T, 0)
	for _, n := range nodes {
		pid := parentOf(n)
		if pid == rootID {
			roots = append(roots, n)
			continue
		}
		if _, ok := present[pid]; !ok {
			roots = append(roots, n)
			continue
		}
		byParent[pid] = append(byParent[pid], n)
	}

	var build func(pid uint) []T
	build = func(pid uint) []T {
		children := byParent[pid]
		out := make([]T, 0, len(children))
		for _, c := range children {
			node := c
			setChildren(&node, build(idOf(node)))
			out = append(out, node)
		}
		return out
	}

	out := make([]T, 0, len(roots))
	for _, r := range roots {
		node := r
		setChildren(&node, build(idOf(node)))
		out = append(out, node)
	}
	return out
}
