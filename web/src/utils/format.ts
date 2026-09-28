export function formatDateTime(time: string | Date | null | undefined): string {
  if (!time) return ''

  const d = new Date(time)

  // 非法日期必须挡住：`new Date('不是日期')` 得到的是 Invalid Date，
  // 它的 getFullYear() 等一律返回 NaN，直接拼出来就是
  // 「NaN-NaN-NaN NaN:NaN:NaN」—— 表格里出现这种东西比留空更让人困惑，
  // 而且它会被 8+ 个表格列直接渲染出来（后端字段格式一变就整列变样）。
  //
  // 处理成空串而不是「原样返回输入」：本函数的契约是「把时间格式化成可展示的
  // 文本」，做不到时留空与上面的 `!time` 是同一条规则 —— 只留一个分支，
  // 调用方不必区分「没传」与「传了但格式不对」。
  if (Number.isNaN(d.getTime())) return ''

  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
