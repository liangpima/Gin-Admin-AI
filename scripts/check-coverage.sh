#!/usr/bin/env bash
#
# 包均覆盖率门槛。
#
# 口径：`go test -cover ./...` 输出里**每个包的百分比取算术平均**
# （与 docs/review-fix-plan.md 一致）。用算术平均而不是「按语句总数加权」，
# 是因为加权口径下小包（pkg/task 这类十几条语句的）补测试几乎没有体现，
# 而它们恰恰是最容易补到 100%、性价比最高的部分。
#
# 为什么需要这个门槛：CI 此前只跑 `go test ./...`，不关心覆盖率。结果是
# 覆盖率只会在无人察觉的情况下倒退 —— 新增生产代码不补测试、顺手删掉某个
# 用例，都不会有任何信号。它防的是「静默倒退」，不是「数字不够高」。
#
# 为什么门槛不设 100%：覆盖率是「哪里没测到」的探照灯，不是目标函数。
# 本项目已明确划出边界 —— 云存储后端（aliyun/tencent/minio）与真实出网的
# 网关方法（Prepay/Refund/QueryTrade）不进单测，硬凑 100% 只能靠 mock 掉
# 整个网络层，那样测的是 mock 而不是代码。理由详见
# docs/review-fix-plan.md 的「覆盖率基线与边界」。
#
# 阈值怎么定：取「当前实测值 - 1.5」左右。留缓冲是因为 CI 跑在
# ubuntu-latest、本地跑在 Windows，个别涉及文件路径与换行的用例覆盖率
# 可能有一两个点的差异，卡在实测值上会让 CI 随机变红。
# **调整阈值时必须同步更新 docs/review-fix-plan.md 里的实测值**，
# 否则阈值和文档会各说各话。
#
# 用法：
#   bash scripts/check-coverage.sh              # 本地
#   bash scripts/check-coverage.sh -count=1     # CI：关掉测试结果缓存
#   COVERAGE_THRESHOLD=80 bash scripts/check-coverage.sh   # 临时抬高

set -euo pipefail

THRESHOLD="${COVERAGE_THRESHOLD:-74.0}"

echo "运行：go test -cover $* ./..."
if ! output=$(go test -cover "$@" ./... 2>&1); then
  # 退出码非 0 有两种成因，必须区分开，否则会把工具链问题误报成测试失败：
  #   a) 测试真的失败 —— 输出里有 FAIL / --- FAIL，必须中止
  #   b) go 工具链缺少 covdata 工具 —— 只有「没有测试文件的包」会触发
  #      （有测试的包照常输出覆盖率行），数据本身是完整的，可以继续。
  #      成因通常是 GOTOOLCHAIN 切换到了不完整的 toolchain 模块缓存，
  #      或本地 Go 发行版缺工具；根治办法是装一个完整的 Go 发行版。
  if printf '%s\n' "$output" | grep -qE '^(FAIL|--- FAIL)'; then
    printf '%s\n' "$output"
    echo
    echo "❌ 测试未通过，覆盖率门槛检查中止。" >&2
    exit 1
  fi
  if printf '%s\n' "$output" | grep -q 'no such tool "covdata"'; then
    echo "⚠️  go 工具链缺少 covdata 工具（只影响没有测试文件的包），继续解析覆盖率。" >&2
  else
    printf '%s\n' "$output"
    echo
    echo "❌ go test 执行失败，覆盖率门槛检查中止。" >&2
    exit 1
  fi
fi

# 只取本仓库自身的包。输出行形如：
#   ok  	go-admin/internal/common	(cached)	coverage: 78.6% of statements
# 另需排除 web/node_modules 下被 go 工具链顺带扫到的第三方包。
percs=$(
  printf '%s\n' "$output" \
    | grep -E '^ok[[:space:]]+go-admin/[^[:space:]]+' \
    | grep -v 'web/node_modules' \
    | grep -oE 'coverage: [0-9.]+%' \
    | grep -oE '[0-9.]+' \
    || true
)

if [ -z "$percs" ]; then
  echo "❌ 未解析到任何覆盖率数据 —— go test 的输出格式变了？" >&2
  echo "   本脚本依赖 'coverage: NN.N% of statements' 这一行。" >&2
  exit 1
fi

read -r avg count <<<"$(printf '%s\n' "$percs" | awk '{s+=$1; n++} END {printf "%.2f %d", s/n, n}')"

echo "包均覆盖率：${avg}%（${count} 个包）"
echo "门槛：      ${THRESHOLD}%"

if awk -v a="$avg" -v t="$THRESHOLD" 'BEGIN { exit (a < t) }'; then
  echo "✅ 覆盖率门槛通过。"
else
  echo "❌ 包均覆盖率 ${avg}% 低于门槛 ${THRESHOLD}%。" >&2
  echo "   新增生产代码请同时补测试。若确需下调门槛，请在" >&2
  echo "   docs/review-fix-plan.md 里写清原因，而不是只改这个数字。" >&2
  exit 1
fi
