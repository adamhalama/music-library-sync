import SwiftUI

extension AppState.Destination {
    var icon: String {
        switch self {
        case .onboarding: "sparkles"
        case .home: "house"
        case .sync: "arrow.down.circle"
        case .freeDL: "sparkle.magnifyingglass"
        case .rekordbox: "square.stack.3d.up"
        case .playlists: "music.note.list"
        case .doctor: "stethoscope"
        case .credentials: "key.horizontal"
        case .config: "slider.horizontal.3"
        }
    }

    /// Sidebar grouping. `Welcome` is routed to on first run rather than
    /// navigated to, so it belongs to no group.
    static let workflows: [AppState.Destination] = [.home, .sync, .freeDL, .rekordbox, .playlists]
    static let system: [AppState.Destination] = [.doctor, .credentials, .config]
}

struct SidebarView: View {
    @EnvironmentObject private var appState: AppState
    @EnvironmentObject private var sidebarContext: SidebarContextStore

    var body: some View {
        VStack(spacing: 0) {
            List(selection: $appState.destination) {
                Section("Workflows") {
                    ForEach(AppState.Destination.workflows) { destination in
                        row(destination).tag(destination)
                    }
                }
                if let context = sidebarContext.context, !context.items.isEmpty {
                    // The "why can't I click this" reason is stated once per
                    // section rather than once per row: repeating it on every
                    // queued source turns an explanation into noise.
                    let firstUnavailable = context.items.first { $0.unavailableReason != nil }?.id
                    Section(context.title) {
                        ForEach(context.items) { item in
                            contextualRow(
                                item,
                                context: context,
                                showsReason: item.id == firstUnavailable
                            )
                        }
                        if let note = context.note {
                            ConstraintNote(text: note)
                                .listRowSeparator(.hidden)
                        }
                    }
                }
            }
            .listStyle(.sidebar)

            Divider()

            List(selection: $appState.destination) {
                Section("System") {
                    ForEach(AppState.Destination.system) { destination in
                        row(destination).tag(destination)
                    }
                }
            }
            .listStyle(.sidebar)
            .scrollDisabled(true)
            .frame(height: 118)

            Divider()
            backendFooter
        }
        .navigationSplitViewColumnWidth(
            min: Metrics.sidebarMinWidth,
            ideal: Metrics.sidebarWidth,
            max: Metrics.sidebarMaxWidth
        )
    }

    private func row(_ destination: AppState.Destination) -> some View {
        HStack(spacing: 8) {
            Label(destination.rawValue, systemImage: destination.icon)
                .lineLimit(1)
            Spacer(minLength: 4)
            if let badge = badge(for: destination) {
                CountBadge(count: badge.count, severity: badge.severity)
            }
        }
    }

    @ViewBuilder
    private func contextualRow(
        _ item: SidebarContextItem,
        context: SidebarContext,
        showsReason: Bool
    ) -> some View {
        let row = HStack(spacing: 8) {
            if let sourceType = item.sourceType {
                SourceGlyph(sourceType: sourceType)
            }
            VStack(alignment: .leading, spacing: 1) {
                Text(item.title)
                    .font(Typography.control)
                    .lineLimit(1)
                if let subtitle = item.subtitle {
                    Text(subtitle)
                        .font(Typography.monoSmall)
                        .foregroundStyle(Theme.textTertiary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 4)
            if let lifecycle = item.lifecycle {
                LifecycleChip(lifecycle: lifecycle)
            }
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())

        if let reason = item.unavailableReason {
            // C2 — an unplanned source is dimmed and states why, instead of
            // being a control that silently does nothing when clicked.
            VStack(alignment: .leading, spacing: 3) {
                row.opacity(0.45)
                if showsReason {
                    ConstraintNote(text: reason)
                }
            }
            .help(reason)
        } else {
            // A `Button`, not `.onTapGesture`: these rows live inside a
            // `List(selection:)` whose own row hit-testing swallows a tap
            // gesture, so the contextual section rendered but never responded
            // to a click. A button inside a list row does receive it.
            Button {
                context.select(item.id)
            } label: {
                row
                    .background(
                        RoundedRectangle(cornerRadius: Metrics.controlRadius)
                            .fill(item.id == context.selectedID ? Theme.accent.opacity(0.16) : .clear)
                    )
            }
            .buttonStyle(.plain)
        }
    }

    private var backendFooter: some View {
        BackendStatusPill(backend: appState.backend, version: appState.backendVersion)
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
    }

    private func badge(for destination: AppState.Destination) -> (count: Int, severity: Severity)? {
        let attention = appState.attention
        switch destination {
        case .sync:
            if attention.syncNeedsYou { return (1, .warn) }
            if attention.syncActive, attention.syncRemaining > 0 { return (attention.syncRemaining, .info) }
            return nil
        case .freeDL:
            return attention.freeDLSelectable > 0 ? (attention.freeDLSelectable, .info) : nil
        case .rekordbox:
            return attention.rekordboxBlockers > 0 ? (attention.rekordboxBlockers, .error) : nil
        case .playlists:
            return attention.playlistsWithoutSnapshot > 0 ? (attention.playlistsWithoutSnapshot, .warn) : nil
        case .doctor:
            if attention.doctorErrors > 0 { return (attention.doctorErrors, .error) }
            return attention.doctorWarnings > 0 ? (attention.doctorWarnings, .warn) : nil
        case .credentials:
            return attention.unhealthyCredentials > 0 ? (attention.unhealthyCredentials, .error) : nil
        case .config:
            return attention.configProblems > 0 ? (attention.configProblems, .error) : nil
        case .home, .onboarding:
            return nil
        }
    }
}

/// The backend status pill pinned under the sidebar.
struct BackendStatusPill: View {
    @ObservedObject var backend: AgentProcess
    let version: String

    var body: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(severity.tint)
                .frame(width: 7, height: 7)
            VStack(alignment: .leading, spacing: 1) {
                Text(backend.state.label)
                    .font(Typography.caption)
                    .fontWeight(.semibold)
                Text("backend \(version)")
                    .font(Typography.monoSmall)
                    .foregroundStyle(Theme.textTertiary)
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }

    private var severity: Severity {
        switch backend.state {
        case .running: .ok
        case .launching: .info
        case .stopped: .idle
        case .failed, .exited: .error
        }
    }
}
