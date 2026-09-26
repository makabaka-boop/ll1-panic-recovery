package main

// LL(1) 预测分析核心逻辑：
//   - 文法解析与校验（1~15 个大写非终结符、至多 30 条产生式、小写终结符、空右部即 ε）
//   - 迭代（不动点）求 nullable / FIRST / FOLLOW，FOLLOW 中绝不放入 ε
//   - 构造 LL(1) 预测分析表，同一格落入多条不同产生式即冲突
//   - 无冲突时回放 栈 / 剩余输入 / 所用规则；非法词或不被接受时返回首个失败步骤

import (
	"fmt"
	"sort"
	"strings"
)

// Production 是去重后编号的产生式，ID 从 1 开始。
type Production struct {
	ID      int    `json:"id"`
	Head    string `json:"head"`
	RHS     string `json:"rhs"` // 空串表示 ε
	Epsilon bool   `json:"epsilon"`
}

// ConflictInfo 描述按 (非终结符, 终结符) 字节序最小的冲突格及其全部竞争规则。
type ConflictInfo struct {
	Nonterminal   string   `json:"nonterminal"`
	Terminal      string   `json:"terminal"` // "$" 表示输入结束符
	ProductionIDs []int    `json:"productionIds"`
	Rules         []string `json:"rules"`
}

// Step 是一次分析动作发生前的栈与剩余输入快照。
type Step struct {
	Step         int    `json:"step"`
	Stack        string `json:"stack"`
	Input        string `json:"input"`
	Action       string `json:"action"`
	ProductionID *int   `json:"productionId,omitempty"`
}

// Failure 是首个失败步骤的信息。
type Failure struct {
	Step   int    `json:"step"`
	Reason string `json:"reason"`
	Token  string `json:"token,omitempty"`
	Stack  string `json:"stack"`
	Input  string `json:"input"`
}

// AnalyzeResult 是 /api/analyze 的完整服务端推导结果。
type AnalyzeResult struct {
	Status       string                      `json:"status"` // "conflict" | "parsed"
	Start        string                      `json:"start"`
	Nonterminals []string                    `json:"nonterminals"`
	Terminals    []string                    `json:"terminals"`
	Productions  []Production                `json:"productions"`
	First        map[string][]string         `json:"first"`
	Follow       map[string][]string         `json:"follow"`
	Table        map[string]map[string][]int `json:"table"`
	Conflict     *ConflictInfo               `json:"conflict"`
	Accepted     bool                        `json:"accepted"`
	Trace        []Step                      `json:"trace"`
	Error        *Failure                    `json:"error"`
}

type rawProd struct {
	head string
	rhs  string
}

type grammar struct {
	nonterminals map[string]bool
	terminals    map[string]bool
	start        string
	prods        []Production
	byHead       map[string][]int
	nullable     map[string]bool
	first        map[string]map[string]bool // 只含终结符；ε 通过 nullable 表达
	follow       map[string]map[string]bool // 只含终结符与 "$"，永不含 ε
	table        map[string]map[string][]int
}

const (
	maxNonterminals = 15
	maxProductions  = 30
	maxTokens       = 40
	maxSteps        = 10000
)

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

// terminalLess 规定列次序："$" 最先，随后按终结符字节序。
func terminalLess(a, b string) bool {
	if a == "$" {
		return b != "$"
	}
	if b == "$" {
		return false
	}
	return a < b
}

func sortedKeys(m map[string]bool, less func(a, b string) bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	if less != nil {
		sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	} else {
		sort.Strings(out)
	}
	return out
}

// parseGrammar 解析形如  A -> α | β  的文法文本（-> 或 → 均可），空候选式表示 ε。
func parseGrammar(text, start string) (*grammar, error) {
	heads := map[string]bool{}
	ntUsed := map[string]bool{}
	terms := map[string]bool{}
	seen := map[string]bool{}
	var raws []rawProd

	for li, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		arrow := strings.Index(line, "->")
		arrowLen := 2
		if p := strings.Index(line, "→"); p >= 0 && (arrow < 0 || p < arrow) {
			arrow, arrowLen = p, len("→")
		}
		if arrow < 0 {
			return nil, fmt.Errorf("第 %d 行缺少产生式箭头 ->：%q", li+1, line)
		}
		head := strings.TrimSpace(line[:arrow])
		if len(head) != 1 || !isUpper(head[0]) {
			return nil, fmt.Errorf("第 %d 行左部必须是单个大写非终结符，得到 %q", li+1, head)
		}
		heads[head] = true
		ntUsed[head] = true

		for _, alt := range strings.Split(line[arrow+arrowLen:], "|") {
			alt = strings.TrimSpace(alt)
			alt = strings.ReplaceAll(alt, " ", "")
			alt = strings.ReplaceAll(alt, "\t", "")
			if alt == "ε" || alt == "ɛ" {
				alt = ""
			} else if strings.ContainsAny(alt, "εɛ") {
				return nil, fmt.Errorf("第 %d 行：ε 只能单独作为整条候选式", li+1)
			}
			for i := 0; i < len(alt); i++ {
				switch c := alt[i]; {
				case isUpper(c):
					ntUsed[string(c)] = true
				case isLower(c):
					terms[string(c)] = true
				default:
					return nil, fmt.Errorf("第 %d 行右部含非法字符 %q（只允许大小写字母，空右部表示 ε）", li+1, string(c))
				}
			}
			key := head + "->" + alt
			if !seen[key] {
				seen[key] = true
				raws = append(raws, rawProd{head: head, rhs: alt})
			}
		}
	}

	if len(raws) == 0 {
		return nil, fmt.Errorf("文法为空：至少需要一条产生式")
	}
	if len(raws) > maxProductions {
		return nil, fmt.Errorf("产生式数量 %d 超过上限 %d", len(raws), maxProductions)
	}
	if len(ntUsed) > maxNonterminals {
		return nil, fmt.Errorf("非终结符数量 %d 超过上限 %d", len(ntUsed), maxNonterminals)
	}
	for nt := range ntUsed {
		if !heads[nt] {
			return nil, fmt.Errorf("非终结符 %s 在右部出现但没有产生式", nt)
		}
	}

	start = strings.TrimSpace(start)
	if len(start) != 1 || !isUpper(start[0]) {
		return nil, fmt.Errorf("开始符必须是单个大写字母")
	}
	if !heads[start] {
		return nil, fmt.Errorf("开始符 %s 没有产生式", start)
	}

	g := &grammar{
		nonterminals: heads,
		terminals:    terms,
		start:        start,
		byHead:       map[string][]int{},
	}
	for i, r := range raws {
		p := Production{ID: i + 1, Head: r.head, RHS: r.rhs, Epsilon: r.rhs == ""}
		g.prods = append(g.prods, p)
		g.byHead[r.head] = append(g.byHead[r.head], p.ID)
	}
	return g, nil
}

// firstSeq 求符号串的 FIRST：终结符集合，以及整串是否可空。
func (g *grammar) firstSeq(s string) (map[string]bool, bool) {
	res := map[string]bool{}
	allNullable := true
	for i := 0; i < len(s); i++ {
		x := string(s[i])
		if g.terminals[x] {
			res[x] = true
			allNullable = false
			break
		}
		for t := range g.first[x] {
			res[t] = true
		}
		if !g.nullable[x] {
			allNullable = false
			break
		}
	}
	return res, allNullable
}

// computeSets 以不动点迭代方式求 nullable / FIRST / FOLLOW，
// 间接 ε 传播与 FOLLOW 传播均可在多轮迭代后收敛。
func (g *grammar) computeSets() {
	nts := sortedKeys(g.nonterminals, nil)
	g.nullable = map[string]bool{}
	g.first = map[string]map[string]bool{}
	for _, nt := range nts {
		g.first[nt] = map[string]bool{}
	}

	// nullable 与 FIRST 联合迭代：
	// A -> β，β 中终结符直接入 FIRST(A)；非终结符并入其 FIRST，
	// 仅当其可空时继续向右看；整条 β 可空则 A 可空（ε 进 FIRST(A)，由 nullable 表达）。
	for changed := true; changed; {
		changed = false
		for _, p := range g.prods {
			allNullable := true
			for i := 0; i < len(p.RHS); i++ {
				x := string(p.RHS[i])
				if g.terminals[x] {
					if !g.first[p.Head][x] {
						g.first[p.Head][x] = true
						changed = true
					}
					allNullable = false
					break
				}
				for t := range g.first[x] {
					if !g.first[p.Head][t] {
						g.first[p.Head][t] = true
						changed = true
					}
				}
				if !g.nullable[x] {
					allNullable = false
					break
				}
			}
			if allNullable && !g.nullable[p.Head] {
				g.nullable[p.Head] = true
				changed = true
			}
		}
	}

	// FOLLOW：FOLLOW(start) 含 "$"；
	// 对 A -> αBβ，FIRST(β) 入 FOLLOW(B)；β 可空时 FOLLOW(A) 入 FOLLOW(B)。
	// FOLLOW 只收集终结符与 "$"，ε 从不进入 FOLLOW。
	g.follow = map[string]map[string]bool{}
	for _, nt := range nts {
		g.follow[nt] = map[string]bool{}
	}
	g.follow[g.start]["$"] = true
	for changed := true; changed; {
		changed = false
		addAll := func(dst, src map[string]bool) {
			for t := range src {
				if t != "ε" && !dst[t] {
					dst[t] = true
					changed = true
				}
			}
		}
		for _, p := range g.prods {
			for i := 0; i < len(p.RHS); i++ {
				x := string(p.RHS[i])
				if !g.nonterminals[x] {
					continue
				}
				terms, tailNullable := g.firstSeq(p.RHS[i+1:])
				addAll(g.follow[x], terms)
				if tailNullable {
					addAll(g.follow[x], g.follow[p.Head])
				}
			}
		}
	}
}

func appendUniqueID(cell []int, id int) []int {
	for _, existing := range cell {
		if existing == id {
			return cell
		}
	}
	return append(cell, id)
}

// buildTable 填充预测分析表：FIRST(β) 各列放 A->β；β 可空时在 FOLLOW(A) 各列放 A->β。
func (g *grammar) buildTable() {
	g.table = map[string]map[string][]int{}
	for _, p := range g.prods {
		terms, nullable := g.firstSeq(p.RHS)
		put := func(t string) {
			if g.table[p.Head] == nil {
				g.table[p.Head] = map[string][]int{}
			}
			g.table[p.Head][t] = appendUniqueID(g.table[p.Head][t], p.ID)
		}
		for t := range terms {
			put(t)
		}
		if nullable {
			for b := range g.follow[p.Head] {
				put(b)
			}
		}
	}
}

// findConflict 按非终结符字节序、再按终结符字节序（"$" 最先）扫描，
// 返回第一个含多条不同产生式的格及全部竞争规则。
func (g *grammar) findConflict() *ConflictInfo {
	columns := append(append([]string{}, sortedKeys(g.terminals, terminalLess)...), "$")
	for _, nt := range sortedKeys(g.nonterminals, nil) {
		for _, col := range columns {
			cell := g.table[nt][col]
			if len(cell) >= 2 {
				ids := append([]int{}, cell...)
				rules := make([]string, 0, len(ids))
				for _, id := range ids {
					p := g.prods[id-1]
					rules = append(rules, fmt.Sprintf("%s -> %s", p.Head, rhsDisplay(p.RHS)))
				}
				return &ConflictInfo{
					Nonterminal:   nt,
					Terminal:      col,
					ProductionIDs: ids,
					Rules:         rules,
				}
			}
		}
	}
	return nil
}

func rhsDisplay(rhs string) string {
	if rhs == "" {
		return "ε"
	}
	return strings.Join(strings.Split(rhs, ""), " ")
}

func (g *grammar) stackText(stack []string) string {
	parts := make([]string, len(stack))
	for i := range stack {
		parts[len(stack)-1-i] = stack[i] // 栈顶在左，栈底 "$" 在右
	}
	return strings.Join(parts, " ")
}

func inputText(tokens []string, ip int) string {
	parts := append(append([]string{}, tokens[ip:]...), "$")
	return strings.Join(parts, " ")
}

// runParser 仅在无冲突时调用，逐格查表回放；失败立即返回首个失败步骤，不伪造后续结果。
func (g *grammar) runParser(tokens []string) (bool, []Step, *Failure) {
	stack := []string{"$", g.start}
	ip := 0
	var trace []Step

	record := func(action string, pid *int) Step {
		return Step{
			Step:         len(trace) + 1,
			Stack:        g.stackText(stack),
			Input:        inputText(tokens, ip),
			Action:       action,
			ProductionID: pid,
		}
	}
	fail := func(reason, token string) (bool, []Step, *Failure) {
		st := record(reason, nil)
		trace = append(trace, st)
		f := &Failure{Step: st.Step, Reason: reason, Stack: st.Stack, Input: st.Input}
		if token != "" {
			f.Token = token
		}
		return false, trace, f
	}

	for {
		if len(trace) >= maxSteps {
			return fail(fmt.Sprintf("分析步骤超过 %d，文法可能无法终止", maxSteps), "")
		}
		lookahead := "$"
		if ip < len(tokens) {
			lookahead = tokens[ip]
		}
		// 词法检查：词序列中的每一项必须是单个小写终结符。
		if lookahead != "$" && !(len(lookahead) == 1 && isLower(lookahead[0])) {
			return fail(fmt.Sprintf("非法词：%q 不是单个小写终结符", lookahead), lookahead)
		}

		top := stack[len(stack)-1]
		switch {
		case top == "$":
			if lookahead == "$" {
				trace = append(trace, record("接受", nil))
				return true, trace, nil
			}
			return fail("栈已到栈底但仍有未消费输入，输入不被接受", lookahead)
		case g.terminals[top]:
			if top == lookahead {
				pid := (*int)(nil)
				trace = append(trace, record("匹配 "+top, pid))
				stack = stack[:len(stack)-1]
				ip++
				continue
			}
			return fail(fmt.Sprintf("栈顶终结符 %s 与当前输入 %s 不匹配，输入不被接受", top, lookahead), lookahead)
		default:
			cell := g.table[top][lookahead]
			if len(cell) == 0 {
				return fail(fmt.Sprintf("分析表 M[%s, %s] 为空，输入不被接受", top, lookahead), lookahead)
			}
			p := g.prods[cell[0]-1]
			id := p.ID
			trace = append(trace, record(fmt.Sprintf("输出 %s -> %s", p.Head, rhsDisplay(p.RHS)), &id))
			stack = stack[:len(stack)-1]
			for i := len(p.RHS) - 1; i >= 0; i-- { // 逆序压栈，保证最左符号在栈顶
				stack = append(stack, string(p.RHS[i]))
			}
		}
	}
}

// analyze 是入口：校验词序列长度，求集合、建表、定位冲突；
// 有冲突时只返回表与冲突，不生成解析回放；无冲突时才运行分析器。
func analyze(text, start string, tokens []string) (*AnalyzeResult, error) {
	if len(tokens) > maxTokens {
		return nil, fmt.Errorf("词序列长度 %d 超过上限 %d", len(tokens), maxTokens)
	}
	g, err := parseGrammar(text, start)
	if err != nil {
		return nil, err
	}
	g.computeSets()
	g.buildTable()

	res := &AnalyzeResult{
		Start:        g.start,
		Nonterminals: sortedKeys(g.nonterminals, nil),
		Terminals:    sortedKeys(g.terminals, terminalLess),
		Productions:  append([]Production{}, g.prods...),
		First:        map[string][]string{},
		Follow:       map[string][]string{},
		Table:        map[string]map[string][]int{},
	}
	for _, nt := range res.Nonterminals {
		fs := sortedKeys(g.first[nt], terminalLess)
		if g.nullable[nt] {
			fs = append(fs, "ε")
		}
		res.First[nt] = fs
		res.Follow[nt] = sortedKeys(g.follow[nt], terminalLess)
	}
	for nt, row := range g.table {
		cols := sortedKeys(mapKeys(row), terminalLess)
		res.Table[nt] = map[string][]int{}
		for _, col := range cols {
			res.Table[nt][col] = append([]int{}, row[col]...)
		}
	}

	if conflict := g.findConflict(); conflict != nil {
		res.Status = "conflict"
		res.Conflict = conflict
		return res, nil // 冲突时不伪造解析结果
	}

	res.Status = "parsed"
	res.Accepted, res.Trace, res.Error = g.runParser(tokens)
	return res, nil
}

func mapKeys(m map[string][]int) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}
