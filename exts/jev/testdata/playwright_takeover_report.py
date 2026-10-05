"""Summarize all completed samples without censoring fallback or failed runs."""
import argparse
import collections
import json
import sys
from pathlib import Path


def summarize(root):
    scenarios, rows, rejections = [], [], []
    for path in sorted(root.glob("*/report.json")):
        report = json.loads(path.read_text(encoding="utf-8"))
        kind = report["kind"]
        current = report["rows"]
        enriched = []
        for row in current:
            # Positive tool evidence, not a model's claim to have used a library.
            calls = "\n".join(row.get("main_tool_calls") or [])
            dependency = any(marker in calls for marker in ("from playwright", "import playwright", "playwright.sync_api", "require('playwright", 'require(\\\"playwright', "npx playwright", "sync_playwright("))
            enriched.append(dict(kind=kind, **row, official_playwright_in_main_calls=dependency,
                                 native_only_business=row["correct"] and not dependency))
        rows.extend(enriched)
        scenarios.append(dict(kind=kind, samples=len(current),
                              off_business=sum(r["correct"] for r in current if r["mode"] == "off"),
                              auto_business=sum(r["correct"] for r in current if r["mode"] == "auto"),
                              auto_warm_full=sum(r["full_takeover"] for r in current if r["mode"] == "auto" and r["warm"]),
                              max_published_reflexes=max((r["published_reflexes"] for r in current), default=0)))
        decisions = path.parent / "auto" / "decisions.jsonl"
        if decisions.exists():
            for line in decisions.read_text(encoding="utf-8").splitlines():
                entry = json.loads(line)
                if entry["kind"] in ("compile_invalid", "declaration_failed"):
                    rejections.append(dict(kind=kind, stage=entry["kind"], reason=entry["data"]))
    totals = {}
    for mode in ("off", "auto"):
        selected = [r for r in rows if r["mode"] == mode]
        tally = dict(samples=len(selected), business_correct=sum(r["correct"] for r in selected),
                     official_playwright_fallback_samples=sum(r["official_playwright_in_main_calls"] for r in selected),
                     native_only_business_correct=sum(r["native_only_business"] for r in selected),
                     warm_samples=sum(r["warm"] for r in selected),
                     warm_full_takeover=sum(r["full_takeover"] for r in selected if r["warm"]),
                     actual_jev_dispatches=sum(r["jev_dispatches"] for r in selected),
                     foreground_ms=sum(r["foreground_ms"] for r in selected),
                     # Parallel scenario wall time is not the sum of durations.
                     settled_task_ms=sum(r["settled_ms"] for r in selected))
        for key in ("llm_usage", "main_usage", "claim_usage", "compile_usage", "jev_usage"):
            usage = collections.Counter()
            for r in selected:
                value = r.get(key) or {}
                for field in ("input_tokens", "output_tokens", "total_tokens"):
                    usage[field] += value.get(field, 0)
                for field in ("requests", "usage_missing"):
                    usage[field] += (value.get("detail") or {}).get(field, 0)
            tally[key] = dict(usage)
        tally["all_provider_tokens"] = tally["llm_usage"]["total_tokens"] + tally["jev_usage"]["total_tokens"]
        tally["usage_complete"] = tally["llm_usage"]["usage_missing"] == 0 and tally["jev_usage"]["usage_missing"] == 0
        totals[mode] = tally
    return dict(root=str(root.resolve()), scenarios=scenarios, totals=totals,
                rejection_counts=dict(collections.Counter((r["kind"] + ":" + r["stage"]) for r in rejections)),
                rejections=rejections,
                incomplete=[s["kind"] for s in scenarios if s["samples"] < 4],
                probe_excluded_from_task_usage=True, monetary_cost_known=False,
                dependency_classification=[dict(kind=r["kind"], mode=r["mode"], index=r["index"], business_correct=r["correct"], official_playwright_in_main_calls=r["official_playwright_in_main_calls"], native_only_business=r["native_only_business"]) for r in rows])


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    parser = argparse.ArgumentParser()
    parser.add_argument("root", type=Path)
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    report = summarize(args.root)
    if args.out:
        args.out.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({k: report[k] for k in ("scenarios", "totals", "incomplete")}, ensure_ascii=False, indent=2))
