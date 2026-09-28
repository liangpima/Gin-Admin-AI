/**
 * 图片/视频选择器的文件类型校验。
 *
 * 抽成纯函数是为了可测：校验逻辑写在组件里就只能靠手工点上传按钮验证，
 * 而这里要守的恰恰是「哪些文件被放过」—— 它是安全边界的一部分。
 *
 * 为什么必须在**前端**也校验一遍（后端 `pkg/upload.ValidateFile` 已有白名单）：
 *   - `accept` 属性只是浏览器的**选择提示**，用户可以改成「所有文件」，
 *     或者直接把文件拖进来，完全绕过它；
 *   - 用户把 `evil.js` 改名成 `evil.jpg` 时，只有后端能靠扩展名白名单拦住；
 *     但反过来把 `photo.png` 改名成 `photo.mp4` 这种「类型对不上」的情况，
 *     前端提前拦住能省掉一次注定失败的往返，并给出可读提示。
 *
 * ⚠️ 前端校验**不是**安全边界，只是体验优化。真正的把关必须在服务端：
 * 攻击者可以直接 POST 到上传接口，不经过任何前端代码。
 */

/**
 * 按类别允许的扩展名（小写，不含点）。
 *
 * ⚠️ 这两份列表必须与后端 `pkg/upload` 的白名单（内置默认值 + `upload.allow_exts`）
 * 及 `config/config.yaml` 保持一致。前端放行、后端拒绝的组合，用户看到的是
 * 「选了文件但传不上去」，会被当成前端坏了。已用 `logic.spec.ts` 把 `.svg`
 * 的**拒绝**钉住 —— 它曾在前端白名单里，后端默认白名单也含它。
 *
 * 为什么 `.svg` 被排除：SVG 可以内嵌 `<script>` 与 `<foreignObject>`。
 * 本地存储时 `/uploads` 有 `UploadSecurity()` 的 CSP `sandbox` 兜着，
 * 但**对象存储部署下 `GetURL()` 返回的是不经过后端的直链**，
 * 那层头不存在 → 上传者控制的同源文档 = 存储型 XSS。
 * 需要矢量图标请走内联 SVG 组件（`utils/icons.ts` 的 `appIcons`），不要走上传。
 */
export const IMAGE_EXTENSIONS = ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp'] as const
export const VIDEO_EXTENSIONS = ['mp4', 'mov', 'avi', 'mkv', 'webm'] as const

export type MediaType = 'image' | 'video'

/** 取出小写扩展名（不含点）；没有扩展名时返回空串 */
export function extensionOf(filename: string): string {
  const i = filename.lastIndexOf('.')
  if (i < 0 || i === filename.length - 1) {
    return ''
  }
  return filename.slice(i + 1).toLowerCase()
}

/** 该类别下允许的扩展名列表 */
export function allowedExtensions(type: MediaType): readonly string[] {
  return type === 'video' ? VIDEO_EXTENSIONS : IMAGE_EXTENSIONS
}

/** 文件体积上限（字节） */
export function maxSizeOf(type: MediaType): number {
  return type === 'video' ? 50 * 1024 * 1024 : 10 * 1024 * 1024
}

/** 体积上限的可读文案 */
export function maxSizeLabel(type: MediaType): string {
  return type === 'video' ? '50MB' : '10MB'
}

export interface FileLike {
  name: string
  size: number
  type?: string
}

export type ValidationResult = { ok: true } | { ok: false; reason: string }

/**
 * 校验单个文件是否可上传。
 *
 * 判定顺序刻意是「体积 → 扩展名 → MIME」：
 * 体积是用户最容易理解也最常见的问题（选错文件），先说它；
 * MIME 放最后是因为它最不可靠 —— 浏览器对未知扩展名一律给空串，
 * 拿它做**唯一**依据会把合法文件挡在外面。
 */
export function validateMediaFile(file: FileLike, type: MediaType): ValidationResult {
  const allowed = allowedExtensions(type)
  const label = type === 'video' ? '视频' : '图片'

  if (file.size > maxSizeOf(type)) {
    return { ok: false, reason: `${file.name} 超过${maxSizeLabel(type)}` }
  }

  const ext = extensionOf(file.name)
  if (!ext) {
    return { ok: false, reason: `${file.name} 没有扩展名，无法确认是${label}` }
  }
  if (!allowed.includes(ext)) {
    return { ok: false, reason: `${file.name} 不是支持的${label}格式` }
  }

  // MIME 只做**方向性**校验：浏览器给了值就必须与类别匹配，没给就不管。
  // 反过来「必须有 MIME 且必须匹配」会把一批合法文件误杀。
  const mime = file.type ?? ''
  if (mime && !mime.startsWith(`${type}/`)) {
    return { ok: false, reason: `${file.name} 的文件类型与${label}不符` }
  }

  return { ok: true }
}

/**
 * 批量校验，返回通过的文件与被拒原因。
 *
 * 不在这里弹提示：提示是 UI 的事，纯函数只负责判定，
 * 这样用例可以直接断言结果而不用去 mock ElMessage。
 */
export function partitionMediaFiles<T extends FileLike>(
  files: T[],
  type: MediaType,
): { accepted: T[]; rejected: string[] } {
  const accepted: T[] = []
  const rejected: string[] = []
  for (const file of files) {
    const result = validateMediaFile(file, type)
    if (result.ok) {
      accepted.push(file)
    } else {
      rejected.push(result.reason)
    }
  }
  return { accepted, rejected }
}
