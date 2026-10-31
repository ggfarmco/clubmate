// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "ClubmateKit",
    platforms: [.macOS("27.0")],
    products: [
        .library(name: "ClubmateKit", targets: ["ClubmateKit"])
    ],
    dependencies: [
        .package(path: "../ClubmateProtocol")
    ],
    targets: [
        .target(name: "ClubmateKit", dependencies: ["ClubmateProtocol"]),
        .testTarget(name: "ClubmateKitTests", dependencies: ["ClubmateKit"]),
    ]
)
