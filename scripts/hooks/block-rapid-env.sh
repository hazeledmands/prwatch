#!/usr/bin/env bash
# PreToolUse hook: block `PRWATCH_RAPID_CHECKS=... go test ...` invocations
# and redirect to ./scripts/rapid, which doesn't trip a permission prompt.
#
# Wired up from .claude/settings.json hooks.PreToolUse[matcher=Bash].
# Reads the tool-call JSON on stdin; if both substrings are present in the
# command, emits a JSON deny decision. Otherwise stays silent (allow).
set -euo pipefail

cmd=$(jq -r '.tool_input.command // ""')
if printf '%s' "$cmd" | grep -q 'PRWATCH_RAPID_CHECKS=' \
  && printf '%s' "$cmd" | grep -q 'go test'; then
  jq -n --arg msg 'Use ./scripts/rapid instead of `PRWATCH_RAPID_CHECKS=N go test ...`.

Usage: ./scripts/rapid [checks] [extra go-test args...]
Examples:
  ./scripts/rapid 50 -run TestProperty_Foo -v
  ./scripts/rapid 1000 -run TestRenderTitleRow

First positional arg sets PRWATCH_RAPID_CHECKS; remaining args are forwarded to go test.' \
    '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:$msg}}'
fi
