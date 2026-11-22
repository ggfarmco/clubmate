#if DEBUG
import OSLog
import QuartzCore
import UIKit

@MainActor
final class FrameMonitor: NSObject {
    private static let log = Logger(subsystem: "com.clubmate.companion.perf", category: "frame")
    private static let signposter = OSSignposter(subsystem: "com.clubmate.companion.perf", category: "frame")
    private static var shared: FrameMonitor?
    private var link: CADisplayLink?
    private var last: CFTimeInterval = 0
    private var mark: (name: String, at: CFTimeInterval)?

    static func startIfAsked() {
        guard shared == nil, UserDefaults.standard.bool(forKey: "frameMonitor") else { return }
        let monitor = FrameMonitor()
        let link = CADisplayLink(target: monitor, selector: #selector(tick))
        link.add(to: .main, forMode: .common)
        monitor.link = link
        shared = monitor
    }

    static func mark(_ name: String) {
        shared?.mark = (name, CACurrentMediaTime())
    }

    @objc private func tick(_ link: CADisplayLink) {
        defer { last = link.timestamp }
        if let mark {
            let after = CACurrentMediaTime() - mark.at
            if after > 1 {
                self.mark = nil
            } else {
                Self.log.info("tick \(mark.name, privacy: .public) \(Int(after * 1000), privacy: .public) ms")
            }
        }
        let frame = link.targetTimestamp - link.timestamp
        let gap = link.timestamp - last
        guard last > 0, frame > 0, gap > frame * 1.5 else { return }
        let dropped = Int((gap / frame).rounded()) - 1
        Self.log.info("hitch \(Int(gap * 1000), privacy: .public) ms dropped \(dropped, privacy: .public)")
        Self.signposter.emitEvent("hitch", "\(dropped) dropped")
    }
}
#endif
