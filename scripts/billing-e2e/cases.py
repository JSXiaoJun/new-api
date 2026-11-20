"""Usage dialects, prices and expected charges for the billing e2e.

Every upstream dialect reports the same canonical usage below in its own field
layout, so a correct gateway charges the same amount whatever protocol the
client used. The runner computes the expected charge from these numbers.
"""

# Canonical usage of one request.
UNCACHED_INPUT = 1000
CACHE_READ = 24000
CACHE_WRITE_5M = 2000   # Claude dialects only
CACHE_WRITE_1H = 3000   # Claude dialects only
OUTPUT = 300            # includes REASONING where the dialect reports it
REASONING = 100

# Prices. Ratio mode: quota per token = ratio (model ratio 1, group ratio 1).
MODEL_RATIO = 1.0
COMPLETION_RATIO = 5.0
CACHE_RATIO = 0.1
CREATE_CACHE_RATIO = 1.25
CACHE_1H_MULTIPLIER = 1.6  # types.ClaudeCacheCreation1hMultiplier
# Expression mode: USD per million tokens; quota = cost * 0.5 (QuotaPerUnit 500000 / 1e6).
EXPR = 'tier("standard", p * 5 + c * 25 + cr * 0.5 + cc * 6.25 + cc1h * 10)'
EXPR_PRICES = {"p": 5, "c": 25, "cr": 0.5, "cc": 6.25, "cc1h": 10}

# name -> channel type, model, whether the dialect reports Claude cache writes.
DIALECTS = {
    "claude":   {"type": 14, "model": "e2e-claude",   "claude": True},
    "openai":   {"type": 1,  "model": "e2e-gpt",      "claude": False},
    "azure":    {"type": 3,  "model": "e2e-azure",    "claude": False},
    "deepseek": {"type": 43, "model": "e2e-deepseek", "claude": False},
    "kimi":     {"type": 25, "model": "e2e-kimi",     "claude": False},
    "glm":      {"type": 26, "model": "e2e-glm",      "claude": False},
    "gemini":   {"type": 24, "model": "e2e-gemini",   "claude": False},
    # An upstream new-api relaying Claude in OpenAI Chat format, with a
    # billing_usage snapshot (the 2026-11-19 incident shape).
    "oc":       {"type": 1,  "model": "e2e-oc",       "claude": True},
}


def usage_for(dialect):
    claude = DIALECTS[dialect]["claude"]
    w5 = CACHE_WRITE_5M if claude else 0
    w1 = CACHE_WRITE_1H if claude else 0
    return {"input": UNCACHED_INPUT, "cache_read": CACHE_READ, "cache_write_5m": w5, "cache_write_1h": w1,
            "output": OUTPUT, "reasoning": 0 if claude else REASONING,
            "prompt_total": UNCACHED_INPUT + CACHE_READ + w5 + w1}


def openai_chat_usage(dialect, scen):
    u = usage_for(dialect)
    base = {"prompt_tokens": u["prompt_total"], "completion_tokens": u["output"], "total_tokens": u["prompt_total"] + u["output"]}
    reasoning = {"completion_tokens_details": {"reasoning_tokens": u["reasoning"]}}
    if dialect == "deepseek":
        return {**base, **reasoning, "prompt_cache_hit_tokens": u["cache_read"], "prompt_cache_miss_tokens": u["input"]}
    if dialect == "kimi":
        return {**base, "cached_tokens": u["cache_read"]}
    if dialect == "oc":
        writes = u["cache_write_5m"] + u["cache_write_1h"]
        snapshot_read = 0 if scen == "snapshot_bad_read" else u["cache_read"]
        snapshot_1h = 1 if scen == "snapshot_bad_write" else u["cache_write_1h"]
        claude = {"input_tokens": u["input"], "cache_read_input_tokens": snapshot_read, "output_tokens": u["output"],
                  "cache_creation_input_tokens": u["cache_write_5m"] + snapshot_1h,
                  "cache_creation": {"ephemeral_5m_input_tokens": u["cache_write_5m"], "ephemeral_1h_input_tokens": snapshot_1h},
                  "claude_cache_creation_5_m_tokens": 0, "claude_cache_creation_1_h_tokens": 0}
        return {**base, "usage_semantic": "openai", "usage_source": "anthropic",
                "prompt_tokens_details": {"cached_tokens": u["cache_read"], "cached_creation_tokens": writes, "cache_write_tokens": writes},
                "claude_cache_creation_5_m_tokens": u["cache_write_5m"], "claude_cache_creation_1_h_tokens": u["cache_write_1h"],
                "billing_usage": {"source": "claude_messages", "semantic": "anthropic", "claude_usage": claude}}
    return {**base, **reasoning, "prompt_tokens_details": {"cached_tokens": u["cache_read"]}}


def openai_responses_usage(dialect, scen="normal"):
    u = usage_for(dialect)
    usage = {"input_tokens": u["prompt_total"], "input_tokens_details": {"cached_tokens": u["cache_read"]},
             "output_tokens": u["output"], "output_tokens_details": {"reasoning_tokens": u["reasoning"]},
             "total_tokens": u["prompt_total"] + u["output"]}
    if dialect == "oc":
        # The shape an upstream new-api emits for Claude behind /v1/responses
        # (relaykit golden claude_to_openai_responses): flat OpenAI fields, the
        # 5m/1h split, and the Claude snapshot.
        chat = openai_chat_usage(dialect, scen)
        writes = u["cache_write_5m"] + u["cache_write_1h"]
        usage["input_tokens_details"].update({"cached_creation_tokens": writes, "cache_write_tokens": writes})
        usage.update({"usage_semantic": "openai", "usage_source": "anthropic",
                      "claude_cache_creation_5_m_tokens": u["cache_write_5m"], "claude_cache_creation_1_h_tokens": u["cache_write_1h"],
                      "billing_usage": chat["billing_usage"]})
    return usage


def charge(dialect, mode, input_side=True, output_tokens=None):
    """Expected quota for the canonical usage (or only its input side)."""
    u = usage_for(dialect)
    out = u["output"] if output_tokens is None else output_tokens
    inp, cr, w5, w1 = (u["input"], u["cache_read"], u["cache_write_5m"], u["cache_write_1h"]) if input_side else (0, 0, 0, 0)
    if mode == "expr":
        cost = inp * EXPR_PRICES["p"] + out * EXPR_PRICES["c"] + cr * EXPR_PRICES["cr"] + w5 * EXPR_PRICES["cc"] + w1 * EXPR_PRICES["cc1h"]
        return cost * 0.5
    return (inp + out * COMPLETION_RATIO + cr * CACHE_RATIO + w5 * CREATE_CACHE_RATIO
            + w1 * CREATE_CACHE_RATIO * CACHE_1H_MULTIPLIER) * MODEL_RATIO


def output_price(mode):
    return EXPR_PRICES["c"] * 0.5 if mode == "expr" else COMPLETION_RATIO * MODEL_RATIO
