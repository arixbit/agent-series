#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/.."

modules=(
  02-hello-llm/first-api-call
  02-hello-llm/token-count
  03-first-agent
  04-multi-tool
  05-session-memory
  06-rag
)

build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT

pass=0
fail=0

for mod in "${modules[@]}"; do
  echo -n "$mod: "
  bin_name=${mod//\//_}
  if (cd "$mod" && go build -o "$build_dir/$bin_name" ./... && go vet ./... && go test ./...); then
    echo "OK"
    ((pass++))
  else
    echo "FAIL"
    ((fail++))
  fi
done

echo "---"
echo "gofmt check:"
unformatted=$(gofmt -l "${modules[@]}" 2>&1)
if [ -n "$unformatted" ]; then
  echo "$unformatted"
  echo "gofmt: FAIL (run gofmt -w .)"
  ((fail++))
else
  echo "gofmt: OK"
fi

echo "---"
echo "passed: $pass, failed: $fail"
exit $fail
