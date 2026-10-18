#!/usr/bin/env bash
# CHANGELOG.md helper for releases (used by .github/workflows/release.yml; runs in Git Bash too).
#
#   tools/changelog.sh notes <version> [file]            print the "## [<version>]" section (without its heading)
#   tools/changelog.sh promote <version> [date] [file]   turn "## [Unreleased]" into "## [<version>] - <date>" and
#                                                        start a new, empty "## [Unreleased]" above it
#
# promote fails if the version already has a section, or if Unreleased is missing or empty. The date defaults to
# today (UTC), and the file to CHANGELOG.md.
set -euo pipefail

section() { # print the body of the section whose heading starts with "## [$1]"
  awk -v v="$1" '
    index($0, "## [" v "]") == 1 { found = 1; next }
    found && /^## \[/ { exit }
    found { print }
  ' "$2"
}

has_text() { [[ "$1" =~ [^[:space:]] ]]; } # (a test, not grep -q: under pipefail an early-exiting grep can fail the pipe)

command="${1:-}"
version="${2:-}"
if [ -z "$command" ] || [ -z "$version" ]; then
  echo "usage: $0 notes <version> [file] | promote <version> [date] [file]" >&2
  exit 2
fi

case "$command" in
  notes)
    file="${3:-CHANGELOG.md}"
    section "$version" "$file"
    ;;
  promote)
    date="${3:-$(date -u +%Y-%m-%d)}"
    file="${4:-CHANGELOG.md}"
    if grep -q "^## \[$version\]" "$file"; then
      echo "$file already has a \"## [$version]\" section" >&2
      exit 1
    fi
    if ! grep -q '^## \[Unreleased\]' "$file"; then
      echo "$file has no \"## [Unreleased]\" section to release as $version" >&2
      exit 1
    fi
    if ! has_text "$(section "Unreleased" "$file")"; then
      echo "the \"## [Unreleased]\" section of $file is empty: nothing to release as $version" >&2
      exit 1
    fi
    temp="$(mktemp)"
    awk -v v="$version" -v d="$date" '
      !done && $0 ~ /^## \[Unreleased\]/ { print "## [Unreleased]"; print ""; print "## [" v "] - " d; done = 1; next }
      { print }
    ' "$file" > "$temp"
    # Keep the file's line endings and permissions: write over it rather than moving the temp file in.
    cat "$temp" > "$file"
    rm -f "$temp"
    ;;
  *)
    echo "unknown command: $command" >&2
    exit 2
    ;;
esac
