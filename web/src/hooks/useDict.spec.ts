import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * 字典加载的单元测试（P2-4）。
 *
 * 这里只测**纯逻辑**：模块级缓存、并发合并、失败降级、取值归一化。
 * 这些行为在源码注释里都写了「为什么」，但此前没有任何用例守着 ——
 * 任何一条坏了都表现为「下拉框选项不对」或「页面莫名报错」，
 * 而且往往要等到某个具体页面才被发现。
 *
 * 用 vi.mock 替掉接口层：字典的正确性不依赖真实后端，
 * 这里要验证的是「什么时候发请求、发几次、结果怎么缓存」。
 */

const getDictDataByType = vi.fn()
vi.mock('@/api/dict', () => ({
  getDictDataByType: (type: string) => getDictDataByType(type),
}))

// onMounted 需要组件实例，这里用 useDict 的纯函数部分，故先 mock 掉它，
// 避免测试输出里出现「onMounted is called when there is no active component instance」
vi.mock('vue', async () => {
  const actual = await vi.importActual<typeof import('vue')>('vue')
  return { ...actual, onMounted: () => {} }
})

const { loadDict, clearDictCache, normalizeDictValue, useDict } = await import('@/hooks/useDict')

/** 造一条后端字典数据 */
function dictItem(label: string, value: unknown, listClass = 'success') {
  return { id: 1, dictType: 't', label, value, sort: 1, cssClass: '', listClass, status: 1 }
}

describe('loadDict 加载与缓存', () => {
  beforeEach(() => {
    clearDictCache()
    getDictDataByType.mockReset()
  })

  it('首次调用发请求，并把后端字段映射成选项', async () => {
    getDictDataByType.mockResolvedValue({ data: [dictItem('正常', 1), dictItem('停用', 0)] })

    const options = await loadDict('sys_user_status')

    expect(getDictDataByType).toHaveBeenCalledTimes(1)
    expect(options).toEqual([
      { label: '正常', value: '1', listClass: 'success', cssClass: '' },
      { label: '停用', value: '0', listClass: 'success', cssClass: '' },
    ])
  })

  it('命中缓存不再发请求', async () => {
    getDictDataByType.mockResolvedValue({ data: [dictItem('正常', 1)] })

    await loadDict('sys_user_status')
    await loadDict('sys_user_status')

    expect(getDictDataByType).toHaveBeenCalledTimes(1)
  })

  it('并发调用共用同一个请求', async () => {
    // 表格里几十个 DictTag 同时挂载就是这个场景：
    // 不去重的话一次列表渲染会打出几十个请求
    getDictDataByType.mockImplementation(
      () =>
        new Promise((resolve) => setTimeout(() => resolve({ data: [dictItem('正常', 1)] }), 10)),
    )

    const [a, b, c] = await Promise.all([
      loadDict('sys_user_status'),
      loadDict('sys_user_status'),
      loadDict('sys_user_status'),
    ])

    expect(getDictDataByType).toHaveBeenCalledTimes(1)
    expect(a).toBe(b)
    expect(b).toBe(c)
  })

  it('加载失败时返回空数组且不写缓存（下次会重试）', async () => {
    // 失败路径会刻意打一条降级警告（这是设计的一部分：字典故障不该静默），
    // 这里既断言它确实打了，也避免测试输出被堆栈刷屏
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    getDictDataByType.mockRejectedValueOnce(new Error('network down'))

    const failed = await loadDict('sys_user_status')
    expect(failed).toEqual([])
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('sys_user_status'), expect.anything())
    warn.mockRestore()

    // 关键：失败不能进缓存。进了缓存意味着字典故障会被「记住」，
    // 用户刷新页面也不会恢复
    getDictDataByType.mockResolvedValue({ data: [dictItem('正常', 1)] })
    const retried = await loadDict('sys_user_status')
    expect(getDictDataByType).toHaveBeenCalledTimes(2)
    expect(retried).toHaveLength(1)
  })

  it('类型为空时直接返回空数组，不发请求', async () => {
    expect(await loadDict('')).toEqual([])
    expect(getDictDataByType).not.toHaveBeenCalled()
  })

  it('后端返回 null/缺字段时降级为空串而不是 undefined', async () => {
    getDictDataByType.mockResolvedValue({ data: [dictItem('奇怪', null)] })

    const [opt] = await loadDict('sys_user_status')
    expect(opt.value).toBe('')
    expect(opt.cssClass).toBe('')
  })
})

describe('clearDictCache 缓存失效', () => {
  beforeEach(() => {
    clearDictCache()
    getDictDataByType.mockReset()
  })

  it('清空指定类型后该类型重新请求，其它类型不受影响', async () => {
    getDictDataByType.mockResolvedValue({ data: [dictItem('x', 1)] })
    await loadDict('a')
    await loadDict('b')

    clearDictCache('a')
    await loadDict('a')
    await loadDict('b')

    // a 请求两次（第二次是缓存被清后重发），b 只有一次
    expect(getDictDataByType).toHaveBeenCalledTimes(3)
  })

  it('不带参数时清空全部', async () => {
    getDictDataByType.mockResolvedValue({ data: [dictItem('x', 1)] })
    await loadDict('a')
    await loadDict('b')

    clearDictCache()
    await loadDict('a')
    await loadDict('b')

    expect(getDictDataByType).toHaveBeenCalledTimes(4)
  })
})

describe('normalizeDictValue 取值归一化', () => {
  it('把数字与字符串统一成字符串', () => {
    // 库里 value 是 varchar，页面行数据里可能是数字 —— 不归一化就永远不相等，
    // 表现为「标签显示成原始值」这种很难定位的问题
    expect(normalizeDictValue(1)).toBe('1')
    expect(normalizeDictValue('1')).toBe('1')
    expect(normalizeDictValue(0)).toBe('0')
  })

  it('null/undefined 归一成空串而不是 "null"/"undefined"', () => {
    expect(normalizeDictValue(null)).toBe('')
    expect(normalizeDictValue(undefined)).toBe('')
  })
})

describe('useDict 取值辅助（字典为空时的降级）', () => {
  beforeEach(() => {
    clearDictCache()
    getDictDataByType.mockReset()
  })

  it('字典为空时 getList 返回 fallback', () => {
    // 字典是可被运营删除的数据：清空后若返回空数组，筛选框会变成空下拉，
    // 用户连最基本的选项都选不了 —— 属于功能级故障，因此必须能传兜底选项
    const { getList } = useDict('sys_user_status')
    const fallback = [{ label: '正常', value: '1', listClass: '', cssClass: '' }]

    expect(getList('sys_user_status')).toEqual([])
    expect(getList('sys_user_status', fallback)).toEqual(fallback)
  })

  it('字典里找不到时 getLabel 降级返回原始值', () => {
    const { getLabel } = useDict('sys_user_status')
    expect(getLabel('sys_user_status', 1)).toBe('1')
    expect(getLabel('sys_user_status', '未知')).toBe('未知')
    expect(getLabel('sys_user_status', null)).toBe('')
  })

  it('找不到时 getTagType 返回 info 而不是空串', () => {
    const { getTagType } = useDict('sys_user_status')
    expect(getTagType('sys_user_status', 1)).toBe('info')
  })
})
