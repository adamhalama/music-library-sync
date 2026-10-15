import AppKit
import SwiftUI

/// The single place in the application that names a color.
///
/// Structural surfaces map onto system colors so the app follows the system
/// light and dark appearance and the user's accent color. Only the semantic
/// status colors (`ok`/`warn`/`err`/`info`/`idle`) are literals, and each one
/// carries the light and dark value from `docs/gui-redesign/app/shell.css`.
enum Theme {
    // MARK: Structural surfaces

    /// `--win`: the content column background.
    static let window = Color(nsColor: .controlBackgroundColor)
    /// `--sidebar` and `--chrome`: sidebar, inspector, toolbar, status bar.
    static let chrome = Color(nsColor: .windowBackgroundColor)
    /// `--sunken`: card headers, grouped boxes, and severity tiles.
    ///
    /// There is no system color for this. `underPageBackgroundColor` is the
    /// closest by name and was the phase 1 choice, but it resolves to #969696
    /// in light appearance — a mid grey that turned every card header and
    /// severity tile into a slab. A low-alpha `labelColor` tint reproduces
    /// `shell.css`'s #f7f7fa / #252528 and still follows the appearance,
    /// because `labelColor` inverts with it.
    static let sunken = Color(nsColor: .labelColor).opacity(0.045)
    /// `--sep`
    static let separator = Color(nsColor: .separatorColor)
    /// `--sep-2`: the heavier rule, for a stepper's unfilled connector and an
    /// unreached step's ring — `--sep` disappears against `--sunken` there.
    static let separatorStrong = Color(nsColor: .tertiaryLabelColor)
    /// `--fill`: badge and segmented-control backgrounds.
    static let fill = Color(nsColor: .quaternaryLabelColor)
    /// `--hover`
    static let hover = Color(nsColor: .quaternaryLabelColor).opacity(0.5)
    /// `--sel`
    static let selection = Color(nsColor: .selectedContentBackgroundColor)

    // MARK: Text

    /// `--text`
    static let text = Color(nsColor: .labelColor)
    /// `--text-2`
    static let textSecondary = Color(nsColor: .secondaryLabelColor)
    /// `--text-3`
    static let textTertiary = Color(nsColor: .tertiaryLabelColor)

    /// `--accent`: always the system accent color, never a brand literal.
    static let accent = Color.accentColor
    /// Foreground on an accent-filled shape (`shell.css` uses `#fff` there).
    /// The only literal white in the app, and it lives here so no view outside
    /// `DesignSystem/` spells a color.
    static let onAccent = Color.white

    // MARK: Semantic status literals

    static let ok = dynamic(light: 0x1C7C3C, dark: 0x4CC26A)
    static let okBackground = dynamic(light: 0xE4F4EA, dark: 0x4CC26A, darkAlpha: 0.14)
    static let warn = dynamic(light: 0x9C5D06, dark: 0xE0A13A)
    static let warnBackground = dynamic(light: 0xFBEED7, dark: 0xE0A13A, darkAlpha: 0.14)
    static let error = dynamic(light: 0xC0362C, dark: 0xFF6B5E)
    static let errorBackground = dynamic(light: 0xFBE6E4, dark: 0xFF6B5E, darkAlpha: 0.14)
    static let info = dynamic(light: 0x0A64D6, dark: 0x3F8CF5)
    static let infoBackground = dynamic(light: 0xE6F0FD, dark: 0x3F8CF5, darkAlpha: 0.14)
    static let idle = dynamic(light: 0x78787F, dark: 0x8C8C93)
    static let idleBackground = dynamic(light: 0xF0F0F2, dark: 0x8C8C93, darkAlpha: 0.13)

    // MARK: Source glyph gradients (`.glyph.sc/.sp/.am/.rb`)

    static let glyphSoundCloud = [rgb(0xFF9243), rgb(0xEA4F06)]
    static let glyphSpotify = [rgb(0x3ADE74), rgb(0x12A04B)]
    static let glyphAppleMusic = [rgb(0xFA5A6E), rgb(0xE01E3C)]
    static let glyphRekordbox = [rgb(0x5A8CFF), rgb(0x2A4FD0)]

    private static func dynamic(
        light: Int,
        dark: Int,
        lightAlpha: Double = 1,
        darkAlpha: Double = 1
    ) -> Color {
        let lightColor = nsColor(light, alpha: lightAlpha)
        let darkColor = nsColor(dark, alpha: darkAlpha)
        return Color(nsColor: NSColor(name: nil) { appearance in
            let match = appearance.bestMatch(from: [.aqua, .darkAqua])
            return match == .darkAqua ? darkColor : lightColor
        })
    }

    private static func rgb(_ hex: Int) -> Color {
        Color(nsColor: nsColor(hex, alpha: 1))
    }

    private static func nsColor(_ hex: Int, alpha: Double) -> NSColor {
        NSColor(
            srgbRed: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255,
            alpha: alpha
        )
    }
}

/// One severity vocabulary shared by doctor rows, plan status, sidebar badges,
/// callouts, and run outcomes, so the same condition never reads two ways.
enum Severity: String, CaseIterable, Sendable {
    case ok
    case info
    case warn
    case error
    case idle

    var tint: Color {
        switch self {
        case .ok: Theme.ok
        case .info: Theme.info
        case .warn: Theme.warn
        case .error: Theme.error
        case .idle: Theme.idle
        }
    }

    var background: Color {
        switch self {
        case .ok: Theme.okBackground
        case .info: Theme.infoBackground
        case .warn: Theme.warnBackground
        case .error: Theme.errorBackground
        case .idle: Theme.idleBackground
        }
    }

    /// The `.sev` bubble glyph: ✓ / i / ! / ✕.
    var badgeSymbol: String {
        switch self {
        case .ok: "checkmark"
        case .info: "info"
        case .warn: "exclamationmark"
        case .error: "xmark"
        case .idle: "minus"
        }
    }

    var symbolName: String {
        switch self {
        case .ok: "checkmark.circle.fill"
        case .info: "info.circle.fill"
        case .warn: "exclamationmark.triangle.fill"
        case .error: "exclamationmark.octagon.fill"
        case .idle: "circle.dashed"
        }
    }

    /// The singular name of one item at this severity.
    var itemTitle: String {
        switch self {
        case .ok: "Passed"
        case .info: "Info"
        case .warn: "Warning"
        case .error: "Error"
        case .idle: "Unclassified"
        }
    }

    /// The plural heading a group of same-severity items is filed under.
    var groupTitle: String {
        switch self {
        case .ok: "Passed"
        case .info: "Info"
        case .warn: "Warnings"
        case .error: "Errors"
        case .idle: "Unclassified"
        }
    }

    /// Worst-first ordering, used to pick a headline severity from a set.
    var rank: Int {
        switch self {
        case .error: 4
        case .warn: 3
        case .info: 2
        case .ok: 1
        case .idle: 0
        }
    }

    init(_ doctor: DoctorSeverity) {
        switch doctor {
        case .info: self = .info
        case .warn: self = .warn
        case .error: self = .error
        case .unknown: self = .idle
        }
    }

    /// `doctor.run` carries both a severity and a status; the status is the
    /// more specific of the two, so it wins when it is one udl actually emits.
    static func forCheck(_ check: DoctorCheck) -> Severity {
        switch check.status.lowercased() {
        case "blocked": return .error
        case "warning": return .warn
        case "ok": return .ok
        default: return Severity(check.severity)
        }
    }

    static func worst(_ severities: [Severity]) -> Severity {
        severities.max(by: { $0.rank < $1.rank }) ?? .idle
    }
}
