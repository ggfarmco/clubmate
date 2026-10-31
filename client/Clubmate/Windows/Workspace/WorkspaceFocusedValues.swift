import SwiftUI

struct WorkspaceSessionKey: FocusedValueKey {
    typealias Value = WorkspaceSession
}

struct WorkspaceWindowIDKey: FocusedValueKey {
    typealias Value = String
}

extension FocusedValues {
    var workspaceSession: WorkspaceSession? {
        get { self[WorkspaceSessionKey.self] }
        set { self[WorkspaceSessionKey.self] = newValue }
    }

    var workspaceWindowID: String? {
        get { self[WorkspaceWindowIDKey.self] }
        set { self[WorkspaceWindowIDKey.self] = newValue }
    }
}
