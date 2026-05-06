cd /d d:\SmartNPC\smartnpc-agent
bin\smartnpc-agent.exe -mcp-bin ..\smartnpc-mcp\bin\smartnpc-mcp.exe -mcp-args="--ws-url=ws://127.0.0.1:18745/ws" -log-level debug run -llm-url http://localhost:8643/v1 -api-key xiami-npc-key -model xiami -speaker XiaMi -persona ..\smartnpc-agent\personas\xiami.json
