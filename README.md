# Agent 系列 — 手写 Agent 框架

这是[《后端写了这么多年，为什么突然想学 Agent》](https://mp.weixin.qq.com/mp/appmsgalbum?__biz=MzIyNTYxNjA0Nw==&action=getalbum&album_id=4507045649216798720#wechat_redirect)系列文章的配套代码仓库。

当前代码发布到第 6 篇。每个目录对应一篇已发布文章的最终代码状态，**拿到就能跑**。

**前置条件：Go 1.23+**（`go.work` 需要 1.23 的 workspace 支持）。

## 目录结构

```
agent-series/
├── 02-hello-llm/           # 第2篇: Token 计数 + 首次 API 调用
│   ├── token-count/        #   子目录: 字符粗估 token 演示
│   └── first-api-call/     #   子目录: DeepSeek Chat API
├── 03-first-agent/         # 第3篇: 单工具天气 Agent + ReAct 循环
├── 04-multi-tool/          # 第4篇: 多工具 Agent (天气/计算器/搜索)
├── 05-session-memory/      # 第5篇: Session + Token 预算 + 压缩策略
├── 06-rag/                 # 第6篇: RAG 知识库检索 + Embedding
├── scripts/test_all.sh     # 全量验证脚本
├── go.work                 # Go workspace
└── .gitignore
```

## 环境变量

| 变量               | 用途                             | 需要它的项目                         |
| ------------------ | -------------------------------- | ------------------------------------ |
| `DEEPSEEK_API_KEY` | DeepSeek Chat API 密钥           | 全部 Chat Agent（除 token-count）    |
| `OPENAI_API_KEY`   | OpenAI 或兼容 Embedding API 密钥 | `06-rag`                             |
| `OPENAI_BASE_URL`  | OpenAI 兼容 API Base URL         | `06-rag`（可选，本地/第三方服务）    |
| `EMBEDDING_MODEL`  | Embedding 模型名                 | `06-rag`（可选，默认 OpenAI small）  |

DeepSeek API Key 获取位置：[platform.deepseek.com](https://platform.deepseek.com) → API Keys。

### 使用 .env 文件（推荐）

把 API Key 写在 `.env` 文件里，不用每次 export：

```bash
# 在对应章节目录下创建 .env
echo 'DEEPSEEK_API_KEY=sk-xxx' > .env

# 代码启动时自动加载，无需手动 export
cd 03-first-agent && go run .
```

每个项目都内置了 `.env` 加载支持。已存在的环境变量不会被 `.env` 覆盖。

第 6 篇如果使用本地 oMLX / Ollama / 第三方 embedding 服务，可以在 `06-rag/.env` 写：

```bash
OPENAI_BASE_URL=http://127.0.0.1:12345/v1
OPENAI_API_KEY=your-local-key
EMBEDDING_MODEL=bge-m3-mlx-4bit
DEEPSEEK_API_KEY=sk-xxx
```

## 运行方式

全部项目使用 `go.work` 管理多模块，在仓库根目录可以直接构建任何子项目。**注意：根目录不是 Go module**，裸跑 `go test ./...` 会失败，请用：

```bash
./scripts/test_all.sh   # 全量验证: build + vet + test + gofmt
```

### 第 2 篇 — Hello LLM

```bash
# Token 计数演示（不需要 API key）
cd 02-hello-llm/token-count && go run .

# 首次 API 调用
export DEEPSEEK_API_KEY="your-key"
cd 02-hello-llm/first-api-call && go run .
```

### 第 3-5 篇 — 独立 Agent 演进

每篇在前一篇基础上增加新能力，各自独立可运行。

```bash
export DEEPSEEK_API_KEY="your-key"

# 第3篇: 单工具天气 Agent
cd 03-first-agent && go run .

# 第4篇: 多工具 (天气 + 计算器 + 搜索)
cd 04-multi-tool && go run .

# 第5篇: 会话记忆 + Token 管理
cd 05-session-memory && go run .
```

### 第 6 篇 — RAG 知识库检索

第 6 篇需要 Embedding API。可以用 OpenAI，也可以用本地 oMLX / Ollama 或其他 OpenAI 兼容服务。

```bash
cd 06-rag

# Embedding 配置二选一：
#
# 方式一：OpenAI Embedding（默认 text-embedding-3-small）
export OPENAI_API_KEY="your-openai-key"

# 方式二：本地 oMLX / Ollama / 第三方兼容服务
export OPENAI_BASE_URL="http://127.0.0.1:12345/v1"
export OPENAI_API_KEY="your-local-key"
export EMBEDDING_MODEL="bge-m3-mlx-4bit"

# 看 embedding 向量和相似度
go run . embed

# 对比关键词搜索和语义搜索
go run . search

# Agent + RAG，需要额外设置 DeepSeek Chat API key
export DEEPSEEK_API_KEY="your-deepseek-key"
go run . agent

# 交互式 RAG CLI
go run .
```

## 预期输出

### 03-first-agent

```
=== 示例: 天气查询 ===
北京当前的天气是 15°C，晴，西北风3级。
```

### 04-multi-tool

```
=== 示例 1: 天气查询 ===
北京天气 15°C，晴，西北风3级。

=== 示例 2: 计算 + 搜索 ===
(15 + 27) * 3 = 126。关于 goroutine：goroutine 是 Go 的轻量级...
```

### 05-session-memory

默认进入交互式 CLI。连续输入多轮问题，可以观察同一个 Session 如何保留上下文：

```
Session Memory CLI
连续输入问题，感受模型如何记住上文。
输入 /tokens 查看上下文用量
输入 /msgs 查看消息列表
输入 /threshold 切换 token 阈值（200 ↔ 110000）
输入 /exit 退出

> 北京天气怎么样？
Agent: 北京 15°C，晴，西北风3级。

> 那上海呢？
Agent: 上海 18°C，多云，东南风2级。

> 我刚才问了哪两个城市？
Agent: 你刚才问了北京和上海。
```

也可以运行固定 demo：

```bash
cd 05-session-memory

# 正常两轮对话，观察短期记忆
go run . basic

# 把 token 阈值降到 200，观察压缩触发
go run . compress

# 打印 messages 数组，观察安全截断边界
go run . boundary
```

### 06-rag

```bash
=== Demo 1: Embedding — 文字变成向量 ===

文本: 密码找回流程：用户点击忘记密码 → 输入邮箱 → 收到重置链接 → 设置新密码
  维度: 1024

「密码找回流程」vs「如何重置密码」的余弦相似度: 0.9317
「密码找回流程」vs「北京天气」的余弦相似度:     0.6052
```

## 常见问题

### DeepSeek API 返回 400

确保设置了 `DEEPSEEK_API_KEY`。如果 key 正确但仍然 400，检查 API key 是否还有余额。

### `go run main.go` 报 `undefined: loadEnv`

这些示例把 `.env` 加载逻辑放在同目录的 `loadenv.go`。请在章节目录内使用：

```bash
go run .
```

`go run main.go` 只会编译单个文件，不会自动包含同目录其他 `.go` 文件。

### token 计数为什么是近似值

第 2 篇的 token-count 示例默认使用字符粗估（1 token ≈ 2 字符），零依赖、不需要网络。第 5 篇会尝试用 tiktoken 做更准的近似计算，初始化失败时自动回退到字符估算。精确值以 DeepSeek API 返回的 `usage` 为准。

## 系列文章

| #   | 文章                   | 代码目录             |
| --- | ---------------------- | -------------------- |
| 1   | 为什么想学 Agent       | 无代码               |
| 2   | LLM 对后端工程师是什么 | `02-hello-llm/`      |
| 3   | Agent 不是聊天机器人   | `03-first-agent/`    |
| 4   | 工具定义与契约         | `04-multi-tool/`     |
| 5   | 短期记忆与上下文窗口   | `05-session-memory/` |
| 6   | 长期记忆与 RAG         | `06-rag/`            |

## License

MIT
