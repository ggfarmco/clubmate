Clubmate builds on the following MIT-licensed projects.

### Ghostty

- Source: https://github.com/ghostty-org/ghostty
- License: MIT, Copyright (c) 2024 Mitchell Hashimoto, Ghostty contributors
- Use: libghostty (`GhosttyKit.xcframework`), shell-integration, themes and terminfo are built from a pinned upstream revision by `scripts/build-ghostty.sh` and bundled into `Clubmate.app`.

### agterm

- Source: https://github.com/umputun/agterm
- License: MIT, Copyright (c) 2026 Umputun
- Use: `scripts/build-ghostty.sh`, `client/project.yml` and the libghostty embedding approach are adapted from agterm's `scripts/setup.sh` and `project.yml`.
- Use: `client/Clubmate/Ghostty/GhosttyApp.swift`, `GhosttyCallbacks.swift`, `TerminalSurfaceView.swift` and `SurfaceDeck.swift` adapt agterm's `agterm/Ghostty/GhosttyApp.swift`, `GhosttyCallbacks.swift`, `GhosttySurfaceView.swift` (+Input, +FirstResponder, +Visibility) and its eager terminal deck: config layering with ssh shell features forced off, coalesced wakeup tick, clipboard callbacks, surface creation at non-zero size with wake retry, keyboard/IME/mouse forwarding, mounted-once surfaces with occlusion for hidden ones.

### macterm

- Source: https://github.com/thdxg/macterm
- License: MIT, Copyright (c) 2026 Macterm
- Use: the approach to driving libghostty's C API from a Swift 6 strict-concurrency AppKit/SwiftUI app, via agterm's bridge, which was adapted from macterm.

### MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
