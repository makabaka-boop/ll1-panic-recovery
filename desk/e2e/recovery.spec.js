// @ts-check
import { test, expect } from '@playwright/test'

// 错误恢复回放模式：显式开启后，分析器在首错之后继续回到可推导位置，
// 回放列表与 Go 分析器、HTTP 响应共用同一事件序列（trace 内嵌 recovery）。
const GRAMMAR = 'E -> T R\nR -> a T R\nR ->\nT -> b'

test.beforeEach(async ({ page }) => {
  await page.goto('/')
  await page.getByTestId('grammar-input').fill(GRAMMAR)
  await page.getByTestId('start-input').fill('E')
})

test('恢复模式：连续错误逐个丢弃，结论含错误而非接受', async ({ page }) => {
  await page.getByTestId('tokens-input').fill('b c d a b')
  await page.getByTestId('recover-toggle').check()
  await page.getByTestId('analyze-btn').click()

  // 结论必须标为含错误，绝不伪报接受
  await expect(page.getByTestId('badge-had-error')).toBeVisible()
  await expect(page.getByTestId('badge-had-error')).toContainText('含错误（已恢复 2 处）')
  await expect(page.getByTestId('badge-accepted')).toHaveCount(0)
  await expect(page.getByTestId('recovery-banner')).toContainText('2 次恢复')

  // 两个相邻的恢复事件：丢弃 c（词下标 1）、丢弃 d（词下标 2）
  const recoveries = page.getByTestId('trace-recovery')
  await expect(recoveries).toHaveCount(2)
  await expect(recoveries.nth(0)).toContainText('丢弃词 c')
  await expect(recoveries.nth(0)).toContainText('词下标 1（c）· 原栈顶 R')
  await expect(recoveries.nth(0)).toContainText('恢复后：栈 R $ ｜ 输入 d a b $')
  await expect(recoveries.nth(1)).toContainText('丢弃词 d')
  await expect(recoveries.nth(1)).toContainText('词下标 2（d）· 原栈顶 R')

  // 恢复后分析器回到可推导位置，余下推导照常出现在同一事件序列
  await expect(page.getByTestId('trace-panel')).toContainText('输出 R -> a T R')
  await expect(page.getByTestId('trace-accept')).toHaveCount(0)
})

test('恢复模式：FOLLOW 同步弹出非终结符后继续推导', async ({ page }) => {
  await page.getByTestId('tokens-input').fill('b a a b')
  await page.getByTestId('recover-toggle').check()
  await page.getByTestId('analyze-btn').click()

  await expect(page.getByTestId('badge-had-error')).toBeVisible()
  const recoveries = page.getByTestId('trace-recovery')
  await expect(recoveries).toHaveCount(1)
  // a ∈ FOLLOW(T)：弹出非终结符 T，并写明词下标、原栈顶与恢复后现场
  await expect(recoveries.nth(0)).toContainText('弹出非终结符 T')
  await expect(recoveries.nth(0)).toContainText('FOLLOW(T)')
  await expect(recoveries.nth(0)).toContainText('词下标 2（a）· 原栈顶 T')
  await expect(recoveries.nth(0)).toContainText('恢复后：栈 R $ ｜ 输入 a b $')
})

test('恢复模式：输入耗尽时在结束符处弹出非终结符，仍标含错误', async ({ page }) => {
  await page.getByTestId('tokens-input').fill('b a')
  await page.getByTestId('recover-toggle').check()
  await page.getByTestId('analyze-btn').click()

  await expect(page.getByTestId('badge-had-error')).toBeVisible()
  const recoveries = page.getByTestId('trace-recovery')
  await expect(recoveries).toHaveCount(1)
  await expect(recoveries.nth(0)).toContainText('输入已结束')
  await expect(recoveries.nth(0)).toContainText('词下标 2（$）· 原栈顶 T')
  // 即使走到结束符也不得伪报接受
  await expect(page.getByTestId('trace-accept')).toHaveCount(0)
  await expect(page.getByTestId('trace-panel')).toContainText('含错误，不接受')
})

test('恢复模式：零长度产生式正常展开，合法输入仍被接受', async ({ page }) => {
  await page.getByTestId('tokens-input').fill('b')
  await page.getByTestId('recover-toggle').check()
  await page.getByTestId('analyze-btn').click()

  // R -> ε 是正常推导而非恢复：无恢复事件，结论为接受
  await expect(page.getByTestId('badge-accepted')).toBeVisible()
  await expect(page.getByTestId('trace-accept')).toBeVisible()
  await expect(page.getByTestId('trace-recovery')).toHaveCount(0)
  await expect(page.getByTestId('trace-panel')).toContainText('输出 R -> ε')
})

test('旧模式兼容：不开恢复开关仍首错即停', async ({ page }) => {
  await page.getByTestId('tokens-input').fill('b a a b')
  // 显式确认开关处于关闭状态（默认即关闭）
  await expect(page.getByTestId('recover-toggle')).not.toBeChecked()
  await page.getByTestId('analyze-btn').click()

  await expect(page.getByTestId('badge-rejected')).toBeVisible()
  await expect(page.getByTestId('failure-banner')).toContainText('M[T, a] 为空')
  await expect(page.getByTestId('trace-fail')).toHaveCount(1)
  await expect(page.getByTestId('trace-recovery')).toHaveCount(0)
  await expect(page.getByTestId('recovery-banner')).toHaveCount(0)
})
