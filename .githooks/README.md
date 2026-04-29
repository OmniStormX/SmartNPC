# Git Hooks (Opt-in)

Repo-local git hooks for SmartNPC. **Opt-in** — `git clone` does NOT enable
them automatically. You must run `task hooks:enable` once on your machine.

## Available hooks

| Hook | Trigger | Action |
|------|---------|--------|
| `post-commit` | Right after `git commit` succeeds | If the commit touched `smapi-mod/`, runs `task mod:install` to rebuild and copy the mod into your local `D:\Stardew Valley\Mods\StardewMCPBridge\`. Non-blocking — failures only print a warning. |

## Enable / disable

```cmd
task hooks:enable      :: turn on
task hooks:status      :: check current state
task hooks:disable     :: turn off
```

`hooks:enable` runs `git config core.hooksPath .githooks`, which only affects
the local clone (never written to remote).

## Skip for a single commit

```bash
SKIP_MOD_INSTALL=1 git commit -m "docs only, no need to rebuild mod"
```

(Powershell: `$env:SKIP_MOD_INSTALL=1; git commit -m "..."` and remember to
unset afterwards.)

## Why not use it?

You don't develop the C# mod, only Go code → no need to enable the hook.
Saves ~15s build time on every commit.
