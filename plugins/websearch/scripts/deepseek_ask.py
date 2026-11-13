#!/usr/bin/env python3
"""DeepSeek chat ask — 0-token websearch plugin script (W4).

DeepSeek Chat API 直连问答（deepseek-v4-flash，thinking disabled），
只输出一个精简 JSON 对象。过滤/截断全部在脚本内完成，agent 只为
蒸馏后的结果付 token。

Usage:
  python3 deepseek_ask.py "什么是 RAG" [--max-tokens 800]

Output (single JSON object on stdout, always):
  {"status": "ok|empty|timeout|unavailable", "method": "deepseek",
   "elapsed_ms": N,
   "results": [{"text": "..."}],
   "count": N}

status semantics (design doc gopi-websearch-plugin-design.md):
  ok          — >=1 条回答
  empty       — API 正常但回答为空
  timeout     — API 在 API_TIMEOUT_SEC 内未应答
  unavailable — 无 API key / HTTP 错误 / 响应不可解析等

API key 从 ~/.hermes/.env 读取（grep DEEPSEEK_API_KEY=），
**绝不打印 key 值**。环境变量 DEEPSEEK_API_KEY 显式设置时优先
（设为空 = 无 key，返回 unavailable，用于模拟无 key 场景）。
"""
import argparse
import json
import os
import signal
import socket
import sys
import time
import urllib.error
import urllib.request

# ---------------------------------------------------------------------------
# Constants — single source of truth, no magic numbers inline (Rule 1)
# ---------------------------------------------------------------------------
DEFAULT_MAX_TOKENS = 800     # 成本控制：默认 max_tokens <= 800
MAX_MAX_TOKENS = 2048        # 硬上限，防止 --max-tokens 失控
API_TIMEOUT_SEC = 30         # 网络超时；LLM 生成需留足时间
MAX_ANSWER_CHARS = 6000      # 回答硬截断（0-token：结果精简才进 context）
API_URL = "https://api.deepseek.com/v1/chat/completions"
MODEL = "deepseek-v4-flash"  # 已探测验证：+ thinking disabled 可正常回答
ENV_FILE = os.path.expanduser("~/.hermes/.env")
ENV_KEY = "DEEPSEEK_API_KEY"


def load_api_key():
    """解析 key 值（不打印）。

    优先级：
      1. 环境变量 DEEPSEEK_API_KEY 显式设置 -> 直接用（空值 = 无 key，
         用于验证无 key 场景，与验收命令 `DEEPSEEK_API_KEY= ...` 一致）
      2. 否则 grep ~/.hermes/.env 中 `DEEPSEEK_API_KEY=` 行
    返回 "" 表示无 key（脚本返回 status=unavailable，不 crash）。
    """
    if ENV_KEY in os.environ:
        return os.environ[ENV_KEY].strip()
    try:
        with open(ENV_FILE, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if line.startswith(ENV_KEY + "="):
                    return line.split("=", 1)[1].strip().strip('"').strip("'")
    except OSError:
        pass
    return ""


def ask(question, max_tokens, api_key):
    """调用 DeepSeek chat API，返回 (answer_text, error_info)。"""
    payload = {
        "model": MODEL,
        "messages": [{"role": "user", "content": question}],
        "max_tokens": max_tokens,
        # thinking disabled：v4-flash 显式禁用推理链，省 token 省时
        "thinking": {"type": "disabled"},
        "stream": False,
    }
    req = urllib.request.Request(
        API_URL,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Content-Type": "application/json",
            "Authorization": "Bearer " + api_key,
        },
    )
    with urllib.request.urlopen(req, timeout=API_TIMEOUT_SEC) as resp:
        data = json.loads(resp.read().decode("utf-8"))
    try:
        return data["choices"][0]["message"]["content"].strip(), None
    except (KeyError, IndexError, TypeError) as e:
        return "", f"unexpected API response shape: {e!r}"


def main():
    # 下游提前关闭管道（如 | head）时按默认行为退出，不打 BrokenPipeError
    signal.signal(signal.SIGPIPE, signal.SIG_DFL)
    ap = argparse.ArgumentParser(
        description="Ask DeepSeek (v4-flash, thinking disabled), return slim JSON."
    )
    ap.add_argument("question", help="question, e.g. \"什么是 RAG\"")
    ap.add_argument("--max-tokens", type=int, default=DEFAULT_MAX_TOKENS,
                    help=f"max answer tokens (default {DEFAULT_MAX_TOKENS}, "
                         f"capped at {MAX_MAX_TOKENS})")
    args = ap.parse_args()

    max_tokens = max(1, min(args.max_tokens, MAX_MAX_TOKENS))
    start = time.monotonic()
    payload = {"status": "unavailable", "method": "deepseek",
               "elapsed_ms": 0, "results": [], "count": 0}

    api_key = load_api_key()
    if not args.question.strip():
        payload["message"] = "empty question"
    elif not api_key:
        # 无 key：返回 unavailable，不 crash（key 值绝不打印）
        payload["message"] = f"no {ENV_KEY} in env or {ENV_FILE}"
    else:
        try:
            answer, err = ask(args.question, max_tokens, api_key)
            if err:
                payload["message"] = err
            elif answer:
                payload["status"] = "ok"
                payload["results"] = [{"text": answer[:MAX_ANSWER_CHARS]}]
                payload["count"] = 1
            else:
                payload["status"] = "empty"
                payload["message"] = "API returned empty answer"
        except (socket.timeout, TimeoutError) as e:
            payload["status"] = "timeout"
            payload["message"] = f"deepseek API timeout after {API_TIMEOUT_SEC}s: {e}"
        except urllib.error.HTTPError as e:
            if e.code == 401:
                payload["message"] = "deepseek API rejected key (HTTP 401)"
            elif e.code == 429:
                payload["message"] = "deepseek API rate limited (HTTP 429)"
            else:
                payload["message"] = f"deepseek API HTTP {e.code}: {e.reason}"
        except urllib.error.URLError as e:
            if isinstance(e.reason, (socket.timeout, TimeoutError)):
                payload["status"] = "timeout"
                payload["message"] = f"deepseek API timeout: {e.reason}"
            else:
                payload["message"] = f"deepseek API unreachable: {e.reason}"
        except json.JSONDecodeError as e:
            payload["message"] = f"deepseek API returned unparseable JSON: {e}"
        except Exception as e:  # noqa: BLE001 — plugin must always emit JSON
            payload["message"] = f"deepseek ask failed: {e!r}"

    payload["elapsed_ms"] = int((time.monotonic() - start) * 1000)
    json.dump(payload, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
