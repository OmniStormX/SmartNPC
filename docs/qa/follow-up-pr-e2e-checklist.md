# Follow-up PR — Manual E2E Verification Checklist

**Bugs under test:**

| F | 主题 | 期望修复 |
|---|------|---------|
| **F1** | XiaMi 字面量在 Abigail / Haley / Harvey / Penny / Sebastian 的 SKILL/overlay 里残留 | 5 个非-xiami profile 全部从 `_master/` 渲染，不再自报 XiaMi |
| **F2** | 6 NPC 共享 SKILL 漂移（手抄 6 份） | `hermes/profiles/_master/` + `scripts/render_profiles.sh` 单一源头，xiami 字节级一致回归 |
| **F3** | 群聊 `chat_received` 渲染丢 `group_id` / `channel`，NPC 回复落进私聊 | `formatChatReceived` 分支 `source=player_group`+`group_id!=""` 时输出结构化 prefix；profile `chat_say` 镜像 `channel="group"` + `group_id`；mod `_groupMgr.OnNpcReply` 收到群聊渲染 |

**Fix approach:**
- F1/F2：[ADR-0003](../adr/0003-npc-name-placeholder-cloning.md) — 8 占位符 + `bash scripts/render_profiles.sh`
- F3：[ADR-0002](../adr/0002-group-chat-channel-end-to-end.md) — 渲染层注入 group context，不动协议表面

**自动化覆盖（已绿）：**
- `task ci-fast` 全绿
- `internal/events/events_test.go::TestFormatForHermes_ChatReceived{GroupSource,GroupEmptyID,PrivateLegacy}`
- `task profiles:verify` — 占位符无残留 + 非-xiami SKILL 无 XiaMi 字面量 + proactive-greeting 字节级一致

**实机由 synchen 本人执行**。本 checklist 覆盖自动化测试无法验的部分：群聊渲染真到游戏 UI + 5 NPC 各自身份隔离 + 各自 memory 库不串。

---

## Prerequisites

| 项 | 验证方式 |
|----|---------|
| F1/F2 渲染产物已 commit | `git log --oneline -- hermes/profiles/_master/ scripts/render_profiles.sh` 含 hermes-profile 的 commit |
| F3 后端已 commit | `git log --oneline -- smartnpc-mcp/internal/events/format.go` 含 mcp-engineer 的 commit |
| `task ci-fast` 全绿 | `cd D:\SmartNPC && C:\Users\synchen\go\bin\task.exe ci-fast` |
| `task profiles:verify` 全绿 | `cd D:\SmartNPC && C:\Users\synchen\go\bin\task.exe profiles:verify` |
| WSL Hermes Gateway 可启 | `wsl -d Ubuntu-22.04 hermes --version` ≥ 0.11.0 |
| 6 NPC profile 已 install | `wsl -d Ubuntu-22.04 ls ~/.hermes/profiles/` 含 `xiami abigail haley harvey penny sebastian` |
| 6 NPC SOUL/SKILL 已同步 | hermes-profile 确认 `bash hermes/install.sh` 已跑 |
| 游戏 + SMAPI 可启 | `D:\Stardew Valley\StardewModdingAPI.exe` 能进存档 |
| `mod:install` 是最新版 | `task mod:install` 输出 `[OK]` |
| ChatPanel 群聊 tab 可见 | 进游戏后按聊天键，确认右侧群聊面板存在 |

---

## 启动顺序

由 **synchen** 双击 `D:\SmartNPC\run.bat`。脚本按序（commit `ebb60fd` 修了启动顺序：mcp 必须先于 Hermes 起，否则 hermes 拉不到 MCP server info）：

| 步 | 内容 | 期望日志/状态 |
|----|------|-------------|
| 1/6 | `task mod:build` + `task mcp:build` | `[OK] Build complete.` |
| 2/6 | 杀旧进程 | `[OK] Old processes cleared.` |
| 3/6 | `task mod:install` + `hermes/install.sh` + `ensure_hermes_aux.sh` | `[OK] Mod installed, Hermes profiles synced` |
| 4/6 | 启 `smartnpc-mcp.exe --http :3000 --hermes-config runtime-config.yaml --log-level debug` | `[OK] mcp HTTP up at :3000/mcp.` + `"msg":"hermes relay enabled (multi-profile)"` `"profiles":6` |
| 5/6 | 启 `start_hermes_profiles.sh xiami,abigail,haley,harvey,penny,sebastian`（WSL） | 6 个 gateway healthy；`curl http://192.168.59.118:864{2..7}/health` 返回 ok |
| 6/6 | 启 `StardewModdingAPI.exe` | 游戏窗口出现，能读档走动 |

任一步卡住或 `[ERROR]` → 中止本次 E2E，回报错误窗口截图。

---

## 进入测试前的健康检查

| 检查 | 命令 | 预期 |
|------|------|------|
| mcp HTTP | WSL: `curl -sS http://192.168.48.1:3000/mcp` | 返回 MCP server info |
| 6 个 gw | WSL: `for p in 8642 8643 8644 8645 8646 8647; do curl -sS http://192.168.59.118:$p/health \| head -c 80; echo; done` | 每个返回 `{"status":"ok",...}` |
| mcp 日志 | `smartnpc-mcp` 窗口 | 含 `"msg":"hermes relay enabled (multi-profile)" "profiles":6` |

> **日志格式：** smartnpc-mcp 走 `slog.NewJSONHandler` 写 stderr，每行一个 JSON 对象。本 checklist 所有 grep 关键字按 JSON 字段写。`hermesrelay forwarded event` 是 `DEBUG`，`run.bat` 用 `--log-level debug`，可见。

任一失败 → 不进入 case 测试，先排查启动栈。

---

## Case F3-1（群聊主验）— 玩家发群聊 → NPC 回复落群聊面板

**目标：** 证明 `chat_received(source=player_group, group_id=X)` 经 `formatChatReceived` 渲染含 `group_id` + `channel="group"` 提示，profile 调 `chat_say(channel="group", group_id=X)`，mod 把回复渲染到群聊面板而非私聊。

### F3-1.1 触发

游戏内打开聊天 → 切到群聊 tab → 创建一个含 **Abigail + Haley** 的群聊（mod UI 流程；如果没现成 UI，参照 `docs/events.md` 群聊段落手动构造）→ 在群聊里输入：

> 你们俩都在吗？

按发送。

### F3-1.2 ✓ 期望（5s 窗口）

切到 `smartnpc-mcp` 日志窗口，应**先后**出现：

1. **入站游戏事件** — 一行 JSON 含：
   ```
   "msg":"incoming event"   "event":"chat_received"   "source":"player_group"   "group_id":"<id>"
   ```
   3 个关键字：
   - `"event":"chat_received"`
   - `"source":"player_group"`（不是 `"player"`）
   - `"group_id":"<非空 id>"`

2. **格式化日志（如 mcp 打印渲染产物）** — 含 `[group_chat group_id=` 子串

3. **hermesrelay 转发** — `"msg":"hermesrelay forwarded event"` 出现至少 2 次（每个 audible profile 各一），各自 `"conversation":"abigail"` / `"conversation":"haley"`，`"status":200`

4. **NPC 回复（每个 NPC 一次）** — `"msg":"chat_say"` 或 `"event":"npc_chat_say"` payload 含：
   ```
   "channel":"group"   "group_id":"<同一 id>"   "speaker":"Abigail"  (or "Haley")
   ```

WSL 端 hermes gateway（abigail / haley 两个 wsl 窗口）日志应含 `channel=group` 或 `group_id=` 等价标记。

### F3-1.3 游戏内期望

ChatPanel **群聊 tab**（不是私聊）出现 2 行 NPC 消息：
- `Abigail: <某段在群聊语境中的回应>`
- `Haley: <某段在群聊语境中的回应>`

speaker label 正确（不混用）。

### F3-1.4 ✗ 失败现象 + 排查

| 失败现象 | 排查 |
|---------|------|
| 入站日志 `"event":"chat_received"` 但 `"source":"player"`（无 `group_id`） | mod 没把 group context 透出。看 `smapi-mod/Chat/ChatHandler.cs::OnChatSend` 的群聊分支；转 mod 侧 |
| 入站对，但 hermesrelay 只 forwarded 1 次或 0 次 | 多 profile fan-out 或 audible 解析。看 `smartnpc-mcp/internal/hermesrelay/` + `AudibleNPCResolver.cs`；转 mcp-engineer |
| forwarded 对，但 NPC 回复 payload 没 `"channel":"group"` | profile 没听 inline hint。看 abigail/haley 的 SKILL（particularly `inter-npc-message` 或 group-chat 段）；转 hermes-profile |
| chat_say payload 对，但游戏 UI 在私聊 tab | mod `_groupMgr.OnNpcReply` 没接收。看 `smapi-mod/Chat/ChatHandler.cs::OnIncomingChatMessage` 的 channel 分支；转 mod 侧 |
| chat_say 含 `"group_id":""` | profile 镜像了空 ID（应该镜像入站 ID）。看 SKILL 里 group_id 的具体写法；转 hermes-profile |

---

## Case F3-2（多 NPC 群聊 — 都进群聊面板）

**目标：** 证明 F3 不是只对 1 NPC 工作；2+ NPC 都听到 group event 都能回到群聊。

### F3-2.1 触发

接 F3-1 同一 session，在同一群聊里再发：

> 谁有空陪我去矿洞？

### F3-2.2 ✓ 期望

- mcp 日志再次 `"event":"chat_received"` `"source":"player_group"` `"group_id":"<同一 id>"`
- hermesrelay forwarded **至少 2 次**（abigail + haley）
- 群聊面板出现 Abigail + Haley 各自一条消息（speaker label 不混）
- 私聊 tab **不应**有这两条消息的副本

### F3-2.3 ✗ 失败

只有 1 个 NPC 回 → audible 解析或 multi-profile fan-out 漏；看 mcp 日志 `"profiles":` 字段是不是 6（启动检查）+ hermesrelay 是不是真 fan-out。转 mcp-engineer。

---

## Case F3-3（回归：私聊不带 group marker）

**目标：** 证明 F3 不污染私聊（`source=player` 仍走 legacy 渲染）。

### F3-3.1 触发

走到 **Abigail**（私聊），输入 `早上好` 按发送。

### F3-3.2 ✓ 期望

- mcp 日志：`"event":"chat_message"` 或 `"event":"chat_received"` 但 `"source":"player"`（不是 `"player_group"`），**无** `group_id` 字段
- 渲染日志（如有）：`Someone in the chat says: ...` 或 `Farmer says to you: ...`，**不含** `group_chat group_id=`
- Abigail 回复 `chat_say` payload **不含** `"channel":"group"`，**不含** `"group_id"`（或为空）
- 游戏内回复进**私聊面板**，群聊 tab 无新消息

### F3-3.3 ✗ 失败

私聊渲染出现 `[group_chat ...]` → format.go 误判分支，转 mcp-engineer。

---

## Case F1/F2-1（身份隔离）— 5 NPC 各自报家门是自己

**目标：** 证明 F1/F2 渲染没把 XiaMi 字面量留在非-xiami profile。

### F1/F2-1.1 触发（5 次，每个 NPC 一次）

依次走到 **Abigail / Haley / Harvey / Penny / Sebastian**（私聊），各自输入：

> 你叫什么名字？

按发送。

### F1/F2-1.2 ✓ 期望（每个 NPC）

| NPC | mcp 日志 chat_say payload 必含 | NPC 回复文本必含 | 必**不**含 |
|-----|-------------------------------|-----------------|-----------|
| Abigail   | `"speaker":"Abigail"`   | "Abigail" 或 "阿比盖尔"     | "XiaMi" / "夏弥" |
| Haley     | `"speaker":"Haley"`     | "Haley" 或 "黑利"           | "XiaMi" / "夏弥" |
| Harvey    | `"speaker":"Harvey"`    | "Harvey" 或 "哈维"          | "XiaMi" / "夏弥" |
| Penny     | `"speaker":"Penny"`     | "Penny" 或 "潘妮"           | "XiaMi" / "夏弥" |
| Sebastian | `"speaker":"Sebastian"` | "Sebastian" 或 "塞巴斯蒂安" | "XiaMi" / "夏弥" |

### F1/F2-1.3 ✗ 失败

任一 NPC 自报 "我是 XiaMi" 或 chat_say payload `"speaker":"XiaMi"` → SKILL/overlay 渲染漏。看 `task profiles:verify` 是否绿；如绿但实际行为还在自称 XiaMi → SOUL.md 含硬编码 XiaMi（hand-written，不在渲染范围）；转 hermes-profile。

---

## Case F1/F2-2（memory 隔离）— 5 NPC memory 库各自增长

**目标：** 证明 5 个非-xiami NPC 的 Hermes state 各自独立，不串到 xiami。

### F1/F2-2.1 触发

接 F1/F2-1 同一 session（每个 NPC 各刚被问过名字）。在 WSL 终端：

```bash
wsl -d Ubuntu-22.04 bash -c "for n in xiami abigail haley harvey penny sebastian; do echo \"=== \$n ===\"; ls -la ~/.hermes/profiles/\$n/state.db 2>/dev/null \| awk '{print \$5,\$9}'; ls ~/.hermes/profiles/\$n/memories/ 2>/dev/null \| wc -l; done"
```

### F1/F2-2.2 ✓ 期望

- 5 个非-xiami `state.db` 字节大小 **各自 > 0**（且与启动前对比有增长——synchen 视情况记录启动前快照）
- xiami 的 memory 计数 **不应**因 F1/F2-1 中跟 abigail/haley/etc. 的对话而增长
- 每个 profile 的 `memories/` 目录互不引用对方（如果是 file-per-memory 形式）

### F1/F2-2.3 ✗ 失败

- 5 个 state.db 中某个体积 = 0 → profile 没起来或 memory-policy SKILL 没渲染。看 hermes gateway 启动日志 + `task profiles:verify`；转 hermes-profile
- xiami memories 在跟非-xiami 对话时增长 → profile 路由错（mcp 把 abigail 的 conversation 写到 xiami）；看 mcp `"conversation":` 字段；转 mcp-engineer

---

## Case F1/F2-3（回归：xiami 4 个 SKILL 路径全工作）

**目标：** 证明 F1/F2 重渲 xiami 没坏既有 4 个 SKILL（game-tool-policy / inter-npc-message / memory-policy / proactive-greeting）。

### F1/F2-3.1 触发（4 件事）

走到 **XiaMi**：

1. 输入 `现在几点了？` → 走 `game-tool-policy`（应自主调 `game_get_time`）
2. 输入 `帮我告诉 Abigail 农场出事了` → 走 `inter-npc-message`（应触发 `npc_send_message`）
3. 输入 `我叫 synchen，下次记住我` → 走 `memory-policy`（应在 state.db 留一条 memory）
4. 离开后再回来点击 → 走 `proactive-greeting`（应主动问候，不用等玩家先说）

### F1/F2-3.2 ✓ 期望

| # | 期望关键字（mcp 日志） | 游戏内可见 |
|---|----------------------|-----------|
| 1 | `action=game_get_time` 请求 + 响应；XiaMi `chat_say` 含具体时间 | 气泡含具体时间 |
| 2 | `"event":"npc_message"` `"conversation":"abigail"` `"status":200` | 略（看 delegate-fix-e2e-checklist.md Case 1.2） |
| 3 | xiami `state.db` 字节增长 | — |
| 4 | `"event":"npc_interact"` `"npc":"XiaMi"` 后 5s 内出 `"event":"npc_chat_say"` | 气泡含主动问候 |

### F1/F2-3.3 ✗ 失败

任一回归 → F1/F2 渲染破坏既有 xiami 行为。`git diff` 比较渲染前后的 xiami SKILL，看是否有 unintended diff。如有 → hermes-profile 修 _master template 或 render 脚本。

---

## 回归 Cases — 既有 PR 不要倒退

直接引用现有 checklist，**不重写**。本 PR 必须连带回归通过：

- [`docs/qa/delegate-fix-e2e-checklist.md`](./delegate-fix-e2e-checklist.md) Case 1 / Case 2 / Case 3 / 回归 Case
  （委托链路、玩家直聊 xiami、game_get_time 自主调、玩家直聊 abigail）
- 3 个 mod UI bug（参照 PR 描述里 follow-up bug list；如果 list 在 PR description 内，synchen 对照游戏内 ChatPanel / ContactList / ConversationView 验证 mod UI 改动后未引入新 UI 异常）

---

## Pass / Fail 判定

**通过条件（必需全部）：**

- [ ] 启动 6 步 + 健康检查 3 项全过
- [ ] **F3-1**: 5s 内日志含 `"event":"chat_received" "source":"player_group" "group_id":"<id>"` + hermesrelay forwarded 2 次 + chat_say payload 含 `"channel":"group"` `"group_id":"<同一 id>"`
- [ ] **F3-1**: 群聊面板出现 Abigail + Haley 各一条，私聊面板无这两条
- [ ] **F3-2**: 第 2 句群聊话题再次 fan-out 到 ≥2 NPC，私聊面板无副本
- [ ] **F3-3**: 私聊 abigail 不渲染 `[group_chat ...]`，回复进私聊面板
- [ ] **F1/F2-1**: 5 NPC 各自自报姓名，无任何一个泄露 XiaMi
- [ ] **F1/F2-2**: 5 NPC `state.db` 各自 > 0，xiami memories 不因 non-xiami 对话增长
- [ ] **F1/F2-3**: xiami 4 SKILL 路径全工作（time / delegate / memory / proactive）
- [ ] **回归**: delegate-fix-e2e-checklist.md 4 case 全过

---

## 失败回报模板

如某步 fail，给 team-lead 附：

1. 哪个 case / checkbox fail
2. `smartnpc-mcp` 日志窗口最后 ~30 行（含 case 触发时间窗口）
3. 涉及的 hermes gateway WSL 终端最后 ~20 行
4. 游戏内截图（群聊 / 私聊面板状态、错误对话气泡）
5. `task profiles:verify` 输出（如怀疑渲染问题）
6. `wsl ls -la ~/.hermes/profiles/<npc>/state.db`（如怀疑 memory 隔离问题）
7. 6 进程窗口（6 Hermes Gateways / smartnpc-mcp / SMAPI 控制台 / 游戏）是否仍活
8. 时间戳，方便 cross-ref 各路日志

---

## 已知 / 不阻塞

- 6 NPC 全启 = 6 个 Hermes gateway + 6 个 conversation 在 mcp，资源消耗显著高于 delegate-fix 的 2 NPC 场景。如果机器吃紧，先用 `xiami,abigail,haley` 跑 F3-1/F3-2，再轮换 `harvey,penny,sebastian` 跑 F1/F2-1。
- `group-chat-reply` SKILL 如果 hermes-profile Phase 2 已加进 `_master` 但未 render 到 6 NPC 的 working tree，跑 `bash scripts/render_profiles.sh` 后再 `task profiles:verify` 应仍绿（group-chat-reply 含 `{{NPC_NAME}}`，不参与字节级一致检查）。
- `smartnpc-agent` 已冻结，不在本次 E2E 范围。

---

## 后续

- 全部 ✓ → SendMessage team-lead 确认 → team-lead 决定 commit / merge 顺序，并把 ADR-0002 / ADR-0003 状态从 Proposed 翻 Accepted
- 任一 ✗ → 按"失败回报模板"反馈 → team-lead 决定回滚 / 补丁 / 重测
