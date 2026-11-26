import Foundation

public enum Handoff {
    public static func title(to: String, text: String) -> String {
        let line = text.split(whereSeparator: \.isNewline).lazy
            .map { $0.trimmingCharacters(in: .whitespaces).trimmingPrefix(while: { "#>*-".contains($0) }).trimmingCharacters(in: .whitespaces) }
            .first { !$0.isEmpty } ?? ""
        let head = "→ " + (to.isEmpty ? "next node" : to)
        return line.isEmpty ? head : head + " · " + line
    }
}
