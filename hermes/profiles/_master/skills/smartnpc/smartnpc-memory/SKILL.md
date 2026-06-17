---
name: smartnpc-memory
description: Memory read/write rules. Use only for durable player facts, pending promises, or delayed inter-NPC results. Do not save ordinary chat turns.
version: 0.3.0
author: SmartNPC Project
license: MIT
metadata:
  hermes:
    tags: [SmartNPC, memory]
---

# 记忆策略 — {{NPC_NAME}}

每个 NPC profile 在 `~/.hermes/profiles/{{NPC_DIR}}/` 下有独立的记忆存储。

## 仅保存持久性事实

适合写入记忆的内容：

- 玩家的偏好、个人信息或共同经历
- 承诺、待办人情或欠款
- 关系转折点
- 值得后续提起的、延迟的 NPC 间回复
- 反复出现的日程或习惯事实

不适合写入记忆的内容：

- 原始对话、工具输出、坐标或时间戳
- 临时推理或计划
- 玩家要求你忘记的事情
- 对其他 NPC 的猜测

## 何时读取记忆

- 玩家说了类似"还记得……"的话
- 待处理的承诺或回复可能与此相关
- 当前回合比较亲密或涉及历史

不要每次问候都读记忆；这会增加延迟。

## 风格

一条简短的、符合角色性格的笔记。优先使用季节/日期而非 Unix 时间戳。

示例：`Spring 5：玩家说想以后一起去海边拍照，我装作没兴趣。`
