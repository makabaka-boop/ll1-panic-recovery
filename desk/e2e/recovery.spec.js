// @ts-check
import { test, expect } from '@playwright/test'

// 错误恢复回放模式的端到端流程：
//   连续错误 / FOLLOW 同步 / 输入耗尽 / 零长度产生式 / 旧模式兼容。
// 恢复测试共用文法：S -> q X d，X -> a（FOLLOW(X) = {d}）。
const RECOVER_GRAMMAR = 'S -> q X d\nX -> a'

async function submit(page, { grammar, start, tokens, recover }) {
  await page.getByTestId('grammar-input').fill(grammar)
  await page.getByTestId('start-input').fill(start)
  await page.getByTestId('tokens-input').fill(tokens)
  const toggle = page.getByTestId('recover-toggle')
  if (recover) await toggle.check()
  else await toggle.uncheck()
  await page.getByTestId('analyze-btn').click()
}

test('FOLLOW 同步：缺词时弹出非终结符并走到结束符，结论标含错误', async ({ page }) => {
  await page.goto('/')
  await submit(page, { grammar: RECOVER_GRAMMAR, start: 'S', tokens: 'q d', recover: true })

  // 结论：含错误，绝不伪报接受
  await expect(page.getByTestId('badge-erroneous')).toBeVisible()
  await expect(page.getByTestId('badge-erroneous')).toContainText('含错误')
  await expect(page.getByTestId('badge-accepted')).toHaveCount(0)
  await expect(page.getByTestId('trace-accept')).toHaveCount(0)

  // 恢复事件：词下标、原栈顶、动作、恢复后的现场齐全
  const events = page.getByTestId('recovery-event')
  await expect(events).toHaveCount(1)
  await expect(events.first()).toContainText('词下标 1（d）')
  await expect(events.first()).toContainText('原栈顶 X')
  await expect(events.first()).toContainText('FOLLOW(X)')
  await expect(events.first()).toContainText('弹出 X')
  await expect(events.first()).toContainText('恢复后 栈：d $')

  // 回放继续：恢复行之后照常匹配 d，收尾行标含错误
  await expect(page.getByTestId('trace-recover')).toHaveCount(1)
  await expect(page.getByTestId('trace-panel')).toContainText('匹配 d')
  await expect(page.getByTestId('trace-errend')).toHaveCount(1)
  await expect(page.getByTestId('trace-errend')).toContainText('含错误')
})

test('连续错误：两个错误词被逐个丢弃，恢复事件按序记录', async ({ page }) => {
  await page.goto('/')
  await submit(page, { grammar: RECOVER_GRAMMAR, start: 'S', tokens: 'q x y d', recover: true })

  await expect(page.getByTestId('badge-erroneous')).toBeVisible()
  const events = page.getByTestId('recovery-event')
  await expect(events).toHaveCount(3) // 丢弃 x、丢弃 y、FOLLOW 同步弹出 X
  await expect(events.nth(0)).toContainText('词下标 1（x）')
  await expect(events.nth(0)).toContainText('丢弃当前词')
  await expect(events.nth(1)).toContainText('词下标 2（y）')
  await expect(events.nth(1)).toContainText('丢弃当前词')
  await expect(events.nth(2)).toContainText('弹出 X')
  await expect(page.getByTestId('trace-recover')).toHaveCount(3)
  await expect(page.getByTestId('trace-errend')).toContainText('含错误')
})

test('输入耗尽：结束符同步弹出非终结符并补入缺失终结符', async ({ page }) => {
  await page.goto('/')
  await submit(page, { grammar: RECOVER_GRAMMAR, start: 'S', tokens: 'q', recover: true })

  await expect(page.getByTestId('badge-erroneous')).toBeVisible()
  const events = page.getByTestId('recovery-event')
  await expect(events).toHaveCount(2)
  await expect(events.nth(0)).toContainText('词下标 1（$）')
  await expect(events.nth(0)).toContainText('弹出 X')
  await expect(events.nth(1)).toContainText('补入 d')
  await expect(events.nth(1)).toContainText('原栈顶 d')
  await expect(events.nth(1)).toContainText('恢复后 栈：$')
  await expect(page.getByTestId('trace-errend')).toContainText('含错误')
  await expect(page.getByTestId('badge-accepted')).toHaveCount(0)
})

test('零长度产生式：ε 规则照常推导，其前错误词被丢弃；合法输入仍接受', async ({ page }) => {
  await page.goto('/')
  await submit(page, { grammar: 'S -> A b\nA ->', start: 'S', tokens: 'x b', recover: true })

  await expect(page.getByTestId('badge-erroneous')).toBeVisible()
  const events = page.getByTestId('recovery-event')
  await expect(events).toHaveCount(1)
  await expect(events.first()).toContainText('词下标 0（x）')
  // 恢复后零长度产生式 A -> ε 照常输出
  await expect(page.getByTestId('trace-panel')).toContainText('输出 A -> ε')
  await expect(page.getByTestId('trace-errend')).toContainText('含错误')

  // 同一文法的合法输入：恢复模式下仍正常接受，无恢复事件
  await submit(page, { grammar: 'S -> A b\nA ->', start: 'S', tokens: 'b', recover: true })
  await expect(page.getByTestId('badge-accepted')).toBeVisible()
  await expect(page.getByTestId('trace-accept')).toContainText('接受')
  await expect(page.getByTestId('recovery-panel')).toHaveCount(0)
})

test('旧模式兼容：不勾选恢复时仍首错即停', async ({ page }) => {
  await page.goto('/')
  await submit(page, { grammar: RECOVER_GRAMMAR, start: 'S', tokens: 'q d', recover: false })

  await expect(page.getByTestId('badge-rejected')).toBeVisible()
  const failure = page.getByTestId('failure-banner')
  await expect(failure).toContainText('第 3 步失败')
  await expect(failure).toContainText('M[X, d]')
  // 首错即停：只有 3 行回放，没有恢复事件与含错误收尾
  await expect(page.getByTestId('trace-fail')).toHaveCount(1)
  await expect(page.locator('ol.trace li')).toHaveCount(1 + 3) // 表头 + 3 步
  await expect(page.getByTestId('recovery-panel')).toHaveCount(0)
  await expect(page.getByTestId('badge-erroneous')).toHaveCount(0)
  await expect(page.getByTestId('trace-errend')).toHaveCount(0)
})

test('冲突文法：恢复模式不改变冲突结论，不生成回放', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: '载入冲突示例' }).click()
  await page.getByTestId('recover-toggle').check()
  await page.getByTestId('analyze-btn').click()

  await expect(page.getByTestId('badge-conflict')).toBeVisible()
  await expect(page.getByTestId('conflict-panel')).toContainText('M[Q, e]')
  await expect(page.getByTestId('trace-panel')).toHaveCount(0)
  await expect(page.getByTestId('recovery-panel')).toHaveCount(0)
  await expect(page.getByTestId('badge-erroneous')).toHaveCount(0)
})
