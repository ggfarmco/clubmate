import SwiftUI

struct ProjectSessionKey: FocusedValueKey {
    typealias Value = ProjectSession
}

struct WorkspaceWindowIDKey: FocusedValueKey {
    typealias Value = String
}

extension FocusedValues {
    var projectSession: ProjectSession? {
        get { self[ProjectSessionKey.self] }
        set { self[ProjectSessionKey.self] = newValue }
    }

    var workspaceWindowID: String? {
        get { self[WorkspaceWindowIDKey.self] }
        set { self[WorkspaceWindowIDKey.self] = newValue }
    }
}
