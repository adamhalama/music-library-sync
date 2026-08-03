import SwiftUI

/// `.empty` — the second failure mode this redesign rules out is ambiguous
/// emptiness. "Not run yet", "still checking", and "found nothing" are three
/// different states and must never render the same way.
struct EmptyStateView: View {
    enum Kind {
        /// The user has not run the step. Nothing is known.
        case notRun(action: String?)
        /// The backend is working; a result is expected.
        case working
        /// The step ran and legitimately produced nothing.
        case empty
        /// The step ran and failed.
        case failed(String)
        /// The step cannot run yet because something else is missing.
        case blocked(String)
    }

    let title: String
    let kind: Kind
    var detail: String?

    var body: some View {
        VStack(spacing: 9) {
            icon
            Text(title)
                .font(Typography.cardTitle)
                .foregroundStyle(Theme.textSecondary)
            if let message {
                Text(message)
                    .font(Typography.control)
                    .foregroundStyle(Theme.textTertiary)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 360)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(40)
    }

    @ViewBuilder private var icon: some View {
        switch kind {
        case .working:
            ProgressView().controlSize(.small)
        case .notRun:
            Image(systemName: "circle.dashed")
                .font(.system(size: 28))
                .foregroundStyle(Theme.textTertiary)
        case .empty:
            Image(systemName: "tray")
                .font(.system(size: 28))
                .foregroundStyle(Theme.textTertiary)
        case .failed:
            Image(systemName: Severity.error.symbolName)
                .font(.system(size: 28))
                .foregroundStyle(Theme.error)
        case .blocked:
            Image(systemName: Severity.warn.symbolName)
                .font(.system(size: 28))
                .foregroundStyle(Theme.warn)
        }
    }

    private var message: String? {
        if let detail { return detail }
        switch kind {
        case .notRun(let action):
            return action.map { "Not run yet — \($0)." } ?? "Not run yet."
        case .working:
            return "Still checking…"
        case .empty:
            return "Ran and found nothing."
        case .failed(let reason):
            return reason
        case .blocked(let reason):
            return reason
        }
    }
}

/// C8 — a cell whose value is still resolving. A blank cell would be ambiguous
/// between "still checking" and "nothing found", so both are drawn.
struct PendingValue<Content: View>: View {
    let isResolved: Bool
    @ViewBuilder var content: () -> Content

    var body: some View {
        if isResolved {
            content()
        } else {
            HStack(spacing: 5) {
                ProgressView().controlSize(.mini).scaleEffect(0.6).frame(width: 10, height: 10)
                Text("checking")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
            }
        }
    }
}
