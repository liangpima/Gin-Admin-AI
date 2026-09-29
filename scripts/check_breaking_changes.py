#!/usr/bin/env python3
"""API 破坏性变更检测（P2-6）。

对比 docs/swagger-baseline.json（基线）与 docs/swagger.json（当前），
识别**破坏性变更**：

  1. 删除了整个 path/method（接口消失）
  2. 删除了响应 schema 的字段（前端消费的字段消失）
  3. 响应字段改了类型（前端按旧类型消费会炸）
  4. 请求参数/字段新增强制必填（旧调用方没传就 400）

发现任何一项且**当前提交信息不含 [BREAKING]** → 退出码 1。

基线的更新方式：故意做破坏性变更时，commit message 带 [BREAKING]，
合并后跑 `make swagger-baseline` 重新固化基线并随该提交入库。

用法：python3 scripts/check_breaking_changes.py <baseline> <current> <commit_msg_file|->
"""
import json
import sys


def load(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def resolve_name(schema):
    """schema 可能是 $ref，解析出定义名；否则返回 None（内联结构不深度比对）。"""
    if not isinstance(schema, dict):
        return None
    ref = schema.get("$ref")
    if ref:
        return ref.rsplit("/", 1)[-1]
    return None


def check_definitions(baseline, current, breaking):
    base_defs = baseline.get("definitions", {})
    cur_defs = current.get("definitions", {})
    for name, bdef in base_defs.items():
        cdef = cur_defs.get(name)
        if cdef is None:
            breaking.append(f"删除了定义 definitions/{name}")
            continue
        bprops = bdef.get("properties", {})
        cprops = cdef.get("properties", {})
        for prop, bspec in bprops.items():
            if prop not in cprops:
                breaking.append(f"definitions/{name} 删除了字段 {prop}")
                continue
            btype = bspec.get("type") or resolve_name(bspec) or "ref?"
            ctype = cprops[prop].get("type") or resolve_name(cprops[prop]) or "ref?"
            if btype != ctype:
                breaking.append(
                    f"definitions/{name}.{prop} 类型变化 {btype} -> {ctype}")
        # 新增强制必填：基线不在 required 里、当前在
        breq = set(bdef.get("required", []))
        creq = set(cdef.get("required", []))
        for prop in sorted(creq - breq):
            if prop in bprops:
                breaking.append(
                    f"definitions/{name}.{prop} 变为必填（旧调用方不传会 400）")


def check_paths(baseline, current, breaking):
    base_paths = baseline.get("paths", {})
    cur_paths = current.get("paths", {})
    for path, ops in base_paths.items():
        cops = cur_paths.get(path)
        if cops is None:
            breaking.append(f"删除了整个接口 path {path}")
            continue
        for method, op in ops.items():
            if method not in cops:
                breaking.append(f"删除了接口 {method.upper()} {path}")
                continue
            # 请求体/参数的必填新增（参数级）
            bparams = {p.get("name"): p for p in ops[method].get("parameters", [])
                       if p.get("name")}
            cparams = {p.get("name"): p for p in cops[method].get("parameters", [])
                       if p.get("name")}
            for pname, bparam in bparams.items():
                cparam = cparams.get(pname)
                if cparam is None:
                    breaking.append(
                        f"{method.upper()} {path} 删除了参数 {pname}")
                elif not bparam.get("required") and cparam.get("required"):
                    breaking.append(
                        f"{method.upper()} {path} 参数 {pname} 变为必填")


def main():
    if len(sys.argv) != 4:
        print(__doc__)
        sys.exit(2)
    baseline_path, current_path, commit_msg_path = sys.argv[1:4]

    baseline = load(baseline_path)
    current = load(current_path)

    breaking = []
    check_definitions(baseline, current, breaking)
    check_paths(baseline, current, breaking)

    if not breaking:
        print("[breaking-check] 无破坏性变更")
        return

    commit_msg = ""
    if commit_msg_path != "-":
        with open(commit_msg_path, encoding="utf-8", errors="ignore") as f:
            commit_msg = f.read()
    else:
        commit_msg = sys.stdin.read()

    if "[BREAKING]" in commit_msg:
        print(f"[breaking-check] 检测到 {len(breaking)} 处破坏性变更，"
              "提交已带 [BREAKING] 标注 —— 放行（请确认已通知所有消费方）：")
        for b in breaking:
            print("  -", b)
        return

    print(f"[breaking-check] 检测到 {len(breaking)} 处破坏性变更"
          "（删除接口/字段、类型变化、必填新增），且提交未带 [BREAKING] 标注：")
    for b in breaking:
        print("  -", b)
    print("\n处理方式二选一：")
    print("  1. 恢复兼容（保留旧字段一个版本周期）")
    print("  2. 确属必要的破坏：amend 提交信息加 [BREAKING] 标注，"
          "合并后跑 make swagger-baseline 固化新基线")
    sys.exit(1)


if __name__ == "__main__":
    main()
