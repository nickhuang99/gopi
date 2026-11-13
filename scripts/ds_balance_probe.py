#!/usr/bin/env python3
"""DeepSeek 余额探针：读余额，并用余额【差额】反推真实成本。

为什么重要：我们此前所有成本模型（缓存命中放大、输出占比、provider 取舍）都
来自定价页推算。余额是**实测**——真实的钱扣了多少。

局限（实测发现，别误用）：余额只报到 2 位小数（0.01 元），所以
**单次小请求看不出消耗**（实测 12+4 tokens → Δ=0.000000）。
正确用法是**批次级对账**：一个 worker 跑一小时 / 一个批量任务 / 一天总量。
estimate() 是定价页口径的推算式，用于和实测 delta 对照——两者不符时，
先怀疑定价表（我们刚发现 gopi 的 pricing 表落后于模型改名）。
"""
import argparse
import json
import os
import sys
import time
import urllib.request

BASE = "https://api.deepseek.com"
SETTINGS = os.path.expanduser("~/.gopi/.ds_flash_settings.json")

# 定价页口径（元/百万 tokens，空闲时段）。仅供对照，不是真相来源。
PRICE_HIT, PRICE_MISS, PRICE_OUT = 0.02, 1.0, 4.0


# ---------- 网络（测试会替换） ----------

def api_key() -> str:
    with open(SETTINGS) as f:
        return json.load(f)["api_key"]


def balance(key: str) -> dict:
    req = urllib.request.Request(
        f"{BASE}/user/balance",
        headers={"Accept": "application/json", "Authorization": f"Bearer {key}"},
    )
    with urllib.request.urlopen(req, timeout=25) as r:
        return json.load(r)


def chat(key: str, prompt: str, max_tokens: int) -> dict:
    body = json.dumps({
        "model": "deepseek-flash",
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": max_tokens,
        "stream": False,
        "thinking": {"type": "disabled"},   # 排除 reasoning token 干扰
    }).encode()
    req = urllib.request.Request(
        f"{BASE}/chat/completions", data=body,
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {key}"},
    )
    with urllib.request.urlopen(req, timeout=120) as r:
        return json.load(r)


# ---------- 纯函数 ----------

def cny_balance(bal: dict):
    """取 CNY 那一行（不是 balance_infos[0]——顺序不保证）。没有则 None。"""
    for info in bal.get("balance_infos", []):
        if info.get("currency") == "CNY":
            return float(info["total_balance"])
    return None


def split_usage(usage: dict):
    """usage → (hit, miss, out)，兼容两种缓存字段形状。"""
    pt = int(usage.get("prompt_tokens", 0) or 0)
    out = int(usage.get("completion_tokens", 0) or 0)
    hit = usage.get("prompt_cache_hit_tokens")
    if hit is None:
        hit = (usage.get("prompt_tokens_details") or {}).get("cached_tokens", 0)
    hit = int(hit or 0)
    miss = usage.get("prompt_cache_miss_tokens")
    miss = int(miss) if miss is not None else max(pt - hit, 0)
    return hit, miss, out


def estimate(hit: int, miss: int, out: int) -> float:
    """定价页口径的推算花费（元）。"""
    return hit / 1e6 * PRICE_HIT + miss / 1e6 * PRICE_MISS + out / 1e6 * PRICE_OUT


def delta_of(before: float, after: float) -> float:
    """余额差额 = 这段时间真实花掉的钱（元）。不足 0 视为 0（不产生负数）。"""
    return max(round(before - after, 10), 0.0)


def compare_line(real: float, est: float, hit: int, miss: int, out: int) -> str:
    """实测 vs 推算的对照行——不符时让人一眼看到。"""
    verdict = "MATCH" if abs(real - est) < max(est * 0.1, 0.01) else "DIVERGES"
    return (f"  real={real:.6f}  estimated={est:.6f}  [{verdict}]  "
            f"(hit={hit} miss={miss} out={out})")


# ---------- 主流程 ----------

def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="DeepSeek balance probe / real-cost check")
    ap.add_argument("--probe", action="store_true", help="发一次受控请求并反推真实成本")
    ap.add_argument("--key", default="", help="API key（省略则读 settings）")
    ap.add_argument("--prompt", default="Reply with exactly: PROBE-OK")
    ap.add_argument("--max-tokens", type=int, default=16)
    ap.add_argument("--settle", type=float, default=6.0, help="等待结算的秒数")
    args = ap.parse_args(argv)

    # --key 让测试无需触碰真实凭证文件
    key = args.key or api_key()

    print("=== 余额（前）===")
    before = cny_balance(balance(key))
    print(f"  CNY: {before}")
    if before is None:
        print("  未找到 CNY 余额行——格式变了？")
        return 1
    if not args.probe:
        print("\n(--probe 可发一次受控请求并反推真实成本)")
        return 0

    print("\n=== 受控请求 ===")
    resp = chat(key, args.prompt, args.max_tokens)   # 失败向上抛，不吞
    usage = resp.get("usage", {})
    hit, miss, out = split_usage(usage)
    print(f"  model={resp.get('model')}  hit={hit} miss={miss} out={out}")

    time.sleep(args.settle)
    print("\n=== 余额（后）===")
    after = cny_balance(balance(key))
    print(f"  CNY: {after}")
    if after is None:
        print("  未找到 CNY 余额行——格式变了？无法对账。")
        return 1

    real = delta_of(before, after)
    est = estimate(hit, miss, out)
    print("\n=== 真实成本 vs 推算 ===")
    print(compare_line(real, est, hit, miss, out))
    if real == 0.0:
        print("  Δ=0 —— 余额只有 2 位小数，单次小请求看不出来。")
        print("  做批次级对账（worker 跑一小时 / 批量任务）才能看出真实花费。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
