"""Aggregate explicitly selected independent reports without pooling reruns.

Usage: python mechanism_report.py REPORT_DIR [REPORT_DIR ...] --out FILE
Pass a directory containing draft-summary.json to include recorded draft audits.
"""
import argparse
import json
from pathlib import Path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("reports", nargs="+")
    parser.add_argument("--out", required=True)
    args = parser.parse_args()
    groups, stages, hashes, drafts = {}, {}, {}, []
    unchanged = True
    for directory in args.reports:
        root = Path(directory).resolve()
        normal = root / "summary.json"
        if normal.exists():
            report = json.loads(normal.read_text(encoding="utf-8"))
            hashes[str(root)] = report.get("production_sha256", {})
            unchanged = unchanged and report.get("production_unchanged") is True
            stages[str(root)] = report.get("stages", {})
            local = {}
            for row in report["rows"]:
                key = row["experiment"] + "/" + row["condition"]
                group = local.setdefault(key, {"passed": 0, "total": 0, "failures": []})
                group["total"] += 1
                group["passed"] += int(row["accepted"])
                if not row["accepted"]:
                    group["failures"].append({"seed": row["seed"], "error": row.get("error", ""), "evidence": row.get("evidence", ""), "observed": row["observed"]})
            # The caller selects the primary run; duplicate conditions are an
            # error rather than an accidental larger statistical sample.
            for key, group in local.items():
                if key in groups:
                    raise ValueError("duplicate condition: " + key)
                group["report"] = str(normal)
                groups[key] = group
        audit = root / "draft-summary.json"
        if audit.exists():
            report = json.loads(audit.read_text(encoding="utf-8"))
            hashes[str(audit)] = report.get("production_sha256", {})
            drafts.append({"report": str(audit), "input": report["input"], "drafts": len(report["rows"]), "passed": sum(row.get("business_pass") is True for row in report["rows"]), "syntax_errors": sum("error" in row for row in report["rows"]), "rows": report["rows"]})
    if not groups and not drafts:
        raise ValueError("no experiment reports")
    production_consistent = bool(hashes) and all(hashes.values()) and all(value == next(iter(hashes.values())) for value in hashes.values())
    out = {"groups": groups, "stages": stages, "draft_audits": drafts, "production_consistent": production_consistent,
           "production_unchanged": unchanged,
           "accepted": bool(groups) and production_consistent and unchanged and all(g["passed"] == g["total"] for g in groups.values()) and all(d["passed"] == d["drafts"] for d in drafts)}
    target = Path(args.out)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(out, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"conditions": len(groups), "acceptance": out["accepted"], "draft_audits": len(drafts), "output": str(target.resolve())}, ensure_ascii=False))


if __name__ == "__main__":
    main()
