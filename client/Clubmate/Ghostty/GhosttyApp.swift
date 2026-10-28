import AppKit
import Observation
import os

import GhosttyKit

import ClubmateKit

let ghosttyLog = Logger(subsystem: "app.clubmate", category: "ghostty")

@MainActor
@Observable
final class GhosttyApp {
    static let shared = GhosttyApp()

    @ObservationIgnored private(set) var app: ghostty_app_t?
    private(set) var theme = Theme.fallback
    private(set) var failure: String?
    private(set) var configFontSize: Double = 13
    private(set) var colorScheme: ghostty_color_scheme_e

    @ObservationIgnored private var config: ghostty_config_t?
    @ObservationIgnored private let surfaces = NSHashTable<TerminalSurfaceView>.weakObjects()
    @ObservationIgnored private var appearanceObservation: NSKeyValueObservation?

    private init() {
        colorScheme = Self.scheme(dark: Self.systemIsDark)
        if let resources = Bundle.main.resourceURL?.appending(path: "ghostty") {
            setenv("GHOSTTY_RESOURCES_DIR", resources.path(percentEncoded: false), 1)
        }
        guard ghostty_init(UInt(CommandLine.argc), CommandLine.unsafeArgv) == GHOSTTY_SUCCESS else {
            fail("ghostty_init failed")
            return
        }
        setlocale(LC_NUMERIC, "C")
        guard let config = loadConfig() else {
            fail("ghostty_config_new failed")
            return
        }
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
            fail("ghostty_app_new failed")
            return
        }
        self.app = app
        self.config = config
        ghostty_app_set_color_scheme(app, colorScheme)
        applyConfig(app, config)
        appearanceObservation = NSApp?.observe(\.effectiveAppearance) { _, _ in
            DispatchQueue.main.async {
                MainActor.assumeIsolated { GhosttyApp.shared.appearanceChanged() }
            }
        }
    }

    func tick() {
        guard let app else { return }
        ghostty_app_tick(app)
    }

    func register(_ view: TerminalSurfaceView) {
        surfaces.add(view)
    }

    private func appearanceChanged() {
        let scheme = Self.scheme(dark: Self.systemIsDark)
        guard scheme != colorScheme, let app, let config else { return }
        colorScheme = scheme
        ghostty_app_set_color_scheme(app, scheme)
        for surface in surfaces.allObjects.compactMap(\.surface) {
            ghostty_surface_set_color_scheme(surface, scheme)
        }
        applyConfig(app, config)
    }

    private func applyConfig(_ app: ghostty_app_t, _ config: ghostty_config_t) {
        ghostty_app_update_config(app, config)
        let derived = GhosttyCallbacks.takeDerivedConfig()
        let applied = derived ?? config
        theme = GhosttyConfigReader.theme(from: applied, dark: colorScheme == GHOSTTY_COLOR_SCHEME_DARK, overlay: Self.overlayURL)
        configFontSize = GhosttyConfigReader.fontSize(applied) ?? configFontSize
        if let derived { ghostty_config_free(derived) }
    }

    private static var systemIsDark: Bool {
        NSApp.map { ColorSchemeChoice.isDark(appearanceName: $0.effectiveAppearance.name.rawValue) } ?? false
    }

    private static func scheme(dark: Bool) -> ghostty_color_scheme_e {
        dark ? GHOSTTY_COLOR_SCHEME_DARK : GHOSTTY_COLOR_SCHEME_LIGHT
    }

    private static var overlayURL: URL {
        ClubmatePaths.resolve().state.appending(path: "ghostty.conf")
    }

    private func fail(_ message: String) {
        ghosttyLog.error("\(message, privacy: .public)")
        failure = message
    }

    private func loadConfig() -> ghostty_config_t? {
        guard let config = ghostty_config_new() else { return nil }
        ghostty_config_load_default_files(config)
        ghostty_config_load_recursive_files(config)
        let overlay = Self.overlayURL.path(percentEncoded: false)
        if FileManager.default.fileExists(atPath: overlay) {
            overlay.withCString { ghostty_config_load_file(config, $0) }
        }
        if let error = Self.forceSSHFeaturesOff(config) { fail(error) }
        ghostty_config_finalize(config)
        for index in 0..<ghostty_config_diagnostics_count(config) {
            if let message = ghostty_config_get_diagnostic(config, index).message {
                ghosttyLog.warning("config: \(String(cString: message), privacy: .public)")
            }
        }
        return config
    }

    private static func forceSSHFeaturesOff(_ config: ghostty_config_t) -> String? {
        let key = "shell-integration-features"
        var bits: UInt32 = 0
        guard key.withCString({ ghostty_config_get(config, &bits, $0, UInt(key.utf8.count)) }) else {
            return "cannot read \(key); ssh shell features left as configured"
        }
        let features = ["cursor", "sudo", "title", "ssh-env", "ssh-terminfo", "path"]
        let value = features.enumerated().map { index, name in
            bits & (1 << UInt32(index)) != 0 && !name.hasPrefix("ssh-") ? name : "no-" + name
        }.joined(separator: ",")
        let file = FileManager.default.temporaryDirectory.appending(path: "clubmate-ghostty-\(UUID().uuidString).conf")
        do {
            try "\(key) = \(value)\n".write(to: file, atomically: true, encoding: .utf8)
        } catch {
            return "ssh shell features override failed: \(error.localizedDescription)"
        }
        file.path(percentEncoded: false).withCString { ghostty_config_load_file(config, $0) }
        try? FileManager.default.removeItem(at: file)
        return nil
    }
}
