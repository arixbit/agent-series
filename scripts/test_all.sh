#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/.."

modules=(
  02-hello-llm/first-api-call
  02-hello-llm/token-count
  03-first-agent
  04-multi-tool
  05-session-memory
)

pass=0
fail=0

for mod in "${modules[@]}"; do
  echo -n "$mod: "
  if (cd "$mod" && go build ./... && go vet ./... && go test ./...); then
    echo "OK"
    ((pass++))
  else
    echo "FAIL"
    ((fail++))
  fi
done

echo "---"
echo "gofmt check:"
unformatted=$(gofmt -l . 2>&1)
if [ -n "$unformatted" ]; then
  echo "$unformatted"
  echo "gofmt: FAIL (run gofmt -w .)"
  ((fail++))
else
  echo "gofmt: OK"
fi

# 清理 build 产物
for mod in "${modules[@]}"; do
  bin=$(basename "$mod")
  test -f "$mod/$bin" && rm "$mod/$bin" || true
done

echo "---"
echo "passed: $pass, failed: $fail"
exit $fail
