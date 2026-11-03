import CoreGraphics

public enum RowFit {
    public static func widths(available: CGFloat, label: CGFloat, detail: CGFloat?, gap: CGFloat, keptLabel: CGFloat, keptDetail: CGFloat) -> (label: CGFloat, detail: CGFloat?) {
        let available = max(available, 0)
        guard let detail else { return (min(label, available), nil) }
        if label + gap + detail <= available { return (label, detail) }
        let labelMin = min(label, keptLabel)
        guard labelMin + gap + min(detail, keptDetail) <= available else { return (min(label, available), nil) }
        let shown = min(detail, available - gap - labelMin)
        return (available - gap - shown, shown)
    }
}
