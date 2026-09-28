import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { FormInstance } from 'element-plus'

/**
 * useCrud 的单元测试。
 *
 * 重点不在「能不能跑」，而在收口之前各页面**各自写错的同一件事**：
 *   - 用户点「取消」不能被当成错误（原实现里有的是空 catch 静默，
 *     有的是连 try/catch 都没有 → unhandled rejection）
 *   - 接口失败必须留痕迹，且不能把 loading 卡住
 */

const ElMessage = { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
const ElMessageBox = { confirm: vi.fn() }
vi.mock('element-plus', () => ({ ElMessage, ElMessageBox }))

const { useCrud } = await import('@/hooks/useCrud')

interface Row {
  id: number
  name: string
}
interface Form {
  id: number
  name: string
}

function setup(overrides: Partial<Parameters<typeof useCrud<Row, Form>>[0]> = {}) {
  const list = vi.fn(async () => ({ data: { list: [{ id: 1, name: '运营岗' }], total: 1 } }))
  const create = vi.fn(async () => ({}))
  const update = vi.fn(async () => ({}))
  const remove = vi.fn(async () => ({}))

  const crud = useCrud<Row, Form>({
    list,
    create,
    update,
    remove,
    createForm: () => ({ id: 0, name: '' }),
    titles: { add: '新增岗位', edit: '编辑岗位' },
    deleteConfirm: '确定删除该岗位？',
    immediate: false, // 不自动加载：测试里显式调用，避免 onMounted 落在组件上下文之外
    ...overrides,
  })

  return { crud, list, create, update, remove }
}

/** 表单校验结果由 stub 控制：hook 只关心「通过 / 不通过」 */
function stubForm(crud: ReturnType<typeof setup>['crud'], valid: boolean) {
  crud.formRef.value = { validate: () => Promise.resolve(valid) } as unknown as FormInstance
}

beforeEach(() => {
  vi.clearAllMocks()
  ElMessageBox.confirm.mockResolvedValue('confirm')
})

describe('loadData', () => {
  it('把搜索条件与分页参数合并后交给列表接口', async () => {
    const { crud, list } = setup({ query: () => ({ name: '运营' }) })
    crud.page.value = 2
    crud.pageSize.value = 20

    await crud.loadData()

    expect(list).toHaveBeenCalledWith({ name: '运营', page: 2, pageSize: 20 })
    expect(crud.tableData.value).toEqual([{ id: 1, name: '运营岗' }])
    expect(crud.total.value).toBe(1)
    expect(crud.loading.value).toBe(false)
  })

  it('pagination 为 false 时不传分页参数（树形表格用）', async () => {
    const { crud, list } = setup({ pagination: false })

    await crud.loadData()

    expect(list).toHaveBeenCalledWith({})
  })

  it('mapRow 用来把嵌套结构摊平（如 tags → tagIds）', async () => {
    const list = vi.fn(async () => ({
      data: { list: [{ id: 1, name: '张三', tags: [{ id: 7 }, { id: 8 }] }], total: 1 },
    }))
    const crud = useCrud<{ id: number; name: string; tags: { id: number }[] }, Form>({
      list,
      createForm: () => ({ id: 0, name: '' }),
      mapRow: (row) => ({ ...row, tags: row.tags }),
      immediate: false,
    })

    await crud.loadData()

    expect(crud.tableData.value[0].tags.map((t) => t.id)).toEqual([7, 8])
  })

  it('列表接口失败时 loading 也要复位，否则表格一直转圈', async () => {
    const list = vi.fn(async () => {
      throw new Error('boom')
    })
    const crud = useCrud<Row, Form>({
      list,
      createForm: () => ({ id: 0, name: '' }),
      immediate: false,
    })

    await expect(crud.loadData()).rejects.toThrow('boom')

    expect(crud.loading.value).toBe(false)
  })
})

describe('loadData 的请求时序', () => {
  /**
   * 用手动 resolve 的 promise 造出「两次请求同时在飞」的局面。
   *
   * 为什么必须这样造：真实场景是「先发的慢、后发的快」，而默认的 mock 是
   * 立即 resolve 的 —— 那样两次请求天然按顺序完成，缺陷根本不会显现。
   * 这与「验证竞态缺陷必须先消除竞速」是同一条教训。
   */
  function deferredList() {
    const resolvers: ((value: unknown) => void)[] = []
    const list = vi.fn(
      () =>
        new Promise((resolve) => {
          resolvers.push(resolve)
        }),
    )
    return { list, resolvers }
  }

  it('先发的请求后返回时，不得覆盖后发请求的结果', async () => {
    const { list, resolvers } = deferredList()
    const crud = useCrud<Row, Form>({
      list: list as never,
      createForm: () => ({ id: 0, name: '' }),
      immediate: false,
    })

    const first = crud.loadData()
    const second = crud.loadData()

    // 后发的先返回（用户已经改了搜索条件再查一次）
    resolvers[1]({ data: { list: [{ id: 2, name: '新条件的结果' }], total: 1 } })
    await second

    // 先发的此时才返回 —— 它带的是**旧条件**的数据，必须被丢弃
    resolvers[0]({ data: { list: [{ id: 1, name: '旧条件的结果' }], total: 99 } })
    await first

    expect(crud.tableData.value).toEqual([{ id: 2, name: '新条件的结果' }])
    // total 也必须来自最后一次请求，否则分页器页数会按旧条件算
    expect(crud.total.value).toBe(1)
  })

  it('过期请求返回时不得复位 loading', async () => {
    const { list, resolvers } = deferredList()
    const crud = useCrud<Row, Form>({
      list: list as never,
      createForm: () => ({ id: 0, name: '' }),
      immediate: false,
    })

    const first = crud.loadData()
    const second = crud.loadData()

    resolvers[0]({ data: { list: [], total: 0 } })
    await first

    // 用户正在等的第二次请求还没回来，转圈不能停
    expect(crud.loading.value).toBe(true)

    resolvers[1]({ data: { list: [], total: 0 } })
    await second

    expect(crud.loading.value).toBe(false)
  })

  it('过期请求不触发 afterLoad（树形页的下拉选项不能被旧数据重算）', async () => {
    const { list, resolvers } = deferredList()
    const afterLoad = vi.fn()
    const crud = useCrud<Row, Form>({
      list: list as never,
      createForm: () => ({ id: 0, name: '' }),
      afterLoad,
      immediate: false,
    })

    const first = crud.loadData()
    const second = crud.loadData()

    resolvers[1]({ data: { list: [{ id: 2, name: '新' }], total: 1 } })
    await second
    resolvers[0]({ data: { list: [{ id: 1, name: '旧' }], total: 1 } })
    await first

    expect(afterLoad).toHaveBeenCalledTimes(1)
    expect(afterLoad).toHaveBeenCalledWith([{ id: 2, name: '新' }])
  })

  it('每次请求各持一份序号，互不干扰', async () => {
    // 反向对照：序号若写成模块级变量，B 页面的请求会让 A 页面正在飞的结果作废
    const a = deferredList()
    const b = deferredList()
    const crudA = useCrud<Row, Form>({
      list: a.list as never,
      createForm: () => ({ id: 0, name: '' }),
      immediate: false,
    })
    const crudB = useCrud<Row, Form>({
      list: b.list as never,
      createForm: () => ({ id: 0, name: '' }),
      immediate: false,
    })

    const pendingA = crudA.loadData()
    const pendingB = crudB.loadData()

    a.resolvers[0]({ data: { list: [{ id: 1, name: 'A 页' }], total: 1 } })
    b.resolvers[0]({ data: { list: [{ id: 2, name: 'B 页' }], total: 1 } })
    await Promise.all([pendingA, pendingB])

    expect(crudA.tableData.value).toEqual([{ id: 1, name: 'A 页' }])
    expect(crudB.tableData.value).toEqual([{ id: 2, name: 'B 页' }])
    expect(crudA.loading.value).toBe(false)
    expect(crudB.loading.value).toBe(false)
  })
})

describe('handleSearch', () => {
  it('回到第 1 页再查（否则第 3 页搜出 1 条会显示空列表）', async () => {
    const { crud, list } = setup()
    crud.page.value = 3

    await crud.handleSearch()

    expect(crud.page.value).toBe(1)
    expect(list).toHaveBeenCalledWith({ page: 1, pageSize: 10 })
  })
})

describe('弹窗', () => {
  it('handleAdd 重置表单并设置新增标题', () => {
    const { crud } = setup()
    Object.assign(crud.form, { id: 9, name: '残留值' })

    crud.handleAdd()

    expect(crud.form).toEqual({ id: 0, name: '' })
    expect(crud.dialogTitle.value).toBe('新增岗位')
    expect(crud.dialogVisible.value).toBe(true)
    expect(crud.isEdit.value).toBe(false)
  })

  it('handleAdd 可以带额外初值（如「在该节点下新增」的 parentId）', () => {
    const { crud } = setup()

    crud.handleAdd({ id: 5 })

    expect(crud.form.id).toBe(5)
    expect(crud.isEdit.value).toBe(false)
  })

  it('handleAdd 传了非对象要吵出来（模板写成 handleAdd(row.id) 会静默丢 parentId）', () => {
    const { crud } = setup()

    // el-table 的 slot row 是 any，vue-tsc 拦不住这种写法：
    // 展开一个数字得到 {}，parentId 停在 0，「新增子部门」会变成新增根部门
    expect(() => crud.handleAdd(42 as unknown as Partial<Form>)).toThrow(/必须是对象/)
  })

  it('handleEdit 先重置再覆盖，避免带上上一条记录的残留字段', () => {
    const { crud } = setup()
    Object.assign(crud.form, { id: 9, name: '上一条' })

    crud.handleEdit({ id: 3, name: '目标行' })

    expect(crud.form).toEqual({ id: 3, name: '目标行' })
    expect(crud.dialogTitle.value).toBe('编辑岗位')
    expect(crud.isEdit.value).toBe(true)
  })

  it('handleEdit 默认只复制表单声明过的字段（不把服务端字段带回提交载荷）', () => {
    const list = vi.fn(async () => ({
      data: { list: [{ id: 1, name: 'a', createdAt: '2026-01-01' }], total: 1 },
    }))
    const crud = useCrud<Row & { createdAt: string }, Form>({
      list,
      createForm: () => ({ id: 0, name: '' }),
      immediate: false,
    })

    crud.handleEdit({ id: 3, name: '目标行', createdAt: '2026-01-01' })

    // createdAt 不在表单字段里，不该被复制进来 —— 否则会被当成用户输入回传
    expect(crud.form).toEqual({ id: 3, name: '目标行' })
  })

  it('rowToForm 可覆盖默认映射（如把 birthday 的 ISO 串截成日期）', () => {
    const { crud } = setup({
      rowToForm: (row) => ({ id: row.id, name: row.name.split(' ')[0] }),
    })

    crud.handleEdit({ id: 3, name: '张 三' })

    expect(crud.form.name).toBe('张')
  })
})

describe('handleSubmit', () => {
  it('校验不通过时不发请求', async () => {
    const { crud, create } = setup()
    stubForm(crud, false)

    await crud.handleSubmit()

    expect(create).not.toHaveBeenCalled()
    expect(crud.submitLoading.value).toBe(false)
  })

  it('新增走 create，成功后关弹窗并刷新', async () => {
    const { crud, create, update, list } = setup()
    stubForm(crud, true)
    crud.handleAdd()
    Object.assign(crud.form, { id: 0, name: '新岗位' })

    await crud.handleSubmit()

    expect(create).toHaveBeenCalledWith({ id: 0, name: '新岗位' })
    expect(update).not.toHaveBeenCalled()
    expect(crud.dialogVisible.value).toBe(false)
    expect(list).toHaveBeenCalled()
    expect(crud.submitLoading.value).toBe(false)
  })

  it('编辑走 update', async () => {
    const { crud, create, update } = setup()
    stubForm(crud, true)
    crud.handleEdit({ id: 3, name: '改名后' })

    await crud.handleSubmit()

    expect(update).toHaveBeenCalledWith({ id: 3, name: '改名后' })
    expect(create).not.toHaveBeenCalled()
  })

  it('提交失败时 submitLoading 复位且弹窗保持打开（用户可重试）', async () => {
    const update = vi.fn(async () => {
      throw new Error('boom')
    })
    const { crud } = setup({ update })
    stubForm(crud, true)
    crud.handleEdit({ id: 3, name: 'x' })

    await expect(crud.handleSubmit()).rejects.toThrow('boom')

    expect(crud.submitLoading.value).toBe(false)
    expect(crud.dialogVisible.value).toBe(true)
  })
})

describe('handleDelete', () => {
  it('用户点「取消」时既不发请求也不抛错（原实现这里会静默吞掉或变成 unhandled rejection）', async () => {
    const { crud, remove } = setup()
    // ElMessageBox 取消时 reject 出字符串 'cancel'
    ElMessageBox.confirm.mockRejectedValue('cancel')

    await expect(crud.handleDelete({ id: 1, name: '运营岗' })).resolves.toBeUndefined()

    expect(remove).not.toHaveBeenCalled()
    expect(ElMessage.error).not.toHaveBeenCalled()
  })

  it('确认后按 id 删除并刷新列表', async () => {
    const { crud, remove, list } = setup()

    await crud.handleDelete({ id: 7, name: '运营岗' })

    expect(ElMessageBox.confirm).toHaveBeenCalledWith('确定删除该岗位？', '提示', {
      type: 'warning',
    })
    expect(remove).toHaveBeenCalledWith(7)
    expect(ElMessage.success).toHaveBeenCalled()
    expect(list).toHaveBeenCalled()
  })

  it('deleteConfirm 传函数时按行生成文案', async () => {
    const { crud } = setup({ deleteConfirm: (row) => `确定删除「${row.name}」？` })

    await crud.handleDelete({ id: 7, name: '运营岗' })

    expect(ElMessageBox.confirm).toHaveBeenCalledWith('确定删除「运营岗」？', '提示', {
      type: 'warning',
    })
  })

  it('接口失败不抛出（提示由拦截器负责），但必须留下排查痕迹', async () => {
    const remove = vi.fn(async () => {
      throw new Error('boom')
    })
    const { crud } = setup({ remove })
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})

    await expect(crud.handleDelete({ id: 1, name: 'x' })).resolves.toBeUndefined()

    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
  })
})
