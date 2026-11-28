import CoreGraphics

public enum RowFit {
    public static func widths(available: CGFloat, label: CGFloat, detail: CGFloat?, gap: CGFloat, keptLabel: CGFloat, keptDetail: CGFloat) -> (label: CGFloat, detail: CGFloat?) {
        let available = max(available, 0)
        guard let detail else { return (min(label, available), nil) }
        if label + gap + detail <= available { return (label, detail) }
        if detail <= keptDetail || min(label, keptLabel) + gap + detail <= available {
            guard gap + detail < available else { return (min(label, available), nil) }
            return (available - gap - detail, detail)
        }
        let labelMin = min(label, keptLabel)
        guard labelMin + gap + keptDetail <= available else { return (min(label, available), nil) }
        return (labelMin, available - gap - labelMin)
    }

    public static func badgesFit(available: CGFloat, label: CGFloat, badges: CGFloat, detail: CGFloat?, gap: CGFloat, keptLabel: CGFloat, keptDetail: CGFloat) -> Bool {
        guard let detail else { return true }
        let fit = widths(available: available - badges, label: label, detail: detail, gap: gap, keptLabel: keptLabel, keptDetail: keptDetail)
        return (fit.detail ?? 0) + 0.5 >= detail && fit.label >= min(label, keptLabel)
    }
}
