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
