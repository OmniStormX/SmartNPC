---
name: pixel-farm-chat-ui
description: create original cozy pixel-farm rpg chat interfaces, social panels, relationship messaging screens, matching skills/talents pages, ui prompts, markdown specs, and implementation-ready design briefs. use when the user asks for stardew-valley-like or cozy farming game ui, pixel art chat systems, npc social chat screens, skills pages, talents pages, ui documentation, image prompts, or frontend-ready schemas. avoid copying official stardew valley assets, logos, characters, or exact layouts.
---

# Pixel Farm Chat UI

## Core purpose

Create original cozy pixel-farm RPG UI outputs centered on two connected systems:

1. **Chat / Social Chat UI**: npc conversations, friend list, relationship hearts, gift actions, inbox tabs, timestamps, and message input.
2. **Skills UI**: player growth screen, skill rows, level pips, talents/perks, total skill level, and navigation icons.

The target mood is warm, rustic, pixel-art, farming-life RPG. It may be inspired by the general feeling of games like Stardew Valley, but all layouts, characters, icons, labels, and assets must remain original.

## Style rules

Use these visual conventions unless the user specifies another direction:

- Warm parchment panels with tan, cream, amber, and soft orange tones.
- Chunky wooden outer frames with darker brown pixel shadows.
- Pixel-art icons for tools, crops, fish, gifts, hearts, books, trophies, and gears.
- Small decorative leaves, flowers, sparkles, vines, and corner ornaments.
- Rounded rectangular panels made from pixel-perfect borders.
- Readable pixel-style UI text, not tiny decorative text.
- Green accents for selection states, active tabs, online indicators, and positive friendship states.
- Red hearts for relationship meters; empty heart outlines for missing relationship progress.

Avoid:

- Official Stardew Valley logos, screenshots, sprites, characters, or exact UI copies.
- Overcrowded layouts that make text unreadable.
- Generic modern chat app styling such as flat blue bubbles, glassmorphism, or mobile messenger layouts unless explicitly requested.
- Excessive gradients, glossy 3D UI, or non-pixel-art illustration.

## Workflow

1. Identify the requested deliverable:
   - image generation prompt
   - Markdown UI specification
   - frontend component spec
   - JSON data model
   - full `SKILL.md`
   - chat screen only
   - skills screen only
   - combined chat + skills package

2. Translate style requests like "星露谷风格" into:
   - "cozy pixel-farm RPG style"
   - "warm parchment-and-wood UI"
   - "original farming-life game interface"

3. Build the output around a reusable information architecture:
   - top navigation/header
   - player or npc identity block
   - main content panel
   - contextual side panel
   - footer actions and navigation icons

4. Keep Chat and Skills screens visually consistent:
   - same wood frame treatment
   - same parchment base color
   - same pixel icon density
   - same button and tab styles
   - same corner ornaments and leaf accents

5. Add implementation value when appropriate:
   - component list
   - UI states
   - sample data
   - JSON schema
   - interaction notes
   - quality checklist

## Chat UI specification

A complete chat/social screen should usually include:

### Header

- Center title: `Chat`, `Social Chat`, or localized title requested by the user.
- Left icon tabs: speech bubble, envelope/inbox, notification badge.
- Right status block: season, day, weekday, weather icon, close button.

### Friend list panel

Include a left column with:

- Tabs: `Friends` and `Recent`.
- Scrollable friend rows.
- Each row contains portrait, name, relationship hearts, small mood/gift/status icon.
- Selected friend highlighted with a green border or warm gold glow.
- Optional button: `Give Gift`.

Default friend sample set:

```text
Lina   - warm herbalist / forager friend
Marek  - cheerful miner or farmhand
Ellie  - artist or flower-loving villager
Bram   - older carpenter or baker
Nora   - red-haired shop assistant or fisher
Toby   - young ranch helper
```

### Active conversation panel

Include:

- Large npc portrait.
- Npc name.
- Birthday or favorite gift note.
- Relationship heart meter.
- Relationship label, for example `Good Friends`.
- Gift icon button.
- Scrollable message history.
- Alternating message bubbles:
  - npc bubbles: parchment/tan, aligned left
  - player bubbles: pale green, aligned right
- Small timestamps.
- Bottom input bar with placeholder text and send button.

Default conversation sample:

```text
Lina: Hey there! How's day going?                         8:42 AM
Player: Pretty good! Just finished watering the crops.          8:43 AM
Lina: I'm gathering herbs by the river. The spring blooms...    8:45 AM
Player: Nice! I could use some for a recipe.                    8:46 AM
Lina: Will do! I might swing by the general store after.         8:47 AM
```

## Skills UI specification

A complete skills screen should usually include:

### Header

- Center title: `Skills`.
- Upper-left player portrait and name.
- Optional role label such as `Farmhand`.
- Right status block reusing the chat UI: season, day, weekday, weather, close button.

### Skill list

Use six default skills unless the user gives a different set:

| id | label | icon idea | default level |
| --- | --- | --- | --- |
| farming | Farming | sprout / crop | 7 |
| mining | Mining | pickaxe | 6 |
| foraging | Foraging | berries / leaves | 8 |
| fishing | Fishing | blue fish | 4 |
| combat | Combat | sword | 5 |
| crafting | Crafting | hammer / anvil / toolkit | 3 |

Each row should contain:

```text
[icon] [skill name] [10-slot level bar] [numeric level]
```

Example text representation:

```text
Farming    filled filled filled filled filled filled filled empty empty empty    7
Mining     filled filled filled filled filled filled empty empty empty empty    6
Foraging   filled filled filled filled filled filled filled filled empty empty    8
Fishing    filled filled filled filled empty empty empty empty empty empty    4
Combat     filled filled filled filled filled empty empty empty empty empty    5
Crafting   filled filled filled empty empty empty empty empty empty empty    3
```

### Talents section

Use three default talent cards unless the user requests custom talents:

1. **Green Thumb**
   - Skill: Farming
   - Icon: sprout with sparkles
   - Description: `Crops have a chance to yield extra produce.`

2. **Deep Pockets**
   - Skill: Mining
   - Icon: ore bag with stones/gold
   - Description: `Chance to find extra ores and minerals.`

3. **Angler's Luck**
   - Skill: Fishing
   - Icon: fish with bubbles/sparkles
   - Description: `Fish bite a little faster.`

### Footer

Include:

```text
Total Skill Level: 33
```

Navigation icons may include:

- book: journal / codex
- heart: social / relationships
- trophy: achievements
- gear: settings

## Chat and Skills linkage

When designing both systems together, connect them mechanically:

- Farming level unlocks crop-related npc dialogue.
- Fishing level unlocks fish tips, fishing gossip, and fisher npc quests.
- Mining level unlocks cave rumors, ore requests, and blacksmith dialogue.
- Foraging level unlocks herb, mushroom, and forest event conversations.
- Combat level unlocks adventurer dialogue and dangerous-area quests.
- Crafting level unlocks handmade gift reactions and recipe conversations.

Example links:

```text
if Foraging >= 8:
  Lina can ask the player to help gather river herbs.

if Mining >= 6:
  Bram can request iron ore or mention deeper cave floors.

if Fishing >= 4:
  Nora can discuss a spring river fish appearing after rain.
```

## Default Markdown output template

When the user asks for a UI document, use this structure unless another format is requested:

```markdown
# [Feature Name] UI Design Spec

## 1. Overview
[Purpose, target player experience, and system role]

## 2. Visual Direction
[Pixel-art, parchment, wood frame, palette, typography, icon style]

## 3. Screen Structure
[Header, side panel, main panel, footer]

## 4. Core Components
[Component-by-component description]

## 5. Interaction States
[Hover, selected, unread, disabled, relationship changes, level-up]

## 6. Sample Content
[Characters, messages, skills, talents]

## 7. Data Model
[JSON-like schema or field list]

## 8. Image Generation Prompt
[One polished prompt if the user needs visual generation]

## 9. Quality Checklist
[Readability, consistency, originality, completeness]
```

## Default image generation prompt pattern

When the user asks for an image prompt, adapt this pattern:

```text
create an original pixel-art game ui screen for a cozy farming-life rpg. use a warm parchment panel, chunky wooden border, soft amber shadows, green leaf accents, readable pixel text, and charming tiny item icons. do not copy official stardew valley assets, logos, characters, or exact layouts. design a [chat / skills / combined social] screen with [specific components]. make it look like a polished in-game menu screenshot, highly readable, warm, rustic, and cohesive.
```

For a chat UI, add:

```text
include a left friend list with portraits, hearts, tabs for friends and recent, a selected npc named lina, a main conversation panel with alternating tan and pale-green message bubbles, timestamps, relationship hearts, a gift icon, season/day widget, bottom input bar, and send button.
```

For a skills UI, add:

```text
include six skill rows for farming, mining, foraging, fishing, combat, and crafting; each row has a pixel icon, name, ten level pips, and a numeric level. add a talents section with green thumb, deep pockets, and angler's luck, plus a footer showing total skill level 33.
```

## Data model pattern

Use this structure when the user asks for frontend-ready data:

```json
{
  "player": {
    "id": "player_001",
    "name": "Lina",
    "role": "Farmhand",
    "season": "Spring",
    "day": 18,
    "weekday": "Tue",
    "weather": "sunny"
  },
  "chat": {
    "selectedNpcId": "npc_lina",
    "friends": [],
    "messages": []
  },
  "skills": [],
  "talents": [],
  "summary": {
    "totalSkillLevel": 33
  }
}
```

## Quality checklist

Before finalizing any output, verify:

- The UI feels like an original cozy pixel-farm RPG, not an exact clone.
- Chat and Skills screens share the same visual language.
- All visible text is readable and not overloaded.
- Relationship hearts, skill pips, and levels are internally consistent.
- Any generated sample data matches the displayed totals.
- The output includes practical implementation details when the user requested a document.
- Chinese output is used when the user writes in Chinese, unless they request English UI copy.
- If the user asks for markdown, provide a clean `.md`-ready document with headings and code blocks.
