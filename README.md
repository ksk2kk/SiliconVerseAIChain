# AI Chain — AI-Driven Blockchain

> 一条全新的 Layer-1 区块链。不挖哈希，挖 AI 算力。

---

## 一句话

**AI Chain 让全球 AI 算力变成可交易的链上资产：用户花钱发任务，矿工跑模型赚币，币通过燃烧持续通缩。**

---

## 架构

```
用户发任务 ──▶ 链上任务市场(TaskPool) ──▶ 智能分发(Dispatcher) ──▶ 矿工推理
                    │                              │                   │
          费用优先队列(Bitcoin式)          能力评分+负载均衡       LM Studio/分布式
                    │                              │                   │
            ┌───────┴──────────────────────────────┴───────────────────┘
            │
            │  双代币: APT(算力币) + NPT(网络币)
            │  AMM 池: 恒定乘积 x*y=k
            │  通缩: 30%Gas燃烧 + 20%任务费燃烧 + 时间衰减
            │
      ┌─────┼─────┐
      │     │     │
    P2P  共识  状态机
  libp2p BFT   MPT
```

## 代码规模

```
语言:              Go 1.24+
代码行数:          ~8,500 行
文件数:            75+
测试数:            70 (100% PASS)
依赖:             libp2p, goleveldb, blake3, gnark(规划), testify
```

## 快速开始

### 1. 下载并编译

```bash
git clone https://github.com/aichain/ai-chain
cd ai-chain
go build ./...
```

### 2. 运行 Step 1 Demo（代币核心）

```bash
go run ./cmd/aichain/
```

输出：
```
Account 0: <Alice地址>
Account 1: <Bob地址>
--- Initializing Genesis ---
Alice: APT=1000000, NPT=0
Bob:   APT=0, NPT=1000000
--- Transfer: Alice -> Bob 100 APT ---
--- Alice swaps 50 APT -> NPT via AMM ---
Alice received: 48 NPT
--- Alice burns 10 APT ---
Alice: APT=999840, NPT=48
```

### 3. 运行全节点集成测试（7 阶段）

```bash
go run ./cmd/aichain-node/
```

输出：
```
╔══════════════════════════════════════════════╗
║   ALL 7 PHASES PASSED — AI Chain operational ║
╚══════════════════════════════════════════════╝

Phase 1: 创世 + 4账户(100万APT/人)         ✅
Phase 2: 转账 + 出块                        ✅
Phase 3: AMM兑换 + 燃烧 + 链完整性验证       ✅
Phase 4: P2P网络 + GossipSub消息            ✅
Phase 5: AI任务(矿工→分发→提交→验证)        ✅
Phase 6: 节点健康统计                        ✅
Phase 7: 分叉拒绝/无效交易/快照回滚           ✅
```

### 4. 连接 LM Studio 运行真实 AI 推理

```bash
# 先启动 LM Studio (https://lmstudio.ai)
# 然后运行:
go run ./cmd/compute-test/ -token "你的LM Studio Token"
```

输出：
```
[1/6] LM Studio 连接      ✅ 3个模型可用
[2/6] 硬件检测             ✅ GPU + VRAM + RAM + 算力分
[3/6] 模型加载             ✅ qwen3.6-35b-a3b-mtp
[4/6] 推理执行             ✅ 244 tokens / 2.9s / 68.1 tok/s
[5/6] 矿工注册 + 任务创建   ✅
[6/6] 任务分发 + 验证       ✅
```

---

## 开发文档

### 项目结构

```
ai-chain/
├── cmd/
│   ├── aichain/main.go           # Step1 Demo: 代币核心演示
│   ├── aichain-node/main.go      # 全节点集成测试 (7阶段)
│   └── compute-test/main.go      # LM Studio 端到端测试
├── internal/
│   ├── types/                    # 核心类型 (Address, Hash, Block, Tx, Token)
│   ├── crypto/                   # Ed25519 + SHA256 + BLAKE3
│   ├── state/
│   │   ├── mpt/                  # Merkle Patricia Trie (Ethereum 兼容)
│   │   ├── account/              # StateDB + StateObject + Journal 回滚
│   │   └── executor/             # 交易执行器 (Gas计量/执行/退款/回执)
│   ├── storage/                  # Database 接口 + LevelDB + MemoryDB
│   ├── blockchain/               # 链管理 (创世/插入/验证/分叉选择)
│   ├── consensus/types/          # 共识投票类型 (Prevote/Precommit)
│   ├── p2p/
│   │   ├── host/                 # libp2p Host (TCP+Noise+Yamux)
│   │   ├── gossip/               # GossipSub (4话题)
│   │   ├── discovery/            # mDNS + Kademlia DHT
│   │   ├── peer/                 # 节点评分/黑名单/修剪
│   │   └── protocol/             # 区块同步/交易同步/Status握手
│   ├── txpool/                   # 交易池 (nonce排序/pending+queued)
│   ├── node/                     # Node 编排 (连接所有子系统)
│   └── config/                   # 全局配置
├── pkg/
│   ├── token/                    # APT/NPT/AMM/Burn/Gas/Economics
│   ├── task/                     # 任务市场/分发/DAG/验证/定价
│   ├── compute/
│   │   ├── interface.go          # ModelRunner 接口
│   │   ├── local/                # LM Studio 客户端 + 硬件检测
│   │   ├── distributed/          # 张量拆分/AllReduce/隐私/协调器
│   │   └── registry/             # 矿工注册表
│   └── digitalhuman/             # (预留) 数字人子系统
```

### 核心数据流

```
用户创建交易
  → txpool 验证签名+nonce+余额
    → 区块提议者打包
      → executor.ApplyTransaction():
          1. 验证签名
          2. 扣 Gas 费
          3. 执行操作(转账/兑换/燃烧/任务)
          4. 退款未用 Gas
          5. 燃烧 BaseFee
          6. 产出 Receipt
        → StateDB.Commit() → 新 State Root
          → 区块上链 → P2P 广播
```

### 任务生命周期

```
TaskStatusPending    ← 用户创建
  → Dispatcher.Dispatch(能力评分+负载均衡)
    → TaskStatusAssigned  ← 分配给最佳矿工
      → TaskStatusRunning   ← 矿工开始推理
        → Verifier.SubmitResult() → 质疑窗口 (100 blocks)
          → TaskStatusCompleted  ← 窗口通过，奖励结算
          → TaskStatusDisputed   ← 被挑战 → 重验证 → Rejected/Accepted
```

### 运行全部测试

```bash
go test ./... -count=1 -timeout 2m
```

---

## 完成状态（诚实报告）

### ✅ 已实现

| 模块 | 完成度 | 说明 |
|------|--------|------|
| 核心类型系统 | 100% | Address, Hash, Account, Transaction, Block, Token, Receipt |
| 密码学 | 100% | Ed25519 签名, SHA256/BLAKE3 哈希, 地址派生 |
| Merkle Patricia Trie | 100% | 完整 MPT 实现, 包括 Proof, Iterator, DB 持久化 |
| StateDB | 100% | 快照/回滚/日志/提交, 双币余额管理 |
| 存储层 | 100% | LevelDB + 内存DB, Batch 写入 |
| 交易执行器 | 90% | 转账/兑换/燃烧/质押 已实现, 合约执行预留 |
| 交易池 | 80% | 基础池+nonce排序, 缺少 RBF 替换和包交易 |
| 区块链管理 | 85% | 创世/插入/验证/分叉检测, 缺少重组逻辑 |
| 双代币经济 | 90% | APT/NPT/AMM/三重燃烧/Gas, 缺少治理参数调整 |
| P2P 网络 | 85% | Host+GossipSub+DHT+mDNS, 缺少 NAT 穿透优化 |
| 共识引擎 | 30% | 投票类型定义, 完整 BFT 状态机未实现 |
| AI 任务系统 | 85% | 任务池/分发/DAG/验证/两阶段提交/定价 |
| LM Studio 集成 | 95% | 完整 OpenAI API 客户端+流式+硬件检测 |
| 分布式推理 | 60% | 分片方案/AllReduce/Tensor/隐私/协调器, 缺少实际网络通信 |
| 矿工注册表 | 90% | 注册/能力更新/信誉跟踪/层级索引 |
| 测试覆盖 | - | 70 个测试全通过, 覆盖主要路径 |

### ❌ 尚未实现

| 功能 | 优先级 | 说明 |
|------|--------|------|
| **完整 BFT 共识** | 🔴 高 | 目前是单节点出块。多节点共识消息(Propose/Prevote/Precommit)和双权重投票逻辑未实现。这是成为真正去中心化链最关键的一步 |
| **智能合约 VM** | 🔴 高 | 目前只有硬编码交易类型。需要 EVM/WASM 运行时来支持用户自定义合约 |
| **数字人子系统 (Step 6)** | 🟡 中 | 规划了 L2 架构(加密记忆+向量检索+RAG+人格状态机), 代码未编写 |
| **P2P 节点间推理通信** | 🟡 中 | AllReduce 模拟单节点。实际跨节点张量传输未实现 |
| **零知识证明 (zkML)** | 🟡 中 | 当前用乐观验证。gnark 集成未做，无法生成推理 zk 证明 |
| **同态加密推理** | 🟡 中 | 隐私 Tier-2 用 XOR 秘密共享模拟，真正 HE 推理未实现 |
| **创世矿工自动部署** | 🟡 中 | 一键部署脚本/deb包/docker-compose 未做 |
| **交易 RBF 替换** | 🟢 低 | Bitcoin 式 fee bumping 未实现 |
| **紧凑区块 (Compact Block)** | 🟢 低 | BIP152 式短ID中继未实现 |
| **区块重组/回滚** | 🟢 低 | 分叉选择和链重组逻辑未实现 |
| **JSON-RPC API** | 🟢 低 | 完整节点 API 服务器未实现 |
| **钱包 CLI/GUI** | 🟢 低 | 只有程序化接口，无用户钱包工具 |
| **区块浏览器** | 🟢 低 | 未实现 |
| **治理系统** | 🟢 低 | 参数投票和协议升级机制未实现 |
| **性能优化** | 🟢 低 | MPT 节点缓存、并行验签、批处理等优化未做 |

### 当前局限

- **单节点出块**：多节点可以 P2P 连接，但无法参与共识。链只能由一个节点推进
- **真实网络未测试**：只在本地测试。未做多机器/vps/跨地域部署验证
- **无持久化网络 ID**：每次重启 PeerID 都变（未持久化 libp2p 私钥）
- **LM Studio 依赖**：推理依赖外部 LM Studio 进程。未直接调用 llama.cpp/ONNX
- **大模型分片未实测**：分布式推理的 AllReduce 在单节点模拟，跨机器延迟未知

---

## 路线图

```
v0.1 ✅  代币核心 (账户/MPT/AMM/燃烧/Gas)
v0.2 ✅  P2P 网络 (libp2p/GossipSub/DHT/mDNS)
v0.3 ✅  AI 任务系统 (任务池/分发/DAG/验证/定价)
v0.4 ✅  计算层集成 (LM Studio/硬件检测/矿工注册)
v0.5 ✅  分布式推理 (张量拆分/AllReduce/隐私/协调器)
v0.6 ⬜  完整共识引擎 (BFT 多节点/双权重投票)
v0.7 ⬜  智能合约 VM + RPC API
v0.8 ⬜  数字人子系统 L2
v0.9 ⬜  zkML 集成 + 网络压力测试
v1.0 ⬜  创世主网上线
```

## 技术栈

| 层 | 技术 |
|----|------|
| 语言 | Go 1.24+ |
| P2P | libp2p v0.48 (TCP/Noise/Yamux/GossipSub) |
| 发现 | mDNS + Kademlia DHT |
| 存储 | LevelDB (goleveldb) |
| 哈希 | SHA-256 + BLAKE3 |
| 签名 | Ed25519 (stdlib) |
| zk | gnark (规划中) |
| 推理 | LM Studio (OpenAI API) / llama.cpp (规划) |
| MPT | 自研 (Ethereum 兼容) |

## 许可证

MIT

---

> 一个 AI 驱动的新链，不是挖哈希，是挖智能。
