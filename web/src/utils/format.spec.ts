import { describe, expect, it } from 'vitest'
import { formatDateTime } from './format'

/**
 * 时间格式化工具的单元测试。
 *
 * 这条函数被 8+ 个表格列直接调用，所以它的失败形态是「整列看起来不对劲」，
 * 而不是抛错 —— 没有用例的话只能靠肉眼在页面上发现。
 */
describe('formatDateTime', () => {
  it('正常格式化（补零到两位）', () => {
    expect(formatDateTime('2026-09-26T09:05:03')).toBe('2026-09-26 09:05:03')
  })

  it('接受 Date 对象', () => {
    expect(formatDateTime(new Date(2026, 0, 2, 3, 4, 5))).toBe('2026-01-02 03:04:05')
  })

  it('空值返回空串', () => {
    for (const v of [null, undefined, '']) {
      expect(formatDateTime(v), `输入 ${String(v)}`).toBe('')
    }
  })

  it('非法日期返回空串，而不是 NaN-NaN-NaN', () => {
    // 缺陷形态：`new Date('乱码')` 得到 Invalid Date，
    // 它的 getFullYear() 等返回 NaN，拼出来就是「NaN-NaN-NaN NaN:NaN:NaN」。
    // 后端字段格式一变，整列都会变成这串东西。
    for (const v of ['不是日期', 'garbage', '2026-13-45T99:99:99', {} as never]) {
      const got = formatDateTime(v as never)
      expect(got, `输入 ${JSON.stringify(v)}`).toBe('')
      expect(got).not.toContain('NaN')
    }
  })

  it('纯日期字符串按 UTC 解析（规范行为），展示值可能偏移但不会是 NaN', () => {
    // `new Date('2026-09-26')` 按 ECMAScript 规范被当作 **UTC 零点**，
    // 而下面取值用的是本地时区方法 —— 东八区因此显示 08:00:00，
    // 部署在 UTC 以西则会显示成前一天 16:00:00。
    //
    // 这是规范行为，不是 formatDateTime 的缺陷，所以用例**不写死小时数**
    // （写死了就会在别的时区跑红）。它在这里的作用是把这个反直觉行为固定下来：
    // 以后有人看到「日期少了一天」时，先想到这里，而不是去改取值方式 ——
    // 那会把所有正常的时间戳也一起改错。
    //
    // 后端返回的是带时间的完整时间戳，正常路径不会走到这一分支。
    const got = formatDateTime('2026-09-26')
    expect(got).toMatch(/^2026-09-2[56] \d{2}:\d{2}:\d{2}$/)
    expect(got).not.toContain('NaN')
  })
})
