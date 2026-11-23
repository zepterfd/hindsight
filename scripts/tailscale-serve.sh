#!/usr/bin/env bash
# Put this Pi's Hindsight on the tailnet at a stable HTTPS address.
#
#   scripts/tailscale-serve.sh           # serve PORT (default 5000) on 443
#   scripts/tailscale-serve.sh --off     # stop serving it
#   scripts/tailscale-serve.sh --dry-run # print what it would run
#
# It runs `tailscale serve`, which makes tailscaled proxy
# https://<this node's MagicDNS name>/ to Hindsight on this machine, with a
# certificate Tailscale issues. Only devices on your tailnet can reach it (this
# is `serve`, not `funnel`: nothing is opened to the internet). It changes
# nothing else, and it does not log in: run `tailscale up` first.
#
# Needs: Tailscale installed and logged in; MagicDNS and HTTPS certificates
# enabled for the tailnet (admin console, DNS). docs/plan/tailscale-routing.md.
set -euo pipefail

port="${PORT:-5000}"
mode=on
dry=false
for a in "$@"; do
  case "$a" in
    --off) mode=off ;;
    --dry-run) dry=true ;;
    -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
    *) echo "unknown argument: $a" >&2; exit 2 ;;
  esac
done
case "$port" in ''|*[!0-9]*) echo "PORT must be a number, not '$port'" >&2; exit 2 ;; esac

if [ "$mode" = on ]; then
  cmd=(tailscale serve --bg --https=443 "http://127.0.0.1:${port}")
else
  cmd=(tailscale serve --https=443 off)
fi

if $dry; then printf '%q ' "${cmd[@]}"; echo; exit 0; fi
command -v tailscale >/dev/null || { echo "tailscale is not installed" >&2; exit 1; }
tailscale status >/dev/null 2>&1 || { echo "tailscale is not running or not logged in: run 'tailscale up'" >&2; exit 1; }
"${cmd[@]}"
[ "$mode" = on ] && tailscale serve status
