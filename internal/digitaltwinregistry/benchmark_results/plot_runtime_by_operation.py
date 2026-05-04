#!/usr/bin/env python3
import argparse
import json
from collections import defaultdict
from pathlib import Path

import matplotlib.pyplot as plt


def load_records(path: Path) -> list[dict]:
    if path.suffix.lower() == ".jsonl":
        records: list[dict] = []
        with path.open("r", encoding="utf-8") as file_handle:
            for line in file_handle:
                line = line.strip()
                if not line:
                    continue
                data = json.loads(line)
                if isinstance(data, dict):
                    records.append(data)
        return records

    with path.open("r", encoding="utf-8") as file_handle:
        payload = json.load(file_handle)
    if isinstance(payload, list):
        return [entry for entry in payload if isinstance(entry, dict)]
    if isinstance(payload, dict) and isinstance(payload.get("records"), list):
        return [entry for entry in payload["records"] if isinstance(entry, dict)]
    raise ValueError("Unsupported input format. Provide JSONL or JSON array.")


def build_operation_totals(records: list[dict]) -> dict[str, float]:
    totals: dict[str, float] = defaultdict(float)
    for record in records:
        operation = record.get("operation")
        duration_ms = record.get("duration_ms")
        if not isinstance(operation, str):
            continue
        try:
            duration = float(duration_ms)
        except (TypeError, ValueError):
            continue
        if duration < 0:
            continue
        totals[operation] += duration
    return dict(totals)


def choose_operations(records: list[dict], top: int) -> list[str]:
    totals = build_operation_totals(records)
    if not totals:
        return []
    ranked = [name for name, _ in sorted(totals.items(), key=lambda pair: pair[1], reverse=True)]
    if top > 0:
        ranked = ranked[:top]
    return ranked


def build_cumulative_series(
    records: list[dict], operations: list[str]
) -> tuple[list[int], dict[str, list[float]]]:
    operation_set = set(operations)
    cumulative: dict[str, float] = {op: 0.0 for op in operations}
    series: dict[str, list[float]] = {op: [0.0] for op in operations}
    x_indices: list[int] = [0]

    for request_index, record in enumerate(records, start=1):
        operation = record.get("operation")
        duration_ms = record.get("duration_ms")
        if isinstance(operation, str) and operation in operation_set:
            try:
                duration = float(duration_ms)
            except (TypeError, ValueError):
                duration = 0.0
            if duration > 0:
                cumulative[operation] += duration

        x_indices.append(request_index)
        for op in operations:
            series[op].append(cumulative[op])

    return x_indices, series


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Plot cumulative runtime per operation over request index."
    )
    parser.add_argument(
        "input",
        type=Path,
        help="Path to runtime_results_dtr.jsonl (or compatible JSON)",
    )
    parser.add_argument(
        "-o",
        "--output",
        type=Path,
        default=Path("runtime_by_operation.png"),
        help="Output image path",
    )
    parser.add_argument(
        "--top",
        type=int,
        default=0,
        help="Only plot top N operations by cumulative runtime (0 = all operations)",
    )
    parser.add_argument(
        "--log-y",
        action="store_true",
        help="Use logarithmic scale on y-axis",
    )
    args = parser.parse_args()

    records = load_records(args.input)
    operations = choose_operations(records, args.top)
    if not operations:
        raise SystemExit("No operation runtime data found in input.")
    x_indices, series = build_cumulative_series(records, operations)

    plt.figure(figsize=(15, 8))
    for op in operations:
        plt.plot(x_indices, series[op], linewidth=1.4, label=op)

    plt.xlabel("Request Index")
    plt.ylabel("Cumulative Runtime (ms)")
    plt.title("Cumulative Runtime per Operation by Request Index")
    plt.grid(axis="y", linestyle="--", linewidth=0.5, alpha=0.5)
    plt.xlim(0, len(records))
    if args.log_y:
        plt.yscale("log")
    plt.legend(loc="upper left", bbox_to_anchor=(1.01, 1.0), fontsize=8)

    plt.tight_layout()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    plt.savefig(args.output, dpi=150)
    print(f"Saved plot: {args.output}")
    print(f"Plotted requests: {len(records)}")
    print(f"Plotted operations: {len(operations)}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
