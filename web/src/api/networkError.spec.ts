import { describe, expect, it } from 'vitest'
import { describeNetworkError } from './networkError'

/**
 * 传输层错误提示的单元测试。
 *
 * 守的是「界面上不出现英文原文」：改动前这里是
 * `ElMessage.error(error.message || '网络错误')`，于是断网时用户看到的是
 * `Network Error`，超时看到 `timeout of 30000ms exceeded`，
 * 服务端 500 看到 `Request failed with status code 500` ——
 * 对中文用户零信息量，也不知道该做什么。
 *
 * 因此除了逐条映射，末尾还有一条「任何输入都不该原样透出英文」的总断言：
 * 单看逐条用例，很容易在新增分支时又漏回 `return error.message`。
 */

describe('describeNetworkError', () => {
  it('超时（ECONNABORTED）给出可行动的提示', () => {
    expect(
      describeNetworkError({ code: 'ECONNABORTED', message: 'timeout of 30000ms exceeded' }),
    ).toBe('请求超时，请检查网络后重试')
  })

  it('Node 侧的 ETIMEDOUT 同样按超时处理', () => {
    expect(describeNetworkError({ code: 'ETIMEDOUT' })).toBe('请求超时，请检查网络后重试')
  })

  it('没有 response 说明请求没发出去 —— 提示检查网络', () => {
    // 这正是改动前会显示 "Network Error" 的场景
    expect(describeNetworkError({ message: 'Network Error' })).toBe('网络连接失败，请检查网络')
  })

  it('403 / 404 / 429 各有明确中文', () => {
    expect(describeNetworkError({ response: { status: 403 } })).toBe('没有访问权限')
    expect(describeNetworkError({ response: { status: 404 } })).toBe('请求的接口不存在')
    expect(describeNetworkError({ response: { status: 429 } })).toBe('操作过于频繁，请稍后再试')
  })

  it('5xx 统一提示服务器异常，而不是把状态码抛给用户', () => {
    expect(describeNetworkError({ response: { status: 500 } })).toBe('服务器异常，请稍后重试')
    expect(describeNetworkError({ response: { status: 502 } })).toBe('服务器异常，请稍后重试')
  })

  it('未覆盖的 4xx 也带上状态码，便于用户报障', () => {
    expect(describeNetworkError({ response: { status: 418 } })).toBe('请求失败（HTTP 418）')
  })

  it('非对象输入不抛错', () => {
    expect(describeNetworkError(undefined)).toBe('网络异常，请稍后重试')
    expect(describeNetworkError(null)).toBe('网络异常，请稍后重试')
    expect(describeNetworkError('boom')).toBe('网络异常，请稍后重试')
  })

  it('任何输入都不会原样透出英文原文', () => {
    const errorMessages = [
      'Network Error',
      'timeout of 30000ms exceeded',
      'Request failed with status code 500',
    ]
    const inputs: unknown[] = [
      { message: errorMessages[0] },
      { code: 'ECONNABORTED', message: errorMessages[1] },
      { message: errorMessages[2], response: { status: 500 } },
      { response: { status: 503 } },
    ]

    for (const input of inputs) {
      const got = describeNetworkError(input)
      for (const raw of errorMessages) {
        expect(got, `不应把 axios 原文透给用户: ${got}`).not.toContain(raw)
      }
      // 提示里不该出现英文单词（`HTTP` 是协议名，属允许项）
      expect(got.replace(/HTTP/g, '')).not.toMatch(/[A-Za-z]{4,}/)
    }
  })
})
