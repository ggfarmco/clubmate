public enum CircledNumber {
    private static let first: UInt16 = 0x2460
    private static let last: UInt16 = 0x2473

    public static func value(_ unit: UInt16) -> Int? {
        (first...last).contains(unit) ? Int(unit - first) + 1 : nil
    }
}
