#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["scikit-learn>=1.3", "numpy>=1.24"]
# ///
"""Train the ShadowTrace anomaly model (Python side of the hybrid design).

Run standalone with uv (auto-installs deps via the PEP 723 header above):
    uv run tools/train.py --events ~/.shadowtrace_events.jsonl \
                          --model ~/.shadowtrace_anomaly.json


Fits a scikit-learn IsolationForest over the forensic event log and exports it to
a plain JSON model that the Go binary loads for inference (`shadowtrace anomaly
score`). Keep the feature vector in lockstep with internal/anomaly/forest.go.

    python3 tools/train.py --events ~/.shadowtrace_events.jsonl \
                           --model ~/.shadowtrace_anomaly.json

Requires: scikit-learn, numpy  (project 'ml' extra: uv sync --extra ml)
"""

from __future__ import annotations

import argparse
import json
import math
import sys
from datetime import datetime, timezone

FEATURE_NAMES = ["hour_sin", "hour_cos", "dow_sin", "dow_cos", "rssi", "known"]
MIN_EVENTS = 50


def parse_ts(ts: str):
    try:
        dt = datetime.fromisoformat(ts)
    except (ValueError, TypeError):
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.astimezone()  # local time, matches the Go side


def features(dt, rssi: float, known: bool):
    hour = dt.hour + dt.minute / 60.0
    dow = dt.weekday()  # Monday=0, matches Go's (weekday+6)%7
    return [
        math.sin(2 * math.pi * hour / 24.0),
        math.cos(2 * math.pi * hour / 24.0),
        math.sin(2 * math.pi * dow / 7.0),
        math.cos(2 * math.pi * dow / 7.0),
        float(rssi),
        1.0 if known else 0.0,
    ]


def load_rows(path: str):
    rows = []
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                ev = json.loads(line)
            except json.JSONDecodeError:
                continue
            if ev.get("event") != "appear" or ev.get("rssi") is None:
                continue
            dt = parse_ts(ev.get("ts", ""))
            if dt is None:
                continue
            rows.append(features(dt, ev["rssi"], bool(ev.get("known"))))
    return rows


def export_tree(tree):
    t = tree.tree_
    return {
        "feature": [int(x) for x in t.feature],
        "threshold": [float(x) for x in t.threshold],
        "left": [int(x) for x in t.children_left],
        "right": [int(x) for x in t.children_right],
        "n_node_samples": [int(x) for x in t.n_node_samples],
    }


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--events", required=True)
    ap.add_argument("--model", required=True)
    ap.add_argument("--min-events", type=int, default=MIN_EVENTS)
    args = ap.parse_args()

    try:
        import numpy as np
        from sklearn.ensemble import IsolationForest
        from sklearn.preprocessing import StandardScaler
    except ImportError:
        sys.exit("Missing ML deps. Install with: uv sync --extra ml")

    rows = load_rows(args.events)
    if len(rows) < args.min_events:
        sys.exit(
            f"Only {len(rows)} usable events (need >= {args.min_events}). "
            "Let the service run longer, then retrain."
        )

    X = np.array(rows, dtype=float)
    scaler = StandardScaler().fit(X)
    Xs = scaler.transform(X)
    forest = IsolationForest(contamination="auto", random_state=0).fit(Xs)

    model = {
        "feature_names": FEATURE_NAMES,
        "mean": [float(x) for x in scaler.mean_],
        "scale": [float(x) for x in scaler.scale_],
        "max_samples": int(forest.max_samples_),
        "offset": float(forest.offset_),
        "trees": [export_tree(est) for est in forest.estimators_],
        "n_train": len(rows),
        "trained_at": datetime.now(timezone.utc).isoformat(),
    }
    with open(args.model, "w", encoding="utf-8") as f:
        json.dump(model, f)
    print(f"Trained on {len(rows)} events -> {args.model} "
          f"({len(model['trees'])} trees)")


if __name__ == "__main__":
    main()
