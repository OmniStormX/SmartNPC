# Hermes Agent Gateway — SmartNPC remote deployment
#
# All profile data (SOUL.md, skills, cron) is baked into the image at build time.
# At runtime, only a .env file is needed — entrypoint.sh generates the full
# Hermes config.yaml from environment variables automatically.
#
# Build: docker compose build  (from deploy/hermes/)
# Run:   docker compose up -d  (reads .env for all config)

FROM python:3.11-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl git && \
    rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir hermes-agent mcp aiohttp langfuse && \
    python -c "import aiohttp, aiohttp.web"

WORKDIR /hermes

# Bake all profile data into image (SOUL.md, skills, cron-recipes)
COPY profiles/ /hermes/profiles/

# Entrypoint generates config.yaml from env vars and starts gateway
COPY scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
