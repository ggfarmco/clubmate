import Foundation

public struct RGB: Equatable, Sendable {
    public var red: Double
    public var green: Double
    public var blue: Double

    public init(red: Double, green: Double, blue: Double) {
        self.red = red
        self.green = green
        self.blue = blue
    }

    public init(hex: UInt32) {
        self.init(red: Double((hex >> 16) & 0xFF) / 255, green: Double((hex >> 8) & 0xFF) / 255, blue: Double(hex & 0xFF) / 255)
    }

    public var hex: UInt32 {
        func byte(_ value: Double) -> UInt32 { UInt32((min(max(value, 0), 1) * 255).rounded()) }
        return byte(red) << 16 | byte(green) << 8 | byte(blue)
    }

    public var luminance: Double {
        func linear(_ value: Double) -> Double {
            value <= 0.03928 ? value / 12.92 : pow((value + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * linear(red) + 0.7152 * linear(green) + 0.0722 * linear(blue)
    }

    public func mixed(with other: RGB, amount: Double) -> RGB {
        RGB(red: red + (other.red - red) * amount, green: green + (other.green - green) * amount, blue: blue + (other.blue - blue) * amount)
    }

    public func contrast(with other: RGB) -> Double {
        let (light, dark) = luminance > other.luminance ? (luminance, other.luminance) : (other.luminance, luminance)
        return (light + 0.05) / (dark + 0.05)
    }
}

public enum ThemeContrast {
    public static func alpha(of foreground: RGB, over background: RGB, reaching target: Double) -> Double {
        lowest { background.mixed(with: foreground, amount: $0).contrast(with: background) >= target }
    }

    public static func ink(_ tone: RGB, toward foreground: RGB, on background: RGB, reaching target: Double) -> RGB {
        tone.mixed(with: foreground, amount: lowest { tone.mixed(with: foreground, amount: $0).contrast(with: background) >= target })
    }

    public static func on(_ fill: RGB, candidates: [RGB], reaching target: Double) -> RGB {
        candidates.first { $0.contrast(with: fill) >= target } ?? candidates.max { $0.contrast(with: fill) < $1.contrast(with: fill) } ?? fill
    }

    private static func lowest(_ passes: (Double) -> Bool) -> Double {
        guard !passes(0) else { return 0 }
        guard passes(1) else { return 1 }
        var low = 0.0
        var high = 1.0
        for _ in 0..<20 {
            let middle = (low + high) / 2
            if passes(middle) { high = middle } else { low = middle }
        }
        return high
    }
}
