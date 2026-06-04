# Render per-NPC Hermes profile files from hermes/profiles/_master/ templates.
# Windows-native equivalent of scripts/render_profiles.sh.
#
# Usage: powershell -NoProfile -File scripts/render_profiles.ps1
#
# Reads hermes/npcs.yaml, copies _master/ to each NPC dir, then replaces
# {{NPC_NAME}}, {{NPC_DIR}}, {{NPC_PORT}}, etc. placeholders.

param(
    [string]$RepoRoot = (Split-Path -Parent (Split-Path -Parent $PSScriptRoot))
)

if (-not $RepoRoot) {
    $RepoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
}

$Master = Join-Path $RepoRoot "hermes\profiles\_master"
$Registry = Join-Path $RepoRoot "hermes\npcs.yaml"
$ProfilesDir = Join-Path $RepoRoot "hermes\profiles"
$RuntimeConfig = Join-Path $RepoRoot "hermes\runtime-config.yaml"

if (-not (Test-Path $Master)) {
    Write-Error "master template directory not found: $Master"
    exit 1
}
if (-not (Test-Path $Registry)) {
    Write-Error "NPC registry not found: $Registry"
    exit 1
}

# Parse npcs.yaml (simple line-by-line parser)
$npcs = @()
$current = $null

foreach ($line in Get-Content $Registry -Encoding UTF8) {
    $line = $line -replace '#.*$', ''
    if ($line -match '^\s*-\s*id:\s*(\S+)') {
        if ($current) { $npcs += $current }
        $current = @{
            id = $Matches[1]
            game_name = ''
            display_name = ''
            gateway_port = ''
            enabled = 'true'
            peer_a_name = ''
            peer_a_display = ''
            peer_b_name = ''
            peer_b_display = ''
        }
    }
    elseif ($current -and $line -match '^\s*(\w+):\s*(.+)$') {
        $key = $Matches[1]
        $val = $Matches[2].Trim().Trim('"').Trim("'")
        if ($current.ContainsKey($key)) {
            $current[$key] = $val
        }
    }
}
if ($current) { $npcs += $current }

# Filter to enabled NPCs
$npcs = $npcs | Where-Object { $_['enabled'] -eq 'true' }

foreach ($npc in $npcs) {
    $dir = $npc['id']
    $target = Join-Path $ProfilesDir $dir

    if (-not (Test-Path $target)) { New-Item -ItemType Directory -Path $target -Force | Out-Null }

    # Copy skills/
    $skillsSrc = Join-Path $Master "skills"
    $skillsDst = Join-Path $target "skills"
    if (Test-Path $skillsDst) { Remove-Item -Recurse -Force $skillsDst }
    if (Test-Path $skillsSrc) { Copy-Item -Recurse -Force $skillsSrc $skillsDst }

    # Copy .env
    $envSrc = Join-Path $Master ".env"
    if (Test-Path $envSrc) { Copy-Item -Force $envSrc (Join-Path $target ".env") }

    # Copy scalar templates. Do NOT touch SOUL.md.
    foreach ($f in @("config-overlay.yaml", "cron-recipes.md", "critical-policy.md")) {
        $src = Join-Path $Master $f
        if (Test-Path $src) { Copy-Item -Force $src (Join-Path $target $f) }
    }

    # Placeholder substitution map
    $replacements = @{
        '{{NPC_NAME}}'       = $npc['game_name']
        '{{NPC_DISPLAY}}'    = $npc['display_name']
        '{{NPC_DIR}}'        = $dir
        '{{NPC_PORT}}'       = $npc['gateway_port']
        '{{PEER_A_NAME}}'    = $npc['peer_a_name']
        '{{PEER_A_DISPLAY}}' = $npc['peer_a_display']
        '{{PEER_B_NAME}}'    = $npc['peer_b_name']
        '{{PEER_B_DISPLAY}}' = $npc['peer_b_display']
    }

    # Find all .yaml and .md files to process
    $files = @()
    $files += Get-ChildItem -Path $target -Filter "*.yaml" -File
    $files += Get-ChildItem -Path $target -Filter "*.md" -File | Where-Object { $_.Name -ne "SOUL.md" }
    if (Test-Path $skillsDst) {
        $files += Get-ChildItem -Path $skillsDst -Recurse -Include "*.md","*.yaml" -File
    }

    foreach ($file in $files) {
        $content = [System.IO.File]::ReadAllText($file.FullName, [System.Text.Encoding]::UTF8)
        foreach ($placeholder in $replacements.Keys) {
            $content = $content.Replace($placeholder, $replacements[$placeholder])
        }
        [System.IO.File]::WriteAllText($file.FullName, $content, (New-Object System.Text.UTF8Encoding $false))
    }

    Write-Host "rendered: $dir ($($npc['game_name']) / $($npc['display_name']), port $($npc['gateway_port']))"
}

# Sync rendered profiles to Hermes runtime directory.
# Hermes on Windows stores profiles under %LOCALAPPDATA%\hermes\profiles\<npc>\
$HermesHome = if ($env:HERMES_HOME) { $env:HERMES_HOME } else { Join-Path $env:LOCALAPPDATA "hermes" }
$HermesProfiles = Join-Path $HermesHome "profiles"

if (Test-Path $HermesHome) {
    Write-Host ""
    Write-Host "syncing to Hermes runtime: $HermesProfiles"

    foreach ($npc in $npcs) {
        $dir = $npc['id']
        $src = Join-Path $ProfilesDir $dir
        $dst = Join-Path $HermesProfiles $dir

        if (-not (Test-Path $dst)) { New-Item -ItemType Directory -Path $dst -Force | Out-Null }

        # Sync .env
        $envFile = Join-Path $src ".env"
        if (Test-Path $envFile) { Copy-Item -Force $envFile (Join-Path $dst ".env") }

        # Sync SOUL.md
        $soulFile = Join-Path $src "SOUL.md"
        if (Test-Path $soulFile) { Copy-Item -Force $soulFile (Join-Path $dst "SOUL.md") }

        # Sync skills/
        $skillsSrc = Join-Path $src "skills"
        if (Test-Path $skillsSrc) {
            $skillsDst = Join-Path $dst "skills"
            if (-not (Test-Path $skillsDst)) { New-Item -ItemType Directory -Path $skillsDst -Force | Out-Null }
            Copy-Item -Recurse -Force "$skillsSrc\*" $skillsDst
        }

        # Merge config-overlay.yaml into config.yaml
        # Simple approach: write overlay as config.yaml (Hermes reads it directly)
        $overlay = Join-Path $src "config-overlay.yaml"
        if (Test-Path $overlay) {
            $cfgDst = Join-Path $dst "config.yaml"
            Copy-Item -Force $overlay $cfgDst
            Write-Host "  [sync] $dir/config.yaml + .env + skills/"
        }
    }
} else {
    Write-Host ""
    Write-Host "[WARN] Hermes home not found at $HermesHome — skip runtime sync."
    Write-Host "       Run 'hermes' once to bootstrap, then re-run this script."
}

# Generate runtime-config.yaml
$rcLines = @(
    "# hermes/runtime-config.yaml"
    "#"
    "# Generated by scripts/render_profiles.ps1 from hermes/npcs.yaml."
    "# Multi-profile fan-out config consumed by smartnpc-mcp --hermes-config."
    "#"
    '# gateway_url uses ${SMARTNPC_HERMES_GATEWAY_HOST}; smartnpc-mcp expands it'
    '# at startup, falling back to $WSL_IP and then 127.0.0.1.'
    ""
    "profiles:"
)

$last = $npcs[-1]
foreach ($npc in $npcs) {
    $rcLines += "  - name: $($npc['id'])"
    $rcLines += "    npc_filter: $($npc['game_name'])"
    $rcLines += '    gateway_url: http://${SMARTNPC_HERMES_GATEWAY_HOST}:' + $npc['gateway_port']
    $rcLines += "    conversation: $($npc['id'])"
    $rcLines += "    model: hermes-agent"
    $rcLines += "    api_key_env: SMARTNPC_HERMES_KEY"
    if ($npc -ne $last) { $rcLines += "" }
}

$rcContent = ($rcLines -join "`n") + "`n"
[System.IO.File]::WriteAllText($RuntimeConfig, $rcContent, (New-Object System.Text.UTF8Encoding $false))
Write-Host "rendered: hermes/runtime-config.yaml from hermes/npcs.yaml"
Write-Host ""
Write-Host "done."
