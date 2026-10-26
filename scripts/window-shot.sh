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

func largestWindow() -> Int? {
    let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
    let candidates = windows.filter { window in
        window[kCGWindowOwnerName as String] as? String == owner && window[kCGWindowLayer as String] as? Int == 0
    }
    let largest = candidates.max { area($0) < area($1) }
    return largest?[kCGWindowNumber as String] as? Int
}

func area(_ window: [String: Any]) -> Double {
    let bounds = window[kCGWindowBounds as String] as? [String: Double] ?? [:]
    return (bounds["Width"] ?? 0) * (bounds["Height"] ?? 0)
}

var found = largestWindow()
for _ in 0..<10 where found == nil {
    Thread.sleep(forTimeInterval: 0.5)
    found = largestWindow()
}
guard let id = found else {
    FileHandle.standardError.write(Data("no on-screen window for \(owner)\n".utf8))
    exit(1)
}
print(id)
SWIFT
)"

mkdir -p "$(dirname "$2")"
screencapture -x -o -l "$ID" "$2"
