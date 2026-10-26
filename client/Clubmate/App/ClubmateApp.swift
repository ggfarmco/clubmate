import SwiftUI

@main
struct ClubmateApp: App {
    private let gallery = CommandLine.arguments.contains("--sdui-gallery")
    private let openProject = OpenProjectModel.launchProjectID()

    var body: some Scene {
        Window("Clubmate", id: "main") {
            Group {
                if gallery {
                    GalleryView()
                } else if let openProject {
                    OpenProjectView(model: OpenProjectModel(projectID: openProject))
                } else {
                    Color.clear.frame(minWidth: 800, minHeight: 500)
                }
            }
            .environment(\.theme, GhosttyApp.shared.theme)
            .eventBanner()
        }
    }
}
