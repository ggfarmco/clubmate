// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "ClubmateKit",
    platforms: [.macOS("27.0")],
    products: [
        .library(name: "ClubmateKit", targets: ["ClubmateKit"])
    ],
    targets: [
        .target(name: "ClubmateKit"),
        .testTarget(name: "ClubmateKitTests", dependencies: ["ClubmateKit"]),
    ]
)
