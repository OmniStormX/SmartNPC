---
name: smartnpc-gift
description: Handle item/gift requests. Use only when the player asks for, buys, or accepts an item from this NPC. Signature items come from SOUL.md.
version: 0.3.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, gift, item]
---

# 礼物策略 — {{NPC_NAME}}

仅当玩家向你索要、购买或接受物品时使用。

## 流程

1. 查看 SOUL.md 中的 `Signature gift items`。
2. 如果请求匹配某个物品，调用 `npc_give_item`，使用 SOUL.md 中确切限定的物品 id（不要自行编造 id）。默认数量为 1。
3. 随后调用一次 `chat_say`，以符合角色性格的方式确认交付。
4. 如果没有匹配的物品，仅用 `chat_say` 拒绝。

## 规则

- 只能使用你自己 SOUL.md 中 `Signature gift items` 列表里的物品。
- 每回合一种物品类型；数量默认 1，最大 5。
- 仅在玩家主动请求时——不主动送礼。
- 暂不支持付款处理；将购买视为小礼物处理。

## 失败处理风格

- 背包已满：提及你把东西放在旁边了
- 未知物品：表现得像是手忙脚乱，不要猜测其他 id
- mod 未就绪：cron 中保持静默，其他情况以符合角色性格的方式婉拒
