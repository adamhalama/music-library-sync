import AppKit
// Prints, for the built-in display (or the only display), its flipped-global
// rect "x y w h" — the coordinate space System Events and screencapture use.
let screens = NSScreen.screens
let main = screens.first { $0.frame.origin == .zero } ?? screens[0]
let target = screens.first { $0.localizedName.localizedCaseInsensitiveContains("built-in") } ?? main
let f = target.frame
let flippedY = main.frame.height - (f.origin.y + f.height)
print("\(Int(f.origin.x)) \(Int(flippedY)) \(Int(f.width)) \(Int(f.height))")
