import SwiftUI

/// The primitive behind the governing principle: a backend constraint is never
/// hidden behind a silently disabled control. `ConstraintNote` states the
/// reason next to the control it limits.
struct ConstraintNote: View {
    let text: String
    var severity: Severity = .info
    var symbol = "info.circle"

    var body: some View {
        Label {
            Text(text)
                .font(Typography.caption)
                .foregroundStyle(Theme.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
        } icon: {
            Image(systemName: symbol)
                .font(Typography.caption)
                .foregroundStyle(severity == .info ? Theme.textTertiary : severity.tint)
        }
        .labelStyle(.titleAndIcon)
        .accessibilityElement(children: .combine)
    }
}

private struct ConstrainedModifier: ViewModifier {
    let reason: String?
    let severity: Severity

    func body(content: Content) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            content
                .disabled(reason != nil)
                .opacity(reason == nil ? 1 : 0.45)
            if let reason {
                ConstraintNote(text: reason, severity: severity)
            }
        }
    }
}

extension View {
    /// Renders the control, disables it, dims it, and states why — never hides
    /// it. Passing `nil` leaves the control fully enabled and adds no note.
    ///
    /// This is the only sanctioned way to disable a control in this app.
    func constrained(by reason: String?, severity: Severity = .info) -> some View {
        modifier(ConstrainedModifier(reason: reason, severity: severity))
    }

    /// A control that stays enabled but carries a standing explanation, for
    /// cases such as C6 (`∞` is sent as `plan_limit: 0`).
    func footnote(_ text: String, severity: Severity = .info) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            self
            ConstraintNote(text: text, severity: severity)
        }
    }
}
