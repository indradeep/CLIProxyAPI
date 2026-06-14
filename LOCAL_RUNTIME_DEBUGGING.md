# CLIProxyAPI Local Runtime Debugging

Last verified: 2026-06-14.

This runbook documents the local ai-infra macOS deployment for this checkout. It is intentionally host-specific. Do not paste API keys, OAuth tokens, auth JSON contents, or bearer headers into issues or docs.

## Runtime Layout

| Item | Path or value |
| --- | --- |
| Live checkout | `/Users/indradeep/workspace/ai-infra/src/CLIProxyAPI` |
| Binary output | `/Users/indradeep/workspace/ai-infra/src/CLIProxyAPI/bin/cliproxyapi` |
| Build command | `go build -o bin/cliproxyapi ./cmd/server` |
| Working directory | `/Users/indradeep/workspace/ai-infra/src/CLIProxyAPI` |
| Config file | `/Users/indradeep/workspace/ai-infra/config/cliproxyapi/config.yaml` |
| Config and auth directory | `/Users/indradeep/workspace/ai-infra/config/cliproxyapi` |
| Main app log | `/Users/indradeep/workspace/ai-infra/config/cliproxyapi/logs/main.log` |
| Per-request logs | `/Users/indradeep/workspace/ai-infra/config/cliproxyapi/logs/v1-chat-completions-*.log` |
| launchd stdout | `/Users/indradeep/workspace/ai-infra/config/cliproxyapi/dev-server.stdout.log` |
| launchd stderr | `/Users/indradeep/workspace/ai-infra/config/cliproxyapi/dev-server.stderr.log` |
| PID file | `/Users/indradeep/workspace/ai-infra/config/cliproxyapi/dev-server.pid` |
| launchd source plist | `/Users/indradeep/workspace/ai-infra/src/launchd/com.ai-infra.cliproxyapi-8318.plist` |
| launchd label | `com.ai-infra.cliproxyapi-8318` |
| Local endpoint | `http://127.0.0.1:8318` |
| Public endpoint | `https://clip2.indradeep.com` |

## Launch Flow

The local service is launched by the user launchd domain with this command shape:

```sh
/Users/indradeep/workspace/ai-infra/src/CLIProxyAPI/bin/cliproxyapi \
  -config /Users/indradeep/workspace/ai-infra/config/cliproxyapi/config.yaml
```

The plist sets:

- `WorkingDirectory` to `/Users/indradeep/workspace/ai-infra/src/CLIProxyAPI`
- `RunAtLoad` to `true`
- `KeepAlive` to `true`
- stdout to `dev-server.stdout.log`
- stderr to `dev-server.stderr.log`

Check the loaded launchd state:

```sh
launchctl print "gui/$(id -u)/com.ai-infra.cliproxyapi-8318"
```

Restart the loaded service after rebuilding the binary:

```sh
launchctl kickstart -k "gui/$(id -u)/com.ai-infra.cliproxyapi-8318"
```

If the agent is not loaded, bootstrap it from the source plist:

```sh
launchctl bootstrap "gui/$(id -u)" /Users/indradeep/workspace/ai-infra/src/launchd/com.ai-infra.cliproxyapi-8318.plist
```

## Config Notes

The active config is outside the repository:

```text
/Users/indradeep/workspace/ai-infra/config/cliproxyapi/config.yaml
```

Current local config characteristics:

- Host: `127.0.0.1`
- Port: `8318`
- Auth directory: `/Users/indradeep/workspace/ai-infra/config/cliproxyapi`
- File logging: enabled
- Request logging: enabled
- WebSocket auth: enabled
- Remote management: local-only, control panel disabled

Sensitive files in the config directory include:

- `local-api-key`
- `management-key`
- OAuth/auth JSON files for Claude, Codex, Antigravity, Kiro, and other providers

Do not move config or auth material back into the source tree. The source tree should only contain code, docs, the built binary, and launchd source files.

## Logs

Use `main.log` for routing, account selection, session-affinity, upstream status, and high-level request results:

```sh
tail -f /Users/indradeep/workspace/ai-infra/config/cliproxyapi/logs/main.log
```

Use the per-request logs for payload-level debugging:

```sh
ls -lt /Users/indradeep/workspace/ai-infra/config/cliproxyapi/logs/v1-chat-completions-*.log | head
```

```sh
latest="$(ls -t /Users/indradeep/workspace/ai-infra/config/cliproxyapi/logs/v1-chat-completions-*.log | head -1)"
sed -n '1,220p' "$latest"
```

The request logs are the right place to inspect:

- incoming Cursor payloads
- translated upstream request bodies
- upstream response status and body
- streaming chunks and final responses
- tool call and tool result shape
- Claude-specific headers and body fields
- prompt-cache/session metadata behavior

The launchd stdout and stderr files are useful for startup and crash-loop debugging:

```sh
tail -f /Users/indradeep/workspace/ai-infra/config/cliproxyapi/dev-server.stdout.log
tail -f /Users/indradeep/workspace/ai-infra/config/cliproxyapi/dev-server.stderr.log
```

## Process Checks

Confirm the listener:

```sh
lsof -nP -iTCP:8318 -sTCP:LISTEN
```

Confirm launchd state:

```sh
launchctl print "gui/$(id -u)/com.ai-infra.cliproxyapi-8318"
```

Confirm startup from stdout:

```sh
tail -20 /Users/indradeep/workspace/ai-infra/config/cliproxyapi/dev-server.stdout.log
```

Expected startup lines include the server binding to `127.0.0.1:8318` and a client count after config/auth load.

## Endpoint Probes

Read the local API key into an environment variable. Do not print it:

```sh
CLIPROXY_KEY="$(cat /Users/indradeep/workspace/ai-infra/config/cliproxyapi/local-api-key)"
```

List models:

```sh
curl -sS http://127.0.0.1:8318/v1/models \
  -H "Authorization: Bearer ${CLIPROXY_KEY}" \
  | jq -r '.data[]?.id' \
  | sort
```

Minimal non-streaming chat probe:

```sh
curl -sS http://127.0.0.1:8318/v1/chat/completions \
  -H "Authorization: Bearer ${CLIPROXY_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.5-extra","stream":false,"messages":[{"role":"user","content":"Reply with ok."}]}'
```

Public endpoint probe, assuming the Cloudflare tunnel is active:

```sh
curl -sS https://clip2.indradeep.com/v1/models \
  -H "Authorization: Bearer ${CLIPROXY_KEY}" \
  | jq -r '.data[]?.id' \
  | sort
```

## Cursor Request Triage

Cursor request IDs are not the same as CLIProxyAPI's short request IDs. Correlate by timestamp, model name, and the nearest `POST "/v1/chat/completions"` line in `main.log`.

For Claude-family models through Cursor's OpenAI-compatible endpoint, inspect both the incoming request and the translated Anthropic request. Cursor may send Anthropic-shaped content blocks through the OpenAI endpoint, including:

- assistant `content` blocks with `type: "tool_use"`
- user `content` blocks with `type: "tool_result"`
- Claude-specific metadata and session/cache hints
- model-specific thinking and output configuration fields

When debugging Claude regressions, check that:

- user messages never translate to empty Claude `content` arrays
- `tool_use.id` survives into matching `tool_result.tool_use_id`
- `tools` remain Anthropic tools upstream, not prose instructions
- Claude-specific headers from Cursor metadata are forwarded when safe
- config-level thinking/output overrides still win where intentionally configured
- streaming responses contain incremental chunks rather than one buffered text body

For GPT-family models, the request shape is usually standard OpenAI chat or responses-style payload. Use GPT request logs as the control group before changing Claude-only translators.

### Claude Context and Long-Request Findings

Latest verified findings from 2026-06-14 Cursor Claude tracing:

- Cursor may keep sending `max_tokens: 4096` even for `clip-claude-opus-4-8-xhigh`.
- The local payload overrides can and should win upstream. The active config currently sets Claude caps by alias class:
  - standard/base: `max_tokens: 8192`
  - `high`: `max_tokens: 16384`
  - `xhigh`: `max_tokens: 32768`
  - `max`: `max_tokens: 65536`
- Verify both downstream and upstream values. A healthy xhigh translation can show downstream `4096` and upstream `32768` in the same request log.
- Prompt caching is working when `cache_read_input_tokens` is high. This reduces input processing/cost, but it does not prevent long hidden-thinking phases.
- A held Cursor request is not automatically a proxy deadlock. In-flight Claude streams may sit at:
  - HTTP `200`
  - `event: message_start`
  - `content_block_start` with `type: "thinking"`
  - repeated `event: ping`
  before later emitting `content_block_delta` text and finishing.

Concrete example: request `2dd2c1af` in `v1-chat-completions-2026-06-14T134554-2dd2c1af.log` started at `13:41:41` and completed at `13:45:54`:

- `main.log`: `200 | 4m13s`
- downstream model: `clip-claude-opus-4-8-xhigh`
- downstream `max_tokens`: `4096`
- upstream model: `claude-opus-4-8`
- upstream `max_tokens`: `32768`
- upstream `thinking`: `{"type":"adaptive"}`
- upstream `output_config.effort`: `xhigh`
- finish reason: `stop`
- prompt tokens: `115706`
- cached prompt tokens: `113210`
- completion/output tokens: `18717`
- thinking tokens: `15484`
- visible/non-thinking output tokens: `3233`

Interpretation: the request was slow because Opus xhigh spent most output tokens in hidden thinking, not because the proxy dropped the stream or prompt caching failed.

When a Claude request is currently held, inspect the staged request-log parts before a final `v1-chat-completions-*.log` exists:

```sh
ls -lt /Users/indradeep/workspace/ai-infra/config/cliproxyapi/logs | head
```

```sh
tail -n 80 /Users/indradeep/workspace/ai-infra/config/cliproxyapi/logs/request-log-parts-api-response-*/part-*.tmp
```

Useful scalar checks once a full request log exists:

```sh
grep -o '"max_tokens":[0-9]*\|"cache_read_input_tokens":[0-9]*\|"output_tokens":[0-9]*\|"thinking_tokens":[0-9]*\|"finish_reason":"[^"]*"' \
  /Users/indradeep/workspace/ai-infra/config/cliproxyapi/logs/v1-chat-completions-*.log
```

If a Cursor-provided request UUID does not appear in the logs, correlate by timestamp, model, client IP, upstream `X-Client-Request-Id`, and the CLIProxyAPI short request ID from `main.log`.

## Cloudflare Tunnel

The public hostname `clip2.indradeep.com` is expected to reach the local service through Cloudflare tunnel configuration in:

```text
/Users/indradeep/workspace/ai-infra/config/cliproxyapi/cloudflared-config.yml
```

The Cloudflare launch agent is separate from the CLIProxyAPI launch agent. If `127.0.0.1:8318` works but `https://clip2.indradeep.com` fails, debug the tunnel separately before changing CLIProxyAPI.

## Common Failure Modes

- `launchctl print` says the agent is missing: the plist is not loaded in the user launchd domain.
- `lsof` shows nothing on `127.0.0.1:8318`: the service is not running or failed before binding.
- stdout shows startup but no request logs appear: the client may be hitting a different port, hostname, or proxy.
- `main.log` shows request completion but Cursor stalls: inspect streaming chunks in the per-request log and verify the final stream terminator.
- Claude returns `messages.N: user messages must have non-empty content`: inspect translator handling for Cursor-sent `tool_result` blocks.
- Claude tool calls come back as text: inspect outbound `tools`, `tool_choice`, and content block translation before blaming Cursor.
- Public endpoint fails while local endpoint works: inspect Cloudflare tunnel config and tunnel process logs.
