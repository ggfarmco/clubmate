import Foundation

public enum SDUITruncate: String, Sendable {
    case tail
    case middle
    case head
}

public struct SDUIItem: Sendable, Equatable, Identifiable {
    public let id: String
    public let label: String
    public let detail: String?
    public let icon: String?
    public let badge: String?
    public let tone: String?
    public let menu: [SDUIMenuEntry]
    public let truncate: SDUITruncate
    public let shortcut: String?
    public let caption: String?
    public let section: Bool

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.id = id
        self.label = label
        detail = fields["detail"]?.stringValue
        icon = fields["icon"]?.stringValue
        badge = fields["badge"]?.stringValue
        tone = fields["tone"]?.stringValue
        menu = fields["menu"]?.arrayValue?.compactMap(SDUIMenuEntry.init) ?? []
        truncate = fields["truncate"]?.stringValue.flatMap(SDUITruncate.init) ?? .tail
        shortcut = fields["shortcut"]?.stringValue
        caption = fields["caption"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
        section = fields["section"]?.boolValue ?? false
    }
}

public struct SDUIListFilter: Sendable, Equatable {
    public let placeholder: String
    public let minRows: Int

    public init?(_ value: JSONValue?) {
        guard let fields = value?.objectValue else { return nil }
        placeholder = fields["placeholder"]?.stringValue ?? ""
        minRows = fields["minRows"]?.numberValue.map(Int.init) ?? 0
    }

    public func shown(rows: Int) -> Bool { rows >= minRows }

    public static func matches(_ item: SDUIItem, _ query: String) -> Bool {
        let q = query.trimmingCharacters(in: .whitespaces)
        guard !q.isEmpty else { return true }
        return [item.label, item.caption].compactMap { $0 }.contains { $0.localizedCaseInsensitiveContains(q) }
    }
}

public struct SDUIMenuEntry: Sendable, Equatable, Identifiable {
    public let action: String
    public let label: String
    public let confirm: String?
    public let key: String?

    public var id: String { action }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let action = fields["action"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.action = action
        self.label = label
        confirm = fields["confirm"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
        key = fields["key"]?.stringValue.flatMap { $0.count == 1 || $0 == "cmd+delete" ? $0 : nil }
    }
}

public struct SDUITreeItem: Sendable, Equatable, Identifiable {
    public let item: SDUIItem
    public let tone: String?
    public let detailTone: String?
    public let detailFaint: Bool
    public let muted: Bool
    public let faint: Bool
    public let action: Bool
    public let hasChildren: Bool
    public let expanded: Bool
    public let children: [SDUITreeItem]
    public let badges: [SDUIHeaderBadge]
    public let changes: SDUIChanges?
    public let labelIcon: String?
    public let pinned: Bool
    public let open: String?
    public let mirrorOf: String?
    public let keepFolded: Bool
    public let help: String?
    public let number: Int?

    public var id: String { item.id }

    public var target: String { mirrorOf ?? item.id }

    public var heading: Bool { action && hasChildren && (item.icon ?? "").isEmpty }

    public var opensPage: Bool { mirrorOf != nil || open != nil }

    public var tap: SDUITreeTap {
        guard hasChildren else { return .select(target) }
        if opensPage { return .open(target, unfold: !expanded && !keepFolded) }
        return action ? .fold(expand: !expanded) : .select(target)
    }

    public var shownChildren: [SDUITreeItem] {
        expanded ? children : children.filter(\.pinned)
    }

    public var visibleItems: [SDUIItem] {
        [item] + shownChildren.flatMap(\.visibleItems)
    }

    public init?(_ value: JSONValue) {
        guard let item = SDUIItem(value), let fields = value.objectValue else { return nil }
        self.item = item
        tone = fields["tone"]?.stringValue
        detailTone = fields["detailTone"]?.stringValue
        detailFaint = fields["detailFaint"]?.boolValue ?? false
        muted = fields["muted"]?.boolValue ?? false
        faint = fields["faint"]?.boolValue ?? false
        action = fields["action"]?.boolValue ?? false
        hasChildren = fields["hasChildren"]?.boolValue ?? false
        expanded = fields["expanded"]?.boolValue ?? false
        children = fields["children"]?.arrayValue?.compactMap(SDUITreeItem.init) ?? []
        badges = fields["badges"]?.arrayValue?.compactMap(SDUIHeaderBadge.init) ?? []
        changes = fields["changes"].flatMap(SDUIChanges.init)
        labelIcon = fields["labelIcon"]?.stringValue
        pinned = fields["pinned"]?.boolValue ?? false
        open = fields["open"]?.stringValue
        mirrorOf = fields["mirrorOf"]?.stringValue
        keepFolded = fields["keepFolded"]?.boolValue ?? false
        help = fields["help"]?.stringValue
        number = fields["number"]?.numberValue.map(Int.init)
    }
}

public struct SDUITreeDock: Sendable, Equatable, Identifiable {
    public struct Add: Sendable, Equatable {
        public let action: String
        public let label: String
    }

    public let id: String
    public let label: String
    public let icon: String?
    public let detail: String?
    public let state: String?
    public let attention: Bool
    public let expanded: Bool
    public let rows: [SDUITreeItem]
    public let depths: [String: Int]
    public let add: Add?
    public let cap: Int

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.id = id
        self.label = label
        icon = fields["icon"]?.stringValue
        detail = fields["detail"]?.stringValue
        state = fields["state"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
        attention = fields["attention"]?.boolValue ?? false
        expanded = fields["expanded"]?.boolValue ?? false
        var flat: [SDUITreeItem] = []
        var depths: [String: Int] = [:]
        func walk(_ values: [JSONValue], _ depth: Int) {
            for value in values {
                guard var row = value.objectValue else { continue }
                let children = row.removeValue(forKey: "children")?.arrayValue ?? []
                guard let item = SDUITreeItem(.object(row)) else { continue }
                flat.append(item)
                depths[item.id] = depth
                if !item.hasChildren { walk(children, depth + 1) }
            }
        }
        walk(fields["rows"]?.arrayValue ?? [], 0)
        rows = flat
        self.depths = depths
        if let add = fields["add"]?.objectValue, let action = add["action"]?.stringValue, let label = add["label"]?.stringValue {
            self.add = Add(action: action, label: label)
        } else {
            add = nil
        }
        cap = max(1, Int(fields["cap"]?.numberValue ?? 6))
    }
}

public struct SDUIChanges: Sendable, Equatable {
    public let added: Int
    public let removed: Int

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let added = fields["added"]?.numberValue, let removed = fields["removed"]?.numberValue else { return nil }
        self.added = Int(added)
        self.removed = Int(removed)
    }
}

public struct SDUIHeaderBadge: Sendable, Equatable {
    public let text: String
    public let tone: String?
    public let outline: Bool
    public let id: String?
    public let action: String?

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let text = fields["text"]?.stringValue else { return nil }
        self.text = text
        tone = fields["tone"]?.stringValue
        outline = fields["outline"]?.boolValue ?? false
        id = fields["id"]?.stringValue
        action = fields["action"]?.stringValue
    }
}

public struct SDUIReposSegment: Sendable, Equatable {
    public let text: String
    public let tone: String?

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let text = fields["text"]?.stringValue else { return nil }
        self.text = text
        tone = fields["tone"]?.stringValue
    }
}

public struct SDUIImpactRow: Sendable, Equatable, Identifiable {
    public let id: String
    public let icon: String
    public let tone: String?
    public let title: String
    public let count: String?

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let id = fields["id"]?.stringValue, let icon = fields["icon"]?.stringValue, let title = fields["title"]?.stringValue else { return nil }
        self.id = id
        self.icon = icon
        self.title = title
        tone = fields["tone"]?.stringValue
        count = fields["count"]?.stringValue
    }
}

public struct SDUIReposRow: Sendable, Equatable, Identifiable {
    public let id: String
    public let order: Int
    public let name: String
    public let landsBy: String
    public let landsIcon: String?
    public let source: String?
    public let state: String
    public let tone: String?
    public let icon: String?
    public let opens: Bool
    public let open: Bool
    public let cursor: Bool
    public let tail: String?
    public let note: String?
    public let actions: String?

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let id = fields["id"]?.stringValue, let name = fields["name"]?.stringValue,
              let landsBy = fields["landsBy"]?.stringValue, let state = fields["state"]?.stringValue else { return nil }
        self.id = id
        order = fields["order"]?.numberValue.flatMap { Int(exactly: $0) } ?? 0
        self.name = name
        self.landsBy = landsBy
        landsIcon = fields["landsIcon"]?.stringValue
        source = fields["source"]?.stringValue
        self.state = state
        tone = fields["tone"]?.stringValue
        icon = fields["icon"]?.stringValue
        opens = fields["opens"]?.boolValue ?? false
        open = fields["open"]?.boolValue ?? false
        cursor = fields["cursor"]?.boolValue ?? false
        tail = fields["tail"]?.stringValue
        note = fields["note"]?.stringValue
        actions = fields["actions"]?.stringValue
    }
}

public struct SDUIQuizRow: Sendable, Equatable, Identifiable {
    public let n: Int?
    public let text: String
    public let tone: String?
    public let label: String?
    public let fold: String?

    public var id: String { n.map(String.init) ?? "fold-" + (fold ?? "") }

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue else { return nil }
        if let fold = fields["fold"]?.stringValue {
            (n, text, tone, label, self.fold) = (nil, "", nil, nil, fold)
            return
        }
        guard let n = fields["n"]?.numberValue.flatMap({ Int(exactly: $0) }), let text = fields["text"]?.stringValue else { return nil }
        self.n = n
        self.text = text
        tone = fields["tone"]?.stringValue
        label = fields["label"]?.stringValue
        fold = nil
    }
}

public struct SDUIQuizOpen: Sendable, Equatable {
    public let label: String
    public let shortcut: String?

    init?(_ json: JSONValue?) {
        guard let fields = json?.objectValue, let label = fields["label"]?.stringValue else { return nil }
        self.label = label
        shortcut = fields["shortcut"]?.stringValue
    }
}

public struct SDUIQuizExcerpt: Sendable, Equatable {
    public let file: String
    public let commit: String
    public let span: String
    public let rows: [SDUIQuizRow]
    public let pick: String
    public let picked: ClosedRange<Int>?
    public let cursor: Int?
    public let open: SDUIQuizOpen?

    init?(_ json: JSONValue?) {
        guard let fields = json?.objectValue, let file = fields["file"]?.stringValue, let commit = fields["commit"]?.stringValue else { return nil }
        self.file = file
        self.commit = commit
        span = fields["span"]?.stringValue ?? ""
        rows = fields["rows"]?.arrayValue?.compactMap(SDUIQuizRow.init) ?? []
        pick = fields["pick"]?.stringValue ?? "none"
        let span = fields["picked"]?.arrayValue?.compactMap { $0.numberValue.flatMap { Int(exactly: $0) } }
        picked = span.flatMap { $0.count == 2 && $0[0] <= $0[1] ? $0[0]...$0[1] : nil }
        cursor = fields["cursor"]?.numberValue.flatMap { Int(exactly: $0) }
        open = SDUIQuizOpen(fields["open"])
    }

    public var lines: [SDUIQuizRow] { rows.filter { $0.n != nil } }
}

public struct SDUIQuizLevel: Sendable, Equatable, Identifiable {
    public let value: String
    public let label: String
    public let key: String

    public var id: String { value }

    init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let value = fields["value"]?.stringValue, let label = fields["label"]?.stringValue, let key = fields["key"]?.stringValue else { return nil }
        (self.value, self.label, self.key) = (value, label, key)
    }
}

public struct SDUIQuizOption: Sendable, Equatable, Identifiable {
    public let value: String
    public let label: String
    public let key: String
    public let on: Bool

    public var id: String { value }

    init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let value = fields["value"]?.stringValue, let label = fields["label"]?.stringValue, let key = fields["key"]?.stringValue else { return nil }
        (self.value, self.label, self.key) = (value, label, key)
        on = fields["on"]?.boolValue ?? false
    }
}

public struct SDUIQuizConfidence: Sendable, Equatable {
    public let label: String
    public let levels: [SDUIQuizLevel]
    public let value: String?
    public let enabled: Bool
    public let needsRating: Bool
    public let sends: Bool
    public let holdMs: Int
    public let hint: String?

    init?(_ json: JSONValue?) {
        guard let fields = json?.objectValue, let label = fields["label"]?.stringValue else { return nil }
        self.label = label
        levels = fields["levels"]?.arrayValue?.compactMap(SDUIQuizLevel.init) ?? []
        value = fields["value"]?.stringValue.flatMap { $0.isEmpty ? nil : $0 }
        enabled = fields["enabled"]?.boolValue ?? false
        needsRating = fields["needsRating"]?.boolValue ?? false
        sends = fields["sends"]?.boolValue ?? false
        holdMs = fields["holdMs"]?.numberValue.flatMap { Int(exactly: $0) } ?? 2000
        hint = fields["hint"]?.stringValue
    }
}

public struct SDUIQuizReveal: Sendable, Equatable {
    public let verdict: String
    public let label: String
    public let yours: String
    public let confidence: String?
    public let answer: String?
    public let why: String?
    public let ref: String?
    public let flag: String?
    public let note: String?

    init?(_ json: JSONValue?) {
        guard let fields = json?.objectValue, let verdict = fields["verdict"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        let text = { (key: String) in fields[key]?.stringValue.flatMap { $0.isEmpty ? nil : $0 } }
        self.verdict = verdict
        self.label = label
        yours = fields["yours"]?.stringValue ?? ""
        confidence = text("confidence")
        answer = text("answer")
        why = text("why")
        ref = text("ref")
        flag = text("flag")
        note = text("note")
    }
}

public struct SDUIQuizLabels: Sendable, Equatable {
    public let skip: String
    public let undo: String
    public let sending: String
    public let fold: String
    public let answerHint: String
    public let you: String
    public let answer: String
    public let allLines: String
    public let secondsLeft: String

    init(_ json: JSONValue?) {
        let fields = json?.objectValue ?? [:]
        skip = fields["skip"]?.stringValue ?? "Skip"
        undo = fields["undo"]?.stringValue ?? "Undo"
        sending = fields["sending"]?.stringValue ?? "Sending"
        fold = fields["fold"]?.stringValue ?? "Fold"
        answerHint = fields["answerHint"]?.stringValue ?? ""
        you = fields["you"]?.stringValue ?? "You:"
        answer = fields["answer"]?.stringValue ?? "Answer:"
        allLines = fields["allLines"]?.stringValue ?? "All {n} lines"
        secondsLeft = fields["secondsLeft"]?.stringValue ?? "{n} s left"
    }
}

public struct SDUIQuiz: Sendable, Equatable {
    public let request: String
    public let type: String
    public let state: String
    public let title: String
    public let question: String
    public let step: String?
    public let hint: String?
    public let concept: String?
    public let dock: Bool
    public let excerpt: SDUIQuizExcerpt?
    public let options: [SDUIQuizOption]
    public let confidence: SDUIQuizConfidence?
    public let reveal: SDUIQuizReveal?
    public let labels: SDUIQuizLabels

    public var asking: Bool { state == "asking" || state == "sending" }
    public var sending: Bool { state == "sending" }
    public var composerNeedsRating: Bool { asking && confidence?.needsRating == true }
}

public struct SDUIOption: Sendable, Equatable, Identifiable {
    public let value: String
    public let label: String

    public var id: String { value }

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let value = fields["value"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.value = value
        self.label = label
    }
}

public struct SDUIChoiceOption: Sendable, Equatable, Identifiable {
    public let value: String
    public let label: String
    public let description: String?
    public let shortcut: String?
    public let hint: String?
    public let on: Bool
    public let aside: SDUIChoiceAside?

    public var id: String { value }

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let value = fields["value"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.value = value
        self.label = label
        description = fields["description"]?.stringValue
        shortcut = fields["shortcut"]?.stringValue
        hint = fields["hint"]?.stringValue
        on = fields["on"]?.boolValue ?? false
        aside = fields["aside"].flatMap(SDUIChoiceAside.init)
    }
}

public struct SDUIChoiceAside: Sendable, Equatable {
    public let id: String
    public let state: String

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let id = fields["id"]?.stringValue else { return nil }
        self.id = id
        state = fields["state"]?.stringValue ?? "open"
    }
}

public struct SDUIComposerDraft: Sendable, Equatable {
    public let text: String
    public let rev: Int

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let text = fields["text"]?.stringValue, let rev = fields["rev"]?.numberValue.flatMap({ Int(exactly: $0) }) else { return nil }
        self.text = text
        self.rev = rev
    }
}

public struct SDUIChoiceOther: Sendable, Equatable {
    public let id: String
    public let label: String
    public let placeholder: String?
    public let shortcut: String?
    public let hint: String?
    public let on: Bool
    public let value: String?
    public let focus: Int
    public let aside: SDUIChoiceAside?

    public init?(_ json: JSONValue) {
        guard let fields = json.objectValue, let id = fields["id"]?.stringValue, let label = fields["label"]?.stringValue else { return nil }
        self.id = id
        self.label = label
        placeholder = fields["placeholder"]?.stringValue
        shortcut = fields["shortcut"]?.stringValue
        hint = fields["hint"]?.stringValue
        on = fields["on"]?.boolValue ?? false
        value = fields["value"]?.stringValue
        focus = fields["focus"]?.numberValue.flatMap { Int(exactly: $0) } ?? 0
        aside = fields["aside"].flatMap(SDUIChoiceAside.init)
    }
}

public struct SDUIColumn: Sendable, Equatable, Identifiable {
    public enum Align: String, Sendable {
        case left
        case right
    }

    public static let minFlexibleWidth: Double = 200

    public let id: String
    public let title: String
    public let align: Align
    public let width: Double?
    public let truncate: SDUITruncate
    public let priority: Int

    public init(id: String, title: String, align: Align, width: Double?, truncate: SDUITruncate = .tail, priority: Int = 0) {
        self.id = id
        self.title = title
        self.align = align
        self.width = width
        self.truncate = truncate
        self.priority = priority
    }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue, let title = fields["title"]?.stringValue else { return nil }
        let width = fields["width"]?.numberValue ?? 0
        self.init(
            id: id,
            title: title,
            align: fields["align"]?.stringValue.flatMap(Align.init) ?? .left,
            width: width > 0 ? width : nil,
            truncate: fields["truncate"]?.stringValue.flatMap(SDUITruncate.init) ?? .tail,
            priority: max(Int(fields["priority"]?.numberValue ?? 0), 0)
        )
    }

    public func scaled(_ factor: Double) -> SDUIColumn {
        SDUIColumn(id: id, title: title, align: align, width: width.map { $0 * factor }, truncate: truncate, priority: priority)
    }

    public static func visible(_ columns: [SDUIColumn], width: Double, gutter: Double) -> [Int] {
        var shown = Array(columns.indices)
        guard width > 0 else { return shown }
        func needed() -> Double {
            let spacing = gutter * Double(max(shown.count - 1, 0))
            return shown.reduce(spacing) { $0 + (columns[$1].width ?? minFlexibleWidth) }
        }
        while needed() > width, let drop = shown.filter({ columns[$0].priority > 0 }).max(by: { columns[$0].priority < columns[$1].priority }) {
            shown.removeAll { $0 == drop }
        }
        return shown
    }
}

public struct SDUISpark: Sendable, Equatable {
    public let values: [Double]
    public let max: Double

    public init(values: [Double], max: Double) {
        self.values = values
        self.max = max
    }

    public init?(values: JSONValue?, max: JSONValue?) {
        guard let max = max?.numberValue, max > 0 else { return nil }
        self.init(values: values?.arrayValue?.compactMap(\.numberValue) ?? [], max: max)
    }
}

public struct SDUICell: Sendable, Equatable {
    public struct Link: Sendable, Equatable {
        public let action: String
        public let label: String

        public init(action: String, label: String) {
            self.action = action
            self.label = label
        }
    }

    public let text: String
    public let bar: Double?
    public let tone: String?
    public let spark: SDUISpark?
    public let since: Date?
    public let link: Link?

    public init(text: String, bar: Double?, tone: String?, spark: SDUISpark?, since: Date? = nil, link: Link? = nil) {
        self.text = text
        self.bar = bar
        self.tone = tone
        self.spark = spark
        self.since = since
        self.link = link
    }

    public static func elapsed(since start: Date, now: Date) -> String {
        let interval = now.timeIntervalSince(start)
        let seconds = interval > 0 ? Int(Swift.min(interval, 1e10)) : 0
        let two = { (v: Int) in v < 10 ? "0\(v)" : "\(v)" }
        return "\(seconds / 3600):\(two(seconds / 60 % 60)):\(two(seconds % 60))"
    }

    public init(_ value: JSONValue) {
        let fields = value.objectValue ?? [:]
        let spark = fields["spark"]?.objectValue
        let tone = fields["tone"]?.stringValue
        self.init(
            text: fields["text"]?.stringValue ?? "",
            bar: fields["bar"]?.numberValue.map { Swift.min(Swift.max($0, 0), 1) },
            tone: tone?.isEmpty == false ? tone : nil,
            spark: spark.flatMap { SDUISpark(values: $0["values"], max: $0["max"]) },
            since: fields["since"]?.numberValue.flatMap { $0 > 0 ? Date(timeIntervalSince1970: $0) : nil },
            link: fields["action"]?.stringValue.flatMap { action in
                guard let label = fields["label"]?.stringValue, !action.isEmpty, !label.isEmpty else { return nil }
                return Link(action: action, label: label)
            }
        )
    }
}

public struct SDUITableRow: Sendable, Equatable, Identifiable {
    public let id: String
    public let depth: Int
    public let hasChildren: Bool
    public let expanded: Bool
    public let muted: Bool
    public let group: Bool
    public let cells: [SDUICell]

    public init(id: String, depth: Int, hasChildren: Bool, expanded: Bool, muted: Bool, group: Bool = false, cells: [SDUICell]) {
        self.id = id
        self.depth = depth
        self.hasChildren = hasChildren
        self.expanded = expanded
        self.muted = muted
        self.group = group
        self.cells = cells
    }

    public init?(_ value: JSONValue) {
        guard let fields = value.objectValue, let id = fields["id"]?.stringValue else { return nil }
        self.init(
            id: id,
            depth: fields["depth"]?.numberValue.map { Swift.max(Int($0), 0) } ?? 0,
            hasChildren: fields["hasChildren"]?.boolValue ?? false,
            expanded: fields["expanded"]?.boolValue ?? false,
            muted: fields["muted"]?.boolValue ?? false,
            group: fields["group"]?.boolValue ?? false,
            cells: fields["cells"]?.arrayValue?.map(SDUICell.init) ?? []
        )
    }
}

public struct SDUISort: Sendable, Equatable {
    public let column: String
    public let desc: Bool

    public init(column: String, desc: Bool) {
        self.column = column
        self.desc = desc
    }

    public var value: JSONValue {
        .object(["column": .string(column), "desc": .bool(desc)])
    }

    public static func next(clicking column: SDUIColumn, current: SDUISort?) -> SDUISort {
        if let current, current.column == column.id {
            return SDUISort(column: column.id, desc: !current.desc)
        }
        return SDUISort(column: column.id, desc: column.align == .right)
    }
}

public struct SDUIChild: Sendable, Equatable, Identifiable {
    public let id: String
    public let node: SDUINode
}

extension SDUINode {
    public var identifiedChildren: [SDUIChild] {
        children.enumerated().map { index, child in
            SDUIChild(id: child.id ?? "\(child.type)@\(index)", node: child)
        }
    }

    public func string(_ key: String) -> String? {
        props[key]?.stringValue
    }

    public func bool(_ key: String) -> Bool? {
        props[key]?.boolValue
    }

    public var docLinks: [String: String] {
        props["docLinks"]?.objectValue?.compactMapValues(\.stringValue) ?? [:]
    }

    public var docCards: [DocCard] {
        DocCard.list(props["docCards"])
    }

    public var images: [ImageRef] {
        ImageRef.list(props["images"])
    }

    public var imageLinks: [String: String] {
        props["imageLinks"]?.objectValue?.compactMapValues(\.stringValue) ?? [:]
    }

    public var taskLinks: [String: String] {
        props["taskLinks"]?.objectValue?.compactMapValues(\.stringValue) ?? [:]
    }

    public func items() -> [SDUIItem] {
        props["items"]?.arrayValue?.compactMap(SDUIItem.init) ?? []
    }

    public func listFilter() -> SDUIListFilter? {
        SDUIListFilter(props["filter"])
    }

    public func treeItems() -> [SDUITreeItem] {
        props["items"]?.arrayValue?.compactMap(SDUITreeItem.init) ?? []
    }

    public func treeDock() -> [SDUITreeDock] {
        props["dock"]?.arrayValue?.compactMap(SDUITreeDock.init) ?? []
    }

    public func headerBadges() -> [SDUIHeaderBadge] {
        props["badges"]?.arrayValue?.compactMap(SDUIHeaderBadge.init) ?? []
    }

    public var headerField: SDUIChild? {
        identifiedChildren.first { $0.node.type == "textField" }
    }

    public var headerButtons: [SDUIChild] {
        identifiedChildren.filter { $0.node.type != "textField" && $0.id != headerStop?.id && $0.node.bool("pill") != true && $0.node.bool("corner") != true }
    }

    public var headerCorner: SDUIChild? {
        identifiedChildren.first { $0.node.type == "button" && $0.node.bool("corner") == true }
    }

    public var headerPill: SDUIChild? {
        identifiedChildren.first { $0.node.bool("pill") == true }
    }

    public func reposSummary() -> [SDUIReposSegment] {
        props["summary"]?.arrayValue?.compactMap(SDUIReposSegment.init) ?? []
    }

    public func reposRows() -> [SDUIReposRow] {
        props["rows"]?.arrayValue?.compactMap(SDUIReposRow.init) ?? []
    }

    public func impactRows() -> [SDUIImpactRow] {
        props["rows"]?.arrayValue?.compactMap(SDUIImpactRow.init) ?? []
    }

    public var headerStatusTone: String? {
        switch string("status") {
        case "running", "childrenRunning", "review", "asksParent": "accent"
        case "waiting", "asks", "childrenWaiting": "warning"
        case "error": "danger"
        default: nil
        }
    }

    public var headerStop: SDUIChild? {
        guard string("status") != nil else { return nil }
        return identifiedChildren.first { $0.node.type == "button" && $0.node.string("action") == "stop" }
    }

    public var headerBar: [SDUIChild] {
        headerButtons.filter { $0.node.bool("overflow") != true }
    }

    public func headerOverflow() -> (shown: [SDUIChild], more: [SDUIChild]) {
        let all = headerButtons
        let shown = Array(headerBar.filter { $0.node.string("variant") != "danger" }.prefix(3))
        let ids = Set(shown.map(\.id))
        return (shown, all.filter { !ids.contains($0.id) })
    }

    public func options() -> [SDUIOption] {
        props["options"]?.arrayValue?.compactMap(SDUIOption.init) ?? []
    }

    public func quiz() -> SDUIQuiz? {
        guard let request = string("request"), let type = string("type"), let state = string("state"), let title = string("title"), let question = string("question") else { return nil }
        let text = { (key: String) in self.string(key).flatMap { $0.isEmpty ? nil : $0 } }
        return SDUIQuiz(
            request: request, type: type, state: state, title: title, question: question,
            step: text("step"), hint: text("hint"), concept: text("concept"), dock: bool("dock") == true,
            excerpt: SDUIQuizExcerpt(props["excerpt"]),
            options: props["options"]?.arrayValue?.compactMap(SDUIQuizOption.init) ?? [],
            confidence: SDUIQuizConfidence(props["confidence"]),
            reveal: SDUIQuizReveal(props["reveal"]),
            labels: SDUIQuizLabels(props["labels"])
        )
    }

    public func choiceOptions() -> [SDUIChoiceOption] {
        props["options"]?.arrayValue?.compactMap(SDUIChoiceOption.init) ?? []
    }

    public func composerDraft() -> SDUIComposerDraft? {
        props["draft"].flatMap(SDUIComposerDraft.init)
    }

    public func choiceOther() -> SDUIChoiceOther? {
        props["other"].flatMap(SDUIChoiceOther.init)
    }

    public func columns() -> [SDUIColumn] {
        props["columns"]?.arrayValue?.compactMap(SDUIColumn.init) ?? []
    }

    public func tableRows() -> [SDUITableRow] {
        props["rows"]?.arrayValue?.compactMap(SDUITableRow.init) ?? []
    }

    public func tableSort() -> SDUISort? {
        guard let fields = props["sort"]?.objectValue, let column = fields["column"]?.stringValue, !column.isEmpty else { return nil }
        return SDUISort(column: column, desc: fields["desc"]?.boolValue ?? false)
    }

    public func spark() -> SDUISpark? {
        SDUISpark(values: props["values"], max: props["max"])
    }

    public func sparklineHeight() -> Double? {
        props["height"]?.numberValue.flatMap { $0 > 0 ? $0 : nil }
    }

    public func confirmation() -> String? {
        string("confirm").flatMap { $0.isEmpty ? nil : $0 }
    }

    public func confirmMessage() -> String? {
        string("confirmMessage").flatMap { $0.isEmpty ? nil : $0 }
    }

    public func diffLines() -> [DiffLine] {
        props["lines"]?.arrayValue?.compactMap { line in
            guard let text = line["text"]?.stringValue else { return nil }
            switch line["sign"]?.stringValue {
            case "+": return DiffLine(text: text, kind: .added)
            case "-": return DiffLine(text: text, kind: .removed)
            default: return DiffLine(text: text, kind: .context)
            }
        } ?? []
    }

    public func diffFolds() -> Bool {
        bool("fold") == true
    }

    public func confirmActionTitle() -> String {
        let label = string("label") ?? ""
        let title = label.hasSuffix("…") ? String(label.dropLast()) : label
        return title.isEmpty ? "OK" : title
    }
}

public enum SDUITreeTap: Equatable, Sendable {
    case select(String)
    case open(String, unfold: Bool)
    case fold(expand: Bool)
}
