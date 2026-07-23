# Payana fork of whatsapp-mcp

This is a fork of [`lharries/whatsapp-mcp`](https://github.com/lharries/whatsapp-mcp)
(MIT, © Luke Harries — see `LICENSE`). It is consumed by the `payana-sales`
plugin in [`payana-apps/claude-commons`](https://github.com/payana-apps/claude-commons),
which clones this repo at a **pinned commit** into `~/.cache/payana/whatsapp-mcp`.

We forked upstream directly (not the 2★, unlicensed `Charlesagui/mcp-whats-app`
intermediate the first attempt vendored) so the supply chain stays MIT-clean and
under a Payana account.

## Divergence from upstream

Each item below is a single reviewable commit on top of the forked base.

**Go bridge (`whatsapp-bridge/`)**
- `handleMessage` dedup guard — preserves history-sync timestamps when the
  bridge reconnects and WhatsApp re-delivers old messages as fresh events.
- `WA_QR_RAW:<code>` line at pairing — lets the `whatsapp-mcp` skill render the
  QR as a Claude Artifact instead of unreadable terminal ASCII.
- Security hardening — loopback bind (`WHATSAPP_BRIDGE_HOST`, default
  `127.0.0.1`), per-run token on `/api/send` + `/api/download` (`X-Bridge-Token`),
  same-origin + `application/json` enforcement, `media_path` allowlist
  (`WHATSAPP_MEDIA_ROOT`, default `$HOME`, sensitive dirs denied), fatal bind.
  Covered by `whatsapp-bridge/security_test.go`.

**Python MCP server (`whatsapp-mcp-server/`)**
- Env-driven store/bridge config (`WHATSAPP_STORE_DIR`, host/port, token) so the
  launcher can keep the session under the per-user cache dir.
- `BridgeNotLinkedError` — a never-paired bridge is reported as an error, not an
  empty result the model would read as "no messages/contacts".
- Accent/typo-tolerant `search_contacts` + `smart_search_contacts`, reimplemented
  from scratch as Payana code using only the stdlib (`unicodedata` + `difflib`) —
  no third-party fuzzy deps, none of the intermediate's code. Covered by
  `whatsapp-mcp-server/test_search.py`.
- `requires-python` capped `<3.14` (pydantic-core has no cp314 wheel).

## We do NOT change

The upstream chat-navigation tools (`list_chats`, `get_chat`,
`get_direct_chat_by_contact`, `get_contact_chats`, `get_last_interaction`) are
kept as-is.

## Re-syncing with upstream

1. `git remote add upstream https://github.com/lharries/whatsapp-mcp` (once).
2. `git fetch upstream && git rebase upstream/main` on the Payana branch.
3. Re-run `go test ./...` (bridge) and `python -m pytest` (server).
4. Bump the pinned commit in `payana-apps/claude-commons`
   (`plugins/payana-sales/.mcp.json` + launcher) to the new SHA.
