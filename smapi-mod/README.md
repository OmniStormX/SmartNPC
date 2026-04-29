# StardewMCPBridge — M2 minimal experiment

A tiny SMAPI mod that listens on `http://127.0.0.1:18745/mail_send` and
displays HUD messages in the running game. Used to validate the end-to-end
chain `MCP tool → Go → HTTP → Mod → in-game UI` before introducing the real
WebSocket bridge in M3.

**This mod is intentionally throwaway.** Do not extend it.

## Prerequisites

- Stardew Valley 1.6+
- SMAPI 4.0+
- .NET 6 SDK: `winget install Microsoft.DotNet.SDK.6`

## Build & Deploy

The csproj uses `Pathoschild.Stardew.ModBuildConfig`, which:
- Auto-locates your SDV install (looks under common Steam paths and registry keys)
- References SMAPI / SDV / XNA assemblies automatically
- Auto-deploys the built mod to `%APPDATA%\StardewValley\Mods\StardewMCPBridge\`

```cmd
d: && cd d:\SmartNPC\smapi-mod
dotnet build -c Debug
```

If the build fails with "could not find Stardew Valley", edit
`StardewMCPBridge.csproj` and uncomment `<GamePath>` with your actual install path.

## Smoke test

1. Launch SMAPI, load any save.
2. Watch the SMAPI console for: `HTTP listener started on http://127.0.0.1:18745/`
3. From another terminal, send a request:

   ```cmd
   curl -X POST http://127.0.0.1:18745/mail_send -H "Content-Type: application/json" -d "{\"text\":\"Hi from SmartNPC!\"}"
   ```

   Expect: `{"ok":true,"message":"displayed"}`
4. In the game, a HUD bubble appears in the bottom-left corner with the text.

## Via MCP

Once you have `smartnpc-mcp.exe` running (or wired into Claude Desktop),
calling the `mail_send` tool will route through the same endpoint.
