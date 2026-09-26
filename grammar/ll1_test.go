package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	}, "\n"), "P", []string{"i"})
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
	res, err := analyze(text, "E", []string{"b"})
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

	res, err := analyze(text, "E", strings.Split("b a b", " "))
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
	res2, err := analyze(text, "E", strings.Split("b b", " "))
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
	res, err := analyze(text, "E", []string{"B"})
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
	res, err := analyze(text, "S", []string{"b", "b", "a"})
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
	if _, err := analyze("A -> b", "A", make([]string, 41)); err == nil {
		t.Fatal("41 个输入词应报错")
	}
}

// ---------- 错误恢复回放模式 ----------

// recoverSample 是恢复测试共用的 LL(1) 文法：
// FOLLOW(T) = {a, $}，FOLLOW(R) = {$}，R 有零长度产生式。
func recoverSample() string {
	return strings.Join([]string{
		"E -> T R",
		"R -> a T R",
		"R ->",
		"T -> b",
	}, "\n")
}

// recoverySteps 过滤出统一事件序列中的恢复事件。
func recoverySteps(trace []Step) []Step {
	var out []Step
	for _, s := range trace {
		if s.Recovery != nil {
			out = append(out, s)
		}
	}
	return out
}

// assertRecoveryProgress 校验每个恢复事件：写明恢复后现场且与下一步快照衔接，
// 且 (栈长, 剩余词数) 字典序严格下降（恢复动作必须推进，回放必然终止）。
func assertRecoveryProgress(t *testing.T, trace []Step) {
	t.Helper()
	count := func(s string) int { return len(strings.Fields(s)) }
	for i, s := range trace {
		rec := s.Recovery
		if rec == nil {
			continue
		}
		if i+1 >= len(trace) {
			t.Fatalf("第 %d 步恢复后没有后续步骤，回放未继续", s.Step)
		}
		next := trace[i+1]
		if rec.AfterStack != next.Stack || rec.AfterInput != next.Input {
			t.Errorf("第 %d 步恢复后现场 (%q, %q) 与下一步 (%q, %q) 不衔接",
				s.Step, rec.AfterStack, rec.AfterInput, next.Stack, next.Input)
		}
		if rec.StackTop == "" || rec.Token == "" {
			t.Errorf("第 %d 步恢复缺少原栈顶或当前词：%+v", s.Step, rec)
		}
		bs, bi := count(s.Stack), count(s.Input)
		as, ai := count(rec.AfterStack), count(rec.AfterInput)
		if as > bs || (as == bs && ai >= bi) {
			t.Errorf("第 %d 步恢复未推进：栈 %d->%d，输入 %d->%d", s.Step, bs, as, bi, ai)
		}
	}
}

// 7) FOLLOW 同步：M[T, a] 为空且 a ∈ FOLLOW(T)，弹出非终结符 T 后继续；
//    最终走到结束符也必须标为含错误，不得伪报接受。
func TestRecoverFollowSync(t *testing.T) {
	res, err := analyzeMode(recoverSample(), "E", strings.Split("b a a b", " "), true)
	if err != nil {
		t.Fatalf("analyzeMode: %v", err)
	}
	if res.Status != "parsed" || !res.RecoverMode {
		t.Fatalf("status=%q recoverMode=%v，期望 parsed 且恢复模式开启", res.Status, res.RecoverMode)
	}
	if res.Accepted || !res.HadError || res.RecoveryCount != 1 {
		t.Fatalf("accepted=%v hadError=%v recoveryCount=%d，期望含错误且恰 1 次恢复",
			res.Accepted, res.HadError, res.RecoveryCount)
	}
	if res.Error != nil {
		t.Fatalf("恢复模式不应返回首错即停的 Failure：%+v", res.Error)
	}

	recs := recoverySteps(res.Trace)
	if len(recs) != 1 {
		t.Fatalf("恢复事件数 = %d，期望 1", len(recs))
	}
	rec := recs[0].Recovery
	if rec.Kind != "pop-nonterminal" {
		t.Errorf("恢复类别 = %q，期望 pop-nonterminal", rec.Kind)
	}
	if rec.TokenIndex != 2 || rec.Token != "a" {
		t.Errorf("出错词 = #%d %q，期望 #2 a（b a a b 的第二个 a）", rec.TokenIndex, rec.Token)
	}
	if rec.StackTop != "T" {
		t.Errorf("原栈顶 = %q，期望 T", rec.StackTop)
	}
	if rec.AfterStack != "R $" || rec.AfterInput != "a b $" {
		t.Errorf("恢复后现场 = (%q, %q)，期望 (R $, a b $)", rec.AfterStack, rec.AfterInput)
	}
	if !strings.Contains(recs[0].Action, "FOLLOW(T)") {
		t.Errorf("恢复动作 %q 应说明 a ∈ FOLLOW(T)", recs[0].Action)
	}

	last := res.Trace[len(res.Trace)-1]
	if last.Action == "接受" || !strings.Contains(last.Action, "含错误") {
		t.Errorf("最后一步 = %q，期望标注含错误而非接受", last.Action)
	}
	assertRecoveryProgress(t, res.Trace)
}

// 8) 连续错误：c、d 连续两个词都无法同步，应逐个丢弃并继续，恢复事件相邻。
func TestRecoverConsecutiveErrors(t *testing.T) {
	res, err := analyzeMode(recoverSample(), "E", strings.Split("b c d a b", " "), true)
	if err != nil {
		t.Fatalf("analyzeMode: %v", err)
	}
	if res.Accepted || res.RecoveryCount != 2 {
		t.Fatalf("accepted=%v recoveryCount=%d，期望含错误且恰 2 次恢复", res.Accepted, res.RecoveryCount)
	}
	recs := recoverySteps(res.Trace)
	if len(recs) != 2 {
		t.Fatalf("恢复事件数 = %d，期望 2", len(recs))
	}
	for i, want := range []struct {
		kind  string
		index int
		token string
	}{
		{"skip-token", 1, "c"},
		{"skip-token", 2, "d"},
	} {
		rec := recs[i].Recovery
		if rec.Kind != want.kind || rec.TokenIndex != want.index || rec.Token != want.token {
			t.Errorf("恢复 #%d = (%s, #%d, %q)，期望 (%s, #%d, %q)",
				i, rec.Kind, rec.TokenIndex, rec.Token, want.kind, want.index, want.token)
		}
	}
	// 两次恢复相邻（连续错误逐个处理），且恢复后分析器回到可推导位置完成余下推导。
	if recs[1].Step != recs[0].Step+1 {
		t.Errorf("连续错误的恢复步骤应相邻：%d 与 %d", recs[0].Step, recs[1].Step)
	}
	last := res.Trace[len(res.Trace)-1]
	if !strings.Contains(last.Action, "含错误") {
		t.Errorf("最后一步 = %q，期望标注含错误", last.Action)
	}
	assertRecoveryProgress(t, res.Trace)
}

// 9) 输入耗尽：词序列提前结束时 lookahead 为 $，弹出剩余非终结符直到栈底；
//    到达结束符仍必须标为含错误。
func TestRecoverInputExhausted(t *testing.T) {
	res, err := analyzeMode(recoverSample(), "E", strings.Split("b a", " "), true)
	if err != nil {
		t.Fatalf("analyzeMode: %v", err)
	}
	if res.Accepted || res.RecoveryCount != 1 {
		t.Fatalf("accepted=%v recoveryCount=%d，期望含错误且恰 1 次恢复", res.Accepted, res.RecoveryCount)
	}
	rec := recoverySteps(res.Trace)[0].Recovery
	if rec.Kind != "pop-nonterminal" || rec.StackTop != "T" {
		t.Errorf("恢复 = (%s, 栈顶 %s)，期望在 $ 处弹出 T", rec.Kind, rec.StackTop)
	}
	if rec.Token != "$" || rec.TokenIndex != 2 {
		t.Errorf("结束符位置 = #%d %q，期望 #2 $（等于词数）", rec.TokenIndex, rec.Token)
	}
	if rec.AfterInput != "$" {
		t.Errorf("恢复后剩余输入 = %q，期望 $", rec.AfterInput)
	}
	last := res.Trace[len(res.Trace)-1]
	if last.Input != "$" || !strings.Contains(last.Action, "含错误") {
		t.Errorf("走到结束符的最后一步 = (%q, %q)，必须标含错误", last.Input, last.Action)
	}
	assertRecoveryProgress(t, res.Trace)
}

// 10) 零长度产生式：R -> ε 的展开是正常推导而非恢复；
//     合法输入在恢复模式下仍被接受，且全程无恢复事件。
func TestRecoverEpsilonProduction(t *testing.T) {
	res, err := analyzeMode(recoverSample(), "E", []string{"b"}, true)
	if err != nil {
		t.Fatalf("analyzeMode: %v", err)
	}
	if !res.Accepted || res.HadError || res.RecoveryCount != 0 {
		t.Fatalf("合法输入在恢复模式下应接受且无恢复：accepted=%v hadError=%v count=%d",
			res.Accepted, res.HadError, res.RecoveryCount)
	}
	if len(recoverySteps(res.Trace)) != 0 {
		t.Fatal("合法输入不应出现恢复事件")
	}
	hasEpsilon := false
	for _, s := range res.Trace {
		if strings.Contains(s.Action, "R -> ε") {
			hasEpsilon = true
		}
	}
	if !hasEpsilon {
		t.Error("回放中应出现零长度产生式 R -> ε 的正常展开")
	}
	if last := res.Trace[len(res.Trace)-1]; last.Action != "接受" {
		t.Errorf("最后一步 = %q，期望 接受", last.Action)
	}
}

// 11) 栈顶终结符与当前词不符：补入该终结符并弹栈（不消耗输入）。
func TestRecoverInsertTerminal(t *testing.T) {
	text := strings.Join([]string{
		"S -> a S b",
		"S -> c",
	}, "\n")
	res, err := analyzeMode(text, "S", strings.Split("a c", " "), true)
	if err != nil {
		t.Fatalf("analyzeMode: %v", err)
	}
	if res.Accepted || res.RecoveryCount != 1 {
		t.Fatalf("accepted=%v recoveryCount=%d，期望含错误且恰 1 次恢复", res.Accepted, res.RecoveryCount)
	}
	rec := recoverySteps(res.Trace)[0].Recovery
	if rec.Kind != "insert-terminal" || rec.StackTop != "b" {
		t.Errorf("恢复 = (%s, 栈顶 %s)，期望补入终结符 b", rec.Kind, rec.StackTop)
	}
	if rec.Token != "$" || rec.TokenIndex != 2 {
		t.Errorf("当前词 = #%d %q，期望 #2 $", rec.TokenIndex, rec.Token)
	}
	// 补入不消耗输入：恢复后剩余输入不变，栈弹出 b。
	if rec.AfterStack != "$" || rec.AfterInput != "$" {
		t.Errorf("恢复后现场 = (%q, %q)，期望 ($, $)", rec.AfterStack, rec.AfterInput)
	}
	assertRecoveryProgress(t, res.Trace)
}

// 12) 输入尾部多余词：栈已到栈底仍有余词，逐个丢弃。
func TestRecoverDiscardTail(t *testing.T) {
	text := strings.Join([]string{
		"S -> a S b",
		"S -> c",
	}, "\n")
	res, err := analyzeMode(text, "S", strings.Split("c b b", " "), true)
	if err != nil {
		t.Fatalf("analyzeMode: %v", err)
	}
	if res.Accepted || res.RecoveryCount != 2 {
		t.Fatalf("accepted=%v recoveryCount=%d，期望含错误且恰 2 次恢复", res.Accepted, res.RecoveryCount)
	}
	recs := recoverySteps(res.Trace)
	for i, rec := range recs {
		if rec.Recovery.Kind != "discard-tail" || rec.Recovery.StackTop != "$" {
			t.Errorf("恢复 #%d = (%s, 栈顶 %s)，期望在栈底丢弃多余词", i, rec.Recovery.Kind, rec.Recovery.StackTop)
		}
		if rec.Recovery.Token != "b" || rec.Recovery.TokenIndex != i+1 {
			t.Errorf("恢复 #%d 丢弃词 = #%d %q，期望 #%d b", i, rec.Recovery.TokenIndex, rec.Recovery.Token, i+1)
		}
	}
	assertRecoveryProgress(t, res.Trace)
}

// 13) 非法词：恢复模式下当作无法同步的词丢弃，不中断回放。
func TestRecoverIllegalToken(t *testing.T) {
	res, err := analyzeMode(recoverSample(), "E", []string{"b", "X"}, true)
	if err != nil {
		t.Fatalf("analyzeMode: %v", err)
	}
	if res.Accepted || res.RecoveryCount != 1 {
		t.Fatalf("accepted=%v recoveryCount=%d，期望含错误且恰 1 次恢复", res.Accepted, res.RecoveryCount)
	}
	rec := recoverySteps(res.Trace)[0].Recovery
	if rec.Kind != "skip-token" || rec.Token != "X" || rec.TokenIndex != 1 {
		t.Errorf("恢复 = (%s, #%d, %q)，期望丢弃非法词 #1 X", rec.Kind, rec.TokenIndex, rec.Token)
	}
	assertRecoveryProgress(t, res.Trace)
}

// 14) 冲突文法：恢复模式保持原冲突结论，不做恢复、不生成回放。
func TestRecoverConflictUntouched(t *testing.T) {
	text := strings.Join([]string{
		"P -> i b P Q",
		"Q -> e P",
		"Q ->",
		"P -> s",
	}, "\n")
	res, err := analyzeMode(text, "P", []string{"i"}, true)
	if err != nil {
		t.Fatalf("analyzeMode: %v", err)
	}
	if res.Status != "conflict" || res.Conflict == nil {
		t.Fatalf("冲突文法在恢复模式下仍应只报冲突，status=%q", res.Status)
	}
	if res.Trace != nil || res.Accepted || res.HadError || res.RecoveryCount != 0 {
		t.Fatalf("冲突时不应恢复或回放：trace=%v accepted=%v hadError=%v count=%d",
			res.Trace, res.Accepted, res.HadError, res.RecoveryCount)
	}
	if res.Conflict.Nonterminal != "Q" || res.Conflict.Terminal != "e" {
		t.Errorf("冲突格 = (%s, %s)，期望 (Q, e) 与原结论一致", res.Conflict.Nonterminal, res.Conflict.Terminal)
	}
}

// 15) 旧模式兼容：同一错误输入，普通模式首错即停，恢复模式走完全程；
//     两种模式共享同一事件序列，恢复模式的前缀与普通模式逐步一致。
func TestNormalModeUnchangedByRecovery(t *testing.T) {
	tokens := strings.Split("b a a b", " ")
	normal, err := analyze(recoverSample(), "E", tokens)
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if normal.RecoverMode || normal.HadError || normal.RecoveryCount != 0 {
		t.Fatalf("普通模式不应带恢复标记：%+v", normal)
	}
	if normal.Accepted || normal.Error == nil {
		t.Fatal("普通模式应首错即停并返回 Failure")
	}
	if !strings.Contains(normal.Error.Reason, "M[T, a]") || normal.Error.Step != len(normal.Trace) {
		t.Errorf("普通模式首错 = %+v（回放 %d 步），期望 M[T, a] 为空且停在末步",
			normal.Error, len(normal.Trace))
	}

	rec, err := analyzeMode(recoverSample(), "E", tokens, true)
	if err != nil {
		t.Fatalf("analyzeMode: %v", err)
	}
	if rec.Error != nil || !rec.HadError {
		t.Fatalf("恢复模式不应返回 Failure，且应标记含错误：err=%+v hadError=%v", rec.Error, rec.HadError)
	}
	if len(rec.Trace) <= len(normal.Trace) {
		t.Fatalf("恢复模式回放 %d 步应长过普通模式的 %d 步", len(rec.Trace), len(normal.Trace))
	}
	// 首个错误之前，两种模式的事件序列必须逐步一致。
	for i := 0; i < len(normal.Trace)-1; i++ {
		if !reflect.DeepEqual(normal.Trace[i], rec.Trace[i]) {
			t.Fatalf("第 %d 步两种模式不一致：普通 %+v / 恢复 %+v", i+1, normal.Trace[i], rec.Trace[i])
		}
	}
	// 普通模式的失败现场即恢复模式首个恢复动作的现场。
	failStep := normal.Trace[len(normal.Trace)-1]
	recStep := recoverySteps(rec.Trace)[0]
	if failStep.Stack != recStep.Stack || failStep.Input != recStep.Input {
		t.Errorf("首错现场 (%q, %q) 与恢复现场 (%q, %q) 不一致",
			failStep.Stack, failStep.Input, recStep.Stack, recStep.Input)
	}
}

// 16) HTTP 响应与 Go 分析器使用同一事件序列：recover:true 经 /api/analyze
//     拿到的 trace 与直接调用 analyzeMode 完全一致；缺省 recover 保持旧语义。
func TestRecoverHTTPSameEventSequence(t *testing.T) {
	grammar := recoverSample()
	tokens := []string{"b", "a", "a", "b"}

	direct, err := analyzeMode(grammar, "E", tokens, true)
	if err != nil {
		t.Fatalf("analyzeMode: %v", err)
	}

	body := `{"grammar":"` + strings.ReplaceAll(grammar, "\n", `\n`) + `","start":"E","tokens":["b","a","a","b"],"recover":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handleAnalyze(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", recorder.Code, recorder.Body.String())
	}
	var httpRes AnalyzeResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &httpRes); err != nil {
		t.Fatalf("响应不是合法 JSON：%v", err)
	}
	if !httpRes.RecoverMode || !httpRes.HadError || httpRes.RecoveryCount != 1 {
		t.Errorf("HTTP 恢复标记 = (recoverMode=%v, hadError=%v, count=%d)，期望 (true, true, 1)",
			httpRes.RecoverMode, httpRes.HadError, httpRes.RecoveryCount)
	}
	if !reflect.DeepEqual(httpRes.Trace, direct.Trace) {
		t.Fatal("HTTP 响应的事件序列与 Go 分析器不一致")
	}

	// 缺省 recover：旧模式首错即停。
	body2 := `{"grammar":"` + strings.ReplaceAll(grammar, "\n", `\n`) + `","start":"E","tokens":["b","a","a","b"]}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader(body2))
	recorder2 := httptest.NewRecorder()
	handleAnalyze(recorder2, req2)
	var oldRes AnalyzeResult
	if err := json.Unmarshal(recorder2.Body.Bytes(), &oldRes); err != nil {
		t.Fatalf("响应不是合法 JSON：%v", err)
	}
	if oldRes.RecoverMode || oldRes.HadError || oldRes.Error == nil {
		t.Errorf("缺省 recover 应保持首错即停：recoverMode=%v hadError=%v error=%+v",
			oldRes.RecoverMode, oldRes.HadError, oldRes.Error)
	}
}
