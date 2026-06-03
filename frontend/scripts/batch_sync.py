#!/usr/bin/env python3
"""
批量同步编排器：artifact_id 列表 -> 全量 trace -> sessions_full.json

严格遵循 SKILL.md 的 Step1-3，每个 session 独立查询、独立写文件、独立解析。

【只能在内网开发机运行】依赖：
  - fornax-cli（已安装并 config set ak/sk）
  - 可访问 agentic-aidp.bytedance.net 内网 API

用法：
  export PATH="$HOME/.local/bin:$PATH"
  python3 scripts/batch_sync.py \
      --csv "查询结果导出 - DataPage (1).csv" \
      --workdir tmp/fornax_full \
      --concurrency 3

  # 产物：tmp/fornax_full/sessions_full.json
  # 之后用 importer 入库：
  #   cd ../backend
  #   go run -mod=vendor ./cmd/importer -file ../frontend/scripts/tmp/fornax_full/sessions_full.json -batch full-647
"""
import argparse
import csv
import glob
import json
import os
import subprocess
import sys
import threading
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

CONV_API = "https://agentic-aidp.bytedance.net/agentic-api/v1/open/artifact/conversation/list"

_print_lock = threading.Lock()


def log(msg):
    with _print_lock:
        print(msg, flush=True)


# --------------------------------------------------------------------------
# 输入
# --------------------------------------------------------------------------
def read_artifact_ids(csv_path):
    ids = []
    with open(csv_path, newline="") as f:
        reader = csv.reader(f)
        header = next(reader, None)  # 跳过表头 artifact_id
        for row in reader:
            if not row:
                continue
            aid = row[0].strip()
            if aid:
                ids.append(aid)
    # 去重保序
    seen = set()
    uniq = []
    for a in ids:
        if a not in seen:
            seen.add(a)
            uniq.append(a)
    return uniq


# --------------------------------------------------------------------------
# Step 1: 批量查会话列表
# --------------------------------------------------------------------------
def fetch_conversations(artifact_ids, chunk=50, retries=3):
    """返回 conversations.json 的 data 列表：[{artifact_id, items:[...]}, ...]"""
    all_data = []
    for i in range(0, len(artifact_ids), chunk):
        batch = artifact_ids[i : i + chunk]
        payload = json.dumps({"artifact_ids": batch}).encode("utf-8")
        last_err = None
        for attempt in range(retries):
            try:
                req = urllib.request.Request(
                    CONV_API,
                    data=payload,
                    headers={"Content-Type": "application/json"},
                    method="POST",
                )
                with urllib.request.urlopen(req, timeout=30) as resp:
                    body = json.loads(resp.read().decode("utf-8"))
                data = body.get("data", []) or []
                all_data.extend(data)
                log(f"[step1] 批 {i//chunk + 1}: {len(batch)} artifact -> {len(data)} 条返回")
                break
            except Exception as e:  # noqa
                last_err = e
                wait = 2 ** attempt
                log(f"[step1][warn] 批 {i//chunk + 1} 第 {attempt+1} 次失败: {e}，{wait}s 后重试")
                time.sleep(wait)
        else:
            log(f"[step1][error] 批 {i//chunk + 1} 最终失败: {last_err}")
    return all_data


# --------------------------------------------------------------------------
# fornax-cli 调用（带重试/退避）
# --------------------------------------------------------------------------
def run_fornax(args, retries=4, base_sleep=2):
    last = None
    for attempt in range(retries):
        try:
            proc = subprocess.run(
                args,
                capture_output=True,
                text=True,
                timeout=120,
            )
        except subprocess.TimeoutExpired as e:
            last = f"timeout: {e}"
            time.sleep(base_sleep * (2 ** attempt))
            continue
        out = (proc.stdout or "") + (proc.stderr or "")
        if proc.returncode == 0 and "rate limit" not in out.lower():
            return True, out
        last = out.strip()[-300:]
        # rate limit 或失败 -> 退避重试
        time.sleep(base_sleep * (2 ** attempt))
    return False, last


def newest_json_array(dir_path):
    """读取 dir 下最新的 .json 文件，返回解析后的 list（span 数组）。"""
    files = sorted(glob.glob(os.path.join(dir_path, "*.json")), key=os.path.getmtime)
    for fp in reversed(files):
        try:
            with open(fp) as f:
                data = json.load(f)
            if isinstance(data, list):
                return data
            # 有些版本包一层 {data:[...]} 或 {spans:[...]}
            if isinstance(data, dict):
                for k in ("data", "spans", "items", "result"):
                    if isinstance(data.get(k), list):
                        return data[k]
        except Exception:
            continue
    return []


# --------------------------------------------------------------------------
# Step 2: trace list -> 提取 trace_id
# --------------------------------------------------------------------------
def list_trace_ids(session_id, list_dir, since=None, until=None):
    os.makedirs(list_dir, exist_ok=True)
    cmd = [
        "fornax-cli", "trace", "list",
        "--trace-filter-expr", f"thread_id='{session_id}'",
        "--page-size", "50",
        "--format", "json",
        "--timeout", "30s",
        "-o", list_dir + "/",
    ]
    if since and until:
        cmd += ["--since", since, "--until", until]
    else:
        cmd += ["--last-n-minutes", "10080"]  # 默认最近 7 天

    ok, out = run_fornax(cmd)
    if not ok:
        log(f"[step2][warn] {session_id} trace list 失败: {out}")
        return []

    spans = newest_json_array(list_dir)
    # 按 trace_id 去重；可选时间二次过滤（取每 trace 最早 span 的 started_at）
    by_trace = {}
    for sp in spans:
        tid = sp.get("trace_id")
        if not tid:
            continue
        st = sp.get("started_at")
        try:
            st = int(st)
        except (TypeError, ValueError):
            st = None
        if tid not in by_trace or (st is not None and (by_trace[tid] is None or st < by_trace[tid])):
            by_trace[tid] = st
    return list(by_trace.keys())


# --------------------------------------------------------------------------
# Step 3: trace get -> trace_<id>.json
# --------------------------------------------------------------------------
def get_trace(trace_id, session_trace_dir):
    """把完整 span 数组写到 session_trace_dir/trace_<trace_id>.json，已存在则跳过。"""
    target = os.path.join(session_trace_dir, f"trace_{trace_id}.json")
    if os.path.exists(target) and os.path.getsize(target) > 0:
        return True  # 断点续传
    os.makedirs(session_trace_dir, exist_ok=True)
    tmp_dir = os.path.join(session_trace_dir, f".tmp_{trace_id}")
    os.makedirs(tmp_dir, exist_ok=True)
    cmd = [
        "fornax-cli", "trace", "get",
        "--trace-id", trace_id,
        "--format", "json",
        "--timeout", "30s",
        "--last-n-minutes", "10080",
        "-o", tmp_dir + "/",
    ]
    ok, out = run_fornax(cmd)
    if not ok:
        log(f"[step3][warn] trace {trace_id} get 失败: {out}")
        return False
    spans = newest_json_array(tmp_dir)
    # 清理临时目录
    for fp in glob.glob(os.path.join(tmp_dir, "*")):
        try:
            os.remove(fp)
        except OSError:
            pass
    try:
        os.rmdir(tmp_dir)
    except OSError:
        pass
    if not spans:
        log(f"[step3][warn] trace {trace_id} 无 span（可能已过 7 天留存）")
        return False
    with open(target, "w") as f:
        json.dump(spans, f, ensure_ascii=False)
    return True


# --------------------------------------------------------------------------
# 单 session 的完整抓取（Step2 + Step3）
# --------------------------------------------------------------------------
def process_session(session_id, trace_root, list_root, since, until):
    list_dir = os.path.join(list_root, session_id)
    session_trace_dir = os.path.join(trace_root, session_id)
    trace_ids = list_trace_ids(session_id, list_dir, since, until)
    got = 0
    for tid in trace_ids:
        if get_trace(tid, session_trace_dir):
            got += 1
        time.sleep(0.5)  # 缓速避免 rate limit
    log(f"[session] {session_id}: trace {got}/{len(trace_ids)} 已落盘")
    return got


# --------------------------------------------------------------------------
# main
# --------------------------------------------------------------------------
def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--csv", required=True, help="artifact_id 列表 CSV（首行表头）")
    ap.add_argument("--workdir", default="tmp/fornax_full", help="所有中间产物与输出目录")
    ap.add_argument("--concurrency", type=int, default=3, help="session 级并发（建议 2-4）")
    ap.add_argument("--since", default=None, help="起始时间 2026-01-01T01:01:00+08:00")
    ap.add_argument("--until", default=None, help="结束时间 2026-01-01T23:59:59+08:00")
    ap.add_argument("--limit", type=int, default=0, help="只处理前 N 个 artifact（调试用，0=全部）")
    args = ap.parse_args()

    workdir = args.workdir
    conv_root = os.path.join(workdir, "conversations")
    list_root = os.path.join(workdir, "list")
    trace_root = os.path.join(workdir, "traces")
    for d in (conv_root, list_root, trace_root):
        os.makedirs(d, exist_ok=True)

    # 1) 读 artifact
    artifact_ids = read_artifact_ids(args.csv)
    if args.limit > 0:
        artifact_ids = artifact_ids[: args.limit]
    log(f"[init] 共 {len(artifact_ids)} 个 artifact_id")

    # 2) Step1：会话列表
    conv_file = os.path.join(workdir, "conversations.json")
    if os.path.exists(conv_file):
        log(f"[step1] 复用已有 {conv_file}")
        data = json.load(open(conv_file))["data"]
    else:
        data = fetch_conversations(artifact_ids)
        json.dump({"data": data}, open(conv_file, "w"), ensure_ascii=False, indent=2)
        log(f"[step1] 写入 {conv_file}")

    # 收集所有 (artifact_id, session_id)，并构建 user_meta
    sessions = []  # list of session_id
    user_meta = {}  # artifact_id -> {user_id, user_name}
    for art in data:
        aid = art.get("artifact_id")
        items = art.get("items", []) or []
        if items and aid not in user_meta:
            uid = items[0].get("user_id")
            user_meta[aid] = {"user_id": str(uid) if uid is not None else None,
                              "user_name": "anonymous"}
        for it in items:
            sid = it.get("neeko_resume_session_id")
            if sid:
                sessions.append(sid)
    sessions = list(dict.fromkeys(sessions))  # 去重保序
    json.dump(user_meta, open(os.path.join(workdir, "user_meta.json"), "w"),
              ensure_ascii=False, indent=2)
    log(f"[init] 共 {len(sessions)} 个 session 待抓取")

    # 3) Step2 + Step3：并发抓取
    done = 0
    total_traces = 0
    with ThreadPoolExecutor(max_workers=args.concurrency) as ex:
        futs = {
            ex.submit(process_session, sid, trace_root, list_root, args.since, args.until): sid
            for sid in sessions
        }
        for fut in as_completed(futs):
            done += 1
            try:
                total_traces += fut.result()
            except Exception as e:  # noqa
                log(f"[session][error] {futs[fut]}: {e}")
            if done % 20 == 0:
                log(f"[progress] {done}/{len(sessions)} session 完成，累计 {total_traces} trace")

    log(f"[fetch-done] {len(sessions)} session 抓完，累计 {total_traces} trace")

    # 4) 解析 -> sessions_full.json
    out_file = os.path.join(workdir, "sessions_full.json")
    parse_script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "parse_fornax.py")
    cmd = [
        sys.executable, parse_script,
        "--conversations", conv_file,
        "--trace-root", trace_root,
        "--user-meta", os.path.join(workdir, "user_meta.json"),
        "--out", out_file,
    ]
    log(f"[parse] {' '.join(cmd)}")
    rc = subprocess.run(cmd).returncode
    if rc != 0:
        log(f"[parse][error] parse_fornax.py 退出码 {rc}")
        sys.exit(rc)
    log(f"[done] 全部完成 -> {out_file}")
    log("[next] 入库： cd ../backend && go run -mod=vendor ./cmd/importer "
        f"-file {os.path.abspath(out_file)} -batch full-647")


if __name__ == "__main__":
    main()
