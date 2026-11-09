import SwiftUI
import UIKit

extension Color {
    static let onAccent = Color(light: 0xFFFFFF, dark: 0x0E1A08)
    static let success = Color(light: 0x1F7F70, dark: 0x4FB3A0)
    static let successInk = Color(light: 0x1A6E61, dark: 0x5CC2AE)
    static let warning = Color(light: 0xB5761E, dark: 0xF2B84B)
    static let warningInk = Color(light: 0x8A5810, dark: 0xF2B84B)
    static let onWarning = Color(light: 0x1A1300, dark: 0x1A1300)
    static let danger = Color(light: 0xC93A30, dark: 0xFF6B5E)
    static let dangerInk = Color(light: 0xB23A33, dark: 0xFF7A6E)
    static let surface1 = Color(light: 0xFFFFFF, dark: 0x1C1C1E)
    static let surface2 = Color(light: 0xE9E9EE, dark: 0x2C2C2E)
    static let selectedRow = Color(light: 0xE3ECE1, dark: 0x37412A)
    static let captionInk = Color(light: 0x636366, dark: 0xA8A8AD)

    init(light: UInt32, dark: UInt32) {
        self.init(uiColor: UIColor { $0.userInterfaceStyle == .dark ? UIColor(hex: dark) : UIColor(hex: light) })
    }
}

private extension UIColor {
    convenience init(hex: UInt32) {
        self.init(red: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255, blue: CGFloat(hex & 0xFF) / 255, alpha: 1)
    }
}
