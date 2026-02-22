#!/usr/bin/env bash

set -euo pipefail

output_file="$(mktemp)"
seen_file="$(mktemp)"
trap 'rm -f "$output_file" "$seen_file"' EXIT

go test -run '^$' -bench '^BenchmarkPreflightBudget_' -benchmem -benchtime=1x -count=1 ./internal/engine | tee "$output_file"

get_budget_ns() {
  case "$1" in
    BenchmarkPreflightBudget_ParseRemote_1000) echo 5000000 ;;
    BenchmarkPreflightBudget_ParseRemote_5000) echo 15000000 ;;
    BenchmarkPreflightBudget_ParseRemote_10000) echo 35000000 ;;
    BenchmarkPreflightBudget_ParseState_1000) echo 6000000 ;;
    BenchmarkPreflightBudget_ParseState_5000) echo 25000000 ;;
    BenchmarkPreflightBudget_ParseState_10000) echo 60000000 ;;
    BenchmarkPreflightBudget_ParseArchive_1000) echo 6000000 ;;
    BenchmarkPreflightBudget_ParseArchive_5000) echo 20000000 ;;
    BenchmarkPreflightBudget_ParseArchive_10000) echo 40000000 ;;
    BenchmarkPreflightBudget_LocalScan_1000) echo 20000000 ;;
    BenchmarkPreflightBudget_LocalScan_5000) echo 80000000 ;;
    BenchmarkPreflightBudget_LocalScan_10000) echo 180000000 ;;
    BenchmarkPreflightBudget_FullPlan_1000) echo 30000000 ;;
    BenchmarkPreflightBudget_FullPlan_5000) echo 140000000 ;;
    BenchmarkPreflightBudget_FullPlan_10000) echo 300000000 ;;
    *) return 1 ;;
  esac
}

failed=0

while IFS= read -r line; do
  [[ "$line" =~ ^BenchmarkPreflightBudget_ ]] || continue

  raw_name="$(awk '{print $1}' <<<"$line")"
  name="${raw_name%-*}"
  ns_per_op="$(awk '{for (i=1; i<=NF; i++) if ($i=="ns/op") {print $(i-1); exit}}' <<<"$line")"

  if ! budget="$(get_budget_ns "$name")"; then
    continue
  fi

  if [[ -z "$ns_per_op" ]]; then
    echo "benchmark guardrail: unable to parse ns/op for ${name}" >&2
    failed=1
    continue
  fi

  echo "$name" >>"$seen_file"
  if (( ns_per_op > budget )); then
    echo "benchmark guardrail: ${name} exceeded budget (${ns_per_op} ns/op > ${budget} ns/op)" >&2
    failed=1
  else
    echo "benchmark guardrail: ${name} within budget (${ns_per_op} ns/op <= ${budget} ns/op)"
  fi
done <"$output_file"

while IFS=' ' read -r expected_name _; do
  [[ -z "$expected_name" ]] && continue
  if ! grep -qx "$expected_name" "$seen_file"; then
    echo "benchmark guardrail: missing benchmark result for ${expected_name}" >&2
    failed=1
  fi
done <<'EOF_EXPECTED'
BenchmarkPreflightBudget_ParseRemote_1000 5000000
BenchmarkPreflightBudget_ParseRemote_5000 15000000
BenchmarkPreflightBudget_ParseRemote_10000 35000000
BenchmarkPreflightBudget_ParseState_1000 6000000
BenchmarkPreflightBudget_ParseState_5000 25000000
BenchmarkPreflightBudget_ParseState_10000 60000000
BenchmarkPreflightBudget_ParseArchive_1000 6000000
BenchmarkPreflightBudget_ParseArchive_5000 20000000
BenchmarkPreflightBudget_ParseArchive_10000 40000000
BenchmarkPreflightBudget_LocalScan_1000 20000000
BenchmarkPreflightBudget_LocalScan_5000 80000000
BenchmarkPreflightBudget_LocalScan_10000 180000000
BenchmarkPreflightBudget_FullPlan_1000 30000000
BenchmarkPreflightBudget_FullPlan_5000 140000000
BenchmarkPreflightBudget_FullPlan_10000 300000000
EOF_EXPECTED

if (( failed != 0 )); then
  exit 1
fi
