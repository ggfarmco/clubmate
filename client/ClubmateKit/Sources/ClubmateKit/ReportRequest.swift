import Foundation

public struct ReportRequest: Decodable, Sendable, Equatable {
    public struct File: Decodable, Sendable, Equatable {
        public let name: String
        public let bytes: Int
        public let path: String
        public let checked: Bool
        public let warning: String
        public let source: String

        public init(name: String, bytes: Int, path: String, checked: Bool, warning: String = "", source: String = "") {
            self.name = name
            self.bytes = bytes
            self.path = path
            self.checked = checked
            self.warning = warning
            self.source = source
        }

        public init(from decoder: any Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            name = try container.decode(String.self, forKey: .name)
            bytes = try container.decodeIfPresent(Int.self, forKey: .bytes) ?? 0
            path = try container.decodeIfPresent(String.self, forKey: .path) ?? ""
            checked = try container.decodeIfPresent(Bool.self, forKey: .checked) ?? false
            warning = try container.decodeIfPresent(String.self, forKey: .warning) ?? ""
            source = try container.decodeIfPresent(String.self, forKey: .source) ?? ""
        }

        private enum CodingKeys: String, CodingKey {
            case name, bytes, path, checked, warning, source
        }
    }

    public static let extraChecked = [Diagnostics.settingsName: true, Diagnostics.appLogName: true, Diagnostics.windowName: false]

    public let workspaceId: String
    public let workspaceName: String
    public let relayHost: String
    public let text: String
    public let summary: String
    public let sessionTitle: String
    public let timeoutMs: Int?
    public let maxBytes: Int
    public let maxText: Int
    public let files: [File]
    public let id: String?

    public init(workspaceId: String, workspaceName: String, relayHost: String, text: String, summary: String = "", sessionTitle: String = "", timeoutMs: Int? = nil, maxBytes: Int, maxText: Int, files: [File], id: String? = nil) {
        self.workspaceId = workspaceId
        self.workspaceName = workspaceName
        self.relayHost = relayHost
        self.text = text
        self.summary = summary
        self.sessionTitle = sessionTitle
        self.timeoutMs = timeoutMs
        self.maxBytes = maxBytes
        self.maxText = maxText
        self.files = files
        self.id = id
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        workspaceId = try container.decodeIfPresent(String.self, forKey: .workspaceId) ?? ""
        workspaceName = try container.decodeIfPresent(String.self, forKey: .workspaceName) ?? ""
        relayHost = try container.decodeIfPresent(String.self, forKey: .relayHost) ?? ""
        text = try container.decodeIfPresent(String.self, forKey: .text) ?? ""
        summary = try container.decodeIfPresent(String.self, forKey: .summary) ?? ""
        sessionTitle = try container.decodeIfPresent(String.self, forKey: .sessionTitle) ?? ""
        timeoutMs = try container.decodeIfPresent(Int.self, forKey: .timeoutMs)
        maxBytes = try container.decode(Int.self, forKey: .maxBytes)
        maxText = try container.decode(Int.self, forKey: .maxText)
        files = try container.decodeIfPresent([File].self, forKey: .files) ?? []
        id = try container.decodeIfPresent(String.self, forKey: .id)
    }

    private enum CodingKeys: String, CodingKey {
        case workspaceId, workspaceName, relayHost, text, summary, sessionTitle, timeoutMs, maxBytes, maxText, files, id
    }

    public var deadline: Duration? {
        timeoutMs.map { .milliseconds(max($0 - ConfirmRequest.margin, 0)) }
    }

    public static func size(_ bytes: Int) -> String {
        let mib = Double(bytes) / Double(1 << 20)
        if mib >= 1 { return mib == mib.rounded() ? "\(Int(mib)) MB" : String(format: "%.1f MB", mib) }
        let kib = Double(bytes) / 1024
        if kib >= 1 { return "\(Int(kib.rounded())) KB" }
        return "\(bytes) B"
    }

    public func overLimit(_ total: Int) -> String? {
        total > maxBytes ? "Report is \(Self.size(total)), the limit is \(Self.size(maxBytes))" : nil
    }

    public func textProblem(text: String, summary: String) -> String? {
        if text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return "Your words can't be empty" }
        if text.utf8.count > maxText { return "Your words are over \(Self.size(maxText))" }
        if summary.utf8.count > maxText { return "Agent's summary is over \(Self.size(maxText))" }
        return nil
    }

    @MainActor
    public static func answer(_ request: ReportRequest, expiry: ConfirmExpiry? = nil, ask: @MainActor (ReportRequest, ConfirmExpiry) async -> ReportReply) async -> ReportReply {
        let expiry = expiry ?? ConfirmExpiry()
        if expiry.expired { return .unanswered(request, reason: expiry.reason) }
        let timer = request.deadline.map { deadline in
            Task { @MainActor in
                guard (try? await Task.sleep(for: deadline)) != nil else { return }
                expiry.expire(ReportReply.timedOut)
            }
        }
        defer { timer?.cancel() }
        let reply = await ask(request, expiry)
        return expiry.expired ? .unanswered(request, reason: expiry.reason) : reply
    }
}

public struct ReportReply: Encodable, Sendable, Equatable {
    public static let timedOut = "timed out"
    public static let noWindow = "no window"
    public static let withdrawn = "withdrawn"

    public let send: Bool
    public let text: String
    public let summary: String
    public let files: [String]
    public let extra: [String: Data]
    public let appVersion: String
    public let reason: String?

    public init(send: Bool, text: String, summary: String, files: [String], extra: [String: Data], appVersion: String, reason: String? = nil) {
        self.send = send
        self.text = text
        self.summary = summary
        self.files = files
        self.extra = extra
        self.appVersion = appVersion
        self.reason = reason
    }

    public static func cancelled(_ request: ReportRequest) -> ReportReply {
        unanswered(request, reason: nil)
    }

    public static func unanswered(_ request: ReportRequest, reason: String?) -> ReportReply {
        ReportReply(send: false, text: request.text, summary: request.summary, files: [], extra: [:], appVersion: "", reason: reason)
    }
}
