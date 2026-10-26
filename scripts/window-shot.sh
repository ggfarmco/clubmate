#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <process-name> <out.png>" >&2
  exit 2
fi

ID="$(swift - "$1" <<'SWIFT'
import CoreGraphics
import Foundation

let owner = CommandLine.arguments[1]
let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
let match = windows.first { window in
    window[kCGWindowOwnerName as String] as? String == owner && window[kCGWindowLayer as String] as? Int == 0
}
guard let id = match?[kCGWindowNumber as String] as? Int else {
    FileHandle.standardError.write(Data("no on-screen window for \(owner)\n".utf8))
    exit(1)
}
print(id)
SWIFT
)"

mkdir -p "$(dirname "$2")"
screencapture -x -o -l "$ID" "$2"
