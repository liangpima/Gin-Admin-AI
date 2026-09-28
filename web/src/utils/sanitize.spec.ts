// @vitest-environment jsdom

import { describe, expect, it } from 'vitest'
import { sanitizeHtml } from './sanitize'

/**
 * `sanitizeHtml` 的单元测试。
 *
 * 它守的是**存储型 XSS**：`sys_agreement.content` 存的是原始 HTML，
 * 而 wangEditor 会把它解析成真实 DOM 节点 —— `<img src=x onerror=...>`
 * 的 onerror 会真的执行。所以这里的用例必须断言
 * **「危险结构被剥掉」而不是「函数被调用过」**。
 */
describe('sanitizeHtml：剥掉可执行结构', () => {
  it('script 标签（含内容）被移除', () => {
    const out = sanitizeHtml('<p>hi</p><script>alert(1)</script>')
    expect(out).not.toContain('<script')
    expect(out).not.toContain('alert(1)')
    expect(out).toContain('hi')
  })

  it('on* 事件属性被移除（本次修复的注入点）', () => {
    const out = sanitizeHtml('<img src="https://a.com/x.png" onerror="alert(1)">')
    expect(out).not.toMatch(/onerror/i)
    expect(out).not.toContain('alert(1)')
    // 图片本身要保留，否则协议里的配图会消失
    expect(out).toContain('https://a.com/x.png')
  })

  it('各类 on* 属性都被移除，不只是 onerror', () => {
    const out = sanitizeHtml('<p onclick="x()" onmouseover="y()" onload="z()">t</p>')
    expect(out).not.toMatch(/onclick|onmouseover|onload/i)
    expect(out).toContain('t')
  })

  it('javascript: 协议的链接与图片被移除', () => {
    const out = sanitizeHtml('<a href="javascript:alert(1)">x</a>')
    expect(out).not.toMatch(/javascript:/i)
  })

  it('srcdoc 被移除（可在 iframe 内再塞一整份文档）', () => {
    const out = sanitizeHtml('<iframe srcdoc="<script>alert(1)</script>"></iframe>')
    expect(out).not.toMatch(/srcdoc/i)
    expect(out).not.toContain('alert(1)')
  })

  it('style / form / input 等被移除', () => {
    const out = sanitizeHtml(
      '<style>body{display:none}</style><form><input name="x"></form><p>ok</p>',
    )
    expect(out).not.toMatch(/<style|<form|<input/i)
    expect(out).toContain('ok')
  })
})

describe('sanitizeHtml：保留正常富文本', () => {
  it('常见排版标签与属性原样保留', () => {
    const html =
      '<h2>标题</h2><p><strong>粗</strong><em>斜</em><u>下划线</u></p>' +
      '<ul><li>项</li></ul><blockquote>引用</blockquote><pre><code>code</code></pre>'
    const out = sanitizeHtml(html)
    expect(out).toContain('<h2>标题</h2>')
    expect(out).toContain('<strong>粗</strong>')
    expect(out).toContain('<em>斜</em>')
    expect(out).toContain('<blockquote>引用</blockquote>')
    expect(out).toContain('<code>code</code>')
  })

  it('正常链接与图片保留', () => {
    const out = sanitizeHtml(
      '<a href="https://example.com">链接</a><img src="https://example.com/a.png">',
    )
    expect(out).toContain('href="https://example.com"')
    expect(out).toContain('src="https://example.com/a.png"')
  })

  it('编辑器产出的 video 与 data-w-e-type 保留', () => {
    // 编辑器的视频菜单产出这种结构，剥掉会让已存的视频消失
    const out = sanitizeHtml(
      '<div data-w-e-type="video"><video src="https://example.com/v.mp4"></video></div>',
    )
    expect(out).toContain('data-w-e-type="video"')
    expect(out).toContain('https://example.com/v.mp4')
  })
})

describe('sanitizeHtml：空值处理', () => {
  it('null / undefined / 空串统一返回空串（编辑器拿到 null 会初始化报错）', () => {
    expect(sanitizeHtml(null)).toBe('')
    expect(sanitizeHtml(undefined)).toBe('')
    expect(sanitizeHtml('')).toBe('')
  })

  it('幂等：净化两次结果一致（否则 v-model 的 watch 会反复触发）', () => {
    const dirty = '<p onclick="x()">a</p><script>bad()</script>'
    const once = sanitizeHtml(dirty)
    expect(sanitizeHtml(once)).toBe(once)
  })
})
