#!/bin/bash
# Sync SmartNPC persona SOUL.md files to Hermes profiles.
# Run from WSL: bash /mnt/d/SmartNPC/scripts/sync_hermes_profiles.sh

set -e

HERMES_HOME="/home/synchen/.hermes"
PERSONAS_DIR="/mnt/d/SmartNPC/smartnpc-agent/personas"
TEMPLATE_PROFILE="$HERMES_HOME/profiles/xiami"

declare -A NPC_PORTS=(
    [xiami]=8643
    [abigail]=8644
    [sebastian]=8645
    [haley]=8646
    [harvey]=8647
    [penny]=8648
)

declare -A NPC_KEYS=(
    [xiami]="xiami-npc-key"
    [abigail]="abigail-npc-key"
    [sebastian]="sebastian-npc-key"
    [haley]="haley-npc-key"
    [harvey]="harvey-npc-key"
    [penny]="penny-npc-key"
)

for npc in "${!NPC_PORTS[@]}"; do
    profile_name="npc_${npc}"
    profile_dir="$HERMES_HOME/profiles/$profile_name"
    port="${NPC_PORTS[$npc]}"
    key="${NPC_KEYS[$npc]}"
    soul_src="$PERSONAS_DIR/$npc/SOUL.md"

    echo "[$npc] Syncing to profile: $profile_name (port $port)"

    # Create profile dir if needed
    mkdir -p "$profile_dir"

    # Copy SOUL.md
    if [ -f "$soul_src" ]; then
        cp "$soul_src" "$profile_dir/SOUL.md"
        echo "  ✓ SOUL.md synced"
    else
        echo "  ⚠ SOUL.md not found at $soul_src"
    fi

    # Create config.yaml from template if not exists
    if [ ! -f "$profile_dir/config.yaml" ]; then
        cp "$TEMPLATE_PROFILE/config.yaml" "$profile_dir/config.yaml"
        # Update port in config
        sed -i "s/API_SERVER_PORT: 8643/API_SERVER_PORT: $port/" "$profile_dir/config.yaml"
        sed -i "s/API_SERVER_KEY: xiami-npc-key/API_SERVER_KEY: $key/" "$profile_dir/config.yaml"
        echo "  ✓ config.yaml created (port $port)"
    fi

    # Create .env from template if not exists
    if [ ! -f "$profile_dir/.env" ]; then
        cp "$TEMPLATE_PROFILE/.env" "$profile_dir/.env"
        sed -i "s/API_SERVER_PORT=8643/API_SERVER_PORT=$port/" "$profile_dir/.env"
        sed -i "s/API_SERVER_KEY=xiami-npc-key/API_SERVER_KEY=$key/" "$profile_dir/.env"
        echo "  ✓ .env created (key $key)"
    else
        # Always update SOUL.md even if .env exists
        echo "  ✓ .env already exists, skipping"
    fi

    # Special: also sync to the existing "xiami" profile (backward compat)
    if [ "$npc" = "xiami" ]; then
        cp "$soul_src" "$HERMES_HOME/profiles/xiami/SOUL.md"
        echo "  ✓ Also synced to legacy 'xiami' profile"
    fi
done

echo ""
echo "Done! All profiles synced."
echo "Start a gateway: hermes -p npc_xiami gateway run --accept-hooks"
