#!/usr/bin/env python3
"""Plot lab metrics.

Usage:
  python3 scripts/plot.py metrics/read-bug.csv
  python3 scripts/plot.py metrics/read-bug.csv metrics/read-fix.csv -o read.png
"""
import argparse
import csv
import os
import sys

import matplotlib.pyplot as plt

MB = 1024 * 1024
COLORS = ["#d64545", "#2f7ed8", "#3aa76d", "#8e5ad6"]


def load(path):
    rows = []
    with open(path, newline="") as f:
        for row in csv.DictReader(f):
            try:
                rows.append({
                    "ts": int(row["ts_unix_ms"]),
                    "goroutines": int(row["goroutines"]),
                    "heap": int(row["heap_alloc_bytes"]),
                    "rss": int(row["rss_bytes"]) if row["rss_bytes"] else None,
                    "cgroup": int(row["cgroup_mem_bytes"]) if row["cgroup_mem_bytes"] else None,
                    "conns": int(row["open_conns"]),
                })
            except (KeyError, ValueError, TypeError):
                # last line cut by the OOM kill, or a broken row: skip it
                continue

    if not rows:
        sys.exit(f"{path}: no valid rows")

    t0 = rows[0]["ts"]
    for r in rows:
        r["t"] = (r["ts"] - t0) / 1000.0

    return rows


def restarts(rows):
    # a restart is a new process: goroutine count drops hard between two samples
    out = []
    for prev, cur in zip(rows, rows[1:]):
        if prev["goroutines"] > 20 and cur["goroutines"] < prev["goroutines"] * 0.2:
            out.append(cur["t"])
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("csv", nargs="+")
    ap.add_argument("-o", "--out", default=None)
    ap.add_argument("--title", default=None)
    args = ap.parse_args()

    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(12, 7), sharex=True)

    for i, path in enumerate(args.csv):
        rows = load(path)
        name = os.path.splitext(os.path.basename(path))[0]
        color = COLORS[i % len(COLORS)]
        t = [r["t"] for r in rows]

        ax1.plot(t, [r["goroutines"] for r in rows], color=color, lw=2, label=name)

        mem_key = "cgroup" if rows[0]["cgroup"] is not None else "rss"
        ax2.plot(t, [(r[mem_key] or 0) / MB for r in rows], color=color, lw=2,
                 label=f"{name} {mem_key}")
        ax2.plot(t, [r["heap"] / MB for r in rows], color=color, lw=1, ls="--",
                 alpha=0.6, label=f"{name} heap")

        for x in restarts(rows):
            for ax in (ax1, ax2):
                ax.axvline(x, color=color, ls=":", lw=1.5)
            ax1.annotate("restart", (x, ax1.get_ylim()[1]), color=color,
                         fontsize=9, ha="left", va="top")

    ax1.set_ylabel("goroutines")
    ax2.set_ylabel("memory (MB)")
    ax2.set_xlabel("seconds")

    for ax in (ax1, ax2):
        ax.grid(alpha=0.25)
        ax.spines[["top", "right"]].set_visible(False)
        ax.legend(frameon=False, loc="upper left")

    fig.suptitle(args.title or " vs ".join(os.path.basename(p) for p in args.csv))
    fig.tight_layout()

    out = args.out or os.path.splitext(args.csv[0])[0] + ".png"
    fig.savefig(out, dpi=200)
    print(f"saved {out}")


if __name__ == "__main__":
    main()