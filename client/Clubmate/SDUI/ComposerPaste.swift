import AppKit

@MainActor
enum ComposerPaste {
    private static let imageExtensions: Set<String> = ["png", "jpg", "jpeg", "gif", "webp"]
    private static let pastedName = "Pasted image.png"

    static func images(on board: NSPasteboard) -> (paths: [String], scratch: URL?)? {
        let urls = board.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        if !urls.isEmpty {
            guard urls.allSatisfy({ imageExtensions.contains($0.pathExtension.lowercased()) }) else { return nil }
            return (urls.map(\.path), nil)
        }
        guard board.string(forType: .string) == nil, let png = png(on: board) else { return nil }
        let scratch = FileManager.default.temporaryDirectory.appending(path: "clubmate-paste/\(UUID().uuidString)", directoryHint: .isDirectory)
        let file = scratch.appending(path: pastedName)
        do {
            try FileManager.default.createDirectory(at: scratch, withIntermediateDirectories: true)
            try png.write(to: file)
        } catch {
            try? FileManager.default.removeItem(at: scratch)
            return nil
        }
        return ([file.path], scratch)
    }

    private static func png(on board: NSPasteboard) -> Data? {
        if let png = board.data(forType: .png) { return png }
        guard let image = NSImage(pasteboard: board), let tiff = image.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff) else { return nil }
        return bitmap.representation(using: .png, properties: [:])
    }
}
