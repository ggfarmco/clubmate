import SwiftUI
import UIKit

enum Motion {
    static var disclosure: Animation? {
        UIAccessibility.isReduceMotionEnabled ? nil : .snappy(duration: 0.2)
    }

    @MainActor static func disclose(_ change: () -> Void) {
        withAnimation(disclosure, change)
    }
}
