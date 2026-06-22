# SiliconVerse AI Chain — 开发手册

## 项目定位

一条全新的 Layer-1 区块链。**不挖哈希，挖 AI 算力。** 用户花代币发布 AI 任务，矿工用 GPU 跑大模型推理获得代币奖励，三重燃烧机制实现代币持续通缩。

## 快速开始

```bash
git clone https://github.com/ksk2kk/SiliconVerseAIChain.git
cd SiliconVerseAIChain
go build ./...
go test ./... -count=1
```

### 三个可执行入口

| 命令 | 功能 |
|------|------|
| `go run ./cmd/aichain/` | Step1 代币核心 Demo：账户创建、转账、AMM 兑换、燃烧 |
| `go run ./cmd/aichain-node/` | 全节点 7 阶段集成测试：创世→P2P→交易→任务→验证 |
| `go run ./cmd/aichain-web/` | **Web 面板**：实时统计、发布任务、自动挖矿、流水线监控 |

Web 面板地址：`http://localhost:8080`

## 目录结构

```
SiliconVerseAIChain/
├── cmd/
│   ├── aichain/main.go            # Step1 代币核心 Demo
│   ├── aichain-node/main.go       # 全节点 7 阶段集成测试
│   ├── aichain-web/main.go        # Web 面板 + 自动挖矿
│   │   └── dashboard.html          # 前端 (embed)
│   └── compute-test/main.go       # LM Studio 推理测试
├── internal/
│   ├── types/                     # Address, Hash, Account, Tx, Block, Token
│   ├── crypto/                    # Ed25519 + SHA256 + BLAKE3 + 地址派生
│   ├── state/
│   │   ├── mpt/                   # Merkle Patricia Trie (Ethereum 兼容)
│   │   ├── account/               # StateDB + 快照回滚 + Journal
│   │   └── executor/              # 交易执行器 (Gas→执行→退款→回执)
│   ├── storage/                   # Database 接口 + LevelDB + MemoryDB
│   ├── blockchain/                # 链管理 + 创世 + 验证 + 分叉选择
│   ├── consensus/
│   │   ├── types/                 # Vote, Proposal, Validator, ValidatorSet
│   │   └── engine/                # Tendermint BFT (Propose→Prevote→Precommit→Commit)
│   ├── p2p/
│   │   ├── host/                  # libp2p Host (TCP+Noise+Yamux)
│   │   ├── gossip/                # GossipSub (4话题: tx/block/vote/task)
│   │   ├── discovery/             # mDNS + Kademlia DHT
│   │   ├── peer/                  # 节点评分/黑名单/地址簿
│   │   └── protocol/              # 区块同步/交易同步/Status 握手
│   ├── txpool/                    # 交易池 (nonce排序)
│   ├── vm/                        # 智能合约 VM (20+操作码, Gas计量)
│   ├── api/jsonrpc/               # JSON-RPC 2.0 服务器 (6端点)
│   ├── node/                      # Node 编排
│   └── config/                    # 全局配置
├── pkg/
│   ├── token/                     # APT/NPT/AMM/Burn(三重燃烧)/Gas/Economics
│   ├── task/                      # 任务市场/分发/DAG拆分/乐观验证/定价
│   ├── compute/
│   │   ├── interface.go           # ModelRunner 接口
│   │   ├── local/                 # LM Studio 客户端 + 硬件检测
│   │   ├── distributed/           # Megatron-LM 张量拆分 + Ring AllReduce + 隐私
│   │   └── registry/              # 矿工注册表
│   └── digitalhuman/              # 数字人 L2 (加密记忆/向量检索/RAG/人格)
├── Dockerfile                     # Docker 镜像
├── docker-compose.yml             # 4节点测试网
├── README.md                      # 项目说明
└── DEV_GUIDE.md                   # 本文件
```

## 核心架构

### 代币经济

```
APT (算力币)     ← 矿工贡献 AI 算力获得
NPT (网络币)     ← 网络节点维护区块链获得
AMM 池          ← 恒定乘积 x*y=k，0.3% 手续费
三重燃烧:
  1. Gas 燃烧 (30%)      ← 每笔交易
  2. 任务费燃烧 (20%)     ← 每个 AI 任务
  3. 时间衰减             ← 每 N 个区块对所有余额施加减持
```

### AI 任务流水线

```
用户发布任务 (Pending)
  → Analyzer 拆分 (Analyzing)
    → Subtask1 → Miner 推理 → Completed
    → Subtask2 → Miner 推理 → Completed
    → Subtask3 → Miner 推理 → Completed
  → 聚合结果 (Completed)
  → 出块 + 矿工奖励
```

### 共识引擎

```
Propose → Prevote → Precommit → Commit → NewHeight
   ↑                    ↓
   └── 超时 → 下一轮 ←──┘

双权重投票:
  投票权 = 0.4 * 算力权重 + 0.6 * 网络质押权重
  +2/3 预投票 → 区块锁定
  +2/3 预提交 → 区块提交
```

### P2P 网络

```
libp2p Host (TCP/Noise/Yamux)
  ├── GossipSub
  │   ├── aichain/tx/0.1      ← 交易广播
  │   ├── aichain/block/0.1   ← 区块广播
  │   ├── aichain/vote/0.1    ← 共识投票
  │   └── aichain/task/0.1    ← 任务通知
  ├── 发现
  │   ├── mDNS (局域网)
  │   └── Kademlia DHT (广域网)
  └── 流协议
      ├── /aichain/status/0.1  ← 链状态握手
      └── /aichain/sync/0.1    ← 区块同步
```

## API 参考

### JSON-RPC 端点

| 方法 | 参数 | 返回 |
|------|------|------|
| `aichain_blockNumber` | 无 | 当前区块高度 (hex) |
| `aichain_getBlockByNumber` | `["0xN"]` | 区块详情 |
| `aichain_getBalance` | `["0xADDR"]` | APT + NPT 余额 |
| `aichain_sendRawTransaction` | `["0xRAW"]` | 交易哈希 |
| `aichain_txpoolStatus` | 无 | pending + queued 数量 |
| `aichain_nodeInfo` | 无 | 链 ID + 高度 + 版本 |

### REST API (Web 面板)

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/node` | GET | 节点硬件 + 链状态 |
| `/api/blocks` | GET | 区块列表 |
| `/api/tasks` | GET | 全部任务 |
| `/api/tasks/mine?creator=0xID` | GET | 按创建者筛选任务 |
| `/api/tasks/publish` | POST | 发布新任务 `{model, prompt, fee, creator}` |
| `/api/mine` | POST | 手动挖矿 `{subtask_id, miner_id}` |
| `/api/transactions` | GET | 交易列表 |
| `/api/stream/current` | GET | **SSE 实时推理流** |
| `/ws` | GET | WebSocket 数据推送 |

## 测试

```bash
# 全部测试
go test ./... -count=1 -timeout 2m

# 单个包
go test ./internal/state/mpt/ -v
go test ./internal/vm/ -v
go test ./internal/consensus/engine/ -v
go test ./pkg/task/ -v
go test ./pkg/compute/local/ -v       # 需要 LM Studio 运行
go test ./pkg/compute/distributed/ -v
go test ./pkg/digitalhuman/ -v
```

## 开发流程

### 添加新交易类型

1. 在 `internal/types/transaction.go` 添加 `TransactionType` 常量
2. 在 `internal/state/executor/state_processor.go` 添加 `execute*` 方法
3. 在 `pkg/token/gas.go` 添加 gas 成本（如需要）

### 添加新 RPC 方法

1. 在 `internal/api/jsonrpc/endpoints.go` 调用 `s.RegisterMethod("name", handler)`
2. handler 签名：`func(params json.RawMessage) (interface{}, error)`

### 添加新 GossipSub 话题

1. 在 `internal/p2p/gossip/gossiper.go` 添加话题常量
2. 在 `internal/node/node.go` 的 `JoinTopics` 中订阅
3. 在 `MessageHandler` 中添加处理方法

## 依赖

```
Go 1.24+
libp2p v0.48          (P2P 网络)
goleveldb v1.0        (持久存储)
blake3 v1.4           (快速哈希)
testify v1.11         (测试框架)
LM Studio (可选)       (本地 AI 推理)
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `8080` | Web 面板端口 |
| `LMSTUDIO_TOKEN` | 内置 token | LM Studio API 认证 |

## Docker 部署

```bash
docker-compose up -d        # 启动 4 节点测试网
docker-compose logs -f      # 查看日志
docker-compose down         # 停止
```

## 架构图

```
┌──────────────────────────────────────────────────────┐
│                  AI Chain Node                       │
│                                                      │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │ JSON-RPC │  │ Web 面板  │  │ SSE 实时流       │  │
│  └────┬─────┘  └────┬─────┘  └────────┬─────────┘  │
│       │             │                 │             │
│  ┌────┴─────────────┴─────────────────┴──────────┐  │
│  │               API Layer                        │  │
│  └────────────────────┬───────────────────────────┘  │
│                       │                              │
│  ┌────────────────────┼───────────────────────────┐  │
│  │  ┌──────────────┐  │  ┌──────────────────────┐ │  │
│  │  │ Task System  │  │  │ Token Economics      │ │  │
│  │  │ Pool/Disp/DAG│  │  │ APT/NPT/AMM/Burn/Gas │ │  │
│  │  └──────┬───────┘  │  └──────────┬───────────┘ │  │
│  │         │          │             │              │  │
│  │  ┌──────┴──────────┴─────────────┴───────────┐ │  │
│  │  │            State Machine (MPT)            │ │  │
│  │  └──────────────────┬───────────────────────┘ │  │
│  └─────────────────────┼─────────────────────────┘  │
│                        │                             │
│  ┌─────────────────────┼─────────────────────────┐  │
│  │  ┌──────────────┐   │   ┌──────────────────┐ │  │
│  │  │ Consensus    │   │   │ P2P Network      │ │  │
│  │  │ BFT Engine   │   │   │ libp2p+GossipSub │ │  │
│  │  └──────┬───────┘   │   └────────┬─────────┘ │  │
│  │         │           │            │            │  │
│  │  ┌──────┴───────────┴────────────┴─────────┐ │  │
│  │  │         Compute Layer                   │ │  │
│  │  │  LM Studio / Distributed / Privacy      │ │  │
│  │  └─────────────────────────────────────────┘ │  │
│  └──────────────────────────────────────────────┘  │
│                                                      │
│  ┌──────────────────────────────────────────────┐   │
│  │           VM (Smart Contracts)               │   │
│  └──────────────────────────────────────────────┘   │
│                                                      │
│  ┌──────────────────────────────────────────────┐   │
│  │        Digital Human L2 (Memory/RAG)         │   │
│  └──────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────┘
```

## 许可证

MIT License — 详见 LICENSE 文件
