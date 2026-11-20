import SwiftUI
import UIKit

enum PadLayout {
    static let measure: CGFloat = 680
    static let margin: CGFloat = 20

    static func on(_ sizeClass: UserInterfaceSizeClass?) -> Bool {
        UIDevice.current.userInterfaceIdiom == .pad && sizeClass == .regular
    }
}
