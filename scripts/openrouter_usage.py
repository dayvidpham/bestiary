#!/usr/bin/env python3
"""Capture OpenRouter rankings-daily and compute creator/family concentration sets.

Usage:
  openrouter_usage.py capture --days 30 --cache DIR
  openrouter_usage.py analyze --cache DIR --bestiary BIN [--threshold 0.90] [--csv OUT.csv]

Data: OpenRouter Data API, CC BY 4.0.
Attribution: "Source: OpenRouter (openrouter.ai/rankings), as of {date}."
The API key comes from OPENROUTER_API_KEY or ~/.config/openrouter/key.
It is never written to any output file.
"""
import argparse, json, os, pathlib, subprocess, sys, time, urllib.request, urllib.error
from datetime import date, timedelta

API = "https://openrouter.ai/api/v1/datasets/rankings-daily"

def api_key():
    k = os.environ.get("OPENROUTER_API_KEY", "").strip()
    if not k:
        p = pathlib.Path.home() / ".config/openrouter/key"
        if p.exists():
            k = p.read_text().strip()
    if not k:
        sys.exit("no API key: set OPENROUTER_API_KEY or write ~/.config/openrouter/key")
    return k

def capture(days: int, cache: pathlib.Path) -> None:
    cache.mkdir(parents=True, exist_ok=True)
    end = date.today() - timedelta(days=1)          # last complete day
    start = end - timedelta(days=days - 1)
    url = f"{API}?start_date={start.isoformat()}&end_date={end.isoformat()}"
    req = urllib.request.Request(url, headers={"Authorization": f"Bearer {api_key()}"})
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=120) as r:
                payload = json.load(r)
            break
        except urllib.error.HTTPError as e:
            if e.code == 429 and attempt < 2:
                wait = int(e.headers.get("Retry-After", "5"))
                time.sleep(max(wait, 5)); continue
            raise
    out = cache / f"rankings-daily_{start.isoformat()}_{end.isoformat()}.json"
    out.write_text(json.dumps(payload, indent=1, sort_keys=True))
    rows = payload.get("data", payload if isinstance(payload, list) else [])
    print(f"captured {len(rows)} rows -> {out}")
    meta = payload.get("meta", {})
    if meta: print(f"meta.as_of: {meta.get('as_of','?')}")

def load_rows(cache: pathlib.Path):
    rows = []
    for f in sorted(cache.glob("rankings-daily_*.json")):
        payload = json.loads(f.read_text())
        rows += payload.get("data", payload if isinstance(payload, list) else [])
    return rows

def resolve(slug: str, bin_: str, memo: dict):
    """permaslug -> (creator, family) via the production parser (bestiary CLI)."""
    import re as _re
    base = slug.split(":", 1)[0]                     # strip :free-style variants
    base = _re.sub(r"-20\d{6}$", "", base)           # strip the -YYYYMMDD snapshot date
    if base in memo: return memo[base]
    orgless = base.split("/", 1)[1] if "/" in base else base
    got = None
    for ref in (f"openrouter/{orgless}", base, orgless):
        r = subprocess.run([bin_, "show", ref, "--output", "json"],
                           capture_output=True, text=True)
        if r.returncode != 0: continue
        try:
            m = json.loads(r.stdout)
        except json.JSONDecodeError:
            continue
        got = (m.get("Creator") or "(no-creator)", m.get("Family") or "(no-family)")
        break
    memo[base] = got
    return got

def analyze(cache: pathlib.Path, bin_: str, threshold: float, csv_out):
    rows = load_rows(cache)
    if not rows: sys.exit(f"no cached rows under {cache}; run capture first")
    days = sorted({r["date"] for r in rows})
    total = other = 0
    per_slug = {}
    for r in rows:
        t = int(r["total_tokens"]); total += t
        slug = r["model_permaslug"]
        if slug == "other": other += t
        else: per_slug[slug] = per_slug.get(slug, 0) + t
    memo = {}
    per_creator, per_family, unmatched = {}, {}, {}
    for slug, t in per_slug.items():
        got = resolve(slug, bin_, memo)
        if got is None:
            unmatched[slug] = t; continue
        c, f = got
        per_creator[c] = per_creator.get(c, 0) + t
        per_family[f] = per_family.get(f, 0) + t
    def conc(d):
        out, run = [], 0
        for k, v in sorted(d.items(), key=lambda kv: -kv[1]):
            run += v; out.append((k, v, run / total))
            if run / total >= threshold: break
        return out
    if csv_out:
        with open(csv_out, "w") as fh:
            fh.write("date,model_permaslug,total_tokens\n")
            for r in sorted(rows, key=lambda r: (r["date"], -int(r["total_tokens"]))):
                fh.write(f'{r["date"]},{r["model_permaslug"]},{r["total_tokens"]}\n')
    print(f"window: {days[0]}..{days[-1]} ({len(days)} days); total tokens {total:,}")
    print(f"truncation bound (the 'other' row): {other:,} = {other/total:.1%} of all tokens")
    um = sum(unmatched.values())
    print(f"unmatched permaslugs: {len(unmatched)} slugs, {um:,} tokens = {um/total:.1%}")
    for name, d in (("CREATOR", per_creator), ("FAMILY", per_family)):
        cs = conc(d)
        print(f"\n{name} concentration set to >= {threshold:.0%} ({len(cs)} entries):")
        for k, v, cum in cs:
            print(f"  {k:<24} {v:>18,}  {v/total:6.1%}  cum {cum:6.1%}")
    if unmatched:
        top = sorted(unmatched.items(), key=lambda kv: -kv[1])[:15]
        print("\ntop unmatched permaslugs:")
        for s, v in top: print(f"  {s:<48} {v:>16,} {v/total:6.1%}")
    print(f'\nSource: OpenRouter (openrouter.ai/rankings), as of {days[-1]}.')

if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    c = sub.add_parser("capture"); c.add_argument("--days", type=int, default=30); c.add_argument("--cache", default=".openrouter-cache")
    a = sub.add_parser("analyze"); a.add_argument("--cache", default=".openrouter-cache"); a.add_argument("--bestiary", required=True); a.add_argument("--threshold", type=float, default=0.90); a.add_argument("--csv")
    ns = ap.parse_args()
    if ns.cmd == "capture": capture(ns.days, pathlib.Path(ns.cache))
    else: analyze(pathlib.Path(ns.cache), ns.bestiary, ns.threshold, ns.csv)
