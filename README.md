# LL(1) 预测分析教学台

前端 Vue（desk）编辑文法与词序列，Go HTTP 服务（grammar）迭代求 FIRST/FOLLOW、
构造 LL(1) 表并回放栈推导，Docker Compose 分别运行两个服务。

## 文法约定

- 非终结符：大写字母 `A–Z`（1～15 个，每个右部出现的非终结符都必须有产生式）
- 终结符：小写字母 `a–z`
- 产生式：`A -> α`，一行多条可用 `A -> α | β`；**空右部即 ε**（`A ->`）
- 开始符：单独指定一个大写字母
- 至多 30 条不同产生式；输入词序列至多 40 项，每项为一个小写终结符

## 计算语义（关键）

1. nullable / FIRST / FOLLOW 均以**迭代到不动点**方式求解，间接 ε 传播
   （如 `A -> B C`、`B -> D`、`D -> ε`）也能正确收敛。
2. **ε 只以 nullable 表达，绝不进入 FOLLOW**；FIRST 输出末尾单独标 ε，
   避免把 FIRST 的 ε 情况混进 FOLLOW 导致冲突格与后续步骤失真。
3. 建表：`FIRST(β)` 各列填 `A -> β`；β 可空时在 `FOLLOW(A)` 各列填 `A -> β`。
   同一条规则重复落入同一格只算一次；同一格出现**不同**产生式即冲突。
4. 有冲突时：返回按（非终结符字节序，终结符字节序，`$` 最先）最小的冲突格
   及其全部竞争规则，**不生成任何解析回放**。
5. 无冲突时才运行分析器，逐步回放 栈 / 剩余输入 / 所用规则；非法词或查表为空
   都在**首个失败步骤**停止并保留现场。

## 目录

```
grammar/          Go 服务（核心算法 + net/http，POST /api/analyze，GET /healthz）
  ll1.go          解析、不动点 FIRST/FOLLOW、LL(1) 表、冲突定位、栈回放
  ll1_test.go     间接 ε 传播 / FOLLOW 冲突 / 递归文法 / 非法词 / 去重 / 上限
desk/             Vue 3 + Vite 前端
  src/App.vue     文法与词序列编辑、请求版本隔离、表与回放展示
  e2e/            Playwright 端到端流程（编辑 → 失败栈 → 冲突）
docker-compose.yml  grammar（内部 :8080）+ desk（宿主机 :8081，nginx 反代 /api）
```

## Docker Compose 运行

```bash
docker compose up --build
# 打开 http://localhost:8081
```

desk 容器中的 nginx 把 `/api/` 反代到 `http://grammar:8080`。

## 本地开发

```bash
# 终端 1：grammar 服务
cd grammar && go run .            # :8080

# 终端 2：desk 开发服务器（/api 代理到 localhost:8080）
cd desk && npm install && npm run dev   # :5173
```

## 测试

```bash
# Go 单元测试
cd grammar && CGO_ENABLED=0 go test -v ./...

# Playwright（会自动拉起 go run . 与 vite dev，需要本机有 go）
cd desk && npx playwright install chromium
npm run test:e2e
```

## API 示例

```bash
curl -s -X POST http://localhost:8080/api/analyze \
  -H 'Content-Type: application/json' \
  -d '{"grammar":"E -> T R\nR -> a T R\nR ->\nT -> b","start":"E","tokens":["b","a","b"]}'
```

返回 `status` 为 `conflict`（含最小冲突格 `conflict`，无 `trace`）或
`parsed`（含 `accepted` 与逐步 `trace`；失败时 `error` 指向首个失败步骤）。
