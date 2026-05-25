# Hermes Agent Gateway — SmartNPC remote deployment
#
# Each container runs ONE NPC profile's Hermes gateway.
# Build once, run 6 times with different NPC_PROFILE / NPC_PORT env vars.

FROM python:3.11-slim AS base

# System deps for hermes-agent (includes git for pip VCS installs, curl for healthcheck)
RUN apt-get update && \
    apt-get install -y --no-install-recommends curl git && \
    rm -rf /var/lib/apt/lists/*

# Install hermes-agent + MCP client dependency
RUN pip install --no-cache-dir hermes-agent mcp

WORKDIR /hermes

# Copy all profile data (mounted or built-in)
COPY profiles/ /root/.hermes/profiles/

# Entrypoint script handles env var expansion
COPY scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE ${NPC_PORT:-8642}

ENTRYPOINT ["/entrypoint.sh"]
