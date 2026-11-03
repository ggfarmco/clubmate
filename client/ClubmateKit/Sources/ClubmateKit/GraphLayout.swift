import CoreGraphics
import Foundation

public struct GraphMetrics: Sendable, Equatable {
    public var cardWidth: CGFloat
    public var headHeight: CGFloat
    public var rowHeight: CGFloat
    public var layerGap: CGFloat
    public var nodeGap: CGFloat
    public var trackGap: CGFloat
    public var markerGap: CGFloat

    public init(cardWidth: CGFloat = 210, headHeight: CGFloat = 52, rowHeight: CGFloat = 26, layerGap: CGFloat = 120, nodeGap: CGFloat = 24, trackGap: CGFloat = 12, markerGap: CGFloat = 48) {
        self.cardWidth = cardWidth
        self.headHeight = headHeight
        self.rowHeight = rowHeight
        self.layerGap = layerGap
        self.nodeGap = nodeGap
        self.trackGap = trackGap
        self.markerGap = markerGap
    }
}

public struct GraphRoute: Sendable, Equatable {
    public enum Kind: Sendable {
        case forward
        case back
        case skip
    }

    public let edge: Int
    public let points: [CGPoint]
    public let kind: Kind

    public func distance(to point: CGPoint) -> CGFloat {
        var best = CGFloat.infinity
        for i in stride(from: 0, to: points.count - 3, by: 3) {
            let p0 = points[i], p1 = points[i + 1], p2 = points[i + 2], p3 = points[i + 3]
            for k in 0...16 {
                let t = CGFloat(k) / 16, u = 1 - t
                let x = u * u * u * p0.x + 3 * u * u * t * p1.x + 3 * u * t * t * p2.x + t * t * t * p3.x
                let y = u * u * u * p0.y + 3 * u * u * t * p1.y + 3 * u * t * t * p2.y + t * t * t * p3.y
                best = min(best, hypot(point.x - x, point.y - y))
            }
        }
        return best
    }

    public var middle: CGPoint {
        guard points.count >= 4 else { return points.first ?? .zero }
        let segments = (points.count - 1) / 3
        let i = 3 * (segments / 2)
        let p0 = points[i], p1 = points[i + 1], p2 = points[i + 2], p3 = points[i + 3]
        let t: CGFloat = segments % 2 == 1 ? 0.5 : 0
        let u = 1 - t
        return CGPoint(x: u * u * u * p0.x + 3 * u * u * t * p1.x + 3 * u * t * t * p2.x + t * t * t * p3.x, y: u * u * u * p0.y + 3 * u * u * t * p1.y + 3 * u * t * t * p2.y + t * t * t * p3.y)
    }
}

public struct GraphLayout: Sendable {
    public let size: CGSize
    public let cards: [String: CGRect]
    public let markers: [String: CGRect]
    public let layers: [[String]]
    public let routes: [GraphRoute]
    private let ports: [String: [String: CGPoint]]
    private let inputs: [String: CGPoint]

    public init(graph: SDUIGraph, metrics m: GraphMetrics) {
        self.init(flow: graph.flow, metrics: m)
    }

    public init(flow: SDUIFlow, metrics m: GraphMetrics, markers sizes: [String: CGSize] = [:]) {
        var seen = Set<String>()
        let items = flow.cards.filter { seen.insert($0.id).inserted }.map(Item.card) + flow.markers.filter { seen.insert($0.id).inserted }.map(Item.marker)
        let index = Dictionary(uniqueKeysWithValues: items.enumerated().map { ($0.element.id, $0.offset) })
        let links = flow.edges.enumerated().compactMap { i, edge -> Link? in
            guard let from = index[edge.from], let to = index[edge.to] else { return nil }
            switch items[from] {
            case .card(let card):
                guard let out = card.outs.firstIndex(where: { $0.id == edge.out }) else { return nil }
                return Link(edge: i, from: from, out: out, to: to)
            case .marker:
                return Link(edge: i, from: from, out: nil, to: to)
            }
        }
        let back = Self.backEdges(items: items, links: links)
        let layer = Self.layers(count: items.count, links: links.filter { !back.contains($0.edge) })
        let columns = (layer.max() ?? -1) + 1
        let stacks = (0..<columns).map { l in items.indices.filter { layer[$0] == l } }
        let row = Dictionary(uniqueKeysWithValues: stacks.flatMap { $0.enumerated().map { ($0.element, $0.offset) } })
        func extent(_ item: Item) -> CGSize {
            switch item {
            case .card(let card): CGSize(width: m.cardWidth, height: m.headHeight + 2 * m.rowHeight / 3 + m.rowHeight * CGFloat(card.outs.count))
            case .marker(let marker): sizes[marker.id] ?? CGSize(width: m.rowHeight, height: m.headHeight)
            }
        }
        let widths = stacks.map { $0.map { extent(items[$0]).width }.max() ?? 0 }
        let carded = stacks.map { $0.contains { items[$0].isCard } }
        let gaps = (0..<max(columns - 1, 0)).map { carded[$0] && carded[$0 + 1] ? m.layerGap : m.markerGap }
        var lefts: [CGFloat] = []
        var x = m.layerGap / 2
        for l in 0..<columns {
            lefts.append(x)
            x += widths[l] + (l < gaps.count ? gaps[l] : 0)
        }

        func route(_ link: Link) -> GraphRoute.Kind {
            back.contains(link.edge) ? .back : layer[link.to] > layer[link.from] + 1 ? .skip : .forward
        }
        func trackOrder(_ kind: GraphRoute.Kind) -> [Link] {
            links.filter { route($0) == kind }.sorted {
                let a = abs(layer[$0.to] - layer[$0.from]), b = abs(layer[$1.to] - layer[$1.from])
                return (a, row[$0.from] ?? 0, $0.out ?? -1, $0.edge) < (b, row[$1.from] ?? 0, $1.out ?? -1, $1.edge)
            }
        }
        let backs = trackOrder(.back), skips = trackOrder(.skip)

        let g = m.trackGap
        let top = g * CGFloat(backs.isEmpty ? 2 : backs.count + 4)
        var cards: [String: CGRect] = [:]
        var markers: [String: CGRect] = [:]
        var ports: [String: [String: CGPoint]] = [:]
        var inputs: [String: CGPoint] = [:]
        var outputs: [String: CGPoint] = [:]
        let pad = m.rowHeight / 3
        var bottom = top
        for (l, stack) in stacks.enumerated() {
            var y = top
            for i in stack {
                let item = items[i]
                let s = extent(item)
                switch item {
                case .card(let card):
                    let rect = CGRect(x: lefts[l], y: y, width: s.width, height: s.height)
                    cards[card.id] = rect
                    inputs[card.id] = CGPoint(x: rect.minX, y: rect.minY + m.headHeight / 2)
                    var outs: [String: CGPoint] = [:]
                    for (k, out) in card.outs.enumerated() where outs[out.id] == nil {
                        outs[out.id] = CGPoint(x: rect.maxX, y: rect.minY + m.headHeight + pad + m.rowHeight * (CGFloat(k) + 0.5))
                    }
                    ports[card.id] = outs
                    y = rect.maxY + m.nodeGap
                    bottom = max(bottom, rect.maxY)
                case .marker(let marker):
                    let rect = CGRect(x: lefts[l] + (widths[l] - s.width) / 2, y: y, width: s.width, height: s.height)
                    markers[marker.id] = rect
                    inputs[marker.id] = CGPoint(x: rect.minX, y: rect.midY)
                    outputs[marker.id] = CGPoint(x: rect.maxX, y: rect.midY)
                    y = rect.maxY + m.nodeGap
                    bottom = max(bottom, rect.maxY)
                }
            }
        }

        func port(_ link: Link) -> CGPoint {
            let from = items[link.from]
            switch from {
            case .card(let card): return link.out.flatMap { ports[card.id]?[card.outs[$0].id] } ?? .zero
            case .marker(let marker): return outputs[marker.id] ?? .zero
            }
        }
        func input(_ item: Int) -> CGPoint {
            inputs[items[item].id] ?? .zero
        }
        func reach(_ rank: Int, of count: Int) -> CGFloat {
            let base = min(3 * g, m.layerGap / 2)
            let step = count > 1 ? min(g, max(0, m.layerGap / 2 - base) / CGFloat(count - 1)) : 0
            return base + step * CGFloat(rank)
        }
        func ranks(_ group: [Link], by key: (Link) -> Int) -> [Int: (rank: Int, count: Int)] {
            var result: [Int: (rank: Int, count: Int)] = [:]
            for members in Dictionary(grouping: group, by: key).values {
                for (rank, link) in members.enumerated() {
                    result[link.edge] = (rank, members.count)
                }
            }
            return result
        }
        func line(_ a: CGPoint, _ b: CGPoint) -> [CGPoint] {
            [a, CGPoint(x: a.x + (b.x - a.x) / 3, y: a.y + (b.y - a.y) / 3), CGPoint(x: a.x + 2 * (b.x - a.x) / 3, y: a.y + 2 * (b.y - a.y) / 3), b]
        }

        var routes: [GraphRoute] = []
        for link in links where route(link) == .forward {
            let p = port(link), i = input(link.to)
            let bend = (layer[link.from] < gaps.count ? gaps[layer[link.from]] : m.layerGap) / 2
            routes.append(GraphRoute(edge: link.edge, points: [p, CGPoint(x: p.x + bend, y: p.y), CGPoint(x: i.x - bend, y: i.y), i], kind: .forward))
        }
        let backFrom = ranks(backs, by: \.from), backTo = ranks(backs, by: \.to)
        for (t, link) in backs.enumerated() {
            let p = port(link), i = input(link.to), y = top - 3 * g - CGFloat(t) * g
            let rise = backFrom[link.edge].map { reach($0.rank, of: $0.count) } ?? 0
            let fall = backTo[link.edge].map { reach($0.rank, of: $0.count) } ?? 0
            let start = CGPoint(x: max(p.x - 2 * g, i.x + g), y: y), end = CGPoint(x: i.x + g, y: y)
            let up = [p, CGPoint(x: p.x + rise, y: p.y), CGPoint(x: p.x + rise - g, y: y)]
            let down = [CGPoint(x: i.x - fall, y: y), CGPoint(x: i.x - fall, y: i.y), i]
            routes.append(GraphRoute(edge: link.edge, points: up + line(start, end) + down, kind: .back))
        }
        let skipFrom = ranks(skips, by: \.from), skipTo = ranks(skips, by: \.to)
        for (t, link) in skips.enumerated() {
            let p = port(link), i = input(link.to), y = bottom + 3 * g + CGFloat(t) * g
            let drop = skipFrom[link.edge].map { reach($0.count - 1 - $0.rank, of: $0.count) } ?? 0
            let lift = skipTo[link.edge].map { reach($0.count - 1 - $0.rank, of: $0.count) } ?? 0
            let start = CGPoint(x: p.x + drop + 3 * g, y: y), end = CGPoint(x: i.x - lift - 3 * g, y: y)
            let down = [p, CGPoint(x: p.x + drop, y: p.y), CGPoint(x: p.x + drop / 2, y: y)]
            let up = [CGPoint(x: i.x - lift / 2, y: y), CGPoint(x: i.x - lift, y: i.y), i]
            routes.append(GraphRoute(edge: link.edge, points: down + line(start, end) + up, kind: .skip))
        }

        self.cards = cards
        self.markers = markers
        self.ports = ports
        self.inputs = inputs
        self.layers = stacks.map { $0.map { items[$0].id } }
        self.routes = routes.sorted { $0.edge < $1.edge }
        let height = skips.isEmpty ? bottom + 2 * g : bottom + CGFloat(skips.count + 4) * g
        self.size = items.isEmpty ? .zero : CGSize(width: x + m.layerGap / 2, height: height)
    }

    public func edge(at point: CGPoint, tolerance: CGFloat) -> Int? {
        var best: (edge: Int, distance: CGFloat)?
        for route in routes where route.points.count >= 4 {
            let d = route.distance(to: point)
            if d <= tolerance, d < best?.distance ?? .infinity { best = (route.edge, d) }
        }
        return best?.edge
    }

    public func port(node: String, out: String) -> CGPoint {
        ports[node]?[out] ?? .zero
    }

    public func input(node: String) -> CGPoint {
        inputs[node] ?? .zero
    }

    private enum Item {
        case card(SDUIFlow.Card)
        case marker(SDUIFlow.Marker)

        var id: String {
            switch self {
            case .card(let card): card.id
            case .marker(let marker): marker.id
            }
        }

        var isCard: Bool {
            if case .card = self { return true }
            return false
        }

        var start: Bool {
            switch self {
            case .card(let card): card.start
            case .marker(let marker): marker.kind == .start
            }
        }
    }

    private struct Link {
        let edge: Int
        let from: Int
        let out: Int?
        let to: Int
    }

    private static func backEdges(items: [Item], links: [Link]) -> Set<Int> {
        var outgoing = Array(repeating: [Link](), count: items.count)
        for link in links {
            outgoing[link.from].append(link)
        }
        var mark = Array(repeating: 0, count: items.count)
        var back = Set<Int>()
        func visit(_ v: Int) {
            mark[v] = 1
            for link in outgoing[v] {
                if mark[link.to] == 1 {
                    back.insert(link.edge)
                } else if mark[link.to] == 0 {
                    visit(link.to)
                }
            }
            mark[v] = 2
        }
        let targets = Set(links.map(\.to))
        let starts = items.indices.filter { items[$0].start }
        let roots = starts.isEmpty ? items.indices.filter { !targets.contains($0) } : starts
        for v in roots + Array(items.indices) where mark[v] == 0 {
            visit(v)
        }
        return back
    }

    private static func layers(count: Int, links: [Link]) -> [Int] {
        var layer = Array(repeating: 0, count: count)
        var indegree = Array(repeating: 0, count: count)
        for link in links {
            indegree[link.to] += 1
        }
        var queue = (0..<count).filter { indegree[$0] == 0 }
        var head = 0
        while head < queue.count {
            let v = queue[head]
            head += 1
            for link in links where link.from == v {
                layer[link.to] = max(layer[link.to], layer[v] + 1)
                indegree[link.to] -= 1
                if indegree[link.to] == 0 {
                    queue.append(link.to)
                }
            }
        }
        return layer
    }
}
