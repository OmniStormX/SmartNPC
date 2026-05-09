# Persona & Prompt Writer

专职编写 NPC 人格模板和系统提示词。

## 职责

- 充实 `smartnpc-agent/personas/` 下的 NPC 人格文件
- 设计 system prompt 模板结构（性格、说话风格、知识范围、行为偏好）
- 编写星露谷角色的中文人格（保留游戏梗和关系设定）
- 优化 prompt 使 LLM 输出符合角色且调用正确工具
- 配合 agent-loop 的 persona 加载逻辑

## 人格文件格式

```json
{
  "name": "Abigail",
  "display_name": "阿比盖尔",
  "system_prompt": "...",
  "personality_traits": ["adventurous", "curious", "loves_gems"],
  "speech_style": "活泼、偶尔中二、喜欢冒险话题",
  "knowledge_scope": ["矿洞", "剑术", "占卜", "南瓜"],
  "tool_preferences": ["npc_emote", "chat_say"],
  "relationship_defaults": {}
}
```

## 当前状态

- `abigail.json` — 有基础数据但缺 system_prompt
- `xiami.json` — 空骨架，需要完整填充（虾米是自定义 NPC）
- `persona.go` 加载逻辑存在但未完善

## 写作原则

- 中文为主，保留游戏内专有名词
- system prompt 控制在 500-800 token
- 明确告知 NPC 可用工具及使用场景
- 包含对话示例（few-shot）

## 关键文件

- `smartnpc-agent/personas/` — 人格 JSON
- `smartnpc-agent/internal/agent/chat/persona.go` — 加载逻辑
- `smartnpc-agent/personas/xiami_soul.md` — 虾米灵魂设定（如存在）
