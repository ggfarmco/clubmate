import SwiftUI

import CompanionKit

struct VoiceBarButton: View {
    @Environment(CompanionModel.self) private var model
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        if let target = navigation.voiceTarget(model) {
            VoiceButton(size: VoiceButton.inBar, online: model.connection(target.computer)?.state == .online, bar: true)
        }
    }
}

extension Destination {
    var floatsMic: Bool {
        switch self {
        case .run: true
        case .section(let id, _): id == AgentsPluginInfo.openQueue
        default: false
        }
    }

    var hasOwnMic: Bool {
        if case .session = self { return true }
        return floatsMic
    }
}
