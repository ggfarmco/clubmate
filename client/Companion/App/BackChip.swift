import SwiftUI

import CompanionKit

struct BackChip: View {
    let label: String
    let below: [String]
    let back: () -> Void
    let jump: (Int) -> Void

    var body: some View {
        Menu {
            ForEach(Array(below.enumerated()).reversed(), id: \.offset) { index, title in
                Button(title) { jump(index) }
            }
        } label: {
            HStack(spacing: 6) {
                Image(systemName: "chevron.backward")
                    .font(.system(size: 20, weight: .semibold))
                Text(label)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
            .padding(.vertical, 8)
            .padding(.horizontal, 14)
            .frame(minHeight: 44)
            .contentShape(.capsule)
        } primaryAction: {
            back()
        }
        .accessibilityIdentifier("back.chip")
        .accessibilityLabel(label)
    }
}

extension EnvironmentValues {
    @Entry var backChipShown = false
}

struct BackChipModifier: ViewModifier {
    let entry: StackEntry
    let tab: CompanionTab
    @Environment(AppNavigation.self) private var navigation

    func body(content: Content) -> some View {
        let path = navigation.path(tab)
        if let index = path.firstIndex(of: entry), index > 0 || navigation.layout == .compact, case .chip(let label) = BackControl(entry) {
            content
                .environment(\.backChipShown, true)
                .navigationBarBackButtonHidden(true)
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        BackChip(label: label, below: [navigation.rootTitle] + path.prefix(index).map(navigation.title)) {
                            navigation.setPath(Array(path.prefix(index)), tab: tab)
                        } jump: { depth in
                            navigation.setPath(Array(path.prefix(depth)), tab: tab)
                        }
                    }
                }
        } else {
            content
        }
    }
}

extension UINavigationController: @retroactive UIGestureRecognizerDelegate {
    override open func viewDidLoad() {
        super.viewDidLoad()
        interactivePopGestureRecognizer?.delegate = self
    }

    public func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
        viewControllers.count > 1
    }
}
