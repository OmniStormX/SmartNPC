# Delegate-Fix — Manual E2E Verification Checklist

**Bug under test:** NPC A 调用 `npc_send_message(to=B)` 后，B 没被任何机制唤起 → 玩家下次跟 B 聊天时 B 一次说多段对话。

**Fix:** `npc_send_message` 的 synthetic event 经 `hermesrelay` POST `/v1/responses` 触发 B 立刻醒，旁路于既有的 MCP logging notification 之上（**新增、不替换**）。

**自动化覆盖（已绿）：** `task ci` 通过，`internal/tools` 含 7 个 NpcSendMessage 测试。本 checklist 覆盖自动化测试无法验的部分：**LLM 真在角色里回话** + **chat_say 真到游戏对话气泡** + **跨 NPC 上下文连贯** + **既有路径未回归**。

**实机由 synchen 本人执行**——agent sandbox 起不了游戏窗口。

---

## Prerequisites

| 项 | 验证方式 |
|----|---------|
| `task ci` 全绿 | `cd D:\SmartNPC && C:\Users\synchen\go\bin\task.exe ci` |
| WSL Hermes Gateway 可启 | `wsl -d Ubuntu-22.04 hermes --version` ≥ 0.11.0 |
| 6 NPC profile SOUL/SKILL 已同步 | hermes-profile 确认 |
| ADR / docs 已同步 | docs-coord 确认 |
| 游戏 + SMAPI 可启 | `D:\Stardew Valley\StardewModdingAPI.exe` 能进存档 |
| `SMARTNPC_HERMES_KEY` | 默认 `smartnpc-test-key`，匹配 profile overlays |
| 双 NPC profile active | `run.bat` 默认 `ACTIVE_PROFILES=xiami,abigail`（Case 1 必需） |

---

## 启动顺序

由 **synchen** 双击 `D:\SmartNPC\run.bat`。脚本按序：

| 步 | 内容 | 期望日志/状态 |
|----|------|-------------|
| 1/6 | `task mod:build` + `task mcp:build` | `[OK] Build complete.` |
| 2/6 | 杀旧进程（Stardew Valley / StardewModdingAPI / smartnpc-mcp / smartnpc-agent） | `[OK] Old processes cleared.` |
| 3/6 | `task mod:install` + `hermes/install.sh` + `ensure_hermes_aux.sh` | `[OK] Mod installed, Hermes profiles synced` |
| 4/6 | 启 `smartnpc-mcp.exe --http :3000 --hermes-config runtime-config.yaml --log-level debug` | `[OK] mcp HTTP up at :3000/mcp.` + 日志窗口含 JSON 行 `"msg":"hermes relay enabled (multi-profile)" "profiles":2` |
| 5/6 | 启 `start_hermes_profiles.sh xiami,abigail`（WSL） | `[OK] Both gateways healthy.` + `curl http://192.168.59.118:864{2,3}/health` 返回 ok |
| 6/6 | 启 `StardewModdingAPI.exe` | 游戏窗口出现，能读档走动 |

任一步卡住或 `[ERROR]` → 中止本次 E2E，回报错误窗口截图。

---

## 进入测试前的健康检查

| 检查 | 命令 | 预期 |
|------|------|------|
| mcp HTTP | WSL: `curl -sS http://192.168.48.1:3000/mcp` | 返回 MCP server info（非 connection refused）|
| xiami gw | WSL: `curl -sS http://192.168.59.118:8642/health` | `{"status":"ok",...}` |
| abigail gw | WSL: `curl -sS http://192.168.59.118:8643/health` | `{"status":"ok",...}` |
| mcp 日志 | 标题 `smartnpc-mcp` 那个窗口 | 含 JSON 行 `"msg":"hermes relay enabled (multi-profile)"` 且 `"profiles":2` |

> **日志格式提示：** smartnpc-mcp 用 `slog.NewJSONHandler` 写 stderr，每行是一个 JSON 对象（字段如 `"msg"`, `"event"`, `"conversation"`, `"profiles"`）；本 checklist 所有 grep 关键字都按 JSON 字段写。`hermesrelay forwarded event` 是 `DEBUG` 级，`run.bat` 已用 `--log-level debug` 启动，可见。

任一失败 → 不进入 case 测试，先排查启动栈。

---

## Case 1（Delegate Fix 主验）— A 委托 B

**目标：** 证明 `npc_send_message(to=B)` 经 hermesrelay 立刻唤醒 B，且玩家下次跟 B 聊天上下文连贯。

### 1.1 触发

游戏内走到 **XiaMi**（农场附近），点击对话，输入：

> 帮我告诉 Abigail 农场出事了

按发送。

### 1.2 ✓ 期望（5s 窗口）

切到 `smartnpc-mcp` 日志窗口，**5 秒内**应出现一行 JSON，含：

```
"msg":"hermesrelay forwarded event"   "event":"npc_message"   "conversation":"abigail"   "status":200
```

3 个关键字（grep 按重要度排）：
1. `"msg":"hermesrelay forwarded event"` — POST 200 落地
2. `"event":"npc_message"` — 旁路触发，非普通 chat
3. `"conversation":"abigail"` — 路由到 Abigail conversation（非 xiami 自己）

游戏内**任一**即通过：
- **Abigail 在 audible 范围**：1-3s 内主动 `chat_say` 一句反应（弹气泡，无需走过去）
- **Abigail 不在 audible 范围**：日志后续 5-10s 内出现 `npc_inbox_get` + `npc_inbox_ack` 调用（silent ack），inbox 应被消费
- 上述都没出现但走到 Abigail 主动开聊时回复**直接引用** XiaMi 委托 → 也算通过（fallback 到 inbox-pull）

### 1.3 ✗ 失败现象 + 排查

| 失败现象 | 排查 |
|---------|------|
| 只看到 `"event":"chat_message" "conversation":"xiami"`（XiaMi 自己回应玩家），**没有** `"event":"npc_message" "conversation":"abigail"` | XiaMi profile 没调 `npc_send_message`。看 xiami gateway 日志（WSL 启动那个 wsl 窗口）有无 tool-call 记录；如无 → SOUL/SKILL 引导不够，转 hermes-profile |
| 看到 `"msg":"hermesrelay non-2xx"` 携 `status` 4xx/5xx + `body` | gateway 端拒收。截 body 字段贴回报，转 mcp-engineer + hermes-profile |
| 看到 `"msg":"hermesrelay group: no profile matched event, dropping"` 携 `"recipient":"abigail"` | NPC 名 case 不对。`runtime-config.yaml` 里是 `Abigail`（PascalCase），如果 `npc_send_message` 的 `to` 字段是 `abigail` 会 miss。让 mcp-engineer 看 SKILL 里 tool 调用样例 |
| Abigail 既无 chat_say 也没 inbox_get 调用 | Abigail 的 SKILL/triggers 没消费 npc_message event。看 abigail gateway 日志确认 conversation 收到 POST，再转 hermes-profile |

### 1.4 上下文连贯校验（Case 1 续）

走到 **Abigail**，输入 `嗨` 按发送。

**✓ 期望：** 回复**应提及 XiaMi 委托内容**（"我刚听 XiaMi 说农场出事了，你没事吧？" 类似），证明 inbox 被消费。**不应**出现 Stardew 经典"一次喷 2+ 段对话"现象。

**✗ 失败：** Abigail 答非所问 / 寒暄 / 一次说多段 → delegate 链路虽通但 SKILL inbox 处理逻辑有问题，转 hermes-profile。

---

## Case 2（M5.B / 5.6）— 玩家直聊 NPC

**目标：** 证明既有"player → NPC"路径未被 delegate fix 破坏（旁路是新增不是替换）。

### 2.1 触发

走到 **XiaMi**，输入：

> 今天天气真好

按发送。

### 2.2 ✓ 期望

| 检查点 | 期望 |
|-------|------|
| mcp 日志（5s 内） | JSON 行含 `"msg":"hermesrelay forwarded event" "event":"chat_message" "conversation":"xiami"` |
| **不应**出现 | `"event":"npc_message"`（玩家发的不是 NPC 间消息） |
| 游戏内（10s 内） | XiaMi 主动 `chat_say` 一段在角色里的回应，弹对话气泡 |

### 2.3 ✗ 失败现象 + 排查

| 失败现象 | 排查 |
|---------|------|
| `"event":"chat_message" "conversation":"xiami"` 出现但 XiaMi 不弹气泡 | 对话气泡是 `chat_say` 工具结果。看 mcp 日志有无 `chat_say` 调用 + ws 端 `wsClient sent action=chat_say`；如无 → xiami profile 没 chat_say tool；如有但游戏没气泡 → 转 mod 侧（C# `Chat/` handler） |
| 完全没 `"event":"chat_message"` | mod 没把玩家输入推给 mcp。看 SMAPI 控制台有无 `OnChatSend` 日志；mcp 日志看 ws 端有无入站 |
| 出现 `"event":"npc_message"`（不该出现） | 走错路径。bug，转 mcp-engineer |

---

## Case 3（M5.C / 5.7）— LLM 自主调 game_get_time

**目标：** 证明 NPC 能自主使用 game query 工具回答事实问题（既有路径，回归层面）。

### 3.1 触发

走到 **XiaMi**，输入：

> 现在几点了？

按发送。

### 3.2 ✓ 期望

| 检查点 | 期望 |
|-------|------|
| mcp 日志（5s 内）| JSON 行含 `"event":"chat_message" "conversation":"xiami"` |
| mcp 日志（接续 5-10s 内）| ws 端出现 `action=game_get_time` 请求 + 响应（XiaMi profile 主动调） |
| 游戏内 | XiaMi `chat_say` 回答**包含具体时间**（如"早上 8 点"），不是泛泛的"白天"或瞎编 |

### 3.3 ✗ 失败现象 + 排查

| 失败现象 | 排查 |
|---------|------|
| XiaMi 凭空回答时间（无 `game_get_time` 调用）| LLM 幻觉。SOUL/SKILL 应强制工具优先；转 hermes-profile |
| `game_get_time` 调用但 mcp 端 `tool error` | 工具实现 bug；转 mcp-engineer |
| `game_get_time` 返回 OK 但 XiaMi 答案与之不符 | LLM 没消化 tool result；转 hermes-profile |

---

## 回归 Case — 玩家直聊 Abigail

走到 **Abigail**，输入 `早上好` 按发送。

**✓ 期望：** mcp 日志 `"event":"chat_message" "conversation":"abigail"`，**不**应再出 `"event":"npc_message"`；Abigail 弹气泡正常回应（自然寒暄、不引用 Case 1 委托内容——除非 inbox 仍未消费）。

**✗ 失败：** Abigail 不回 / 调用错 conversation / 仍引 Case 1 委托 → 改动影响既有路径，回报。

---

## Pass / Fail 判定

**通过条件（必需全部）：**

- [ ] 启动 6 步 + 健康检查 4 项全过
- [ ] **Case 1**: 5s 内日志含 `"event":"npc_message" "conversation":"abigail"`
- [ ] **Case 1**: Abigail 主动 chat_say 或 silent inbox ack 或下次直聊时上下文连贯
- [ ] **Case 1.4**: 走到 Abigail 开聊时引用 XiaMi 委托
- [ ] **Case 2**: XiaMi 对"今天天气真好"自然回应，日志只出 `"event":"chat_message"`，无 `"event":"npc_message"`
- [ ] **Case 3**: XiaMi 答时间前调用 `game_get_time`，回答含具体时间
- [ ] **回归 Case**: Abigail 对"早上好"自然回应，路径未坏

---

## 失败回报模板

如某步 fail，给 team-lead 附：

1. 哪个 case / checkbox fail
2. `smartnpc-mcp` 日志窗口最后 ~30 行
3. xiami / abigail 两个 WSL gateway 终端的最后 ~20 行
4. 游戏内截图（对话气泡 / 错误 / NPC 朝向）
5. 4 进程窗口（Hermes Gateways / smartnpc-mcp / SMAPI 控制台 / 游戏）是否仍活
6. 时间戳，方便 cross-ref 各路日志

---

## 已知 / 不阻塞

- `smartnpc-agent/internal/agent/echo` 偶发 WDAC 拦截 `echo.test.exe`（"An Application Control policy has blocked this file"）—— 首次 ci-fast 可能 fail 一次，再跑即过；CLAUDE.md 注 `截至 2026-04-30 不再拦截`。复现时记入 PR description。
- 群聊 / M6 流程不在 delegate-fix scope，不验证。

---

## 后续

- 全部 ✓ → SendMessage team-lead 确认 → team-lead 决定 commit / merge 顺序
- 任一 ✗ → 按"失败回报模板"反馈 → team-lead 决定回滚 / 补丁 / 重测
