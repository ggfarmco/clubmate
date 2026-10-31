import Foundation

public struct BusFeed: Sendable {
    public enum Step: Sendable, Equatable {
        case event(BusEvent)
        case closed(after: Int64)
        case ignored
    }

    public private(set) var subscription: String
    private var after: Int64

    public init(subscription: String, after: Int64) {
        self.subscription = subscription
        self.after = after
    }

    public mutating func receive(_ notification: RPCNotification) -> Step {
        guard let params = notification.params.objectValue, params["subscription"]?.stringValue == subscription else { return .ignored }
        switch notification.method {
        case "bus.event":
            guard let event = try? params["event"]?.decode(BusEvent.self) else { return .ignored }
            after = Int64(event.seq)
            return .event(event)
        case "bus.closed":
            return .closed(after: after)
        default:
            return .ignored
        }
    }

    public mutating func resubscribed(_ subscription: String) {
        self.subscription = subscription
    }
}

extension CoreClient {
    public func busEvents(pattern: String, after: Int64 = .max) async throws -> AsyncStream<BusEvent> {
        let notifications = subscribeNotifications()
        let initial = BusFeed(subscription: try await busSubscribe(pattern: pattern, after: after), after: after)
        let (stream, continuation) = AsyncStream.makeStream(of: BusEvent.self)
        let pump = Task { [weak self] in
            var feed = initial
            for await notification in notifications {
                switch feed.receive(notification) {
                case .event(let event):
                    continuation.yield(event)
                case .closed(let resume):
                    guard let next = try? await self?.busSubscribe(pattern: pattern, after: resume) else {
                        continuation.finish()
                        return
                    }
                    feed.resubscribed(next)
                case .ignored:
                    continue
                }
            }
            continuation.finish()
            let subscription = feed.subscription
            Task { try? await self?.busUnsubscribe(subscription) }
        }
        continuation.onTermination = { _ in pump.cancel() }
        return stream
    }
}
