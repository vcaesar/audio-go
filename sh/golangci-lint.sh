#! /bin/sh

cd "$(dirname "$0")/.." || exit 1

export CGO_ENABLED=0

rc=0
for goos in darwin linux windows; do
    echo "golangci-lint GOOS=$goos"
    GOOS=$goos GOARCH=amd64 golangci-lint run ./... || rc=$?
done

if [ $rc -ne 0 ]; then
  echo "golangci-lint failed" >&2
  exit $rc
fi
