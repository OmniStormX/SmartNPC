"""Generate SmartNPC four-layer architecture diagram as Excalidraw JSON."""
import json, uuid

def uid():
    return uuid.uuid4().hex[:20]

def make_rect(x, y, w, h, bg="#ffffff", stroke="#64748b", sw=1.5, rd=3):
    return {
        "id": uid(), "type": "rectangle",
        "x": x, "y": y, "width": w, "height": h,
        "angle": 0, "strokeColor": stroke,
        "backgroundColor": bg, "fillStyle": "solid",
        "strokeWidth": sw, "roughness": 1, "opacity": 100,
        "groupIds": [], "frameId": None,
        "roundness": {"type": rd}, "seed": 1,
        "version": 1, "isDeleted": False,
        "boundElements": None, "updated": 1,
        "link": None, "locked": False,
    }

def make_text(x, y, text, size=14, color="#1e293b", bold=False):
    return {
        "id": uid(), "type": "text",
        "x": x, "y": y, "width": 10, "height": size + 8,
        "angle": 0, "strokeColor": "transparent",
        "backgroundColor": "transparent", "fillStyle": "solid",
        "strokeWidth": 1, "roughness": 1, "opacity": 100,
        "groupIds": [], "frameId": None,
        "roundness": None, "seed": 1,
        "version": 1, "isDeleted": False,
        "boundElements": None, "updated": 1,
        "link": None, "locked": False,
        "text": text, "fontSize": size, "fontFamily": 5,
        "textAlign": "center", "verticalAlign": "top",
        "containerId": None, "originalText": text,
        "autoResize": True, "lineHeight": 1.25,
    }

def make_arrow(start_id, end_id, color="#64748b", style="solid", label=""):
    dash = style == "dashed"
    return {
        "id": uid(), "type": "arrow",
        "x": 0, "y": 0, "width": 10, "height": 10,
        "angle": 0, "strokeColor": color,
        "backgroundColor": "transparent", "fillStyle": "solid",
        "strokeWidth": 1.8, "roughness": 1, "opacity": 100,
        "groupIds": [], "frameId": None,
        "roundness": {"type": 2}, "seed": 1,
        "version": 1, "isDeleted": False,
        "boundElements": None, "updated": 1,
        "link": None, "locked": False,
        "points": [[0, 0], [0, 40]],
        "startBinding": {
            "elementId": start_id, "focus": 0,
            "gap": 4, "fixedPoint": [0.5, 1],
        },
        "endBinding": {
            "elementId": end_id, "focus": 0,
            "gap": 4, "fixedPoint": [0.5, 0],
        },
        "startArrowhead": None,
        "endArrowhead": "arrow",
    }

# ── Canvas ──
W = 1500
elements = []

# ═══ Layer Backgrounds ═══
LAYER_H = 175
layers = [
    (20, 50,  1440, LAYER_H, "#f0fdf4", "#10b981"),  # Decision
    (20, 255, 1440, LAYER_H, "#fffbeb", "#f59e0b"),  # Orchestration
    (20, 460, 1440, 105,      "#fdf2f8", "#ec4899"),  # Protocol
    (20, 590, 1440, LAYER_H, "#f0f9ff", "#0ea5e9"),  # Execution
    (20, 790, 1440, 115,      "#f8fafc", "#94a3b8"),  # Legend
]
for x, y, w, h, bg, stroke in layers:
    elements.append(make_rect(x, y, w, h, bg, stroke, 2, 3))

# ═══ Main Title ═══
elements.append(make_text(W//2 - 250, 10, "SmartNPC — NPC Agent 自运转四层架构", 22, "#0f172a", True))
elements.append(make_text(W//2 - 250, 40, "Schedule 时间驱动 + Event 事件驱动 · Windows + WSL2 跨边界", 12, "#64748b"))

# ═══ Layer Titles ═══
elements.append(make_text(W//2 - 250, 58,  "🧠 决策层 — Hermes Agent (WSL2)", 14, "#065f46", True))
elements.append(make_text(W//2 - 250, 263, "⚙️ 编排层 — smartnpc-mcp (Go, Windows)", 14, "#92400e", True))
elements.append(make_text(W//2 - 250, 468, "🔌 协议层 — WebSocket + MCP Streamable HTTP", 14, "#9d174d", True))
elements.append(make_text(W//2 - 250, 598, "🎮 执行层 — C# SMAPI Mod (StardewMCPBridge)", 14, "#075985", True))

# ═══ Layer 1: NPC Profile Cards ═══
npcs = [
    ("xiami ⭐",     "SOUL + 16 SKILLs\nstate.db · :8642"),
    ("abigail",      "SOUL + 16 SKILLs\nstate.db · :8643"),
    ("haley",        "SOUL + 16 SKILLs\nstate.db · :8644"),
    ("harvey",       "SOUL + 16 SKILLs\nstate.db · :8645"),
    ("penny",        "SOUL + 16 SKILLs\nstate.db · :8646"),
    ("sebastian",    "SOUL + 16 SKILLs\nstate.db · :8647"),
]
cw, ch = 215, 115
gap = 12
total_w = 6 * cw + 5 * gap
sx = 40 + (1440 - total_w) // 2
cy = 80
for i, (name, desc) in enumerate(npcs):
    hl = (i == 0)
    c = make_rect(sx + i*(cw+gap), cy, cw, ch,
                  "#d1fae5" if hl else "#ecfdf5",
                  "#059669" if hl else "#10b981",
                  2 if hl else 1, 3)
    elements.append(c)
    elements.append(make_text(sx + i*(cw+gap) + cw//2 - 80, cy + 10, name, 14, "#065f46", True))
    elements.append(make_text(sx + i*(cw+gap) + cw//2 - 100, cy + 45, desc, 11, "#047857"))

# Layer 1 annotation
elements.append(make_text(60, 210, "💾 state.db 长期记忆 · SOUL.md 人格 · critical-policy.md 硬规则 · config.yaml MCP 连接", 11, "#065f46"))
elements.append(make_text(1100, 210, "☁ LLM API (api.deepseek.com) ← HTTPS ← Hermes Agent", 11, "#6366f1"))

# ═══ Layer 2: Orchestration Cards ═══
orch = [
    ("Scheduler",        "Tick → FiredEntry[]\nper-NPC DaySchedule"),
    ("Workflow Engine",  "8 step types\nPrecompileSkill"),
    ("hermesrelay",      "POST /v1/responses\ninstructions 注入"),
    ("MCPRunner",        "CallTool · CallSkill\nLLMChoice · WaitIdle"),
    ("npcWorkflowWorker","per-NPC goroutine\n串行消费 + 主动取消"),
]
ocw, och = 270, 100
ogap = 10
ototal = 5 * ocw + 4 * ogap
osx = 40 + (1440 - ototal) // 2
ocy = 285
for i, (name, desc) in enumerate(orch):
    hl = (i < 3)
    c = make_rect(osx + i*(ocw+ogap), ocy, ocw, och,
                  "#fef3c7" if hl else "#fef9c3",
                  "#d97706" if hl else "#f59e0b",
                  2 if hl else 1, 3)
    elements.append(c)
    elements.append(make_text(osx + i*(ocw+ogap) + ocw//2 - 80, ocy + 10, name, 13, "#92400e", True))
    elements.append(make_text(osx + i*(ocw+ogap) + ocw//2 - 100, ocy + 48, desc, 11, "#78350f"))

elements.append(make_text(W//2 - 250, 398, "Worker → Engine → MCPRunner → ws request · 同 NPC 串行，新触发 cancel 旧 workflow", 11, "#92400e"))

# ═══ Layer 3: Protocol Cards ═══
prot = [
    ("WebSocket :18745/ws",  "JSON 文本帧\nrequest(id) ↔ response(id)"),
    ("MCP HTTP :3000/mcp",       "Streamable HTTP\ntools/list · tools/call"),
    ("Event Push",               "chat_message · npc_interact\nday_started · game_time_tick"),
]
pw, ph = 450, 72
pgap = 15
ptotal = 3 * pw + 2 * pgap
psx = 40 + (1440 - ptotal) // 2
ppy = 488
for i, (name, desc) in enumerate(prot):
    hl = (i == 1)
    c = make_rect(psx + i*(pw+pgap), ppy, pw, ph,
                  "#fce7f3", "#be185d" if hl else "#ec4899",
                  2 if hl else 1, 3)
    elements.append(c)
    elements.append(make_text(psx + i*(pw+pgap) + pw//2 - 100, ppy + 10, name, 13, "#9d174d", True))
    elements.append(make_text(psx + i*(pw+pgap) + pw//2 - 100, ppy + 38, desc, 11, "#831843"))

# ═══ Layer 4: Execution Cards ═══
exec_c = [
    ("FollowSystem",     "19 NpcBehaviorMode\nPumpOnGameTick 逐帧驱动"),
    ("NpcActionQueue",   "per-NPC FIFO 排队\n即时 | 串行 | 可抢占"),
    ("MessageRouter",    "action → handler\n分发 + ws ack 返回"),
    ("18x 行为 Handler", "Wander · ClearDebris\nWater · Harvest · Plant\nDeposit · Deliver …"),
    ("Chat UI + 面板",   "Tab/F2/Esc 快捷键\n聊天气泡 + 联系人列表"),
]
ew, eh = 270, 100
egap = 10
etotal = 5 * ew + 4 * egap
esx = 40 + (1440 - etotal) // 2
ey = 620
for i, (name, desc) in enumerate(exec_c):
    hl = (i == 0)
    c = make_rect(esx + i*(ew+egap), ey, ew, eh,
                  "#e0f2fe" if hl else "#f0f9ff",
                  "#0284c7" if hl else "#0ea5e9",
                  2 if hl else 1, 3)
    elements.append(c)
    elements.append(make_text(esx + i*(ew+egap) + ew//2 - 80, ey + 10, name, 13, "#075985", True))
    elements.append(make_text(esx + i*(ew+egap) + ew//2 - 100, ey + 45, desc, 11, "#0369a1"))

elements.append(make_text(W//2 - 250, 735, "Agent 驱动 5x 移速 → 寻路至目标 tile → 执行游戏操作 → 头顶气泡反馈 → Idle 恢复 2x", 11, "#0369a1"))

# ═══ Cross-boundary annotations ═══
elements.append(make_text(550, 243, "▼ MCP tool call 回连 WIN_HOST_IP:3000/mcp", 10, "#10b981"))
elements.append(make_text(950, 243, "▲ hermesrelay POST /v1/responses → WSL_IP:8642-8647", 10, "#d97706"))

# ═══ Legend ═══
elements.append(make_text(W//2 - 250, 798, "图 例", 14, "#475569", True))
elements.append(make_text(60, 820, "⏰ Schedule 驱动: day_started → npc_plan_day → Scheduler 存储 Entry[] → game_time_tick → Tick → Worker → skill_call → LLM → tool call → ws → FollowSystem 执行", 11, "#92400e"))
elements.append(make_text(60, 848, "📡 Event 驱动:   chat_message / npc_interact → ws event → hermesrelay 路由 → POST /v1/responses → LLM 决策 → MCP tool call → ws → C# 气泡 + 面板", 11, "#9d174d"))
elements.append(make_text(60, 876, "── 实线 = 直接调用 / ws 请求     - - - 虚线 = 跨边界 MCP/HTTP     ⚡ 同 NPC 严格串行 · 10min 精度 · ≤ 匹配防丢帧", 10, "#64748b"))

# ── Output ──
diagram = {
    "type": "excalidraw",
    "version": 2,
    "source": "https://excalidraw.com",
    "elements": elements,
    "appState": {"viewBackgroundColor": "#ffffff", "gridSize": 20},
    "files": {},
}

out_path = "D:/SmartNPC/docs/diagrams/four-layer-architecture.excalidraw"
with open(out_path, "w", encoding="utf-8") as f:
    json.dump(diagram, f, indent=2, ensure_ascii=False)

print(f"OK: {len(elements)} elements → {out_path}")
