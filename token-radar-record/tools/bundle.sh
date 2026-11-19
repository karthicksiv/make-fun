#!/bin/sh
# Rebuild the transcript bundle from this session's log with the full run record.
S=${S:?set S to the scratch directory that holds fetched/ and stage/}
cd "$S/fetched/kit/token-radar-01-kit" && node cli/makefun.mjs bundle --repo /home/user/make-fun --out /home/user/make-fun/token-radar-01/transcripts.tar.gz \
  --system-prompt "$S/system-prompt.txt" --no-context \
  --instructions "withheld:the session's skills and plugin files belong to the account owner and were not written for this build" \
  --mcp "Firecrawl=https://mcp.firecrawl.dev/v2/mcp" --mcp "github=https://api.githubcopilot.com/mcp/" --mcp "claude-code-remote=https://claude.ai/code" \
  --notes "Built in Claude Code on the web, a cloud session. That harness mounts its MCP servers itself and gives the session no MCP config to read, so the endpoints here are the nearest public ones: Firecrawl is the account's claude.ai connector, recorded at Firecrawl's documented hosted endpoint; github is the GitHub MCP server the harness provides through its own repository-scoped proxy, recorded at the upstream server's public endpoint; claude-code-remote is the harness's own session and environment server, which has no public endpoint, recorded at the product that mounts it. Firecrawl fetched make.fun's skill.md and the bounty page and looked up its own endpoint; github listed branches; claude-code-remote read environment docs. None wrote any of the entry's code." 2>&1 | grep -v "searching \|scanning " | tail -4 | cut -c1-300
