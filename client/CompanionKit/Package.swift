// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "CompanionKit",
    platforms: [.iOS("27.0"), .macOS("27.0")],
    products: [
        .library(name: "CompanionKit", targets: ["CompanionKit"])
    ],
    dependencies: [
        .package(path: "../ClubmateProtocol")
    ],
    targets: [
        .target(name: "CompanionKit", dependencies: ["ClubmateProtocol"]),
        .testTarget(name: "CompanionKitTests", dependencies: ["CompanionKit"], resources: [.copy("Fixtures")]),
    ]
)
