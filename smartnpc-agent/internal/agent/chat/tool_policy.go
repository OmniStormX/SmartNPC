package chat

import "fmt"

// DefaultNPCToolPolicy is the shared tool usage guidance injected into every
// NPC's system prompt. Individual NPCs can add overrides via tool_overrides.
const DefaultNPCToolPolicy = `你可以在游戏世界中执行以下行动：
- npc_move_to: 移动到指定位置
- npc_summon: 传送到玩家附近
- npc_follow_start: 开始跟随玩家
- npc_follow_stop: 停止跟随
- npc_lead_to: 带路到目标位置
- npc_get_nearby: 观察附近的人
- npc_get_environment: 感受当前环境
- npc_get_position: 查看自己的位置
- npc_face_direction: 转向某个方向
- npc_send_message: 给其他NPC传话
- game_get_time: 查询当前时间（通常已预取）
- game_get_weather: 查询天气（通常已预取）
- friendship_get: 查询好感度（通常已预取）

规则：
- 玩家让你移动时，必须调用工具执行，不要说"我无法移动"
- 时间/天气/好感度系统已预取到上下文，无需重复查询（除非需要刷新）
- 不要向玩家暴露坐标数字或JSON数据
- 将工具结果自然融入你的对话风格`

// BuildToolPolicy returns the tool policy for a given profile and speaker.
// profile is currently a reserved knob for future specialised policies
// (combat NPC, shopkeeper, etc.); today all profiles fall back to the
// default policy.
func BuildToolPolicy(profile, speaker string) string {
	_ = profile // reserved
	base := DefaultNPCToolPolicy
	if speaker != "" {
		base += fmt.Sprintf("\n- 所有工具调用时，npc 参数必须填 %q", speaker)
	}
	return base
}
