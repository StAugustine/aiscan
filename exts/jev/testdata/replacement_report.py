"""Analyze each retained paid replacement experiment without pooling attempts.

Usage: python replacement_report.py output/reflex-replacement-live-...
Costs use the experiment's official tariff snapshot, never inferred invoices.
"""
import collections
import json
import math
import pathlib
import sys


def tariff(report, model):
    prices = report.get("prices", {})
    if isinstance(prices, str):
        prices = json.loads(prices or "{}")
    if model in prices:
        return prices[model]
    if model == report["model"] and "llm_input_miss_per_million" in prices:
        return {"input": prices["llm_input_miss_per_million"], "cache_read": prices["llm_input_hit_per_million"], "output": prices["llm_output_per_million"]}
    if model == report["jev_model"] and "jev_input_per_million" in prices:
        return {"input": prices["jev_input_per_million"], "cache_read": prices["jev_input_per_million"], "output": prices["jev_output_per_million"]}
    return None


def cost(usage, prices):
    if usage is None or prices is None:
        return 0.0, 1
    detail = usage.get("detail", {})
    missing = int(detail.get("usage_missing", 0))
    cached = int(detail.get("cache_read", 0))
    total_input = int(usage.get("input_tokens", 0))
    if cached > total_input:
        return 0.0, missing + 1
    return ((total_input - cached) * prices["input"] + cached * prices.get("cache_read", prices["input"]) + int(usage.get("output_tokens", 0)) * prices["output"]) / 1_000_000, missing


def analyze(directory):
    report = json.loads((directory / "report.json").read_text(encoding="utf-8"))
    audits = {}
    for family in report["libraries"]:
        path = directory / family / "library" / "decisions.jsonl"
        audits[family] = [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines()] if path.exists() else []
    groups = {}
    all_known = 0.0
    all_missing = 0
    all_jev_requests = 0
    for family in report["libraries"]:
        for arm in ["cold_learning", "ordinary_llm", "reflex_llm_judge", "reflex_jev"]:
            attempts = [a for a in report["attempts"] if a["family"] == family and a["arm"] == arm]
            build, runtime, missing, executed, successes, execution_calls, provider_blocked = 0.0, 0.0, 0, 0, 0, 0, 0
            wall = []
            for a in attempts:
                if "elapsed_ms" not in a or not a["elapsed_ms"]:
                    continue  # blocked attempts are not free successful tasks
                executed += 1
                successes += bool(a["success"])
                provider_blocked += any(request.get("http_status") in [401, 402, 403] or "API error (402)" in request.get("error", "") for request in a.get("llm_requests", []))
                wall.append(a["elapsed_ms"])
                for request in a.get("llm_requests", []):
                    value, gaps = cost(request.get("usage"), tariff(report, request["model"]))
                    missing += gaps
                    if request["purpose"] in ["claim", "compilation"]:
                        build += value
                    else:
                        runtime += value
                    execution_calls += request["purpose"] == "execution"
                if arm == "reflex_llm_judge":
                    continue  # local proxy usage is already charged as finite_judge LLM
                usage = a.get("jev_usage", {})
                jev_total, jev_missing = cost(usage, tariff(report, report["jev_model"]))
                missing += jev_missing
                all_jev_requests += int(usage.get("detail", {}).get("requests", 0))
                turn = f"{arm}-{a['group']}"
                recorded_build = sum(cost(row["data"].get("usage"), tariff(report, report["jev_model"]))[0] for row in audits[family]
                                     if row["kind"].startswith("jev_") and row["data"].get("background") and row["data"].get("turn_id") == turn)
                build += recorded_build
                runtime += max(0.0, jev_total - recorded_build)
            known = build + runtime
            all_known += known
            all_missing += missing
            groups[f"{family}/{arm}"] = {
                "attempts": len(attempts), "executed": executed, "blocked": len(attempts) - executed,
                "successes": successes, "llm_execution_calls": execution_calls,
                "provider_blocked": provider_blocked,
                "successes_over_provider_available_attempts": f"{successes}/{executed - provider_blocked}",
                "compilation_known_usd": round(build, 9), "runtime_known_usd": round(runtime, 9),
                "missing_usage_or_tariff": missing,
                "total_usd": round(known, 9) if not missing and executed else None,
                "all_attempt_cost_per_success_usd": round(known / successes, 9) if not missing and successes else None,
                "mean_wall_ms_including_wait_for_background": round(sum(wall) / len(wall), 1) if wall else None,
            }
    summary = {
        "experiment": directory.name, "billing_basis": "official tariff snapshot and returned usage; not invoice",
        "price_source": report.get("price_source"), "groups": groups,
        "known_cost_usd": round(all_known, 9), "missing_usage_or_tariff": all_missing,
        "total_usd": round(all_known, 9) if not all_missing else None,
        "real_jev_requests": all_jev_requests,
        "llm_requests_by_purpose": dict(collections.Counter(r["purpose"] for r in report.get("requests", []))),
        "break_even": None,

        "library_origin": report.get("library_origin"),
        "compilation_accounting": "inherited library; compilation cost excluded from this warm run" if report.get("library_origin") else "current cold experiment",
        "conclusion": "replacement not established" if any(g["blocked"] or g["successes"] != g["executed"] for k, g in groups.items() if not k.endswith("cold_learning")) else "tested replacement gate passed",
    }
    amortization = {}
    for family in report["libraries"]:
        base = groups[f"{family}/ordinary_llm"]
        target = groups[f"{family}/reflex_jev"]
        finite = groups[f"{family}/reflex_llm_judge"]
        arms = [base, target, finite]
        if any(g["executed"] < 5 or g["blocked"] or g["successes"] != g["executed"] or g["missing_usage_or_tariff"] for g in arms):
            continue
        saving = base["runtime_known_usd"] / base["executed"] - target["runtime_known_usd"] / target["executed"]
        cold = groups[f"{family}/cold_learning"]
        if saving > 0 and not cold["missing_usage_or_tariff"] and not report.get("library_origin"):
            amortization[family] = {"runtime_saving_per_task_usd": round(saving, 9), "compilation_break_even_tasks": math.ceil(cold["compilation_known_usd"] / saving),
                                    "cold_training_including_execution_break_even_tasks": math.ceil((cold["compilation_known_usd"] + cold["runtime_known_usd"]) / saving)}
    summary["break_even"] = amortization or None
    # Breakeven is informative only when the paired arms ran successfully and
    # measured runtime saving is positive. Never amortize an unavailable source.
    (directory / "cost-analysis.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    lines = [f"# {directory.name}", "", "按实验记录的官方费率与返回用量估算，非账单。每轮独立统计，失败尝试保留。", "",
             "| 场景 / 组别 | 成功 / 实际运行 | 阻塞 | 编译 USD | 运行 USD | 全部尝试 / 成功任务 USD |", "|---|---:|---:|---:|---:|---:|"]
    if report.get("library_origin"):
        lines[4:4] = ["本轮复用已有自主生成的合格库，费用仅包含本轮运行；未计入来源实验的编译成本，不计算回本。", ""]
    for name, group in groups.items():
        unit = group["all_attempt_cost_per_success_usd"]
        build_display = f"{group['compilation_known_usd']:.6f}" if group["executed"] else "—"
        runtime_display = f"{group['runtime_known_usd']:.6f}" if group["executed"] else "—"
        lines.append(f"| {name} | {group['successes']}/{group['executed']} | {group['blocked']} | {build_display} | {runtime_display} | {unit if unit is not None else '无法确认'} |")
    lines += ["", f"已返回用量的费用：${all_known:.6f}；缺失用量/价格：{all_missing}。",
              "未完成替代验收，不计算节省率或回本次数。" if not amortization else "回本估算：" + json.dumps(amortization, ensure_ascii=False), ""]
    (directory / "cost-analysis.md").write_text("\n".join(lines), encoding="utf-8")
    return summary


if __name__ == "__main__":
    for arg in sys.argv[1:]:
        result = analyze(pathlib.Path(arg))
        print(json.dumps({k: v for k, v in result.items() if k != "groups"}, ensure_ascii=False))
