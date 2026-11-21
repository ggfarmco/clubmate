import SwiftUI
import UIKit

enum Motion {
    static let navigationSettle: Duration = {
        #if DEBUG
        let asked = UserDefaults.standard.double(forKey: "menuSettle")
        if asked > 0 { return .milliseconds(Int(asked * 1000)) }
        #endif
        return .milliseconds(600)
    }()

    static var disclosure: Animation? {
        UIAccessibility.isReduceMotionEnabled ? nil : .snappy(duration: 0.2)
    }

    @MainActor static func disclose(_ change: () -> Void) {
        withAnimation(disclosure, change)
    }
}
