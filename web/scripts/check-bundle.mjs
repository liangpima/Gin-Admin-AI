#!/usr/bin/env node
/**
 * 产物体积门禁（P3-A5）。
 *
 * 为什么需要它：本次问题的本质是「产物悄悄涨到 1.27 MB 而无人发现」——
 * vite 确实给了 chunk > 500 kB 的警告，但**警告不是门禁**：它不阻断、没人看，
 * 于是三个月后原样复发。这和 P2-2 那个「golangci-lint 因版本不对根本没在检查
 * 代码、continue-on-error 又让它连失败都不显眼」是同一类失效。
 *
 * 检查三类：
 *   1. 首屏 JS 的 gzip 合计 —— 用户真正要下载的东西
 *   2. 首屏 CSS 的 gzip 合计
 *   3. 单个 chunk 的原始体积（防某条懒加载路由悄悄变成巨石）
 *
 * 用法：node scripts/check-bundle.mjs        （需先 vite build）
 * 退出码：0 通过；1 超阈值；2 找不到产物
 */
import { readFileSync, readdirSync } from 'node:fs'
import { gzipSync } from 'node:zlib'
import { join } from 'node:path'

const DIST = 'dist'
const ASSETS = join(DIST, 'assets')

// ── 阈值 ────────────────────────────────────────────────────────
// 首屏（index.html 直接引用的资源）。当前实测：JS 137.9 kB / CSS 13.1 kB。
const FIRST_PAINT_JS_GZIP_KB = 150
const FIRST_PAINT_CSS_GZIP_KB = 60

// 单个 chunk 原始体积上限。
const CHUNK_RAW_KB = 500

// 例外：第三方库的体积由上游决定，压不下来。给它一个**显式**上限，
// 而不是把它排除在检查之外 —— 排除掉它就等于允许它无限增长。
// 这里的名字来自 vite.config.ts 的 manualChunks（固定名，非哈希名）。
const CHUNK_ALLOWLIST = {
  'vendor-wangeditor': {
    limitKb: 900,
    why: 'wangEditor 富文本库，体积由上游决定；已异步化，只在该页加载',
  },
}

const kb = (bytes) => bytes / 1024
const fmt = (bytes) => `${kb(bytes).toFixed(1)} kB`

let html
try {
  html = readFileSync(join(DIST, 'index.html'), 'utf8')
} catch {
  console.error(`✗ 找不到 ${DIST}/index.html —— 请先跑 vite build`)
  process.exit(2)
}

/** index.html 里直接引用的资源 = 首屏必须下载的 */
const firstPaint = [
  ...new Set([...html.matchAll(/assets\/[a-zA-Z0-9_.-]+\.(?:js|css)/g)].map((m) => m[0])),
]

if (firstPaint.length === 0) {
  console.error('✗ index.html 里没解析出任何 assets 引用，脚本可能已失效')
  process.exit(2)
}

const problems = []

console.log('首屏资源（index.html 直接引用）：')
let fpJs = 0
let fpCss = 0
for (const rel of firstPaint) {
  const buf = readFileSync(join(DIST, rel))
  const gz = gzipSync(buf).length
  console.log(
    `  ${rel.padEnd(40)} 原始 ${fmt(buf.length).padStart(10)}  gzip ${fmt(gz).padStart(10)}`,
  )
  if (rel.endsWith('.js')) fpJs += gz
  else fpCss += gz
}

console.log('\n首屏合计：')
console.log(`  JS  gzip ${fmt(fpJs)}   阈值 ${FIRST_PAINT_JS_GZIP_KB} kB`)
console.log(`  CSS gzip ${fmt(fpCss)}   阈值 ${FIRST_PAINT_CSS_GZIP_KB} kB`)
if (kb(fpJs) > FIRST_PAINT_JS_GZIP_KB) {
  problems.push(`首屏 JS gzip ${fmt(fpJs)} 超过 ${FIRST_PAINT_JS_GZIP_KB} kB`)
}
if (kb(fpCss) > FIRST_PAINT_CSS_GZIP_KB) {
  problems.push(`首屏 CSS gzip ${fmt(fpCss)} 超过 ${FIRST_PAINT_CSS_GZIP_KB} kB`)
}

// ── 单个 chunk ──────────────────────────────────────────────────
console.log(
  `\n单个 chunk（上限 ${CHUNK_RAW_KB} kB，另设 ${Object.keys(CHUNK_ALLOWLIST).length} 项例外）：`,
)
const chunks = readdirSync(ASSETS)
  .filter((f) => f.endsWith('.js'))
  .map((f) => ({ name: f, bytes: readFileSync(join(ASSETS, f)).length }))
  .sort((a, b) => b.bytes - a.bytes)

for (const c of chunks) {
  const base = c.name.replace(/-[A-Za-z0-9_-]{8}\.js$/, '')
  const allow = CHUNK_ALLOWLIST[base]
  const limit = allow ? allow.limitKb : CHUNK_RAW_KB
  const over = kb(c.bytes) > limit
  if (over) {
    problems.push(
      `chunk ${base} ${fmt(c.bytes)} 超过 ${limit} kB${allow ? `（例外项：${allow.why}）` : ''}`,
    )
  }
  // 只打印前 6 大的 + 任何超标的，避免刷屏
  const idx = chunks.indexOf(c)
  if (idx < 6 || over || allow) {
    console.log(
      `  ${c.name.padEnd(40)} ${fmt(c.bytes).padStart(10)}` +
        (allow ? `   [例外 ≤ ${allow.limitKb} kB]` : '') +
        (over ? '   ✗ 超标' : ''),
    )
  }
}
if (chunks.length > 6) console.log(`  …另有 ${chunks.length - 6} 个更小的 chunk`)

// ── 结论 ────────────────────────────────────────────────────────
if (problems.length) {
  console.error(`\n✗ 产物体积门禁未通过（${problems.length} 项）：`)
  for (const p of problems) console.error(`   · ${p}`)
  console.error(
    '\n处理方式：先想办法降下来（异步化 / 按需引入 / 拆 chunk）。' +
      '\n确实降不下来的第三方大包，往 CHUNK_ALLOWLIST 里加一条**带理由和上限**的例外，' +
      '\n不要直接删掉检查、也不要调大 chunkSizeWarningLimit 了事。',
  )
  process.exit(1)
}

console.log('\n✓ 产物体积门禁通过')
