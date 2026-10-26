import AppKit

import GhosttyKit

import ClubmateKit

@MainActor
final class GhosttyApp {
    static let shared = GhosttyApp()

    private(set) var app: ghostty_app_t?
    private(set) var theme = Theme.fallback
    let colorScheme: ghostty_color_scheme_e

    private var config: ghostty_config_t?

    private init() {
        let dark = NSApp?.effectiveAppearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
        colorScheme = dark ? GHOSTTY_COLOR_SCHEME_DARK : GHOSTTY_COLOR_SCHEME_LIGHT
        if let resources = Bundle.main.resourceURL?.appending(path: "ghostty") {
            setenv("GHOSTTY_RESOURCES_DIR", resources.path(percentEncoded: false), 1)
        }
        guard ghostty_init(UInt(CommandLine.argc), CommandLine.unsafeArgv) == GHOSTTY_SUCCESS else { return }
        setlocale(LC_NUMERIC, "C")
        guard let config = Self.loadConfig() else { return }
        var runtime = ghostty_runtime_config_s()
        runtime.supports_selection_clipboard = false
        runtime.wakeup_cb = { _ in GhosttyCallbacks.wakeup() }
        runtime.action_cb = { _, target, action in GhosttyCallbacks.action(target, action) }
        runtime.read_clipboard_cb = { userdata, location, state, mimes, count, list in
            GhosttyCallbacks.readClipboard(userdata, location, state, mimes, count, list)
        }
        runtime.confirm_read_clipboard_cb = { userdata, confirm, state, request in
            GhosttyCallbacks.confirmReadClipboard(userdata, confirm, state, request)
        }
        runtime.write_clipboard_cb = { _, location, content, count, confirm in
            GhosttyCallbacks.writeClipboard(location, content, count, confirm)
        }
        runtime.close_surface_cb = { userdata, _ in GhosttyCallbacks.closeSurface(userdata) }
        guard let app = ghostty_app_new(&runtime, config) else {
            ghostty_config_free(config)
            return
        }
        self.app = app
        self.config = config
        ghostty_app_set_color_scheme(app, colorScheme)
        ghostty_app_update_config(app, config)
        let derived = GhosttyCallbacks.takeDerivedConfig()
        theme = GhosttyConfigReader.theme(from: derived ?? config, dark: dark, overlay: Self.overlayURL)
        if let derived { ghostty_config_free(derived) }
    }

    func tick() {
        guard let app else { return }
        ghostty_app_tick(app)
    }

    private static var overlayURL: URL {
        ClubmatePaths.resolve().state.appending(path: "ghostty.conf")
    }

    private static func loadConfig() -> ghostty_config_t? {
        guard let config = ghostty_config_new() else { return nil }
        ghostty_config_load_default_files(config)
        let overlay = overlayURL.path(percentEncoded: false)
        if FileManager.default.fileExists(atPath: overlay) {
            overlay.withCString { ghostty_config_load_file(config, $0) }
        }
        ghostty_config_load_recursive_files(config)
        forceSSHFeaturesOff(config)
        ghostty_config_finalize(config)
        return config
    }

    private static func forceSSHFeaturesOff(_ config: ghostty_config_t) {
        let key = "shell-integration-features"
        var bits: UInt32 = 0
        guard key.withCString({ ghostty_config_get(config, &bits, $0, UInt(key.utf8.count)) }) else { return }
        let features = ["cursor", "sudo", "title", "ssh-env", "ssh-terminfo", "path"]
        let value = features.enumerated().map { index, name in
            bits & (1 << UInt32(index)) != 0 && !name.hasPrefix("ssh-") ? name : "no-" + name
        }.joined(separator: ",")
        let file = FileManager.default.temporaryDirectory.appending(path: "clubmate-ghostty-\(UUID().uuidString).conf")
        guard (try? "\(key) = \(value)\n".write(to: file, atomically: true, encoding: .utf8)) != nil else { return }
        file.path(percentEncoded: false).withCString { ghostty_config_load_file(config, $0) }
        try? FileManager.default.removeItem(at: file)
    }
}
