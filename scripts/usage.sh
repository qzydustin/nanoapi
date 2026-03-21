#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${1:?Usage: $0 <base_url> <config.yaml> [5h|24h|7d|30d]}"
CONFIG="${2:?Usage: $0 <base_url> <config.yaml> [5h|24h|7d|30d]}"
RANGE="${3:-}"

from_param=""
range_label="ALL TIME"
if [ -n "$RANGE" ]; then
  case "$RANGE" in
    5h)  secs=18000;    range_label="LAST 5 HOURS" ;;
    24h) secs=86400;    range_label="LAST 24 HOURS" ;;
    7d)  secs=604800;   range_label="LAST 7 DAYS" ;;
    30d) secs=2592000;  range_label="LAST 30 DAYS" ;;
    *)   echo "Invalid range: $RANGE (use 5h|24h|7d|30d)"; exit 1 ;;
  esac
  if date -v-1d >/dev/null 2>&1; then
    from=$(date -u -v-"${secs}S" +"%Y-%m-%dT%H:%M:%SZ")
  else
    from=$(date -u -d "-${secs} seconds" +"%Y-%m-%dT%H:%M:%SZ")
  fi
  from_param="?from=$from"
fi

fmt_num() {
  awk "BEGIN { n=$1; if(n>=1000000) printf \"%.1fM\",n/1000000; else if(n>=1000) printf \"%.1fK\",n/1000; else printf \"%d\",n }"
}

tokens=$(awk '/- id:/{id=$3} /^[[:space:]]*key:/{print id":"$2}' "$CONFIG")

echo "=== $range_label ==="
echo
printf "%-14s %8s %8s %8s %8s %8s %8s  %s\n" \
  "TOKEN" "REQS" "INPUT" "OUTPUT" "CACHE_R" "CACHE_W" "REASON" "LAST_USED"
printf "%-14s %8s %8s %8s %8s %8s %8s  %s\n" \
  "-----" "----" "-----" "------" "-------" "-------" "------" "---------"

t_reqs=0 t_input=0 t_output=0 t_cache_r=0 t_cache_w=0 t_reasoning=0

while IFS=: read -r id key; do
  summary=$(curl -sf -H "Authorization: Bearer $key" "$BASE_URL/api/usage${from_param}" 2>/dev/null) || {
    printf "%-14s  (error fetching usage)\n" "$id"
    continue
  }

  read -r reqs input output cache_r cache_w reasoning <<< \
    "$(echo "$summary" | jq -r '[.summary.request_count, .summary.input_tokens, .summary.output_tokens, .summary.cache_read_input_tokens, .summary.cache_creation_input_tokens, .summary.reasoning_tokens] | @tsv')"

  t_reqs=$((t_reqs + reqs))
  t_input=$((t_input + input))
  t_output=$((t_output + output))
  t_cache_r=$((t_cache_r + cache_r))
  t_cache_w=$((t_cache_w + cache_w))
  t_reasoning=$((t_reasoning + reasoning))

  last_used="-"
  if [ "$reqs" != "0" ]; then
    ts=$(curl -sf -H "Authorization: Bearer $key" "$BASE_URL/api/logs${from_param}" 2>/dev/null \
      | jq -r '.records[0].timestamp // empty' 2>/dev/null) || true
    if [ -n "$ts" ]; then
      last_used=$(perl -MTime::Piece -e '
        my $t = Time::Piece->strptime($ARGV[0] =~ s/\.\d+Z?$//r, "%Y-%m-%dT%H:%M:%S");
        print localtime($t->epoch)->strftime("%Y-%m-%d %H:%M")
      ' "$ts" 2>/dev/null || echo "${ts:0:16}" | tr 'T' ' ')
    fi
  fi

  printf "%-14s %8s %8s %8s %8s %8s %8s  %s\n" \
    "$id" "$(fmt_num "$reqs")" "$(fmt_num "$input")" "$(fmt_num "$output")" \
    "$(fmt_num "$cache_r")" "$(fmt_num "$cache_w")" "$(fmt_num "$reasoning")" "$last_used"
done <<< "$tokens"

printf "%-14s %8s %8s %8s %8s %8s %8s\n" \
  "-----" "----" "-----" "------" "-------" "-------" "------"
printf "%-14s %8s %8s %8s %8s %8s %8s\n" \
  "TOTAL" "$(fmt_num $t_reqs)" "$(fmt_num $t_input)" "$(fmt_num $t_output)" \
  "$(fmt_num $t_cache_r)" "$(fmt_num $t_cache_w)" "$(fmt_num $t_reasoning)"
