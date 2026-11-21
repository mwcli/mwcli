#!/usr/bin/env bash
# Checks VERSION and prints whether it is already released.
# VERSION must be X.Y.Z (optionally -prerelease) and, unless its tag vX.Y.Z
# already exists, greater than every existing v* tag.
# Prints "released=true" or "released=false" for GitHub Actions outputs.
set -euo pipefail
cd "$(dirname "$0")/.."

version=$(tr -d '[:space:]' < VERSION)
if [[ ! $version =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "VERSION '$version' is not a semantic version (X.Y.Z)" >&2
  exit 1
fi
tag="v$version"
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  echo "released=true"
  exit 0
fi
latest=$(git tag -l 'v*' --sort=-v:refname | head -n1)
if [[ -n $latest ]]; then
  highest=$(printf '%s\n%s\n' "$latest" "$tag" | sort -V | tail -n1)
  if [[ $highest != "$tag" ]]; then
    echo "VERSION $version is not greater than the latest tag $latest" >&2
    exit 1
  fi
fi
echo "released=false"
