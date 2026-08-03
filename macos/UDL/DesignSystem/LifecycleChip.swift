import SwiftUI

/// C2's per-source lifecycle vocabulary, shared by Sync, Free DL, and
/// Rekordbox. `udl` plans and runs one source at a time, so exactly one row can
/// read `Needs you` while the rest read `Queued`.
enum Lifecycle: String, CaseIterable, Sendable {
    case queued
    case planning
    case needsYou
    case running
    case done
    case failed
    case skipped
    case notRun

    var label: String {
        switch self {
        case .queued: "Queued"
        case .planning: "Planning…"
        case .needsYou: "Needs you"
        case .running: "Running"
        case .done: "Done"
        case .failed: "Failed"
        case .skipped: "Skipped"
        case .notRun: "Not run yet"
        }
    }

    var severity: Severity {
        switch self {
        case .queued, .notRun: .idle
        case .planning, .running: .info
        case .needsYou: .warn
        case .done: .ok
        case .failed: .error
        case .skipped: .idle
        }
    }

    /// True while the backend owns the source and the user cannot act on it.
    var isBackendOwned: Bool {
        self == .planning || self == .running
    }

    /// Maps the wire's `SourceSnapshot.lifecycle` string.
    init(wire: String) {
        switch wire.lowercased() {
        case "planning", "preflight": self = .planning
        case "confirm", "confirming", "awaiting_confirmation": self = .needsYou
        case "running", "downloading", "executing": self = .running
        case "done", "completed", "succeeded": self = .done
        case "failed", "error": self = .failed
        case "skipped": self = .skipped
        default: self = .queued
        }
    }
}

struct LifecycleChip: View {
    let lifecycle: Lifecycle
    var isAnimating = true

    var body: some View {
        HStack(spacing: 5) {
            if lifecycle.isBackendOwned && isAnimating {
                ProgressView()
                    .controlSize(.mini)
                    .scaleEffect(0.7)
                    .frame(width: 8, height: 8)
            } else {
                Circle()
                    .fill(lifecycle.severity.tint)
                    .frame(width: 6, height: 6)
            }
            Text(lifecycle.label)
                .font(Typography.pill)
                .foregroundStyle(lifecycle.severity.tint)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 2)
        .background(lifecycle.severity.background, in: RoundedRectangle(cornerRadius: 5))
        .accessibilityElement(children: .combine)
        .accessibilityLabel(lifecycle.label)
    }
}
