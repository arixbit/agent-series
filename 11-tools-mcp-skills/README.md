# 第 11 篇：让 Agent 使用远程 MCP 与 Skill

这个示例把一个可通过 HTTP 访问的 MCP Server 当作第三方服务的替身，再让第 9 篇的 Runtime 连接它：

```text
11-tools-mcp-skills/
├── server/main.go                  # HTTP MCP Server：公布并执行天气工具
├── client/main.go                  # Agent 应用：连接 Server、发现 Skill、运行 Agent
├── client/skill.go                 # 把 Skill 目录转成按需调用的 load_skill 工具
├── weather/weather.go              # mock 天气数据的真正实现
└── skills/weather-report/SKILL.md  # 本次任务的步骤和约束
```

先在 `.env` 中配置：

```dotenv
DEEPSEEK_API_KEY=你的_Key
```

打开第一个终端，启动 MCP Server：

```bash
cd 11-tools-mcp-skills
go run ./server
```

Server 默认提供 MCP 地址：

```text
http://127.0.0.1:8080/mcp
```

再打开第二个终端，运行 Agent：

```bash
cd 11-tools-mcp-skills
go run ./client http://127.0.0.1:8080/mcp
```

Client 连接成功后进入交互模式：

```text
天气 Agent 已就绪（输入 quit 退出）

> 请使用 weather-report Skill 查询上海天气。
```

可以继续输入问题；同一进程会复用 MCP Session，并把上一轮对话历史带入下一轮。输入 `quit` 退出。

Client 会依次：

1. 扫描 `skills/*/SKILL.md` 的 `name` 和 `description`，发现有哪些 Skill，但不把正文交给模型。
2. 把按需读取 Skill 正文的能力注册为 `load_skill` 工具。
3. 通过 Streamable HTTP 连接 MCP Server，再用 `ListTools` 发现天气工具。
4. 进入交互循环。用户明确要求使用 `weather-report` 时，模型先调用 `load_skill`。
5. Skill 正文作为工具结果进入上下文，模型再按其步骤调用 MCP 天气工具。
6. 保存本轮对话历史，供下一次输入继续使用。

```text
Skill 元数据 -> load_skill 工具 ──────────┐
                                           ├-> Agent.Run
MCP URL -> ListTools -> agent.Tool ─────────┘
                              |
                              └-> Execute -> CallTool -> MCP URL

用户明确要求使用 Skill
  -> 模型调用 load_skill
  -> SKILL.md 正文进入对话上下文
  -> 模型按步骤调用 MCP 工具
```

天气结果是 `weather/weather.go` 中写死的 mock 数据，不是实时天气。本示例需要 `DEEPSEEK_API_KEY`，不需要 `SERPER_API_KEY`。
