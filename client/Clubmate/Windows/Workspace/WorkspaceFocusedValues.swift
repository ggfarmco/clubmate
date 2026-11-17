import SwiftUI

struct WorkspaceSessionKey: FocusedValueKey {
    typealias Value = WorkspaceSession
}

struct WorkspaceWindowIDKey: FocusedValueKey {
    typealias Value = String
}

struct FocusedPaneKey: FocusedValueKey {
    typealias Value = String
}

extension FocusedValues {
    var focusedPane: String? {
        get { self[FocusedPaneKey.self] }
        set { self[FocusedPaneKey.self] = newValue }
    }

    var workspaceSession: WorkspaceSession? {
        get { self[WorkspaceSessionKey.self] }
        set { self[WorkspaceSessionKey.self] = newValue }
    }

    var workspaceWindowID: String? {
        get { self[WorkspaceWindowIDKey.self] }
        set { self[WorkspaceWindowIDKey.self] = newValue }
    }
}
