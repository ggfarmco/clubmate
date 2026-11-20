import SwiftUI

import CompanionKit

struct OfflineRow: View {
    let connection: ComputerConnection
    @Environment(AppNavigation.self) private var navigation

    var body: some View {
        if connection.state != .online {
            Section {
                HStack(spacing: 8) {
                    if connection.state == .unauthorized {
                        Button {
                            navigation.showingAddComputer = true
                        } label: {
                            banner
                        }
                    } else {
                        ConnectionBanner(connection: connection)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    Button {
                        navigation.connectionSheet = ConnectionSheet(computer: connection.computer.id)
                    } label: {
                        Image(systemName: "stethoscope")
                            .frame(width: 44, height: 44)
                            .contentShape(.rect)
                    }
                    .accessibilityLabel("Check connection")
                    .accessibilityIdentifier("status.connection." + connection.computer.id)
                }
                .buttonStyle(.borderless)
                .accessibilityElement(children: .contain)
                .accessibilityIdentifier("offline.row")
                .columnRow()
            }
        }
    }

    private var banner: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(connection.state.label)
                Text("Pair again").font(.footnote).foregroundStyle(.tint)
            }
        } icon: {
            Image(systemName: "lock.slash").foregroundStyle(Color.dangerInk)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
    }
}
