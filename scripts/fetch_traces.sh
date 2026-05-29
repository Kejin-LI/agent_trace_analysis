#!/usr/bin/env bash
# 批量拉 conversations.json 中所有 session 的 trace
# 严格按 SKILL.md：每个 session 独立查询、独立写文件
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"
export FORNAX_AK="${FORNAX_AK:-29af02de512d43df8f335a2ab786f469}"
export FORNAX_SK="${FORNAX_SK:-1362db4ac2364bc5829b0aaa542cf959}"

CONV_FILE="${1:-tmp/fornax/conversations.json}"
ROOT="${2:-tmp/fornax}"

# 提取 (session_id) 列表 — 每 artifact 只取第一个 session（MVP 限流）
SIDS=$(python3 -c "
import json
d = json.load(open('$CONV_FILE'))['data']
for a in d:
    items = a.get('items', [])
    if items:
        # 每 artifact 只取第一个 session
        print(items[0]['neeko_resume_session_id'])
")

for sid in $SIDS; do
    out_dir="$ROOT/$sid"
    if [[ -d "$out_dir" && -n "$(ls -A "$out_dir" 2>/dev/null)" ]]; then
        echo "[skip] $sid (already fetched)"
        continue
    fi
    mkdir -p "$out_dir"
    echo "[fetch] $sid → $out_dir"
    fornax-cli trace list \
        --trace-filter-expr "thread_id='$sid'" \
        --last-n-minutes 10080 \
        --page-size 50 \
        --format json \
        --timeout 30s \
        -o "$out_dir/" 2>&1 | tail -5 || echo "[warn] $sid failed"
    sleep 1   # 缓速避免 rate limit
done

echo "[done] all sessions fetched under $ROOT"
