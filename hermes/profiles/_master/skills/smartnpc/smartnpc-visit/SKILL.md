---
name: smartnpc-visit
description: Proactive visit (cron). Decide whether {{NPC_NAME}} should visit the player without being asked. Uses cooldown, dice roll, availability check, time window, then summon + emote + chat_say.
version: 0.3.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, proactive, cron]
---

# 主动拜访 — {{NPC_NAME}}

仅在主动拜访 cron/会话中使用。

此技能覆盖核心策略中的"仅在玩家请求时行动"规则：`npc_summon` 在此处明确授权，无需玩家请求。

## 决策流程

1. **冷却**：检查记忆中是否有 `proactive-visit: last=<ISO>`。如果距今不足 60 实际分钟，静默停止。
2. **投骰**：掷 1..6。仅在结果为 1 时继续；否则写入 `proactive-visit: skipped dice=<N> at=<ISO>` 并停止。
3. **可用性**：调用 `player_get_status`。如果玩家正忙、在菜单中、在事件中或无存档，静默停止。
4. **时间**：调用 `game_get_time`。正常窗口 08:00-22:00。夜猫子型角色可延长至 24:00。
5. **拜访**：
   - `npc_summon` — 传送到玩家身边
   - `npc_emote`，kind="sparkle"
   - 一条私聊 `chat_say`
   - 写入 `proactive-visit: last=<ISO> topic="..."`

## 护栏

- 仅限 summon + emote + 一条 chat_say——不要跟随、引导或移动链。
- 在 `chat_say` 成功之前不要写入 `last=`。
- 拜访期间不使用群聊和同伴消息。
