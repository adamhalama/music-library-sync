import SwiftUI

/// `.statusbar` — a 46pt bar applied with `.safeAreaInset(edge: .bottom)`.
/// The left side always states what the screen currently knows; the right side
/// carries at most one secondary and one primary action.
struct StatusBar<Summary: View, Actions: View>: View {
    let summary: Summary
    let actions: Actions

    init(@ViewBuilder summary: () -> Summary, @ViewBuilder actions: () -> Actions) {
        self.summary = summary()
        self.actions = actions()
    }

    var body: some View {
        HStack(spacing: 11) {
            summary
                .font(Typography.control)
                .foregroundStyle(Theme.textSecondary)
                .lineLimit(1)
            Spacer(minLength: 8)
            actions
                .controlSize(.regular)
        }
        .padding(.horizontal, 14)
        .frame(height: Metrics.statusBarHeight)
        .frame(maxWidth: .infinity)
        .background(Theme.chrome)
        .overlay(alignment: .top) {
            Rectangle().fill(Theme.separator).frame(height: 0.5)
        }
    }
}

extension View {
    func udlStatusBar<Summary: View, Actions: View>(
        @ViewBuilder summary: () -> Summary,
        @ViewBuilder actions: () -> Actions
    ) -> some View {
        let bar = StatusBar(summary: summary, actions: actions)
        return safeAreaInset(edge: .bottom, spacing: 0) { bar }
    }
}

/// `.summary` — a count with its severity color, e.g. "2 errors · 4 warnings".
struct SummaryCount: View {
    let value: Int
    let noun: String
    var severity: Severity = .idle

    var body: some View {
        HStack(spacing: 4) {
            Text(value, format: .number)
                .monospacedDigit()
                .fontWeight(.semibold)
                .foregroundStyle(severity == .idle ? Theme.text : severity.tint)
            Text(noun)
        }
    }
}

/// Joins summary fragments with the interpunct separator used in the mockups.
struct SummaryLine<Content: View>: View {
    @ViewBuilder var content: () -> Content

    var body: some View {
        HStack(spacing: 8) {
            content()
        }
    }
}
