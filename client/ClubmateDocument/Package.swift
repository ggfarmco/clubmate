// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "ClubmateDocument",
    platforms: [.iOS("27.0"), .macOS("27.0")],
    products: [
        .library(name: "ClubmateDocument", targets: ["ClubmateDocument"])
    ],
    targets: [
        .target(name: "ClubmateDocument", resources: [.copy("Web")]),
        .testTarget(name: "ClubmateDocumentTests", dependencies: ["ClubmateDocument"]),
    ]
)
