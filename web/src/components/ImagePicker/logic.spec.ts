import { describe, expect, it } from 'vitest'
import {
  allowedExtensions,
  extensionOf,
  maxSizeOf,
  partitionMediaFiles,
  validateMediaFile,
} from './logic'

/**
 * 上传文件类型校验的单元测试。
 *
 * 为什么值得单独测：这是「哪些文件会被放过」的判定，而组件里原来的实现
 * **只看体积**（`accept` 属性只是浏览器的选择提示，改成「所有文件」或直接拖入
 * 就能绕过）。于是 `type='image'` 的图片选择器可以传上 `.html` / `.js`，
 * 落在可直链访问的 `/uploads/` 下 —— 内容是攻击者可控的 HTML/脚本，
 * 属于存储型 XSS 的入口。
 */
describe('extensionOf', () => {
  it('取小写扩展名', () => {
    expect(extensionOf('a.PNG')).toBe('png')
    expect(extensionOf('archive.tar.gz')).toBe('gz')
  })

  it('没有扩展名或点结尾时返回空串', () => {
    expect(extensionOf('noext')).toBe('')
    expect(extensionOf('trailing.')).toBe('')
    expect(extensionOf('')).toBe('')
  })
})

describe('validateMediaFile：体积', () => {
  it('图片超过 10MB 被拒', () => {
    const r = validateMediaFile({ name: 'big.png', size: 10 * 1024 * 1024 + 1 }, 'image')
    expect(r.ok).toBe(false)
    expect(r.ok === false && r.reason).toContain('10MB')
  })

  it('视频上限是 50MB（与图片不同）', () => {
    expect(maxSizeOf('video')).toBe(50 * 1024 * 1024)
    expect(validateMediaFile({ name: 'v.mp4', size: 20 * 1024 * 1024 }, 'video').ok).toBe(true)
    expect(validateMediaFile({ name: 'v.mp4', size: 50 * 1024 * 1024 + 1 }, 'video').ok).toBe(false)
  })
})

describe('validateMediaFile：扩展名', () => {
  it('图片类别接受常见图片扩展名', () => {
    for (const ext of ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp']) {
      expect(validateMediaFile({ name: `a.${ext}`, size: 100 }, 'image').ok, ext).toBe(true)
    }
  })

  it('图片类别拒绝脚本/网页类文件（存储型 XSS 入口）', () => {
    // 这些正是「改后缀就能绕过 accept」的典型载荷
    for (const ext of ['html', 'htm', 'js', 'mjs', 'php', 'exe']) {
      const r = validateMediaFile({ name: `evil.${ext}`, size: 100 }, 'image')
      expect(r.ok, ext).toBe(false)
    }
  })

  it('.svg 必须被拒（前端要与后端白名单一致）', () => {
    // 回归：`IMAGE_EXTENSIONS` 里曾含 `svg`，后端默认白名单也含它。
    // 它被移除的原因是**部署形态相关**：本地存储下 `/uploads` 有 CSP `sandbox`
    // 兜着，而对象存储部署下 `GetURL()` 是不过后端的直链，那层头不存在 ——
    // SVG 内嵌的 <script> 会以同源文档身份执行（存储型 XSS）。
    //
    // 这条断言同时钉住「前后端两份白名单一致」：若后端加回了 svg 而前端没加，
    // 用户会看到「选了文件传不上去」；反之则是前端放行、后端报 400。
    const r = validateMediaFile({ name: 'logo.svg', size: 100 }, 'image')
    expect(r.ok).toBe(false)
  })

  it('图片类别拒绝视频扩展名，反之亦然', () => {
    expect(validateMediaFile({ name: 'a.mp4', size: 100 }, 'image').ok).toBe(false)
    expect(validateMediaFile({ name: 'a.png', size: 100 }, 'video').ok).toBe(false)
  })

  it('没有扩展名被拒（无法确认类型）', () => {
    const r = validateMediaFile({ name: 'noext', size: 100 }, 'image')
    expect(r.ok).toBe(false)
    expect(r.ok === false && r.reason).toContain('没有扩展名')
  })

  it('扩展名大小写不敏感', () => {
    expect(validateMediaFile({ name: 'A.PNG', size: 100 }, 'image').ok).toBe(true)
  })
})

describe('validateMediaFile：MIME 只做方向性校验', () => {
  it('浏览器给了 MIME 且与类别不符 → 拒绝', () => {
    // 把 .png 改名成 .jpg 这类「扩展名对、内容类别不对」的情况
    const r = validateMediaFile(
      { name: 'fake.jpg', size: 100, type: 'application/x-msdownload' },
      'image',
    )
    expect(r.ok).toBe(false)
    expect(r.ok === false && r.reason).toContain('不符')
  })

  it('浏览器没给 MIME（空串）→ 放行，不误杀', () => {
    // 未知扩展名时浏览器会给空串。若把 MIME 当唯一依据，合法文件会被挡在外面。
    expect(validateMediaFile({ name: 'a.png', size: 100, type: '' }, 'image').ok).toBe(true)
    expect(validateMediaFile({ name: 'a.png', size: 100 }, 'image').ok).toBe(true)
  })

  it('MIME 与类别一致 → 放行', () => {
    expect(validateMediaFile({ name: 'a.png', size: 100, type: 'image/png' }, 'image').ok).toBe(
      true,
    )
    expect(validateMediaFile({ name: 'a.mp4', size: 100, type: 'video/mp4' }, 'video').ok).toBe(
      true,
    )
  })
})

describe('partitionMediaFiles', () => {
  it('分类出通过与拒绝，并保留每个拒绝原因', () => {
    const files = [
      { name: 'ok1.png', size: 100 },
      { name: 'evil.html', size: 100 },
      { name: 'ok2.jpg', size: 100 },
      { name: 'huge.png', size: 10 * 1024 * 1024 + 1 },
    ]

    const { accepted, rejected } = partitionMediaFiles(files, 'image')

    expect(accepted.map((f) => f.name)).toEqual(['ok1.png', 'ok2.jpg'])
    expect(rejected).toHaveLength(2)
    expect(rejected.join(' ')).toContain('evil.html')
    expect(rejected.join(' ')).toContain('huge.png')
  })

  it('全部合法时不产生拒绝项', () => {
    const { accepted, rejected } = partitionMediaFiles([{ name: 'a.png', size: 1 }], 'image')
    expect(accepted).toHaveLength(1)
    expect(rejected).toEqual([])
  })
})

describe('allowedExtensions', () => {
  it('图片与视频的扩展名列表不重叠', () => {
    const overlap = allowedExtensions('image').filter((e) =>
      (allowedExtensions('video') as readonly string[]).includes(e),
    )
    expect(overlap).toEqual([])
  })
})
