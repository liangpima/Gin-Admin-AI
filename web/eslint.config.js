import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import tseslint from 'typescript-eslint'
import prettierConfig from 'eslint-config-prettier/flat'

/**
 * ESLint 扁平配置（eslint 10）。
 *
 * 与后端 golangci-lint 同一套策略：**先把基线跑出来、清零，再转为 CI 阻断**。
 * 后端那次的教训是「观察项失效比没有观察项更危险」—— 所以这里不做
 * `continue-on-error` 那类软性放行，清零之后直接由 CI 强制。
 *
 * ## 关于「格式交给谁」
 *
 * 初版配置想用 `flat/strongly-recommended` 来避开格式规则（理由是格式交给
 * Prettier），但实测**这个假设不成立**：strongly-recommended 依然带
 * `max-attributes-per-line` 等格式规则，本仓库 1794 条告警里 1787 条是格式类，
 * 真正的代码问题被完全淹没 —— 等于配置写了「格式交给 Prettier」，
 * 实际却由 ESLint 在管，两边打架。
 *
 * 正确做法是用 `eslint-config-prettier` 显式关闭所有与 Prettier 冲突的规则，
 * 而不是手写规则清单：清单会随插件版本新增规则而漂移（就是上面那个坑的成因）。
 */
export default tseslint.config(
  {
    ignores: [
      'dist/**',
      'node_modules/**',
      // 这两个是 unplugin 生成的类型声明，不该由人维护，也不该被 lint
      'src/auto-imports.d.ts',
      'src/components.d.ts',
      'src/types/wangeditor.d.ts',
    ],
  },

  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/strongly-recommended'],

  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        // .vue 里的 <script lang="ts"> 需要由 TS 解析器接手
        parser: tseslint.parser,
      },
    },
  },

  {
    rules: {
      // no-undef 对 TS 是多余且有害的：类型层面的未定义由 tsc 负责，
      // 而本项目还用 unplugin-auto-import 注入了 ref/computed 等全局 API，
      // ESLint 看不到它们，会把正常代码报成未定义。
      'no-undef': 'off',

      'vue/multi-word-component-names': 'off', // views 下都是 index.vue
      'vue/require-default-prop': 'off', // 全部用 TS 类型 + withDefaults 表达

      // ── 保留为 warning、由 CI 的 --max-warnings 0 兜住 ──
      //
      // 不直接写成 error：这两条是「风格偏好」而非「写错了」，
      // 编辑器里标黄比标红更合适；而 CI 侧要的是硬门槛，用 --max-warnings 0
      // 表达即可（基线为 0，所以等价于 error）。
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/no-unused-vars': [
        'warn',
        // 下划线开头的参数/变量是有意忽略（例如只取第二参的场景）
        { argsIgnorePattern: '^_', varsIgnorePattern: '^_' },
      ],
    },
  },

  // 必须放在最后：它按「与 Prettier 冲突」的名单关规则，放前面会被上面的覆盖
  prettierConfig
)
