// @ts-check
import { test, expect } from '@playwright/test'

// 一条完整教学流程：
//   编辑文法 -> 提交 -> 查看 FIRST/FOLLOW 与分析表
//   -> 改成不被接受的词序列 -> 查看首个失败步骤与失败栈
//   -> 再次编辑（旧推导应标记过期）-> 载入冲突文法 -> 只报冲突、不出现解析回放
test('从编辑文法到查看失败栈，再到冲突格', async ({ page }) => {
  await page.goto('/')

  // 1) 编辑一条含 ε 产生式的文法并接受合法序列
  await page.getByTestId('grammar-input').fill('E -> A b\nA ->')
  await page.getByTestId('start-input').fill('E')
  await page.getByTestId('tokens-input').fill('b')
  await page.getByTestId('analyze-btn').click()

  await expect(page.getByTestId('badge-accepted')).toBeVisible()
  await expect(page.getByTestId('trace-accept')).toContainText('接受')
  // FIRST(A) 展示 ε，而 FOLLOW(A) 只有 b —— ε 与 FOLLOW 严格分离
  const setsTable = page.getByTestId('sets-table')
  await expect(setsTable).toContainText('ε')
  await expect(setsTable.locator('tr').filter({ hasText: /^A/ })).toContainText('b')

  // 2) 改成不被接受的词序列：x 对 E -> b 而言查表为空，定位首个失败步骤
  await page.getByTestId('tokens-input').fill('x')
  await page.getByTestId('analyze-btn').click()

  await expect(page.getByTestId('badge-rejected')).toBeVisible()
  const failure = page.getByTestId('failure-banner')
  await expect(failure).toContainText('第 1 步失败')
  await expect(failure).toContainText('M[E, x]')

  // 失败栈：唯一的回放行被标记为失败，且保留现场快照
  const failRow = page.getByTestId('trace-fail')
  await expect(failRow).toHaveCount(1)
  await expect(failRow).toContainText('E $')   // 栈
  await expect(failRow).toContainText('x $')   // 剩余输入
  await expect(page.getByTestId('trace-accept')).toHaveCount(0)
  // 3) 接受旧结果之后继续编辑：页面必须提示当前展示的是上一请求版本的服务端推导
  await page.getByTestId('tokens-input').fill('b b')
  await expect(page.getByTestId('stale-warn')).toBeVisible()
  await expect(page.getByTestId('stale-warn')).toContainText('请求版本')

  // 4) 载入悬空 else 冲突文法：出现字节序最小冲突格，竞争规则齐全，且无回放
  await page.getByRole('button', { name: '载入冲突示例' }).click()
  await page.getByTestId('analyze-btn').click()

  await expect(page.getByTestId('badge-conflict')).toBeVisible()
  const conflictPanel = page.getByTestId('conflict-panel')
  await expect(conflictPanel).toContainText('M[Q, e]')
  await expect(page.getByTestId('conflict-rules')).toContainText('Q -> e P')
  await expect(page.getByTestId('conflict-rules')).toContainText('Q -> ε')
  // 冲突时绝不伪造解析结果：没有回放面板，也没有接受标记
  await expect(page.getByTestId('trace-panel')).toHaveCount(0)
  await expect(page.getByTestId('badge-accepted')).toHaveCount(0)
  await expect(page.getByTestId('badge-rejected')).toHaveCount(0)
})
