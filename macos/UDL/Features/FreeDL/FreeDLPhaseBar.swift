import SwiftUI

/// C7 — the stepper that never advances on its own.
///
/// Each step is one backend RPC with its own run ID. Nothing in this view
/// changes the current phase; only a click does, and a step whose prerequisite
/// has not run is rendered, disabled, and told why rather than hidden. A
/// completed step stays clickable, so going back to it is always possible.
struct FreeDLPhaseBar: View {
    let current: FreeDLPhase
    let lifecycle: (FreeDLPhase) -> Lifecycle
    let unavailableReason: (FreeDLPhase) -> String?
    let select: (FreeDLPhase) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 0) {
                ForEach(FreeDLPhase.allCases) { phase in
                    step(phase)
                    if phase != FreeDLPhase.allCases.last {
                        Rectangle()
                            .fill(lifecycle(phase) == .done ? Theme.accent : Theme.separatorStrong)
                            .frame(width: 30, height: 1.5)
                            .padding(.horizontal, 9)
                    }
                }
                Spacer(minLength: 12)
            }
            if let phase = FreeDLPhase.allCases.first(where: { unavailableReason($0) != nil }),
               let reason = unavailableReason(phase) {
                ConstraintNote(text: "\(phase.title): \(reason)")
            }
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Theme.separator).frame(height: 0.5)
        }
    }

    @ViewBuilder
    private func step(_ phase: FreeDLPhase) -> some View {
        let state = lifecycle(phase)
        let reason = unavailableReason(phase)
        let isCurrent = phase == current

        Button {
            select(phase)
        } label: {
            HStack(spacing: 7) {
                bubble(phase, state: state, isCurrent: isCurrent)
                VStack(alignment: .leading, spacing: 1) {
                    Text(phase.title)
                        .font(Typography.control)
                        .fontWeight(isCurrent ? .semibold : .regular)
                        .foregroundStyle(isCurrent ? Theme.text : Theme.textSecondary)
                    // C7 — a step whose prerequisite does not exist is dimmed
                    // *and* told why, on screen. The tooltip alone was not a
                    // stated reason.
                    Text(reason ?? state.label)
                        .font(Typography.caption)
                        .foregroundStyle(
                            reason != nil
                                ? Theme.textTertiary
                                : (state == .notRun ? Theme.textTertiary : state.severity.tint)
                        )
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(reason != nil)
        .opacity(reason == nil ? 1 : 0.45)
        .help(reason ?? "\(phase.method) — \(phase.effect)")
        .accessibilityLabel("Step \(phase.number), \(phase.title), \(state.label)")
    }

    private func bubble(_ phase: FreeDLPhase, state: Lifecycle, isCurrent: Bool) -> some View {
        ZStack {
            Circle()
                .fill(state == .done ? Theme.accent : Color.clear)
                .frame(width: 21, height: 21)
            Circle()
                .stroke(
                    state == .done ? Color.clear : (isCurrent ? Theme.accent : Theme.separatorStrong),
                    lineWidth: 1.5
                )
                .frame(width: 21, height: 21)
            if state == .done {
                Image(systemName: "checkmark")
                    .font(Typography.pill)
                    .foregroundStyle(Theme.onAccent)
            } else if state.isBackendOwned {
                ProgressView().controlSize(.mini).scaleEffect(0.6)
            } else {
                Text("\(phase.number)")
                    .font(Typography.pill)
                    .foregroundStyle(isCurrent ? Theme.accent : Theme.textTertiary)
            }
        }
        .overlay {
            if isCurrent && state != .done {
                Circle()
                    .stroke(Theme.accent.opacity(0.18), lineWidth: 3)
                    .frame(width: 24, height: 24)
            }
        }
    }
}
