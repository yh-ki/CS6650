#!/usr/bin/env python3
"""
graph.py — generate all report graphs from load-test CSV output.

Usage:
    python3 graph.py                    # uses CSVs in current directory
    python3 graph.py --dir ./results    # specify directory

Produces one PNG per scenario per graph type:
    latency-{label}.png       — CDF of read and write latency
    intervals-{label}.png     — histogram of read-write intervals for same key

Requires: pip install matplotlib pandas numpy
"""

import argparse
import os
import sys
from pathlib import Path

import matplotlib
matplotlib.use("Agg")  # no display needed — write PNGs directly
import matplotlib.pyplot as plt
import matplotlib.ticker as ticker
import numpy as np
import pandas as pd

# ── style ──────────────────────────────────────────────────────────────────────
plt.rcParams.update({
    "figure.dpi": 150,
    "font.family": "sans-serif",
    "font.size": 11,
    "axes.spines.top": False,
    "axes.spines.right": False,
    "axes.grid": True,
    "grid.alpha": 0.3,
    "grid.linestyle": "--",
})

READ_COLOR  = "#378ADD"   # blue
WRITE_COLOR = "#D85A30"   # coral
INTERVAL_COLOR = "#1D9E75"  # teal

LABELS = {
    "w01-r99": "1% writes / 99% reads",
    "w10-r90": "10% writes / 90% reads",
    "w50-r50": "50% writes / 50% reads",
    "w90-r10": "90% writes / 10% reads",
}

# ── helpers ────────────────────────────────────────────────────────────────────

def cdf(series: pd.Series):
    """Return (sorted_values, cumulative_fraction) for a CDF plot."""
    s = series.dropna().sort_values()
    y = np.arange(1, len(s) + 1) / len(s)
    return s.values, y


def percentile_label(series: pd.Series, p: float) -> str:
    v = series.quantile(p)
    return f"p{int(p*100)}={v:.1f}ms"


# ── latency CDF ────────────────────────────────────────────────────────────────

def plot_latency_cdf(df: pd.DataFrame, label: str, out_dir: Path) -> None:
    """
    CDF of read and write latency on the same axes.
    A CDF shows the long tail clearly: if 95% of requests are below X ms
    the curve is flat after that point, making outliers obvious.
    """
    reads  = df[df["kind"] == "read" ]["latency_ms"]
    writes = df[df["kind"] == "write"]["latency_ms"]

    fig, ax = plt.subplots(figsize=(8, 5))
    title = LABELS.get(label, label)
    ax.set_title(f"Latency CDF — {title}", pad=12)

    if len(reads) > 0:
        x, y = cdf(reads)
        ax.plot(x, y * 100, color=READ_COLOR, linewidth=2, label="reads")
        # Annotate p50, p95, p99
        for p in [0.50, 0.95, 0.99]:
            xp = reads.quantile(p)
            ax.axvline(xp, color=READ_COLOR, linewidth=0.8, linestyle=":")
            ax.text(xp, p * 100 - 8, f" p{int(p*100)}", color=READ_COLOR,
                    fontsize=9, va="top")

    if len(writes) > 0:
        x, y = cdf(writes)
        ax.plot(x, y * 100, color=WRITE_COLOR, linewidth=2, label="writes")
        for p in [0.50, 0.95, 0.99]:
            xp = writes.quantile(p)
            ax.axvline(xp, color=WRITE_COLOR, linewidth=0.8, linestyle=":")
            ax.text(xp, p * 100 + 1, f" p{int(p*100)}", color=WRITE_COLOR,
                    fontsize=9, va="bottom")

    ax.set_xlabel("Latency (ms)")
    ax.set_ylabel("Cumulative % of requests")
    ax.set_ylim(0, 105)
    ax.yaxis.set_major_formatter(ticker.PercentFormatter())
    ax.legend(framealpha=0.7)

    # Stale-read annotation
    total_reads = len(reads)
    stale_reads = int(df[(df["kind"] == "read") & (df["is_stale"] == True)].shape[0])
    if total_reads > 0:
        pct = stale_reads / total_reads * 100
        ax.text(0.98, 0.05,
                f"Stale reads: {stale_reads}/{total_reads} ({pct:.1f}%)",
                transform=ax.transAxes, ha="right", va="bottom",
                fontsize=9, color="gray")

    fig.tight_layout()
    out = out_dir / f"latency-{label}.png"
    fig.savefig(out)
    plt.close(fig)
    print(f"  wrote {out}")


# ── read/write latency side-by-side histogram ──────────────────────────────────

def plot_latency_histograms(df: pd.DataFrame, label: str, out_dir: Path) -> None:
    """
    Side-by-side histograms of read and write latency.
    Complementary to the CDF — shows the shape of the distribution
    (unimodal? bimodal? heavy tail?).
    """
    reads  = df[df["kind"] == "read" ]["latency_ms"].dropna()
    writes = df[df["kind"] == "write"]["latency_ms"].dropna()

    fig, axes = plt.subplots(1, 2, figsize=(12, 4), sharey=False)
    title = LABELS.get(label, label)
    fig.suptitle(f"Latency distribution — {title}", y=1.02)

    for ax, data, color, kind in [
        (axes[0], reads,  READ_COLOR,  "reads"),
        (axes[1], writes, WRITE_COLOR, "writes"),
    ]:
        if len(data) == 0:
            ax.set_title(f"{kind} (no data)")
            continue
        ax.hist(data, bins=60, color=color, alpha=0.85, edgecolor="none")
        ax.set_title(f"{kind}  (n={len(data):,})")
        ax.set_xlabel("Latency (ms)")
        ax.set_ylabel("Count")
        # Mark p95 and p99
        for p, ls in [(0.95, "--"), (0.99, ":")]:
            xp = data.quantile(p)
            ax.axvline(xp, color="black", linewidth=1, linestyle=ls,
                       label=f"p{int(p*100)}={xp:.0f}ms")
        ax.legend(fontsize=9, framealpha=0.7)

    fig.tight_layout()
    out = out_dir / f"latency-hist-{label}.png"
    fig.savefig(out, bbox_inches="tight")
    plt.close(fig)
    print(f"  wrote {out}")


# ── read-write interval histogram ─────────────────────────────────────────────

def plot_intervals(interval_csv: Path, label: str, out_dir: Path) -> None:
    """
    Histogram of the time between a write to a key and the next read of the
    same key, as generated by the load tester.

    This graph answers: "how local-in-time are my reads and writes?"
    A tight distribution (most intervals < 1s) means the generator is
    successfully clustering reads and writes on the same key.
    The assignment asks you to discuss this — small key space = tight intervals.
    """
    if not interval_csv.exists():
        print(f"  [skip] {interval_csv} not found")
        return

    df = pd.read_csv(interval_csv)
    if df.empty or "interval_ms" not in df.columns:
        print(f"  [skip] {interval_csv} has no data")
        return

    intervals = df["interval_ms"].dropna()
    if len(intervals) == 0:
        return

    fig, ax = plt.subplots(figsize=(8, 4))
    title = LABELS.get(label, label)
    ax.set_title(f"Read-write interval (same key) — {title}", pad=10)

    ax.hist(intervals, bins=50, color=INTERVAL_COLOR, alpha=0.85, edgecolor="none")
    ax.set_xlabel("Time between write and subsequent read of same key (ms)")
    ax.set_ylabel("Count")

    p50 = intervals.quantile(0.50)
    p95 = intervals.quantile(0.95)
    for xp, ls, txt in [(p50, "--", f"p50={p50:.0f}ms"), (p95, ":", f"p95={p95:.0f}ms")]:
        ax.axvline(xp, color="black", linewidth=1, linestyle=ls, label=txt)

    ax.legend(fontsize=9, framealpha=0.7)
    ax.text(0.98, 0.95, f"n={len(intervals):,}",
            transform=ax.transAxes, ha="right", va="top", fontsize=9, color="gray")

    fig.tight_layout()
    out = out_dir / f"intervals-{label}.png"
    fig.savefig(out)
    plt.close(fig)
    print(f"  wrote {out}")


# ── stale-read summary bar chart ───────────────────────────────────────────────

def plot_stale_summary(all_results: dict, out_dir: Path) -> None:
    """
    Bar chart showing stale-read percentage across all four scenarios.
    Good for the report executive summary.
    """
    labels, pcts = [], []
    for label, df in sorted(all_results.items()):
        reads = df[df["kind"] == "read"]
        if len(reads) == 0:
            continue
        stale_pct = reads["is_stale"].mean() * 100
        labels.append(LABELS.get(label, label))
        pcts.append(stale_pct)

    if not labels:
        return

    fig, ax = plt.subplots(figsize=(9, 4))
    ax.set_title("Stale read rate by scenario", pad=10)
    bars = ax.bar(labels, pcts, color=READ_COLOR, alpha=0.85)
    ax.set_ylabel("Stale reads (%)")
    ax.set_ylim(0, max(pcts) * 1.3 + 1)

    for bar, pct in zip(bars, pcts):
        ax.text(bar.get_x() + bar.get_width() / 2, bar.get_height() + 0.2,
                f"{pct:.1f}%", ha="center", va="bottom", fontsize=10)

    plt.xticks(rotation=15, ha="right")
    fig.tight_layout()
    out = out_dir / "stale-summary.png"
    fig.savefig(out, bbox_inches="tight")
    plt.close(fig)
    print(f"  wrote {out}")


# ── main ───────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Generate load-test graphs")
    parser.add_argument("--dir", default=".", help="Directory containing CSV files")
    parser.add_argument("--out", default=None, help="Output directory (default: same as --dir)")
    args = parser.parse_args()

    src = Path(args.dir)
    out_dir = Path(args.out) if args.out else src
    out_dir.mkdir(parents=True, exist_ok=True)

    # Discover all result CSVs.
    result_files = sorted(src.glob("results-*.csv"))
    if not result_files:
        print(f"No results-*.csv files found in {src}")
        sys.exit(1)

    all_results = {}
    for f in result_files:
        # Extract label from filename: results-w01-r99.csv → w01-r99
        label = f.stem.removeprefix("results-")
        print(f"\nProcessing {f.name} (label={label})")

        df = pd.read_csv(f)
        # Normalise boolean column — CSV stores "true"/"false" strings.
        df["is_stale"] = df["is_stale"].map({"true": True, "false": False, True: True, False: False})
        all_results[label] = df

        plot_latency_cdf(df, label, out_dir)
        plot_latency_histograms(df, label, out_dir)

        interval_file = src / f"intervals-{label}.csv"
        plot_intervals(interval_file, label, out_dir)

    plot_stale_summary(all_results, out_dir)
    print(f"\nDone. All graphs written to {out_dir}/")


if __name__ == "__main__":
    main()
