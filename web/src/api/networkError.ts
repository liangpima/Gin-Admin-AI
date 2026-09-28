/**
 * 传输层错误的用户提示（纯函数，无副作用）。
 *
 * 单独成文件而不是写在 api/index.ts 里：index.ts 在模块求值时就会创建 axios
 * 实例、注册拦截器、并 import router —— 为了测一个字符串映射去 mock 那一整套
 * 不划算，也容易让「测试为什么失败」多几层噪音。
 */

/**
 * 把 axios 的传输层错误翻成中文提示。
 *
 * 为什么不直接用 `error.message`：那是 axios 生成的英文原文
 * （`Request failed with status code 500`、`timeout of 30000ms exceeded`、
 * `Network Error`）—— 对中文用户没有任何信息量，还把实现细节摊在界面上：
 * 用户既看不懂，也不知道该做什么。
 *
 * 业务错误不经过这里：那条路径上后端已经给了可行动的中文文案，
 * 只有「请求根本没到达业务层」才落到本函数。
 *
 * 判定刻意用鸭子类型而不是 `axios.isAxiosError`：本模块不依赖 axios 运行时，
 * 只需要 error 上有没有那两个字段。副作用是测试可以直接传普通对象。
 */
export function describeNetworkError(error: unknown): string {
  if (!error || typeof error !== 'object') {
    return '网络异常，请稍后重试'
  }

  const e = error as { code?: string; response?: { status?: number } }

  // 超时。axios 在浏览器侧用 ECONNABORTED 表示超时（ETIMEDOUT 是 Node 侧的码），
  // 两个都认，避免运行时环境差异导致超时被提示成「网络连接失败」。
  if (e.code === 'ECONNABORTED' || e.code === 'ETIMEDOUT') {
    return '请求超时，请检查网络后重试'
  }

  const status = e.response?.status
  if (status === undefined) {
    // 没有响应：请求压根没发出去（断网、DNS 解析失败、混合内容、CORS 预检被拒）
    return '网络连接失败，请检查网络'
  }

  // 401 已被响应拦截器的续期逻辑接管，正常走不到这里
  if (status === 403) return '没有访问权限'
  if (status === 404) return '请求的接口不存在'
  if (status === 408) return '请求超时，请稍后重试'
  if (status === 429) return '操作过于频繁，请稍后再试'
  if (status >= 500) return '服务器异常，请稍后重试'
  return `请求失败（HTTP ${status}）`
}
