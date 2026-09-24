import { onMounted, reactive, ref, type Ref } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance } from 'element-plus'

/**
 * 列表页 CRUD 样板的收口。
 *
 * 抽这个 hook 不是因为「重复代码看着烦」，而是因为那 10 份样板里**各自都写错了
 * 同一件事**：删除确认与接口调用被塞进同一个 try/catch（或干脆没有 try/catch）。
 *
 *   - 有 try/catch 的：`catch {}` 把「用户点了取消」和「接口真的失败」混为一谈，
 *     前者是正常操作，却和后者一样静默；
 *   - 没有 try/catch 的（如 system/post）：用户点「取消」会让 ElMessageBox 的
 *     reject 变成 unhandled promise rejection，控制台报红而页面看着没事。
 *
 * 收口之后这两件事被彻底分开：取消直接 return，接口失败才留痕迹，
 * 而且面向用户的提示仍由响应拦截器统一负责（不在这里重复弹 toast）。
 *
 * 有意**不**收口的东西：
 *   - 表单字段与校验规则（每个页面都不一样，抽出来只会变成一堆回调配置）
 *   - 模板结构（各页列宽/操作列差异大）
 *   - `resetQuery`（它要重置的是页面自己的搜索字段，hook 不知道有哪些）
 */
export interface CrudPageResult<T> {
  list: T[]
  total: number
}

export interface UseCrudOptions<T extends { id: number }, F extends object, P, R = T> {
  /**
   * 列表接口。参数由「搜索条件 + 分页字段」合并而成，
   * 因此调用点直接透传即可：`(params: RoleQuery) => getRoleList(params)`。
   *
   * 给 params 标注真实类型（而不是在页面里写 `as` 强转）是有意的：
   * 各 api 模块的查询参数是强类型接口（`RoleQuery` / `PostQuery`…），
   * 强转会把「新增了搜索字段却忘了传」这类问题从编译期挪到运行期。
   *
   * 返回值有两种真实形状，都接受：
   *   - 分页列表 `{ list, total }`
   *   - 树形列表（部门/菜单）直接返回数组
   * 页面不必为此各写一层适配。
   */
  list: (params: P) => Promise<{ data: R[] | CrudPageResult<R> }>
  create?: (payload: F) => Promise<unknown>
  update?: (payload: F) => Promise<unknown>
  remove?: (id: number) => Promise<unknown>
  /** 表单初始值。handleAdd 用它重置，handleEdit 也先重置再覆盖 */
  createForm: () => F
  /**
   * 搜索条件。**不含** page/pageSize —— 那两项由 hook 管理。
   * 这样「搜索框的值」与「当前页码」不会再各存一份、各自重置。
   */
  query?: () => Partial<Omit<P, 'page' | 'pageSize'>>
  /** 行 → 表单。默认**只复制表单声明过的字段**（见 defaultRowToForm） */
  rowToForm?: (row: T) => F
  /**
   * 接口行 → 表格行，用于补出接口没直接给、但表格/表单要用的字段
   * （如把 `roles` 展开成 `roleIds`）。传了它就要把 R 声明成接口行的类型。
   */
  mapRow?: (row: R) => T
  /**
   * 列表加载完成后调用（收到的是加工后的行）。
   * 树形页面用它派生下拉选项（部门树/菜单树），保证新增节点后选项同步刷新。
   */
  afterLoad?: (rows: T[]) => void
  /** 弹窗标题 */
  titles?: { add?: string; edit?: string }
  /** 删除确认文案，可按行定制 */
  deleteConfirm?: string | ((row: T) => string)
  /** 成功提示文案 */
  messages?: { submit?: string; remove?: string }
  /** 每页条数，默认 10 */
  pageSize?: number
  /** 是否分页。树形表格传 false —— 此时不传分页参数，也不会有 page/pageSize */
  pagination?: boolean
  /** 是否在 onMounted 自动加载，默认 true */
  immediate?: boolean
}

export function useCrud<
  T extends { id: number },
  F extends object,
  P = Record<string, unknown>,
  R = T,
>(options: UseCrudOptions<T, F, P, R>) {
  const {
    list,
    create,
    update,
    remove,
    createForm,
    query,
    rowToForm,
    mapRow,
    afterLoad,
    titles = {},
    deleteConfirm = '确定删除该记录？',
    messages = {},
    pageSize: initialPageSize = 10,
    pagination = true,
    immediate = true,
  } = options

  const loading = ref(false)
  const submitLoading = ref(false)
  const tableData = ref([]) as Ref<T[]>
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(initialPageSize)
  const dialogVisible = ref(false)
  const dialogTitle = ref('')
  const isEdit = ref(false)
  const formRef = ref<FormInstance>()
  // 用 reactive 而不是 ref：页面里 form.xxx 的写法在 script 与 template 两侧一致，
  // 不必区分 form.value.xxx
  const form = reactive(createForm()) as F

  async function loadData() {
    loading.value = true
    try {
      const merged: Record<string, unknown> = { ...(query?.() ?? {}) }
      if (pagination) {
        merged.page = page.value
        merged.pageSize = pageSize.value
      }
      // 这里必须强转：merged 由「松散的搜索字段 + 分页字段」拼出，
      // 而 P 是调用方声明的强类型查询接口。页面侧因此不必再写 `as`，
      // 强转收敛到这一处（「新增搜索字段却忘了传」由页面标注的类型守住）。
      const res = await list(merged as unknown as P)
      // 两种返回形状：分页 `{ list, total }` / 树形直接是数组
      const payload = res.data
      const rows = Array.isArray(payload) ? payload : payload.list
      tableData.value = mapRow ? rows.map(mapRow) : (rows as unknown as T[])
      total.value = Array.isArray(payload) ? payload.length : payload.total
      afterLoad?.(tableData.value)
    } finally {
      // 失败时也要复位，否则表格会一直转圈（错误提示由拦截器负责）
      loading.value = false
    }
  }

  /** 搜索：必须回到第 1 页，否则在第 3 页搜出 1 条结果会显示空列表 */
  async function handleSearch() {
    page.value = 1
    await loadData()
  }

  /**
   * 行 → 表单的默认实现：以表单初始值的**键**为准做白名单复制。
   *
   * 不用 `Object.assign(form, row)` 的原因：列表行里通常带着服务端字段
   * （createdAt / points / memberNo…），全量覆盖会让它们随后被
   * `handleSubmit` 当成用户输入回传。白名单只认表单自己声明的字段，
   * 与后端「未提供即不改」的部分更新语义也对得上。
   */
  function defaultRowToForm(row: T): F {
    const out = createForm() as unknown as Record<string, unknown>
    const source = row as unknown as Record<string, unknown>
    for (const key of Object.keys(out)) {
      if (key in source) out[key] = source[key]
    }
    return out as F
  }

  function openDialog(edit: boolean, values: F) {
    Object.assign(form, values)
    isEdit.value = edit
    dialogTitle.value = edit ? (titles.edit ?? '编辑') : (titles.add ?? '新增')
    dialogVisible.value = true
  }

  /** extra 用于「在某个节点下新增」这类场景（如部门/菜单传 parentId） */
  function handleAdd(extra?: Partial<F>) {
    // 运行期护栏：模板里写成 handleAdd(row.id) 时，展开一个数字会**静默**得到 {}，
    // parentId 停在 0 —— 表现是「新增子部门」变成了新增根部门，数据错了还不报错。
    // 而 el-table 的 slot row 是 any，`row.id` 也是 any，vue-tsc 拦不住这种写法，
    // 所以只能在这里吵一声（见 useCrud.spec.ts 的用例）。
    if (extra !== undefined && typeof extra !== 'object') {
      throw new Error(
        '[useCrud] handleAdd 的额外初值必须是对象，例如 handleAdd({ parentId: row.id })',
      )
    }
    openDialog(false, { ...createForm(), ...(extra ?? {}) })
  }

  function handleEdit(row: T) {
    openDialog(true, rowToForm ? rowToForm(row) : defaultRowToForm(row))
  }

  async function handleSubmit() {
    const valid = await formRef.value?.validate().catch(() => false)
    if (!valid) return

    const submit = isEdit.value ? update : create
    if (!submit) {
      // 属于接线错误（页面提供了表单却没给对应接口），要吵而不是静默
      throw new Error(`[useCrud] 未提供 ${isEdit.value ? 'update' : 'create'}，无法提交表单`)
    }

    submitLoading.value = true
    try {
      await submit(form)
      ElMessage.success(messages.submit ?? '操作成功')
      dialogVisible.value = false
      await loadData()
    } finally {
      submitLoading.value = false
    }
  }

  async function handleDelete(row: T) {
    if (!remove) {
      throw new Error('[useCrud] 未提供 remove，无法删除')
    }

    const text = typeof deleteConfirm === 'function' ? deleteConfirm(row) : deleteConfirm
    try {
      await ElMessageBox.confirm(text, '提示', { type: 'warning' })
    } catch {
      // 用户点了取消/关闭：ElMessageBox 会 reject 出 'cancel' / 'close'。
      // 这是正常操作，必须在这里就地返回，不能落进下面的接口异常分支
      return
    }

    try {
      await remove(row.id)
      ElMessage.success(messages.remove ?? '删除成功')
      await loadData()
    } catch (err) {
      // 面向用户的提示由响应拦截器统一负责，这里只留排查用的痕迹
      console.warn('[useCrud] 删除失败', err)
    }
  }

  if (immediate) {
    onMounted(() => {
      void loadData()
    })
  }

  return {
    loading,
    submitLoading,
    tableData,
    total,
    page,
    pageSize,
    dialogVisible,
    dialogTitle,
    isEdit,
    form,
    formRef,
    loadData,
    handleSearch,
    handleAdd,
    handleEdit,
    handleSubmit,
    handleDelete,
  }
}
