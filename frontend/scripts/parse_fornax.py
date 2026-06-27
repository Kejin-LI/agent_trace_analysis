#!/usr/bin/env python3
"""
解析 fornax-cli trace list 输出 → 前端可用的 sessions.json

使用：
  python3 scripts/parse_fornax.py \
      --conversations tmp/fornax/conversations.json \
      --trace-root tmp/fornax \
      --user-meta tmp/fornax/user_meta.json \
      --out docs/plans/prototype/data/sessions.json

字段对齐前端原型 SESSIONS 字典（session-detail.html）。
"""
import argparse
import json
import os
import glob
from datetime import datetime, timezone, timedelta


def load_json(p):
    with open(p) as f:
        return json.load(f)


def safe_int(v, default=0):
    try:
        return int(v) if v not in (None, "") else default
    except (TypeError, ValueError):
        return default


def extract_user_prompt(spans):
    """从第一个 LLMCall span 的 input.messages 提取最后一条 user message."""
    for s in spans:
        if s.get("span_type") != "model":
            continue
        try:
            inp = json.loads(s.get("input") or "{}")
        except Exception:
            continue
        msgs = inp.get("messages") if isinstance(inp, dict) else None
        if not isinstance(msgs, list):
            continue
        for m in reversed(msgs):
            if m.get("role") != "user":
                continue
            content = m.get("content")
            if isinstance(content, str):
                return content
            if isinstance(content, list):
                texts = [c.get("text", "") for c in content if isinstance(c, dict)]
                return " ".join(t for t in texts if t)
    # 兜底：根 span input 的 prompt_text 字段
    for s in spans:
        if s.get("parent_id") == "0":
            try:
                inp = json.loads(s.get("input") or "{}")
                if isinstance(inp, dict) and inp.get("prompt_text"):
                    return inp["prompt_text"]
            except Exception:
                pass
    return ""


def aggregate_tokens(spans):
    """汇总所有 model span 的 input/output token."""
    inp = 0
    out = 0
    for s in spans:
        if s.get("span_type") != "model":
            continue
        tags = s.get("custom_tags") or {}
        inp += safe_int(tags.get("input_tokens"))
        out += safe_int(tags.get("output_tokens"))
        # 兜底：output.usage
        if not tags.get("input_tokens"):
            try:
                ous = json.loads(s.get("output") or "{}")
                usage = ous.get("usage") if isinstance(ous, dict) else None
                if usage:
                    inp += safe_int(usage.get("prompt_tokens"))
                    out += safe_int(usage.get("completion_tokens"))
            except Exception:
                pass
    return inp, out


def compute_durations(spans):
    """返回 (total_ms, llm_pure_ms, tool_ms)
    llm_pure_ms = sum(model.duration) - sum(子 tool.duration of model)
    """
    root = next((s for s in spans if s.get("parent_id") == "0"), None)
    total_ms = safe_int(root.get("duration")) if root else 0

    model_spans = [s for s in spans if s.get("span_type") == "model"]
    tool_spans = [s for s in spans if s.get("span_type") == "tool"]

    model_total = sum(safe_int(s.get("duration")) for s in model_spans)
    # 计算 tool 是某 model 的子 span 的耗时
    model_ids = {s["span_id"] for s in model_spans}
    tool_under_model = sum(
        safe_int(s.get("duration"))
        for s in tool_spans
        if s.get("parent_id") in model_ids
    )
    llm_pure = max(0, model_total - tool_under_model)

    tool_total = sum(safe_int(s.get("duration")) for s in tool_spans)
    return total_ms, llm_pure, tool_total


def count_turns(spans):
    """根 span 的直接子 model span 数 = Agent 思考步数."""
    root = next((s for s in spans if s.get("parent_id") == "0"), None)
    if not root:
        return 0
    rid = root["span_id"]
    return sum(
        1 for s in spans if s.get("parent_id") == rid and s.get("span_type") == "model"
    )


def get_model_name(spans):
    for s in spans:
        if s.get("span_type") == "model":
            tags = s.get("custom_tags") or {}
            if tags.get("model_name"):
                return tags["model_name"]
    return ""


def get_status(spans):
    root = next((s for s in spans if s.get("parent_id") == "0"), None)
    if not root:
        return "unknown"
    sc = root.get("status_code")
    if sc == 0 or root.get("status") == "success":
        return "success"
    return "error"


def get_tool_calls(spans):
    return sum(1 for s in spans if s.get("span_type") == "tool")


# ---------------------------------------------------------------------------
# 健康度评分（5 维雷达，对齐 dashboard.html 看板口径）
# ---------------------------------------------------------------------------
# 每维 0-100，总分 = 5 维等权平均。chip 取最低分维度的命名。
#
#   响应：100 − clip((duration_s − 60)/60 × 50, 0, 70)；trace 失败=0
#   稳定：成功率 × 100 − 5 × 失败次数（只看后果，不看模式）
#   思考：100 − |turns − 6| × 5（6 turns 黄金区）
#   资源：100 − max(0,(avg_token_per_turn/128k − 0.6) × 100)
#   编排：100 − serial_penalty + diversity_bonus（只看模式，与稳定不重复）
#         serial_penalty: 同名工具连续 ≥3 扣 10 / ≥5 扣 20 / ≥7 扣 30
#         diversity_bonus: unique_tools/tool_calls ≥0.6 +20 / ≥0.4 +10
#         无工具调用时给中位 80 分（不可评估）。死循环 chip 归在编排下。
#
# trace 失败时硬封顶：响应=0，总分上限 50。
# ---------------------------------------------------------------------------

DIM_LABELS = {
    "response": "响应",
    "stability": "稳定",
    "thinking": "思考",
    "resource": "资源",
    "orchestration": "编排",
}
# chip 子分类（对齐看板首页的异常聚类标签）
# 每维一个 resolver，根据 features 进一步判定具体话术
CHIP_FALLBACK = {
    "response": "响应慢",
    "stability": "工具失败",
    "thinking": "思考异常",
    "resource": "上下文异常",
    "orchestration": "串行瓶颈",
}


def compute_rules(features, trace_status, turns):
    """5 条硬底线规则，对齐看板异常聚类。返回 [{name, failed_label, passed, detail}, ...]
    - name: 目标态命名，用于详情页"规则评估"列（✓/✗）
    - failed_label: 失败态命名，用于异常列 / 聚类 / 看板 chip（全平台统一）
    """
    rules = []
    rules.append({
        "name": "执行成功",
        "failed_label": "执行失败",
        "passed": trace_status == 0,
        "detail": "trace 根 span 状态正常" if trace_status == 0 else "trace 根 span 异常退出",
    })
    has_loop = bool(features.get("has_loop", False))
    rules.append({
        "name": "无死循环",
        "failed_label": "出现死循环",
        "passed": not has_loop,
        "detail": "无同名 tool 连续 ≥3 次" if not has_loop else "检测到同名 tool 连续调用 ≥3 次",
    })
    fails = int(features.get("tool_failures", 0) or 0)
    stable_ok = fails == 0
    rules.append({
        "name": "工具稳定",
        "failed_label": "工具失败",
        "passed": stable_ok,
        "detail": f"{fails} 次失败" if not stable_ok else "0 失败",
    })
    turns_ok = 3 <= turns <= 9
    # 轮数失败态需区分过少 / 过多
    if turns < 3:
        turns_failed = "轮数过少"
    elif turns > 9:
        turns_failed = "轮数过多"
    else:
        turns_failed = "轮数异常"
    rules.append({
        "name": "轮数合理",
        "failed_label": turns_failed,
        "passed": turns_ok,
        "detail": f"{turns} 轮" + ("（黄金区 3-9）" if turns_ok else "（建议 3-9）"),
    })
    avg_tk = features.get("avg_tokens_per_turn", 0) or 0
    ctx_ok = avg_tk <= 76000
    rules.append({
        "name": "上下文未爆",
        "failed_label": "上下文超限",
        "passed": ctx_ok,
        "detail": f"单轮平均 {round(avg_tk / 1000, 1)}k Token" + ("（≤ 76k）" if ctx_ok else "（超过 76k）"),
    })
    return rules


def resolve_chip(min_dim, features, turns):
    """根据最低维度 + 子特征，返回看板对齐的 chip 名称."""
    if min_dim == "response":
        # cp_ratio 高 → 计算占主导，关键路径过长；cp_ratio 低 → 等待占主导，排队过久
        return "关键路径过长" if features.get("cp_ratio", 0) > 0.7 else "排队过久"

    if min_dim == "stability":
        mcp_retries = features.get("mcp_retries", 0)
        skill_retries = features.get("skill_retries", 0)
        # 注：死循环作为"调度模式"问题，归到 orchestration；这里只看后果
        if mcp_retries > skill_retries and mcp_retries > 0:
            return "MCP 失败"
        if skill_retries > 0:
            return "Skill 失败"
        return "工具失败"

    if min_dim == "thinking":
        if turns >= 10:
            return "过度思考"
        if turns < 3:
            return "思考过简"
        return "慢思考无效"  # 中间区间，需 AI 评委兜底

    if min_dim == "resource":
        # 当前数据里 OOM 罕见，主要是上下文超限
        if features.get("avg_tokens_per_turn", 0) > 76_000:
            return "长上下文超限"
        return "显存 OOM 重跑"

    if min_dim == "orchestration":
        # 优先级：死循环模式 > 串行重试 > 调度单一
        if features.get("has_loop", False):
            return "出现死循环"
        max_run = features.get("max_serial_run", 0)
        if max_run >= 5:
            return "工具串行重复"
        tool_calls = features.get("tool_calls", 0) or 0
        unique_tools = features.get("unique_tools", 0) or 0
        if tool_calls and unique_tools / tool_calls < 0.3:
            return "调度单一"
        return "调度欠佳"

    return CHIP_FALLBACK.get(min_dim, "异常")


def _clip(v, lo, hi):
    return max(lo, min(hi, v))


def compute_health(f):
    """根据聚合特征返回 (score, color, chip, radar{}, reasons[])."""
    has_fail = f.get("has_root_fail", False)
    duration_s = f.get("duration_ms", 0) / 1000
    tool_fail_rate = f.get("tool_fail_rate", 0) / 100  # 0-1
    turns = f.get("turns", 0)
    avg_tok = f.get("avg_tokens_per_turn", 0)
    tool_calls = f.get("tool_calls", 0) or 0
    tool_failures = f.get("tool_failures", 0) or 0
    unique_tools = f.get("unique_tools", 0) or 0
    max_serial_run = f.get("max_serial_run", 0) or 0

    # 1. 响应（不变）
    if has_fail:
        response = 0
    else:
        penalty = _clip((duration_s - 60) / 60 * 50, 0, 70)
        response = round(100 - penalty)

    # 2. 稳定 = 后果（不再处理"重试模式"，改在编排里）
    #    成功率 × 100 − 5 × 失败次数（每次失败再扣 5 分，强化对 0 失败的鼓励）
    success_rate = (1 - tool_fail_rate)
    stability = round(success_rate * 100 - 5 * tool_failures)
    stability = _clip(stability, 0, 100)

    # 3. 思考（不变）
    thinking = round(100 - abs(turns - 6) * 5)
    thinking = _clip(thinking, 0, 100)

    # 4. 资源（不变）
    over = max(0, (avg_tok / 128_000) - 0.6)
    resource = round(100 - over * 100)
    resource = _clip(resource, 0, 100)

    # 5. 编排 = 调度模式（与稳定不重复）
    #    score = 100 − serial_penalty + diversity_bonus
    #    serial_penalty: 同名工具连续调用 ≥3 扣 10 / ≥5 扣 20 / ≥7 扣 30
    #    diversity_bonus: unique_tools / tool_calls ≥0.6 +20，0.4-0.6 +10，<0.4 +0
    if tool_calls == 0:
        # 无工具调用：纯对话，编排不可评估，给中位 80 分（不影响总分太多）
        orchestration = 80
    else:
        if max_serial_run >= 7:
            serial_penalty = 30
        elif max_serial_run >= 5:
            serial_penalty = 20
        elif max_serial_run >= 3:
            serial_penalty = 10
        else:
            serial_penalty = 0

        diversity = unique_tools / tool_calls if tool_calls else 0
        if diversity >= 0.6:
            diversity_bonus = 20
        elif diversity >= 0.4:
            diversity_bonus = 10
        else:
            diversity_bonus = 0

        orchestration = 100 - serial_penalty + diversity_bonus
        orchestration = _clip(orchestration, 0, 100)

    radar = {
        "response": response,
        "stability": stability,
        "thinking": thinking,
        "resource": resource,
        "orchestration": orchestration,
    }

    # 总分：等权平均；trace 失败封顶 50
    score = round(sum(radar.values()) / 5)
    if has_fail:
        score = min(score, 50)

    # color 分档
    if score >= 85:
        color = "green"
    elif score >= 70:
        color = "orange"
    elif score >= 50:
        color = "purple"
    else:
        color = "red"

    # chip：trace 失败硬规则；总分≥85 健康；否则按最低维度调子分类
    if has_fail:
        chip = "trace 失败"
    elif score >= 85:
        chip = "健康"
    else:
        min_dim = min(radar.items(), key=lambda kv: kv[1])[0]
        chip = resolve_chip(min_dim, f, turns)

    # reasons：所有 < 70 的维度，用维度名 + 实际分数
    reasons = [f"{DIM_LABELS[k]}{v}" for k, v in radar.items() if v < 70]

    return score, color, chip, radar, reasons


def compute_session_features(traces, total_duration_ms):
    """从 traces 列表（已包含每个 trace 的解析结果）+ 原始 spans 列表聚合特征."""
    has_root_fail = False
    total_tools = 0
    failed_tools = 0
    tool_name_count = {}
    # 每类工具的重试次数（按 custom_tags 区分 MCP / Skill）
    mcp_retries = 0
    skill_retries = 0
    # 死循环检测：根 span 下子 tool 按时间序排列，是否有同名连续 ≥3 次
    has_loop = False
    # 编排维度信号：所有 trace 中同名工具连续调用的最长长度（不论是否最终成功，只看模式）
    max_serial_run = 0
    sum_llm_pure = 0

    for t in traces:
        if t.get("status") != "success":
            has_root_fail = True
        sum_llm_pure += t.get("llm_pure_ms", 0)

        # 收集本 trace 内 tool span 的 (name, mcp/skill 类型, 起始时间)
        tool_seq = []
        local_name_count = {}
        for sp in t.get("_raw_spans", []):
            if sp.get("span_type") != "tool":
                continue
            total_tools += 1
            if sp.get("status_code", 0) != 0:
                failed_tools += 1
            name = sp.get("span_name", "")
            tags = sp.get("custom_tags") or {}
            kind = "mcp" if tags.get("mcp_server_name") else ("skill" if tags.get("skill_name") else "other")
            tool_seq.append((safe_int(sp.get("started_at")), name, kind))
            local_name_count[name] = local_name_count.get(name, 0) + 1
            tool_name_count[name] = tool_name_count.get(name, 0) + 1

        # 按时间排序后扫描连续同名段
        tool_seq.sort()
        run_name = None
        run_len = 0
        for _, name, kind in tool_seq:
            if name == run_name:
                run_len += 1
                if run_len >= 3:
                    has_loop = True
            else:
                run_name = name
                run_len = 1
            if run_len > max_serial_run:
                max_serial_run = run_len

        # 重试归类：同名 tool 在本 trace 内出现 c 次 → c-1 次是重试
        for name, c in local_name_count.items():
            if c <= 1:
                continue
            extra = c - 1
            # 取该 name 在本 trace 第一个 span 的 kind 作为归类依据
            kind = next((k for _, n, k in tool_seq if n == name), "other")
            if kind == "mcp":
                mcp_retries += extra
            elif kind == "skill":
                skill_retries += extra

    retries = sum(c - 1 for c in tool_name_count.values() if c > 1)
    tool_fail_rate = (failed_tools / total_tools * 100) if total_tools else 0
    tool_retry_rate = (retries / total_tools * 100) if total_tools else 0

    total_tokens = sum(t.get("input_tokens", 0) + t.get("output_tokens", 0) for t in traces)
    total_turns = sum(t.get("turns", 0) for t in traces)
    avg_tok_per_turn = (total_tokens / total_turns) if total_turns else 0
    cp_ratio = (sum_llm_pure / total_duration_ms) if total_duration_ms else 0

    # 编排维度新增信号
    # 1) max_serial_run：所有 trace 中"同名工具连续调用"的最长长度（只统计模式，不论结果）
    # 2) unique_tools / total_tools：调度多样性
    unique_tools = len(tool_name_count)

    return {
        "has_root_fail": has_root_fail,
        "tool_fail_rate": tool_fail_rate,
        "tool_retry_rate": tool_retry_rate,
        "avg_tokens_per_turn": avg_tok_per_turn,
        "cp_ratio": cp_ratio,
        "duration_ms": total_duration_ms,
        "turns": total_turns,
        "tool_calls": total_tools,
        "tool_failures": failed_tools,
        "tool_retries": retries,
        "mcp_retries": mcp_retries,
        "skill_retries": skill_retries,
        "has_loop": has_loop,
        "max_serial_run": max_serial_run,
        "unique_tools": unique_tools,
    }


def short_id(uuid_or_id):
    """取前 6 位作为前端 URL ?id= 的简写."""
    s = uuid_or_id.replace("-", "").replace("_", "")
    # ses_ 开头的特殊处理
    if uuid_or_id.startswith("ses_"):
        return uuid_or_id[4:10]
    return s[:6]


def parse_trace(trace_file):
    spans = load_json(trace_file)
    if not spans:
        return None
    root = next((s for s in spans if s.get("parent_id") == "0"), None)
    if not root:
        return None

    total_ms, llm_pure_ms, tool_ms = compute_durations(spans)
    inp_tok, out_tok = aggregate_tokens(spans)

    return {
        "trace_id": root["trace_id"],
        "span_id": root["span_id"],
        "started_at_ms": safe_int(root.get("started_at")),
        "title": extract_user_prompt(spans),
        "model_name": get_model_name(spans),
        "turns": count_turns(spans),
        "duration_ms": total_ms,
        "llm_pure_ms": llm_pure_ms,
        "tool_ms": tool_ms,
        "input_tokens": inp_tok,
        "output_tokens": out_tok,
        "tool_calls": get_tool_calls(spans),
        "status": get_status(spans),
        "spans": [
            {
                "span_id": s["span_id"],
                "parent_id": s.get("parent_id"),
                "span_name": s.get("span_name"),
                "span_type": s.get("span_type"),
                "duration_ms": safe_int(s.get("duration")),
                "started_at_ms": safe_int(s.get("started_at")),
                "status_code": s.get("status_code"),
                "input": s.get("input") or "",
                "output": s.get("output") or "",
                "custom_tags": s.get("custom_tags") or {},
            }
            for s in spans
        ],
        # 内部用：聚合特征时需要 raw spans，导出前会剥离
        "_raw_spans": spans,
    }


def build_session(artifact_id, session_id, created_at_ms, user_meta, trace_dir):
    """把同一 session 的多个 trace 聚合为一个 session 对象（取最早 trace 的 prompt 作 title）."""
    trace_files = sorted(glob.glob(os.path.join(trace_dir, "trace_*.json")))
    traces = []
    for tf in trace_files:
        t = parse_trace(tf)
        if t:
            traces.append(t)
    if not traces:
        return None
    # 按 started_at 升序
    traces.sort(key=lambda x: x["started_at_ms"])

    primary = traces[0]
    # 聚合
    total_ms = sum(t["duration_ms"] for t in traces)
    total_in = sum(t["input_tokens"] for t in traces)
    total_out = sum(t["output_tokens"] for t in traces)
    total_turns = sum(t["turns"] for t in traces)
    total_tools = sum(t["tool_calls"] for t in traces)

    # 计算 5 维健康度
    features = compute_session_features(traces, total_ms)
    score, color, chip, radar, reasons = compute_health(features)

    # 5 条硬底线规则（trace 失败 = 任一 trace status != success）
    trace_status = 0 if all(t.get("status") == "success" for t in traces) else 1
    rules = compute_rules(features, trace_status, features.get("turns", 0))

    # 导出前剥离 _raw_spans，避免 JSON 体积爆炸
    for t in traces:
        t.pop("_raw_spans", None)

    sid_short = short_id(session_id)

    return {
        "id": sid_short,                         # 前端 URL ?id= 用
        "session_id": session_id,
        "artifact_id": artifact_id,
        "user": user_meta.get("user_name", "anonymous"),
        "user_id": user_meta.get("user_id"),
        "title": primary["title"] or f"Session {sid_short}",
        "trace": primary["trace_id"],            # 主 trace
        "model_name": primary["model_name"],
        "turns": total_turns,
        "trace_count": len(traces),
        "duration_ms": total_ms,
        "input_tokens": total_in,
        "output_tokens": total_out,
        "tool_calls": total_tools,
        "started_at_ms": primary["started_at_ms"],
        # 健康度（5 维等权平均）
        "score": score,
        "color": color,
        "chip": chip,
        "radar": radar,
        "health_reasons": reasons,
        "rules": rules,
        "features": {
            "tool_fail_rate": round(features["tool_fail_rate"], 1),
            "tool_retry_rate": round(features["tool_retry_rate"], 1),
            "avg_tokens_per_turn": round(features["avg_tokens_per_turn"]),
            "cp_ratio": round(features["cp_ratio"], 2),
            "mcp_retries": features["mcp_retries"],
            "skill_retries": features["skill_retries"],
            "has_loop": features["has_loop"],
            "tool_calls": features.get("tool_calls", 0),
            "tool_failures": features.get("tool_failures", 0),
            "unique_tools": features.get("unique_tools", 0),
            "max_serial_run": features.get("max_serial_run", 0),
        },
        "traces": traces,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--conversations", required=True)
    ap.add_argument("--trace-root", required=True, help="目录下每个子目录是一个 session 的 trace_*.json")
    ap.add_argument("--user-meta", default=None, help="可选: artifact_id → {user_id,user_name} 映射 JSON")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    convs = load_json(args.conversations)["data"]
    user_meta_all = load_json(args.user_meta) if args.user_meta and os.path.exists(args.user_meta) else {}

    sessions = []
    for art in convs:
        aid = art["artifact_id"]
        umeta = user_meta_all.get(aid, {})
        for it in art["items"]:
            sid = it["neeko_resume_session_id"]
            sid_short = short_id(sid)
            tdir = os.path.join(args.trace_root, sid_short)
            if not os.path.isdir(tdir):
                # 兼容：尝试按完整 session_id 命名
                tdir = os.path.join(args.trace_root, sid)
            if not os.path.isdir(tdir):
                continue
            sess = build_session(aid, sid, it["created_at_ms"], umeta, tdir)
            if sess:
                sessions.append(sess)

    # 按时间倒序
    sessions.sort(key=lambda x: x["started_at_ms"], reverse=True)

    out = {"sessions": sessions, "generated_at": datetime.now(timezone(timedelta(hours=8))).isoformat()}
    os.makedirs(os.path.dirname(args.out), exist_ok=True)
    with open(args.out, "w") as f:
        json.dump(out, f, ensure_ascii=False, indent=2)
    print(f"wrote {len(sessions)} sessions to {args.out}")


if __name__ == "__main__":
    main()
