import DOMPurify from 'dompurify'

/**
 * 富文本 HTML 的净化策略。
 *
 * 为什么必须有：`sys_agreement.content` 存的是**原始 HTML**，而它会被
 * 灌进 wangEditor 的 `contenteditable`。编辑器把 HTML 解析成真实 DOM 节点，
 * 于是 `<img src=x onerror=...>` 的 onerror 会真的执行 ——
 * 只要有人能写协议内容（任何持有 agreement 写权限的租户管理员），
 * 就能在**其他管理员**打开编辑页时执行任意脚本（存储型 XSS）。
 *
 * ⚠️ 不要用「前端没有 v-html 所以不会 XSS」来推断这里安全：
 * contenteditable 编辑器本身就是 HTML 注入点，`v-html` 只是其中一种形态。
 *
 * 策略选择：用 DOMPurify 的**默认白名单**（它覆盖了编辑器产出的全部常规标签，
 * 且默认就会剥掉 `<script>`、`on*` 事件属性、`javascript:` 协议），
 * 只在默认基础上**额外禁掉**几类富文本用不到、但能造成界面伪装或注入的东西。
 * 不自己写正则白名单 —— 手写 HTML sanitizer 的漏网方式远超想象，
 * 而它给出的「已净化」错觉比不净化更危险。
 */
const PURIFY_CONFIG = {
  // 默认白名单里包含这些标签，但富文本协议内容不需要它们：
  //   style  → 可用 `body{display:none}` 之类做界面伪装/钓鱼
  //   form/input/button/textarea/select/option → 编辑器无法产出，属于注入面
  //   base/link/meta → 会改变整个文档的解析基准与资源加载
  FORBID_TAGS: [
    'style',
    'form',
    'input',
    'button',
    'textarea',
    'select',
    'option',
    'base',
    'link',
    'meta',
  ],
  // srcdoc 可以在 iframe 里再塞一整份文档，是绕过白名单的经典入口
  // （iframe 本身保留：编辑器的视频菜单会用它嵌第三方视频）
  FORBID_ATTR: ['srcdoc'],
  // 默认即 false，显式写出来表明这是有意的策略而非遗漏
  ALLOW_UNKNOWN_PROTOCOLS: false,
}

/**
 * 净化一段富文本 HTML。
 *
 * 入参允许 null/undefined（接口可能给 null），统一返回空串 ——
 * 编辑器拿到 null 会在初始化时报错。
 */
export function sanitizeHtml(html: string | null | undefined): string {
  if (!html) return ''
  return DOMPurify.sanitize(html, PURIFY_CONFIG)
}
