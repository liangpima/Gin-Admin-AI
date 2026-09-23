<template>
  <!-- 触发器：登录表单里只占一行，点击才弹窗并加载验证码图 -->
  <div
    class="click-captcha__trigger"
    :class="{ 'is-verified': verified }"
    role="button"
    :tabindex="verified ? -1 : 0"
    @click="open"
    @keyup.enter="open"
  >
    <el-icon class="click-captcha__trigger-icon">
      <CircleCheck v-if="verified" />
      <Aim v-else />
    </el-icon>
    <span class="click-captcha__trigger-text">
      {{ verified ? '已完成人机验证' : '点击进行人机验证' }}
    </span>
    <el-button v-if="verified" link type="primary" size="small" @click.stop="reset">重新验证</el-button>
  </div>

  <!-- 刻意不设 close-on-click-modal="false"：点遮罩关闭符合弹窗的通用预期，
       关闭后由 onClosed 清掉未完成的点击，重新打开即从零开始 -->
  <el-dialog
    v-model="visible"
    :show-close="false"
    align-center
    append-to-body
    width="min(480px, 88vw)"
    class="click-captcha-dialog"
    @closed="onClosed"
  >
    <div class="click-captcha">
      <div class="click-captcha__canvas">
        <img
          v-if="bgUrl"
          ref="imgRef"
          :src="bgUrl"
          class="click-captcha__img"
          draggable="false"
          @click="handleClick"
        />
        <div v-else class="click-captcha__placeholder">加载中…</div>

        <!-- 编号标记：点击可回到该步重新选择 -->
        <div
          v-for="(point, idx) in displayPoints"
          :key="idx"
          class="click-captcha__mark"
          :style="{ left: point.left + '%', top: point.top + '%' }"
          :title="`点击可回到第 ${idx + 1} 步重新选择`"
          @click.stop="undoFrom(idx)"
        >
          <span>{{ idx + 1 }}</span>
        </div>

        <div v-if="result === 'success'" class="click-captcha__overlay click-captcha__overlay--success">
          <el-icon :size="34"><CircleCheck /></el-icon>
          <span class="click-captcha__overlay-msg">验证成功</span>
        </div>
        <div v-if="result === 'fail'" class="click-captcha__overlay click-captcha__overlay--fail">
          <el-icon :size="34"><CircleClose /></el-icon>
          <span class="click-captcha__overlay-msg">{{ message }}</span>
        </div>
      </div>

      <!-- 提示区：点过的字符变主色，让用户一眼看出进度 -->
      <div class="click-captcha__prompt" :class="{ 'is-error': result === 'fail' }">
        <template v-if="result === 'success'">验证成功</template>
        <template v-else-if="result === 'fail'">{{ message }}</template>
        <template v-else>
          请依次点击
          <span
            v-for="(ch, idx) in chars"
            :key="idx"
            class="click-captcha__char"
            :class="{ 'is-clicked': idx < clickedPoints.length }"
          >{{ ch }}</span>
        </template>
      </div>

      <!-- 刷新按钮：两侧装饰线，视觉上把「换一张」与提示区隔开 -->
      <div class="click-captcha__refresh-box">
        <div class="click-captcha__refresh-line click-captcha__refresh-line--l"></div>
        <button
          type="button"
          class="click-captcha__refresh-btn"
          :disabled="loading"
          title="换一张"
          @click="refresh"
        >⟳</button>
        <div class="click-captcha__refresh-line click-captcha__refresh-line--r"></div>
      </div>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { CircleCheck, CircleClose, Aim } from '@element-plus/icons-vue'
import { getCaptcha, verifyCaptcha, type CaptchaPoint } from '@/api/captcha'

const emit = defineEmits<{
  (e: 'success', token: string): void
}>()

const visible = ref(false)
const loading = ref(false)
const verified = ref(false)
const bgUrl = ref('')
const bgWidth = ref(640)
const bgHeight = ref(200)
const chars = ref('')
const token = ref('')
const clickedPoints = ref<CaptchaPoint[]>([])
const result = ref<'success' | 'fail' | ''>('')
const message = ref('')
const imgRef = ref<HTMLImageElement>()

/**
 * 用百分比定位标记，而不是按显示尺寸换算像素。
 *
 * 图片以 width:100% 自适应弹窗宽度，若按像素定位，窗口尺寸一变
 * 标记就会与图片上的字符错位（弹窗宽度、浏览器缩放都会触发）。
 * 百分比只依赖「点击坐标 / 原图尺寸」，与显示尺寸无关。
 */
const displayPoints = computed(() =>
  clickedPoints.value.map((p) => ({
    left: (p.x / bgWidth.value) * 100,
    top: (p.y / bgHeight.value) * 100,
  })),
)

async function loadCaptcha() {
  loading.value = true
  result.value = ''
  message.value = ''
  clickedPoints.value = []
  try {
    const res = await getCaptcha()
    const data = res.data
    token.value = data.token
    bgUrl.value = data.bg
    bgWidth.value = data.bgWidth
    bgHeight.value = data.bgHeight
    chars.value = data.chars
  } catch {
    message.value = '获取验证码失败'
    result.value = 'fail'
  } finally {
    loading.value = false
  }
}

function open() {
  // 已验证时不再弹窗，避免重复验证；要重验走触发器上的「重新验证」
  if (verified.value) return
  visible.value = true
  if (!bgUrl.value) {
    loadCaptcha()
  }
}

function refresh() {
  loadCaptcha()
}

/**
 * 弹窗关闭后的收尾。
 *
 * 点遮罩关闭等于「取消」：必须清掉未完成的点击，否则重新打开时图上
 * 还留着上次的半截标记，用户不知道还要点几下、也分不清哪些是新的。
 * 图片本身保留（token 5 分钟内有效），重开无需重新请求；
 * 若已过期，Verify 会返回「验证码已过期」并自动换图。
 */
function onClosed() {
  if (verified.value) return
  clickedPoints.value = []
  result.value = ''
  message.value = ''
}

/** 清空验证状态并丢弃当前图，供登录失败后由父组件调用 */
function reset() {
  verified.value = false
  bgUrl.value = ''
  clickedPoints.value = []
  result.value = ''
  message.value = ''
}

function handleClick(e: MouseEvent) {
  if (result.value === 'success' || loading.value) return

  const img = imgRef.value
  if (!img) return

  const rect = img.getBoundingClientRect()
  if (!rect.width || !rect.height) return

  // 显示坐标 → 原图坐标：后端按原图尺寸校验，必须先换算回去
  const x = Math.round(((e.clientX - rect.left) / rect.width) * bgWidth.value)
  const y = Math.round(((e.clientY - rect.top) / rect.height) * bgHeight.value)

  clickedPoints.value.push({ x, y })

  if (clickedPoints.value.length === chars.value.length) {
    verify()
  }
}

/**
 * 点击编号回退：删除该点**及其之后**的所有点。
 *
 * 不能只删中间那一个 —— 点击顺序必须与提示顺序一致，删掉中间点后
 * 后面的点会前移一位，与目标字符的对应关系全部错位，用户再怎么点
 * 都会验证失败（参考实现就是这么做的，属于隐性缺陷）。
 * 「回到第 N 步重选」的语义既直观又不会破坏顺序。
 */
function undoFrom(idx: number) {
  if (result.value === 'success') return
  clickedPoints.value.splice(idx)
}

async function verify() {
  try {
    const res = await verifyCaptcha({
      token: token.value,
      points: clickedPoints.value,
    })
    const data = res.data
    if (data.success) {
      result.value = 'success'
      verified.value = true
      emit('success', data.token)
      // 留一点时间让用户看到成功反馈再关闭
      setTimeout(() => {
        visible.value = false
      }, 700)
    } else {
      result.value = 'fail'
      message.value = data.message || '验证失败'
      setTimeout(() => {
        refresh()
      }, 1200)
    }
  } catch {
    result.value = 'fail'
    message.value = '验证请求失败'
    setTimeout(() => {
      refresh()
    }, 1200)
  }
}

defineExpose({ open, refresh, reset })
</script>

<style lang="scss" scoped>
@use '@/assets/styles/responsive.scss' as *;

.click-captcha__trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  height: 40px;
  padding: 0 12px;
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  background: var(--el-fill-color-blank);
  font-size: 14px;
  color: var(--el-text-color-regular);
  cursor: pointer;
  user-select: none;
  box-sizing: border-box;
  transition: border-color 0.2s;

  &:hover {
    border-color: var(--el-color-primary);
  }

  &.is-verified {
    border-color: var(--el-color-success);
    color: var(--el-color-success);
    cursor: default;
  }
}

.click-captcha__trigger-icon {
  font-size: 16px;
}

.click-captcha__trigger-text {
  flex: 1;
  text-align: left;
}

.click-captcha {
  width: 100%;
}

.click-captcha__canvas {
  position: relative;
  // 按后端出图比例 640:200 先占住高度。图片加载前若没有占位高度，
  // 弹窗会先塌成一条再撑开；用固定 line-height 占位则相反 ——
  // 加载态比成品还高，移动端看起来就是「弹窗很高」然后突然缩回去。
  aspect-ratio: 16 / 5;
  line-height: 0;
  cursor: crosshair;
  user-select: none;
  background: var(--el-fill-color-lighter);
  border-radius: 6px;
  overflow: hidden;
}

.click-captcha__img {
  display: block;
  width: 100%;
  height: auto;
}

.click-captcha__placeholder {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

.click-captcha__mark {
  position: absolute;
  transform: translate(-50%, -50%);
  box-sizing: border-box;
  width: 22px;
  height: 22px;
  line-height: 20px;
  font-size: 12px;
  font-weight: 500;
  text-align: center;
  color: #fff;
  background-color: var(--el-color-primary);
  border: 1px solid #fff;
  border-radius: 50%;
  box-shadow: 0 0 6px rgba(0, 0, 0, 0.35);
  cursor: pointer;

  &:hover {
    background-color: var(--el-color-danger);
  }
}

.click-captcha__overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  line-height: 1.4;
  color: #fff;

  &--success {
    background: rgba(103, 194, 58, 0.85);
  }

  &--fail {
    background: rgba(245, 108, 108, 0.85);
  }
}

.click-captcha__overlay-msg {
  font-size: 14px;
}

.click-captcha__prompt {
  margin-top: 12px;
  font-size: 15px;
  text-align: center;
  color: var(--el-text-color-secondary);

  &.is-error {
    color: var(--el-color-danger);
  }
}

.click-captcha__char {
  margin-left: 8px;
  font-size: 22px;
  font-weight: 500;
  letter-spacing: 1px;
  color: var(--el-color-danger);

  &.is-clicked {
    color: var(--el-color-primary);
  }
}

.click-captcha__refresh-box {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 10px;
}

.click-captcha__refresh-line {
  flex: 1;
  height: 1px;
  background-color: var(--el-border-color-lighter);
}

.click-captcha__refresh-btn {
  width: 32px;
  height: 32px;
  padding: 0;
  font-size: 20px;
  line-height: 30px;
  color: var(--el-text-color-secondary);
  background: transparent;
  border: none;
  border-radius: 50%;
  cursor: pointer;

  &:hover:not(:disabled) {
    color: var(--el-color-primary);
    background: var(--el-fill-color-light);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }
}

@include mobile {
  .click-captcha__mark {
    width: 20px;
    height: 20px;
    line-height: 18px;
  }

  // 移动端垂直空间紧张，提示区与刷新区一并收紧。
  // 图片高度由宽度决定（3.2:1 比例），压不动，能省的就是这两块间距。
  .click-captcha__prompt {
    margin-top: 8px;
    font-size: 14px;
  }

  .click-captcha__char {
    font-size: 20px;
  }

  .click-captcha__refresh-box {
    margin-top: 6px;
  }

  .click-captcha__refresh-btn {
    width: 28px;
    height: 28px;
    font-size: 18px;
    line-height: 26px;
  }
}
</style>

<!--
  弹窗外壳样式必须放在非 scoped 块里。
  el-dialog 带 append-to-body 会把节点 teleport 到 body 下，而 scoped 选择器
  依赖本组件根节点上的 data-v 属性；弹窗外壳不在组件树内，scoped 规则匹配不到，
  所以这里用自定义类名做全局限定（只影响本组件的弹窗，不污染其他对话框）。

  class 能落到 .el-dialog 上：el-dialog 虽然 inheritAttrs: false，
  但显式把 $attrs 透传给了内部的 dialog-content，最终合并到弹窗根节点。
-->
<style lang="scss">
@use '@/assets/styles/responsive.scss' as *;

.click-captcha-dialog {
  // 必须显式居中：align-center 会让 el-dialog 给遮罩层加内联 display:flex，
  // 而项目全局的移动端规则把 margin 覆盖成了 `16px auto !important`
  // （原本是 align-center 依赖的 `margin: auto`）。垂直方向不再是 auto 后，
  // flex 默认的 align-items: stretch 生效，弹窗被拉伸到「视口高度 − 32px」——
  // 内容只有 180px 却撑成 668px，这就是移动端「弹窗特别高」的真正原因。
  // align-self: center 让它回到内容高度并居中，且不影响其他弹窗
  //（项目里只有本组件用了 align-center）。
  align-self: center;

  // el-dialog 的 <header> 是**无条件渲染**的：没有标题也会渲染一个空标题行，
  // 加上 padding-bottom 白占约 43px。移动端这占了整个弹窗近 1/5 高度。
  .el-dialog__header {
    display: none;
  }

  // 弹窗自身已有 padding: var(--el-dialog-padding-primary)，body 再补左右下
  // 就会变成双重内边距（上下各多 16px），所以只保留顶部 10px ——
  // 图片紧贴弹窗上沿会显得局促。左右与底部的间距由弹窗 padding 提供。
  //
  // 必须带 !important：项目全局在移动端有
  // `.el-dialog__body { padding: 12px 16px !important }`。
  // 本选择器多一个类、优先级更高，同为 !important 时由本规则胜出。
  .el-dialog__body {
    padding: 10px 0 0 !important;
  }
}

// 移动端：弹窗内边距 12px（全局约定是 92% 宽度，这里把内边距调紧一点）
@include mobile {
  .click-captcha-dialog {
    --el-dialog-padding-primary: 12px;
    border-radius: 8px;
  }
}
</style>
