import ClubmateProtocol

public extension SDUINode {
    static let paneFocusTypes: Set<String> = ["table", "graph", "flow", "lanes", "runstrip"]

    var paneFocusOwner: String? {
        if Self.paneFocusTypes.contains(type), let id, !ghostOnly { return id }
        for child in children {
            if let owner = child.paneFocusOwner { return owner }
        }
        return nil
    }

    private var ghostOnly: Bool {
        guard type == "flow", let cards = flow()?.cards, !cards.isEmpty else { return false }
        return cards.allSatisfy(\.ghost)
    }
}
