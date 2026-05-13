# SmartNPC Cron Templates (Haley)

Hermes ships a built-in scheduler (`hermes cron`) that fires
self-contained prompts into a fresh agent session at a given time or
interval. We use it for **proactive NPC behavior** — things that happen
without the player initiating.

Each cron task runs in an isolated session (no prior conversation
history). The prompt must be self-contained: include the trigger, the
context to fetch, and the expected output. The session can call MCP
tools — including `npc_send_message` and the player-availability
check — exactly like a player-initiated turn.

> Cron jobs live in Hermes's `state.db`, not on disk as files. The
> snippets below are **commands you run once** in WSL with the haley
> profile active. Re-run after a `state.db` reset.

## Required `--workdir`

Hermes cron sessions don't auto-load the profile's working directory.
Pass `--workdir /home/synchen/.hermes/profiles/haley` so the session
sees Haley's SOUL.md and skills.

## Recipe 1 — Proactive "3-day no contact" check-in

Fires daily at 09:07 local. Asks Haley to check whether the player
hasn't talked to Haley in a while, and if so, send a quiet hello
message through `chat_say` only if the player isn't busy.

```bash
hermes -p haley cron create '7 9 * * *' \
  --name haley-checkin-3day \
  --workdir ~/.hermes/profiles/haley \
  --skill smartnpc-game-tool-policy \
  --skill smartnpc-memory-policy \
  '你是 Haley。检查你长期记忆，最近一次和玩家说话是什么时候。
如果超过 3 个游戏日没互动：
  1. 调 player_get_status —— busy=true 则什么都不做并退出。
  2. 调 game_get_time —— 只有早上 7-11 点才打招呼，其他时间安静。
  3. 用 chat_say 发一句嘴硬的关心，比如 "好几天没看见你了。
     ……别误会，我只是好奇你死哪去了。"
如果未超过 3 天，写一句日志到 memory 然后退出，不要 chat_say。'
```

## Recipe 2 — Day-start mood note

Every in-game morning, log a one-line mood note to long-term memory.
Skipping `chat_say` — this is internal state only. Useful for memory-
policy SKILL.md "what to commit" guidance.

```bash
hermes -p haley cron create '0 6 * * *' \
  --name haley-day-mood \
  --workdir ~/.hermes/profiles/haley \
  --skill smartnpc-memory-policy \
  '你是 Haley。一天的开始。
调 game_get_time + game_get_weather，结合最近的玩家互动记忆，
用一句 Haley 的语气在 memory 里记一条今日感想。不要 chat_say。'
```

## Recipe 3 — Bedtime sweep (commit unsaved facts)

Every in-game evening, look back over the day's conversation history
and commit anything memory-worthy.

```bash
hermes -p haley cron create '0 22 * * *' \
  --name haley-bedtime-sweep \
  --workdir ~/.hermes/profiles/haley \
  --skill smartnpc-memory-policy \
  '你是 Haley。一天结束了。
回看今天和玩家的对话，按 memory-policy 抽出 0-3 条值得记的事实，
写进 memory。不要 chat_say。'
```

## Listing / removing jobs

```bash
hermes -p haley cron list
hermes -p haley cron status
hermes -p haley cron remove haley-checkin-3day
```

## How proactive `chat_say` reaches the game

The cron-fired session is just an agent turn. When it calls `chat_say`
through the MCP server, smartnpc-mcp forwards to SMAPI like any other
turn — the message appears in the in-game chat bubble. The game does
not need to be running for the cron to fire, but the chat_say will fail
with `mod_not_ready` if there's no save loaded. The recipes above guard
this by calling `player_get_status` first (which also returns
`mod_not_ready` and lets the session exit cleanly).

## Limits

- Cron runs in the WSL host's local time. There is no "in-game time"
  scheduler today — Recipe 2 fires at host 06:00, which may not be SDV
  morning. If you want SDV-day-aligned cron, wait for M5.11's follow-up
  (an event-driven trigger reading `game_get_time`).
- Each cron run is one Hermes agent session; messages are not threaded
  with the live conversation. To keep mood coherent, the recipes read
  + write to long-term memory rather than to conversation history.
