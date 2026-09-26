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
