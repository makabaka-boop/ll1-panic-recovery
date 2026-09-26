package main

import (
	"reflect"
	"strings"
	"testing"
)

//  1. 间接 ε 传播：A -> B C，B -> D，D -> ε，C -> ε
//     A 必须经过多轮迭代才可空；FIRST(A) 必须含 x。
func TestIndirectEpsilonPropagation(t *testing.T) {
	g, err := parseGrammar(strings.Join([]string{
		"A -> B C",
		"B -> D",
		"D ->",
		"C ->",
		"A -> x",
	}, "\n"), "A")
	if err != nil {
		t.Fatalf("parseGrammar: %v", err)
	}
	g.computeSets()

	for _, nt := range []string{"A", "B", "C", "D"} {
		if !g.nullable[nt] {
			t.Errorf("nullable[%s] = false，期望 true（间接 ε 传播未收敛）", nt)
		}
	}
	if got := sortedKeys(g.first["A"], terminalLess); !reflect.DeepEqual(got, []string{"x"}) {
		t.Errorf("FIRST(A) = %v，期望 [x]", got)
	}
	for _, nt := range []string{"A", "B", "C", "D"} {
		if _, hasEps := g.follow[nt]["ε"]; hasEps {
			t.Fatalf("FOLLOW(%s) 中混入了 ε", nt)
		}
	}
}

//  2. FOLLOW 引起的冲突（悬空 else 的单字母版本）：
//     P -> i b P Q，Q -> e P | ε，FOLLOW(Q) 含 e，M[Q,e] 有两条规则。
//     若 FOLLOW 迭代传播写错（少轮次 / 把 ε 混入），该冲突会被漏掉或列序失真。
func TestFollowConflict(t *testing.T) {
	g, err := parseGrammar(strings.Join([]string{
		"P -> i b P Q",
		"Q -> e P",
		"Q ->",
		"P -> s",
	}, "\n"), "P")
	if err != nil {
		t.Fatalf("parseGrammar: %v", err)
	}
	g.computeSets()
	g.buildTable()

	if _, hasEps := g.follow["Q"]["ε"]; hasEps {
		t.Fatal("FOLLOW(Q) 不应包含 ε")
	}
	if !g.follow["Q"]["e"] {
		t.Fatal("FOLLOW(Q) 应包含 e（经 Q -> ε 回填）")
	}
	if !g.follow["Q"]["$"] {
		t.Fatal("FOLLOW(Q) 应包含 $")
	}

	conflict := g.findConflict()
	if conflict == nil {
		t.Fatal("期望在 M[Q,e] 检测到冲突，但未检测到")
	}
	if conflict.Nonterminal != "Q" || conflict.Terminal != "e" {
		t.Fatalf("最小冲突格 = (%s,%s)，期望 (Q,e)", conflict.Nonterminal, conflict.Terminal)
	}
	if len(conflict.ProductionIDs) != 2 {
		t.Fatalf("竞争规则数 = %d，期望 2：%v", len(conflict.ProductionIDs), conflict.ProductionIDs)
	}

	// 通过对外 API 再确认：冲突状态下不产生任何伪造的解析回放。
	res, err := analyze(strings.Join([]string{
		"P -> i b P Q",
		"Q -> e P",
		"Q ->",
		"P -> s",
	}, "\n"), "P", []string{"i"}, false)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Status != "conflict" {
		t.Fatalf("status = %q，期望 conflict", res.Status)
	}
	if res.Trace != nil || res.Accepted {
		t.Fatalf("冲突时不应回放解析：trace=%v accepted=%v", res.Trace, res.Accepted)
	}
}

// 3a) 递归文法：左递归 E -> E + T | T 不是 LL(1)，M[E, ...] 多格冲突。
func TestLeftRecursiveGrammarConflict(t *testing.T) {
	text := strings.Join([]string{
		"E -> E a T",
		"E -> T",
		"T -> b",
	}, "\n")
	res, err := analyze(text, "E", []string{"b"}, false)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Status != "conflict" {
		t.Fatalf("左递归文法应报告冲突，status = %q", res.Status)
	}
	if res.Conflict.Nonterminal != "E" {
		t.Fatalf("冲突非终结符 = %s，期望 E（非终结符字节序最小）", res.Conflict.Nonterminal)
	}
	if len(res.Conflict.Rules) != 2 {
		t.Fatalf("竞争规则 = %v，期望 2 条", res.Conflict.Rules)
	}
}

// 3b) 递归文法：右递归算术表达式文法是 LL(1)，接受与拒绝序列都要正确回放。
func TestRightRecursiveGrammarTrace(t *testing.T) {
	text := strings.Join([]string{
		"E -> T R",
		"R -> a T R",
		"R ->",
		"T -> b",
	}, "\n")

	res, err := analyze(text, "E", strings.Split("b a b", " "), false)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Status != "parsed" || !res.Accepted {
		t.Fatalf("期望接受 b a b，status=%s accepted=%v err=%+v", res.Status, res.Accepted, res.Error)
	}
	last := res.Trace[len(res.Trace)-1]
	if last.Action != "接受" {
		t.Errorf("最后一步动作 = %q，期望 接受", last.Action)
	}
	if !strings.HasPrefix(last.Stack, "$") || last.Input != "$" {
		t.Errorf("接受时栈应为 $、剩余输入应为 $，得到 stack=%q input=%q", last.Stack, last.Input)
	}

	// 拒绝序列：M[R, b] 为空，必须在首个失败步骤停下并保留现场。
	res2, err := analyze(text, "E", strings.Split("b b", " "), false)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res2.Accepted || res2.Error == nil {
		t.Fatal("b b 不应被接受，且应返回首个失败步骤")
	}
	if !strings.Contains(res2.Error.Reason, "M[R, b]") {
		t.Errorf("失败原因 = %q，期望指向 M[R, b] 为空", res2.Error.Reason)
	}
	if res2.Error.Step != len(res2.Trace) {
		t.Errorf("失败步骤号 %d 与回放长度 %d 不一致", res2.Error.Step, len(res2.Trace))
	}
}

// 4) 非法词（非小写终结符）在真正匹配前即报第一个失败步骤。
func TestIllegalToken(t *testing.T) {
	text := "E -> b"
	res, err := analyze(text, "E", []string{"B"}, false)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Accepted || res.Error == nil {
		t.Fatal("大写词 B 对小写终结符文法应为非法词")
	}
	if !strings.Contains(res.Error.Reason, "非法词") || res.Error.Token != "B" {
		t.Errorf("失败 = %+v，期望标记非法词 B", res.Error)
	}
	if res.Error.Step != 1 {
		t.Errorf("非法词应在第 1 步失败，实际第 %d 步", res.Error.Step)
	}
}

//  5. 同一格被同一条产生式同时经 FIRST 与 FOLLOW 放入时只算一条，不能误报冲突。
//     S -> A a，A -> b A | ε：A->ε 经 FIRST(ε)=FOLLOW(A) 与尾空回填各放一次到 M[A,a]。
func TestDuplicateProductionInCellNotConflict(t *testing.T) {
	text := strings.Join([]string{
		"S -> A a",
		"A -> b A",
		"A ->",
	}, "\n")
	res, err := analyze(text, "S", []string{"b", "b", "a"}, false)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Status == "conflict" {
		t.Fatalf("同一条 ε 规则重复入格被误报为冲突：%+v", res.Conflict)
	}
	if !res.Accepted {
		t.Fatalf("b b a 应被接受，err=%+v", res.Error)
	}
}

// 6) 上限校验：产生式至多 30 条（不同候选式），输入词至多 40 个。
func TestLimits(t *testing.T) {
	lines := make([]string, 0, 31)
	for i := 0; i < 31; i++ {
		lines = append(lines, "A -> "+string(rune('a'+i)))
	}
	if _, err := parseGrammar(strings.Join(lines, "\n"), "A"); err == nil {
		t.Fatal("31 条不同产生式应报错")
	}
	if _, err := analyze("A -> b", "A", make([]string, 41), false); err == nil {
		t.Fatal("41 个输入词应报错")
	}
}

// ---- 错误恢复回放模式 ----
//
// 恢复测试共用文法：S -> q X d，X -> a。
// FIRST(S)={q}，FIRST(X)={a}，FOLLOW(X)={d}，FOLLOW(S)={$}。
const recoverGrammar = "S -> q X d\nX -> a"

// checkRecoveryProgress 校验「所有动作保证推进」：每次恢复要么栈变短、要么剩余输入变短；
// 且恢复事件与回放步骤是同一事件序列（步骤号、动作文本一致），总步数不超上限。
func checkRecoveryProgress(t *testing.T, res *AnalyzeResult) {
	t.Helper()
	if len(res.Trace) > maxSteps {
		t.Fatalf("回放步数 %d 超过上限 %d", len(res.Trace), maxSteps)
	}
	for _, ev := range res.Recovery {
		before := res.Trace[ev.Step-1]
		if before.Action != ev.Action {
			t.Errorf("恢复事件步骤 %d 与回放动作不一致：%q vs %q", ev.Step, ev.Action, before.Action)
		}
		bStack, aStack := len(strings.Fields(before.Stack)), len(strings.Fields(ev.Stack))
		bInput, aInput := len(strings.Fields(before.Input)), len(strings.Fields(ev.Input))
		if aStack >= bStack && aInput >= bInput {
			t.Errorf("步骤 %d 的恢复未推进：栈 %d→%d，剩余输入 %d→%d", ev.Step, bStack, aStack, bInput, aInput)
		}
	}
}

// lastAction 返回回放最后一步的动作文本。
func lastAction(res *AnalyzeResult) string {
	return res.Trace[len(res.Trace)-1].Action
}

//  7. FOLLOW 同步：输入 q d 缺少 a，M[X, d] 为空且 d ∈ FOLLOW(X)，弹出 X 后继续到结束符。
//     即使走到结束符也必须标「含错误」，不得伪报接受。
func TestRecoveryFollowSync(t *testing.T) {
	res, err := analyze(recoverGrammar, "S", []string{"q", "d"}, true)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Mode != "recover" || res.Status != "parsed" {
		t.Fatalf("mode=%q status=%q，期望 recover/parsed", res.Mode, res.Status)
	}
	if res.Accepted {
		t.Fatal("发生过恢复的输入即使走到结束符也不得接受")
	}
	if res.Error != nil {
		t.Fatalf("恢复模式不应返回首错即停的 Failure：%+v", res.Error)
	}
	if len(res.Recovery) != 1 {
		t.Fatalf("恢复事件数 = %d，期望 1：%+v", len(res.Recovery), res.Recovery)
	}
	ev := res.Recovery[0]
	if ev.TokenIndex != 1 || ev.Token != "d" || ev.StackTop != "X" {
		t.Errorf("事件 = %+v，期望词下标 1、词 d、原栈顶 X", ev)
	}
	if !strings.Contains(ev.Action, "FOLLOW") || !strings.Contains(ev.Action, "弹出 X") {
		t.Errorf("恢复动作 = %q，期望说明 d ∈ FOLLOW(X) 并弹出 X", ev.Action)
	}
	if ev.Stack != "d $" || ev.Input != "d $" {
		t.Errorf("恢复后现场 = 栈 %q 输入 %q，期望 d $ / d $", ev.Stack, ev.Input)
	}
	if got := lastAction(res); !strings.Contains(got, "含错误") {
		t.Errorf("最后一步动作 = %q，期望标注含错误", got)
	}
	// 恢复后继续正常推导：第 4 步应匹配 d。
	if res.Trace[3].Action != "匹配 d" {
		t.Errorf("恢复后第 4 步 = %q，期望 匹配 d", res.Trace[3].Action)
	}
	checkRecoveryProgress(t, res)
}

// 8) 连续错误：q x y d 中 x、y 连续两个错误词都被逐个丢弃，随后 FOLLOW 同步弹出 X。
func TestRecoveryConsecutiveErrors(t *testing.T) {
	res, err := analyze(recoverGrammar, "S", []string{"q", "x", "y", "d"}, true)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Accepted || len(res.Recovery) != 3 {
		t.Fatalf("accepted=%v 事件数=%d，期望 false/3", res.Accepted, len(res.Recovery))
	}
	for i, ev := range res.Recovery[:2] {
		if !strings.Contains(ev.Action, "丢弃当前词") {
			t.Errorf("事件 %d 动作 = %q，期望丢弃当前词", i, ev.Action)
		}
		if ev.TokenIndex != i+1 {
			t.Errorf("事件 %d 词下标 = %d，期望 %d", i, ev.TokenIndex, i+1)
		}
	}
	if !strings.Contains(res.Recovery[2].Action, "弹出 X") {
		t.Errorf("第 3 次恢复 = %q，期望 FOLLOW 同步弹出 X", res.Recovery[2].Action)
	}
	// 丢弃 x 后剩余输入应从 y 开始。
	if res.Recovery[0].Input != "y d $" {
		t.Errorf("丢弃 x 后剩余输入 = %q，期望 y d $", res.Recovery[0].Input)
	}
	if got := lastAction(res); !strings.Contains(got, "含错误") {
		t.Errorf("最后一步动作 = %q，期望标注含错误", got)
	}
	checkRecoveryProgress(t, res)
}

// 9) 输入耗尽：q 之后输入结束，先按结束符同步弹出 X，再补入终结符 d（弹栈不消费输入）。
func TestRecoveryInputExhausted(t *testing.T) {
	res, err := analyze(recoverGrammar, "S", []string{"q"}, true)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Accepted || len(res.Recovery) != 2 {
		t.Fatalf("accepted=%v 事件数=%d，期望 false/2", res.Accepted, len(res.Recovery))
	}
	pop, insert := res.Recovery[0], res.Recovery[1]
	if pop.Token != "$" || pop.TokenIndex != 1 || pop.StackTop != "X" {
		t.Errorf("结束符同步事件 = %+v，期望词 $、下标 1、栈顶 X", pop)
	}
	if !strings.Contains(pop.Action, "结束符") || !strings.Contains(pop.Action, "弹出 X") {
		t.Errorf("结束符同步动作 = %q", pop.Action)
	}
	if !strings.Contains(insert.Action, "补入 d") || insert.StackTop != "d" {
		t.Errorf("补入事件 = %+v，期望补入 d、原栈顶 d", insert)
	}
	if insert.TokenIndex != 1 {
		t.Errorf("补入不应消费输入，词下标 = %d，期望仍为 1", insert.TokenIndex)
	}
	if insert.Stack != "$" {
		t.Errorf("补入 d 后栈 = %q，期望 $", insert.Stack)
	}
	if got := lastAction(res); !strings.Contains(got, "含错误") {
		t.Errorf("最后一步动作 = %q，期望标注含错误", got)
	}
	checkRecoveryProgress(t, res)
}

//  10. 零长度产生式：ε 规则在恢复模式下照常参与推导；其前的错误词被丢弃。
//     同一文法的合法输入在恢复模式下仍应正常接受、无恢复事件。
func TestRecoveryEpsilonProduction(t *testing.T) {
	text := "S -> A b\nA ->"
	res, err := analyze(text, "S", []string{"x", "b"}, true)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Accepted || len(res.Recovery) != 1 {
		t.Fatalf("accepted=%v 事件数=%d，期望 false/1", res.Accepted, len(res.Recovery))
	}
	ev := res.Recovery[0]
	if ev.TokenIndex != 0 || ev.StackTop != "S" || !strings.Contains(ev.Action, "丢弃") {
		t.Errorf("事件 = %+v，期望词下标 0、栈顶 S、丢弃", ev)
	}
	hasEpsStep := false
	for _, s := range res.Trace {
		if s.Action == "输出 A -> ε" {
			hasEpsStep = true
			if s.ProductionID == nil {
				t.Error("ε 产生式步骤应携带产生式编号")
			}
		}
	}
	if !hasEpsStep {
		t.Error("恢复后应照常输出零长度产生式 A -> ε")
	}
	if got := lastAction(res); !strings.Contains(got, "含错误") {
		t.Errorf("最后一步动作 = %q，期望标注含错误", got)
	}
	checkRecoveryProgress(t, res)

	// 合法输入：恢复模式不改变接受语义。
	clean, err := analyze(text, "S", []string{"b"}, true)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if !clean.Accepted || len(clean.Recovery) != 0 {
		t.Fatalf("合法输入在恢复模式下应接受且无恢复：accepted=%v recovery=%+v", clean.Accepted, clean.Recovery)
	}
	if lastAction(clean) != "接受" {
		t.Errorf("合法输入最后一步 = %q，期望 接受", lastAction(clean))
	}
}

// 11) 输入尾部多余词：栈已空后逐个丢弃，每个词各记一次恢复事件。
func TestRecoveryTailDiscard(t *testing.T) {
	res, err := analyze("S -> a", "S", []string{"a", "b", "c"}, true)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Accepted || len(res.Recovery) != 2 {
		t.Fatalf("accepted=%v 事件数=%d，期望 false/2", res.Accepted, len(res.Recovery))
	}
	for i, ev := range res.Recovery {
		if ev.StackTop != "$" || !strings.Contains(ev.Action, "多余词") {
			t.Errorf("事件 %d = %+v，期望栈顶 $ 且丢弃多余词", i, ev)
		}
		if ev.TokenIndex != i+1 {
			t.Errorf("事件 %d 词下标 = %d，期望 %d", i, ev.TokenIndex, i+1)
		}
	}
	if res.Recovery[0].Input != "c $" {
		t.Errorf("丢弃 b 后剩余输入 = %q，期望 c $", res.Recovery[0].Input)
	}
	checkRecoveryProgress(t, res)
}

// 12) 终止性硬上限：40 个全错词也必须在有限步内结束，且每步恢复都推进。
func TestRecoveryAlwaysTerminates(t *testing.T) {
	tokens := make([]string, 40)
	for i := range tokens {
		tokens[i] = "x"
	}
	res, err := analyze(recoverGrammar, "S", tokens, true)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Accepted {
		t.Fatal("全错输入不得接受")
	}
	if len(res.Recovery) != 41 { // 40 次丢弃 + 结束符同步弹出 S
		t.Fatalf("恢复事件数 = %d，期望 41", len(res.Recovery))
	}
	checkRecoveryProgress(t, res)
}

// 13) 冲突文法：恢复模式不改变冲突结论，不生成回放与恢复事件。
func TestRecoveryConflictKeepsConclusion(t *testing.T) {
	text := strings.Join([]string{
		"P -> i b P Q",
		"Q -> e P",
		"Q ->",
		"P -> s",
	}, "\n")
	res, err := analyze(text, "P", []string{"i"}, true)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Status != "conflict" || res.Conflict == nil {
		t.Fatalf("status=%q，期望 conflict 且带冲突格", res.Status)
	}
	if res.Conflict.Nonterminal != "Q" || res.Conflict.Terminal != "e" {
		t.Errorf("冲突格 = (%s,%s)，期望 (Q,e)", res.Conflict.Nonterminal, res.Conflict.Terminal)
	}
	if res.Trace != nil || len(res.Recovery) != 0 || res.Accepted {
		t.Errorf("冲突时不应有回放/恢复/接受：trace=%v recovery=%v accepted=%v", res.Trace, res.Recovery, res.Accepted)
	}
}

// 14) 旧模式兼容：不开恢复时首错即停语义不变，无恢复事件，mode 为 strict。
func TestStrictModeUnchanged(t *testing.T) {
	res, err := analyze(recoverGrammar, "S", []string{"q", "d"}, false)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if res.Mode != "strict" {
		t.Errorf("mode = %q，期望 strict", res.Mode)
	}
	if res.Accepted || res.Error == nil {
		t.Fatal("普通模式应在首个错误步骤失败")
	}
	if res.Error.Step != 3 || !strings.Contains(res.Error.Reason, "M[X, d]") {
		t.Errorf("失败 = %+v，期望第 3 步 M[X, d] 为空", res.Error)
	}
	if len(res.Trace) != 3 {
		t.Errorf("普通模式应首错即停，回放步数 = %d，期望 3", len(res.Trace))
	}
	if len(res.Recovery) != 0 {
		t.Errorf("普通模式不应产生恢复事件：%+v", res.Recovery)
	}
}
