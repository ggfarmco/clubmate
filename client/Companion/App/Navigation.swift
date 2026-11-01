import Observation
import SwiftUI

import CompanionKit

struct SidebarItem: Hashable {
    let computer: String
    let plugin: String
}

struct OpenDoc: Identifiable, Hashable {
    let computer: String
    let workspace: String
    let path: String

    var id: Self { self }
}

@MainActor
@Observable
final class AppNavigation {
    var sidebar: SidebarItem?
    var detail: String?
    var macDetails: String?
    var needsYouList = false
    var workspaces: [String: String] = [:]
    var showingAddComputer = false
    var doc: OpenDoc?
    var preferredColumn: NavigationSplitViewColumn = .sidebar
    var layout: CompanionLayout = .compact
    var visibility: NavigationSplitViewVisibility
    var sidebarHidden: Bool {
        didSet { UserDefaults.standard.set(sidebarHidden, forKey: "sidebarHidden") }
    }

    init() {
        let hidden = UserDefaults.standard.bool(forKey: "sidebarHidden")
        sidebarHidden = hidden
        visibility = hidden ? .doubleColumn : .all
    }

    func open(computer: String, plugin: String, detail: String? = nil) {
        macDetails = nil
        needsYouList = false
        sidebar = SidebarItem(computer: computer, plugin: plugin)
        self.detail = detail
        preferredColumn = detail == nil ? .content : .detail
    }

    func openDoc(computer: String, workspace: String, path: String) {
        doc = OpenDoc(computer: computer, workspace: workspace, path: path)
    }

    func showMac(_ computer: String) {
        macDetails = computer
        needsYouList = false
        sidebar = nil
        detail = nil
        preferredColumn = .content
    }

    func showNeedsYou() {
        needsYouList = true
        macDetails = nil
        sidebar = nil
        detail = nil
        preferredColumn = .content
    }

    func arrange() {
        let chosen = sidebar != nil || macDetails != nil || needsYouList
        switch layout.columns(chosen: chosen, sidebarHidden: sidebarHidden) {
        case .stack:
            switch CompanionLayout.stackColumn(chosen: chosen, detail: detail != nil) {
            case .sidebar: preferredColumn = .sidebar
            case .content: preferredColumn = .content
            case .detail: preferredColumn = .detail
            }
        case .two:
            visibility = .doubleColumn
        case .three:
            visibility = .all
        }
    }

    func visibilityChanged() {
        guard layout == .wide, sidebar != nil || macDetails != nil || needsYouList else { return }
        sidebarHidden = visibility != .all
    }

    func workspace(for connection: ComputerConnection) -> Workspace? {
        let list = connection.workspaces
        return list.first { $0.id == workspaces[connection.computer.id] } ?? list.first
    }
}

@MainActor
@Observable
final class PairingFlow {
    enum Phase: Equatable {
        case idle
        case pairing(String)
        case failed(String)
    }

    enum Source {
        case link
        case paste
        case scan
    }

    var phase: Phase = .idle
    var review: PairingReview?
    var paired: Computer?

    var failureMessage: String? {
        if case .failed(let message) = phase { return message }
        return nil
    }

    func received(_ text: String, from source: Source, model: CompanionModel, navigation: AppNavigation) {
        let link: PairingLink
        do {
            link = try PairingLink.parse(text, policy: model.policy)
        } catch {
            phase = .failed(error.message)
            return
        }
        let review = PairingReview(link: link, existing: model.computers)
        if Self.autoConfirm || !review.needsConfirmation(scanned: source == .scan) {
            pair(link, model: model, navigation: navigation)
            return
        }
        self.review = review
    }

    func confirm(model: CompanionModel, navigation: AppNavigation) {
        guard let review else { return }
        self.review = nil
        pair(review.link, model: model, navigation: navigation)
    }

    func pair(_ link: PairingLink, model: CompanionModel, navigation: AppNavigation) {
        phase = .pairing(link.computerName)
        Task {
            do {
                let computer = try await model.pair(link)
                phase = .idle
                paired = computer
                navigation.showingAddComputer = false
                navigation.open(computer: computer.id, plugin: AgentsPluginInfo.id)
            } catch {
                phase = .failed((error as? RemoteError)?.message ?? error.localizedDescription)
            }
        }
    }

    func dismissFailure() {
        if case .failed = phase { phase = .idle }
    }

    private static var autoConfirm: Bool {
        #if DEBUG
        UserDefaults.standard.bool(forKey: "autoConfirmPairing")
        #else
        false
        #endif
    }
}
