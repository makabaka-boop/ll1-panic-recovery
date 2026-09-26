<script setup>
import { ref, computed, reactive } from 'vue'

// ---- 编辑器状态 ----
const grammarText = ref(`E -> T R
R -> a T R
R ->
T -> b`)
const startSymbol = ref('E')
const inputText = ref('b a b')

// ---- 请求与结果 ----
const loading = ref(false)
const errorMessage = ref('')
const result = ref(null)
let requestSeq = 0                 // 单调递增的请求版本号
const resultSeq = ref(0)
const editVersion = ref(0)        // 每次编辑自增，用于提示当前结果已过期
const resultEditVersion = ref(0)

const stale = computed(() => result.value !== null && resultEditVersion.value !== editVersion.value)

const tokens = computed(() => inputText.value.trim().split(/\s+/).filter(Boolean))

const columns = computed(() => {
  if (!result.value) return []
  return ['$', ...result.value.terminals]
})

const prodById = computed(() => {
  const m = new Map()
  result.value?.productions.forEach(p => m.set(p.id, p))
  return m
})

function rhsDisplay(p) {
  if (p.epsilon) return 'ε'
  return p.rhs.split('').join(' ')
}

function cellText(ids) {
  return ids.map(id => {
    const p = prodById.value.get(id)
    return p ? `${id}` : String(id)
  }).join(', ')
}

function isConflictCell(nt, col) {
  const c = result.value?.conflict
  return c && c.nonterminal === nt && c.terminal === col
}

function cellRuleText(ids) {
  return ids.map(id => {
    const p = prodById.value.get(id)
    return p ? `${id}: ${p.head} -> ${rhsDisplay(p)}` : String(id)
  }).join('\n')
}

function touch() {
  editVersion.value++
}

async function submit() {
  errorMessage.value = ''
  const seq = ++requestSeq
  loading.value = true
  try {
    const resp = await fetch('/api/analyze', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        grammar: grammarText.value,
        start: startSymbol.value,
        tokens: tokens.value
      })
    })
    const data = await resp.json()
    // 只接受当前请求版本：过期响应一律丢弃，避免页面混入旧推导。
    if (seq !== requestSeq) return
    if (!resp.ok) {
      result.value = null
      errorMessage.value = data.error || `服务端返回 ${resp.status}`
      return
    }
    result.value = data
    resultSeq.value = seq
    resultEditVersion.value = editVersion.value
  } catch (e) {
    if (seq !== requestSeq) return
    result.value = null
    errorMessage.value = '无法连接 grammar 服务：' + e.message
  } finally {
    if (seq === requestSeq) loading.value = false
  }
}

function loadSample(kind) {
  if (kind === 'll1') {
    grammarText.value = `E -> T R
R -> a T R
R ->
T -> b`
    startSymbol.value = 'E'
    inputText.value = 'b a b'
  } else {
    // 悬空 else 的单字母版本：Q 的 e 列必然冲突
    grammarText.value = `P -> i b P Q
Q -> e P
Q ->
P -> s`
    startSymbol.value = 'P'
    inputText.value = 'i b s e s'
  }
  touch()
}

function rowClass(step) {
  const r = result.value
  if (!r) return ''
  if (step.action === '接受') return 'accept'
  if (r.error && r.error.step === step.step) return 'fail'
  return ''
}
</script>

<template>
  <header>
    <h1>LL(1) 预测分析教学台</h1>
    <p>迭代求解 FIRST / FOLLOW（ε 不进 FOLLOW）· 冲突时只报最小冲突格与竞争规则，不伪造解析回放</p>
  </header>

  <main>
    <!-- 左栏：编辑 -->
    <section>
      <div class="panel">
        <h2>文法与输入</h2>
        <label class="field">
          产生式（大写非终结符，小写终结符；空右部即 ε，可用 | 并列）
          <textarea
            data-testid="grammar-input"
            v-model="grammarText"
            @input="touch"
            spellcheck="false"
          ></textarea>
        </label>
        <label class="field">
          开始符
          <input
            class="start"
            type="text"
            maxlength="1"
            data-testid="start-input"
            v-model="startSymbol"
            @input="touch"
          />
        </label>
        <label class="field">
          词序列（空白分隔，至多 40 项，每项一个小写终结符）
          <input
            type="text"
            data-testid="tokens-input"
            v-model="inputText"
            @input="touch"
          />
        </label>
        <button data-testid="analyze-btn" :disabled="loading" @click="submit">
          {{ loading ? '计算中…' : '计算并分析' }}
        </button>
        <button class="secondary" @click="loadSample('ll1')">载入 LL(1) 示例</button>
        <button class="secondary" @click="loadSample('conflict')">载入冲突示例</button>
        <p class="hint">
          约束：1～15 个大写非终结符、至多 30 条不同产生式。ε 仅以空右部表达，
          FIRST 中的 ε 与 FOLLOW 严格分开。
        </p>
      </div>

      <div v-if="errorMessage" class="error-banner" data-testid="error-banner">{{ errorMessage }}</div>
    </section>

    <!-- 右栏：服务端推导结果 -->
    <section>
      <div v-if="stale" class="stale-warn" data-testid="stale-warn">
        输入已修改：以下为请求版本 #{{ resultSeq }} 的服务端推导，重新提交后才会刷新。
      </div>

      <div v-if="!result && !errorMessage && !loading" class="panel hint">
        提交后此处展示服务端返回的 FIRST / FOLLOW、LL(1) 分析表与栈回放。
      </div>

      <template v-if="result">
        <!-- 状态 -->
        <div class="panel" data-testid="status-panel">
          <h2>分析结论
            <span v-if="result.status === 'conflict'" class="badge conflict" data-testid="badge-conflict">
              非 LL(1)：存在冲突格
            </span>
            <span v-else-if="result.accepted" class="badge ok" data-testid="badge-accepted">接受</span>
            <span v-else class="badge no" data-testid="badge-rejected">不接受</span>
          </h2>
          <div class="meta">开始符：{{ result.start }} ｜ 非终结符：{{ result.nonterminals.join(' ') }}
            ｜ 终结符：{{ result.terminals.join(' ') }} ｜ 请求版本 #{{ resultSeq }}</div>
        </div>

        <!-- 产生式 -->
        <div class="panel">
          <h2>产生式（编号即表内引用）</h2>
          <div class="rules" data-testid="rules">
            <div v-for="p in result.productions" :key="p.id">
              <span class="rid">{{ p.id }}.</span>{{ p.head }} -&gt;
              <span :class="{ eps: p.epsilon }">{{ rhsDisplay(p) }}</span>
            </div>
          </div>
        </div>

        <!-- FIRST / FOLLOW -->
        <div class="panel">
          <h2>FIRST 与 FOLLOW（迭代至不动点）</h2>
          <table class="sets" data-testid="sets-table">
            <thead><tr><th>N</th><th>FIRST</th><th>FOLLOW</th></tr></thead>
            <tbody>
              <tr v-for="nt in result.nonterminals" :key="nt">
                <td>{{ nt }}</td>
                <td>{{ result.first[nt].join(' ') }}</td>
                <td>{{ result.follow[nt].join(' ') }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- LL(1) 表 -->
        <div class="panel">
          <h2>LL(1) 预测分析表 M[N, a]</h2>
          <table class="ll1" data-testid="ll1-table">
            <thead>
              <tr>
                <th>N ＼ a</th>
                <th v-for="col in columns" :key="col">{{ col }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="nt in result.nonterminals" :key="nt">
                <th>{{ nt }}</th>
                <td
                  v-for="col in columns"
                  :key="col"
                  :class="{ 'cell-conflict': isConflictCell(nt, col) }"
                  :title="result.table[nt] && result.table[nt][col] ? cellRuleText(result.table[nt][col]) : ''"
                >
                  {{ result.table[nt] && result.table[nt][col] ? cellText(result.table[nt][col]) : '' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- 冲突详情：此时不回放解析 -->
        <div v-if="result.conflict" class="panel" data-testid="conflict-panel">
          <h2>按字节序最小的冲突格</h2>
          <p class="rules">
            <strong>M[{{ result.conflict.nonterminal }}, {{ result.conflict.terminal }}]</strong>
            同时对应 {{ result.conflict.productionIds.length }} 条产生式：
          </p>
          <div class="rules" data-testid="conflict-rules">
            <div v-for="rule in result.conflict.rules" :key="rule">• {{ rule }}</div>
          </div>
          <p class="hint">该文法不是 LL(1)：服务端不会为其生成任何解析回放（避免伪造推导步骤）。</p>
        </div>

        <!-- 无冲突：栈回放 -->
        <div v-else class="panel" data-testid="trace-panel">
          <h2>预测分析回放（栈顶在左）</h2>
          <div v-if="result.error" class="error-banner" data-testid="failure-banner" style="margin-bottom:10px">
            第 {{ result.error.step }} 步失败：{{ result.error.reason }}
            <template v-if="result.error.token">（当前词：{{ result.error.token }}）</template>
          </div>
          <ol class="trace">
            <li class="head"><span>步骤</span><span>栈</span><span>剩余输入</span><span>动作</span></li>
            <li
              v-for="s in result.trace"
              :key="s.step"
              :class="rowClass(s)"
              :data-testid="s.action === '接受' ? 'trace-accept' : (result.error && result.error.step === s.step ? 'trace-fail' : 'trace-row')"
            >
              <span>{{ s.step }}</span>
              <span>{{ s.stack }}</span>
              <span>{{ s.input }}</span>
              <span>{{ s.action }}</span>
            </li>
          </ol>
        </div>
      </template>
    </section>
  </main>
</template>
