import CoreGraphics
import Foundation

let owner = CommandLine.arguments.dropFirst().first ?? "Clubmate Dev"
let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
for window in windows where window[kCGWindowOwnerName as String] as? String == owner && window[kCGWindowLayer as String] as? Int == 0 {
    print(window[kCGWindowNumber as String] as? Int ?? 0)
    exit(0)
}
exit(1)
