import Foundation

extension CompanionModel {
    public func gone(_ workspace: WorkspaceKey, _ destination: Destination) -> Bool {
        switch destination {
        case .session(let id):
            guard let store = agents(computer: workspace.computer, workspace: workspace.workspace), store.fetched else { return false }
            return store.session(id) == nil
        case .task(let id):
            guard let store = backlog(computer: workspace.computer, workspace: workspace.workspace), store.fetched else { return false }
            return store.task(id) == nil
        default:
            return false
        }
    }
}
