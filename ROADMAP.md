# SiliconVerse AI Chain — AI 可执行任务清单

以下每个任务都是**独立、可验证、有明确验收标准**的工作包。
任何 AI（Claude、GPT、Gemini 等）都可以领取一个任务，实现后提交 PR。

---

## 任务 1：多节点共识压力测试工具

**难度**：⭐⭐⭐ &nbsp; **预计工时**：4-6h &nbsp; **依赖**：无

### 背景
BFT 共识引擎已实现（`internal/consensus/engine/tendermint.go`），但目前只在单节点测试过。
需要一个压力测试工具来验证多节点场景下的正确性。

### 要求
1. 在 `cmd/consensus-test/main.go` 创建测试入口
2. 启动 N 个共识引擎实例（N 可配置，默认 4）
3. 每个实例通过内存 channel 模拟 P2P 通信（Proposal/Prevote/Precommit 消息传递）
4. 模拟以下故障场景：
   - 1 个节点宕机（停止发送消息）
   - 1 个节点发送矛盾投票（双花攻击）
   - 网络延迟（随机 100-500ms 延迟）
5. 输出每个高度达成共识的时间、轮次、投票分布
6. 断言：只要有 ≥2/3 节点诚实，链持续增长且最终一致

### 关键文件
- 新建：`cmd/consensus-test/main.go`
- 参考：`internal/consensus/engine/tendermint.go`
- 参考：`internal/consensus/engine/tendermint_test.go`

### 验收标准
```
$ go run ./cmd/consensus-test/ --nodes=4 --heights=20
[PASS] 20 heights committed in 45.2s
[PASS] All 4 nodes have identical chain
[PASS] 1 node failure: chain continues (18/20 heights ok)
[PASS] Double-vote detected and evidence recorded
```

---

## 任务 2：激励测试网水龙头（Faucet）

**难度**：⭐⭐ &nbsp; **预计工时**：3-4h &nbsp; **依赖**：JSON-RPC API

### 背景
目前测试网的代币分配靠硬编码的创世文件。需要一个水龙头让任何人可以领取测试币。

### 要求
1. 在 `cmd/faucet/main.go` 创建水龙头服务
2. 提供 Web 页面：输入地址 → 验证 → 领取 100 APT + 100 NPT
3. 限制：每 IP 每 24 小时只能领一次
4. 支持 GitHub OAuth 登录（防机器人）
5. 水龙头余额实时显示
6. 提供 `/api/faucet/claim` POST 端点（接受地址，返回 tx hash）

### 关键文件
- 新建：`cmd/faucet/main.go`
- 参考：`internal/api/jsonrpc/endpoints.go`
- 参考：`pkg/token/apt.go`（MintAPT）

### 验收标准
```
$ go run ./cmd/faucet/
[Faucet] Listening on :9090
[Faucet] Balance: 1,000,000 APT, 1,000,000 NPT

$ curl -X POST localhost:9090/api/faucet/claim -d '{"address":"0x..."}'
{"tx_hash":"0xabc...","amount":"100 APT + 100 NPT","remaining_balance":"999,900 APT"}

$ curl -X POST localhost:9090/api/faucet/claim -d '{"address":"0x..."}'
{"error":"rate limited: try again in 23h 59m"}
```

---

## 任务 3：模型注册表 + AI 算力基准测试

**难度**：⭐⭐⭐ &nbsp; **预计工时**：5-7h &nbsp; **依赖**：LM Studio 或 llama.cpp

### 背景
目前矿工注册表（`pkg/compute/registry/miner.go`）只记录硬件规格。需要一个标准化的 AI 模型基准测试系统，让矿工证明自己的真实推理能力。

### 要求
1. 在 `pkg/compute/benchmark/` 创建基准测试模块
2. 定义标准测试用例集：
   - `bench_tiny`: 100 个短问答（预期 < 2 tok/s 则不合格）
   - `bench_medium`: 10 个 500 字文本生成
   - `bench_large`: 1 个 2000 字长文生成
3. 矿工运行基准测试后，结果自动上链（提交 `TxUpdateCapability`）
4. 基准测试分数 = f(tokens_per_second, quality_score, memory_efficiency)
5. 在 Web 面板中显示每个矿工的基准测试分数和排名

### 关键文件
- 新建：`pkg/compute/benchmark/runner.go`
- 新建：`pkg/compute/benchmark/testcases.go`
- 修改：`pkg/compute/registry/miner.go`
- 修改：`cmd/aichain-web/dashboard.html`

### 验收标准
```
$ go run ./cmd/benchmark/
[Benchmark] Running bench_tiny... 100/100 complete (4.2s, 23.8 tok/s)
[Benchmark] Running bench_medium... 10/10 complete (18.7s, 19.2 tok/s)
[Benchmark] Running bench_large... 1/1 complete (8.3s, 15.1 tok/s)
[Benchmark] Score: 82/100 (Tier 3 capable)
[Benchmark] Submitting on-chain... tx=0xdef... confirmed
```

---

## 任务 4：实时区块浏览器后端

**难度**：⭐⭐⭐ &nbsp; **预计工时**：5-6h &nbsp; **依赖**：JSON-RPC API

### 背景
Web 面板（`cmd/aichain-web/`）显示了基本数据，但没有完整的区块浏览器功能。需要一个独立的区块浏览器后端，提供完整的链上数据查询。

### 要求
1. 在 `cmd/explorer/main.go` 创建区块浏览器后端
2. API 端点：
   - `GET /block/:height` — 区块详情（含交易列表）
   - `GET /tx/:hash` — 交易详情（含 receipt、logs）
   - `GET /address/:addr` — 地址详情（余额、交易历史、任务历史）
   - `GET /search?q=xxx` — 搜索（区块号/哈希/地址/任务ID）
   - `GET /api/stats` — 网络统计（TPS、平均出块时间、活跃地址数）
3. 每个端点返回 JSON，前端可独立渲染
4. 支持分页（`?page=1&limit=20`）
5. 地址页面显示：APT/NPT 余额、发送/接收的交易列表、发布的任务列表

### 关键文件
- 新建：`cmd/explorer/main.go`

### 验收标准
```
$ go run ./cmd/explorer/
[Explorer] Listening on :8090

$ curl localhost:8090/block/42
{"height":42,"hash":"0xabc...","txs":[...3 transactions...],"time":"15:04:05"}

$ curl localhost:8090/address/0x1234...
{"address":"0x1234...","apt":"1000.5","npt":"500","tx_count":15,"tasks":[...5 tasks...]}
```

---

## 任务 5：矿工惩罚机制（Slashing）

**难度**：⭐⭐⭐⭐ &nbsp; **预计工时**：6-8h &nbsp; **依赖**：共识引擎

### 背景
目前矿工作恶没有经济惩罚。需要实现类似 Ethereum 的 Slashing 机制：
- 提交无效 AI 推理结果 → 罚没质押金
- 双重投票（double-vote） → 罚没并踢出验证者集合
- 长期离线 → 逐步扣除质押

### 要求
1. 在 `internal/consensus/slashing/` 创建惩罚模块
2. 实现三种惩罚条件：
   - `SlashInvalidResult`: 验证节点发现 AI 结果造假 → 罚没矿工 10% NPT 质押
   - `SlashDoubleVote`: 同一高度投了矛盾票 → 罚没 50% NPT 质押 + 永久踢出
   - `SlashDowntime`: 连续 1000 个区块未参与 → 罚没 1% NPT 质押
3. 被罚没的 NPT 进入燃烧池（销毁）
4. 举报者获得罚没金额的 10% 作为奖励
5. 惩罚事件记录在链上（`SlashingEvent` 日志）

### 关键文件
- 新建：`internal/consensus/slashing/conditions.go`
- 新建：`internal/consensus/slashing/evidence.go`
- 修改：`internal/consensus/engine/tendermint.go`（double-vote 检测）
- 修改：`pkg/task/verifier.go`（无效结果检测）

### 验收标准
```
$ go test ./internal/consensus/slashing/ -v
[PASS] TestSlashDoubleVote — validator slashed 50%, permanently removed
[PASS] TestSlashInvalidResult — miner slashed 10%, reporter rewarded
[PASS] TestSlashDowntime — offline validator loses 1% per 1000 blocks
[PASS] TestSlashFundsBurned — slashed NPT sent to burn address
```

---

## 任务 6：轻客户端（SPV）验证

**难度**：⭐⭐⭐⭐ &nbsp; **预计工时**：7-9h &nbsp; **依赖**：MPT 证明

### 背景
目前每个节点需要同步完整区块链。需要像比特币 SPV（Simplified Payment Verification）一样的轻客户端，
只下载区块头 + Merkle 证明即可验证交易。

### 要求
1. 在 `internal/light/` 创建轻客户端模块
2. 功能：
   - 从全节点下载区块头（只存 header，不存交易）
   - 请求特定交易的 Merkle 证明（`/api/proof/tx/:hash`）
   - 验证证明：用 `internal/state/mpt/trie.go` 的 `Prove()` 和 `VerifyProof()`
   - 请求 AI 任务结果的验证证明
3. 内存占用：< 50 MB（对比全节点的 GB 级别）
4. 提供 `cmd/light-client/main.go` 入口
5. 命令行交互：`light-client balance 0xADDR` → 查询余额（通过 SPV 证明）

### 关键文件
- 新建：`internal/light/verifier.go`
- 新建：`internal/light/header_store.go`
- 新建：`cmd/light-client/main.go`
- 修改：`internal/state/mpt/trie.go`（完善 Prove/VerifyProof）

### 验收标准
```
$ go run ./cmd/light-client/ --connect=localhost:8545
[Light] Syncing headers... 342 headers downloaded (1.2 MB)
[Light] Ready.

> balance 0x1234...
APT: 500.25 (verified by Merkle proof)
NPT: 100.00 (verified by Merkle proof)

> task task-abc123...
Status: Completed
Result: [verified by Merkle proof]
```

---

## 任务 7：代币经济模拟器

**难度**：⭐⭐ &nbsp; **预计工时**：3-4h &nbsp; **依赖**：代币经济模块

### 背景
在部署主网前，需要模拟代币经济模型在各种场景下的表现：
APT 供应量会如何变化？燃烧率是否足够对抗通胀？矿工收益是否可持续？

### 要求
1. 在 `cmd/econ-sim/main.go` 创建模拟器
2. 可配置参数：
   - 初始供应量、初始价格
   - 日活跃用户数、日均任务数
   - 矿工数量、平均算力
   - 燃烧率（BaseBurn、TaskBurn、TimeDecay）
3. 模拟 N 天的经济演化（N 可配置，默认 365 天）
4. 输出 CSV 格式的每日数据：APT 供应量、NPT 供应量、价格、燃烧量、矿工收益
5. 生成简单 ASCII 图表展示趋势
6. 检测异常：通胀 > 5%/年 → 警告；矿工收益 < 电费成本 → 警告

### 关键文件
- 新建：`cmd/econ-sim/main.go`
- 参考：`pkg/token/economics.go`
- 参考：`pkg/token/burn.go`

### 验收标准
```
$ go run ./cmd/econ-sim/ --days=365 --users=10000 --tasks-per-day=50000
[Sim] Day 365/365 complete
[Sim] Final APT supply: 847,203,441 (from 1,000,000,000 initial)
[Sim] Total burned: 152,796,559 APT (15.3%)
[Sim] Annual deflation rate: 2.1%
[Sim] Average miner daily revenue: 42.3 APT
[Sim] Supply projection chart:
  1B |████████████
     |█████████▇▆▆▅▅▄▄▃▃  (deflationary)
     |___________________
     Day 0            Day 365
```

---

## 任务 8：AI 推理结果质量评估

**难度**：⭐⭐⭐⭐ &nbsp; **预计工时**：5-7h &nbsp; **依赖**：LM Studio

### 背景
目前乐观验证只检查矿工是否提交了结果，不检查结果质量。矿工可以用乱码通过验证。
需要一个自动质量评估系统。

### 要求
1. 在 `pkg/compute/quality/` 创建质量评估模块
2. 对每个完成的 AI 任务，自动运行质量检查：
   - **完整性检查**：结果长度 >= prompt 要求的长度
   - **相关性检查**：用嵌入模型（`text-embedding-nomic-embed-text-v1.5`）比较 prompt 和 result 的余弦相似度
   - **重复性检查**：检测明显复制粘贴的垃圾内容
   - **格式检查**：如果 prompt 要求 JSON/Markdown/列表，验证格式
3. 质量分数 0-100，低于 30 分的结果被拒绝，矿工质押金罚没
4. 质量分数记录在链上，影响矿工信誉分
5. 在 Web 面板中显示每次任务的质量分数

### 关键文件
- 新建：`pkg/compute/quality/evaluator.go`
- 新建：`pkg/compute/quality/checks.go`
- 修改：`pkg/task/verifier.go`

### 验收标准
```
$ go test ./pkg/compute/quality/ -v
[PASS] TestQualityGood — score=92 (completeness=25/25 relevance=35/35 format=32/40)
[PASS] TestQualityGarbage — score=5 (gibberish detected, rejected)
[PASS] TestQualityTooShort — score=0 (result 10 chars, required 1000 chars)
[PASS] TestQualityEmbedding — cosine_sim=0.87 (relevant), cosine_sim=0.12 (off-topic)
```

---

## 任务 9：P2P 网络监控仪表板

**难度**：⭐⭐ &nbsp; **预计工时**：3-4h &nbsp; **依赖**：P2P 模块

### 背景
目前 Web 面板只显示节点数量，不显示网络拓扑、消息流量、延迟分布。
需要一个 P2P 专用监控页面。

### 要求
1. 在 Web 面板中添加 `/p2p` 路由
2. 显示：
   - 网络拓扑图（节点连接关系，使用 Canvas/SVG 绘制）
   - 每个 peer 的延迟、消息数、有效消息比例
   - GossipSub 话题的消息速率（tx/s, block/s, vote/s）
   - DHT 路由表大小
   - 带宽使用（入站/出站 Mbps）
3. 从 `internal/p2p/peer/manager.go` 获取评分数据
4. 从 `internal/p2p/gossip/gossiper.go` 获取话题统计
5. 每秒自动刷新

### 关键文件
- 新建：`internal/p2p/stats.go`（统计数据收集器）
- 修改：`cmd/aichain-web/main.go`（添加 /p2p 路由）
- 修改：`cmd/aichain-web/dashboard.html`（添加网络面板）

### 验收标准
```
打开 http://localhost:8080/p2p
看到：
┌─────────────────────────────────────────────┐
│  P2P Network Monitor                        │
│  ┌─────────┐     ┌─────────┐               │
│  │ Node A  │─────│ Node B  │  12 peers     │
│  │ 15ms    │     │ 23ms    │  2.3 MB/s ↓   │
│  └────┬────┘     └────┬────┘  1.1 MB/s ↑   │
│       │               │                     │
│  ┌────┴───────────────┴────┐               │
│  │        Node C (self)    │               │
│  └─────────────────────────┘               │
│  Topics: tx=5.2/s block=0.3/s vote=1.1/s   │
└─────────────────────────────────────────────┘
```

---

## 任务 10：合约部署 + 调用 CLI

**难度**：⭐⭐⭐ &nbsp; **预计工时**：4-5h &nbsp; **依赖**：VM 模块

### 背景
智能合约 VM 已实现（`internal/vm/`），但没有部署和调用合约的工具。
需要一个 CLI 工具让开发者部署和交互合约。

### 要求
1. 在 `cmd/aicli/main.go` 完善 CLI（目前是空壳）
2. 支持命令：
   - `aicli deploy <contract.wasm>` — 编译并部署合约，返回合约地址
   - `aicli call <contract_addr> <method> <args...>` — 调用合约方法
   - `aicli query <contract_addr> <method> <args...>` — 查询合约状态（只读）
   - `aicli balance <address>` — 查询余额
   - `aicli send <to> <amount> --token apt|npt` — 转账
3. 合约用 Go 编写 → 编译为 VM 字节码
4. 提供 3 个示例合约：
   - `SimpleStorage`: 存储/读取一个值
   - `TokenVesting`: 代币线性解锁
   - `AITaskEscrow`: AI 任务保证金托管

### 关键文件
- 修改：`cmd/aicli/main.go`
- 新建：`examples/contracts/simple_storage.go`
- 新建：`examples/contracts/token_vesting.go`
- 新建：`examples/contracts/ai_task_escrow.go`
- 参考：`internal/vm/interpreter.go`

### 验收标准
```
$ go run ./cmd/aicli/ deploy ./examples/contracts/simple_storage.go
Contract deployed at: 0xabcd1234...

$ go run ./cmd/aicli/ call 0xabcd1234... store "hello world"
Tx: 0xtx001... confirmed

$ go run ./cmd/aicli/ query 0xabcd1234... read
Result: "hello world"
```

---

## 如何领取任务

1. Fork 仓库：`https://github.com/ksk2kk/SiliconVerseAIChain`
2. 创建分支：`feature/task-N-description`
3. 实现功能 + 测试（`go test ./...` 必须通过）
4. 提交 PR，标题格式：`[Task N] 功能描述`
5. 在 PR 中附上验收标准截图/输出

### 代码规范

- Go: 遵循标准库风格，`gofmt` 格式化
- 测试: 每个新模块必须有测试文件
- 日志: 使用 `log.Printf`，关键操作加毫秒时间戳
- 提交: 小步提交，每个 commit 做一件事

---

> 挑选你感兴趣的任务，让 AI Chain 变得更强。
> 每个任务都是独立可完成的，不需要理解整个项目。
