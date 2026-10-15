import SwiftUI

/// One screen for the whole lifecycle: running, cancelled, and finished are
/// states of the same view, not separate pages.
struct SyncRunView: View {
    @EnvironmentObject private var appState: AppState

    @State private var rowFilter: SyncRowFilter = .all
    /// nil means "follow udl"; a set means the user took over.
    @State private var expanded: Set<String>?
    @State private var activityExpanded = true
    @State private var confirmingStop = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                header

                if let notResumed = appState.notResumedMessage(for: .sync) {
                    // C15 — a restart never replays mutating requests.
                    Callout(title: notResumed, severity: .warn)
                }

                if let progress = appState.syncRun.progress {
                    progressCard(progress)
                }

                // Failures stay pinned above the log so they survive scrollback.
                if !failures.isEmpty {
                    failureCard
                }

                sourceSections

                activityCard

                if let message = appState.syncRun.terminalMessage {
                    Callout(title: message, severity: terminalSeverity)
                }
            }
            .padding(.horizontal, Metrics.contentPaddingHorizontal)
            .padding(.vertical, Metrics.contentPadding)
        }
        .workspaceToolbar(title: "Run Sync", subtitle: subtitle) {
            Picker("Rows", selection: $rowFilter) {
                ForEach(SyncRowFilter.allCases) { Text($0.rawValue.capitalized).tag($0) }
            }
            .frame(width: 150)

            if appState.syncRun.phase.isActive {
                Button("Stop", role: .destructive) { confirmingStop = true }
            }
        }
        .workspaceInspector { inspector }
        .udlStatusBar {
            statusSummary
        } actions: {
            if appState.syncRun.phase.isActive {
                // ⌘. is the Run menu's "Cancel Running Work" and cancels
                // directly; this button is the one that asks first.
                Button("Stop run", role: .destructive) { confirmingStop = true }
            } else {
                Button("Configure another run") { appState.resetSyncRun() }
                    .buttonStyle(.borderedProminent)
            }
        }
        .sidebarContext(sidebarContext)
        .confirmationDialog("Stop this run?", isPresented: $confirmingStop) {
            Button("Stop run", role: .destructive) {
                Task { await appState.cancelActiveSync() }
            }
            Button("Keep running", role: .cancel) {}
        } message: {
            Text("The track downloading now is discarded. Tracks already finished stay on disk and stay recorded in the state file, so a later run resumes from here.")
        }
    }

    // MARK: Header

    private var header: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 5) {
                HStack(spacing: 8) {
                    Text(title).font(Typography.sectionTitle)
                    LifecycleChip(lifecycle: runLifecycle)
                    // C2 — which source of how many udl is on right now.
                    if let position = appState.syncSourcePosition {
                        Text("Source \(position.index) of \(position.total)")
                            .font(Typography.control)
                            .foregroundStyle(Theme.textSecondary)
                    }
                }
                Text(lede)
                    .font(Typography.control)
                    .foregroundStyle(Theme.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
    }

    private var title: String {
        switch appState.syncRun.phase {
        case .idle, .starting: "Starting run"
        case .running: "Running sync"
        case .canceling: "Stopping run"
        case .succeeded: "Run finished"
        case .canceled: "Run cancelled"
        case .partialFailure, .dependencyFailure, .failed: "Run failed"
        }
    }

    private var lede: String {
        if appState.syncRun.phase.isActive {
            return "udl works one source at a time. Sources it has not reached yet report nothing at all, so they read Queued rather than showing an empty result."
        }
        return "The run is over. Its rows and activity stay on screen until you configure another run."
    }

    private var subtitle: String {
        appState.syncRun.runID.map { "sync.event · run \($0)" } ?? "sync.start"
    }

    private var runLifecycle: Lifecycle {
        switch appState.syncRun.phase {
        case .idle: .notRun
        case .starting, .running: appState.pendingPrompt != nil ? .needsYou : .running
        case .canceling: .running
        case .succeeded: .done
        case .partialFailure, .dependencyFailure, .failed: .failed
        case .canceled: .skipped
        }
    }

    private var terminalSeverity: Severity {
        switch appState.syncRun.phase {
        case .succeeded: .ok
        case .partialFailure, .dependencyFailure, .failed: .error
        default: .warn
        }
    }

    // MARK: Progress

    private func progressCard(_ snapshot: StructuredProgressSnapshot) -> some View {
        // C3 — a run blocked on a ui.* request is not a running run, and its
        // meters must not animate as if it were.
        let frozen = appState.pendingPrompt != nil
        let global = snapshot.progress.global
        let track = snapshot.track
        return Card(title: "Progress", subtitle: frozen ? "Frozen — backend paused" : nil) {
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text("Whole run").font(Typography.caption).foregroundStyle(Theme.textSecondary)
                    Spacer()
                    Text(global.total > 0 ? "\(global.completed) of \(global.total) tracks" : "Preparing run…")
                        .font(Typography.mono)
                        .foregroundStyle(Theme.textTertiary)
                }
                FrozenProgressView(
                    value: Double(max(global.completed, 0)),
                    total: Double(max(global.total, 1)),
                    isFrozen: frozen
                )
            }

            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(track.name.isEmpty ? "Current track" : track.name)
                        .font(Typography.caption)
                        .foregroundStyle(Theme.textSecondary)
                        .lineLimit(1)
                    Spacer()
                    Text(track.name.isEmpty ? "No track yet" : track.lifecycle)
                        .font(Typography.mono)
                        .foregroundStyle(Theme.textTertiary)
                }
                if track.progressKnown {
                    FrozenProgressView(
                        value: min(max(track.progressPercent, 0), 100),
                        total: 100,
                        isFrozen: frozen
                    )
                } else {
                    // Failure mode 2: "no percentage" is not "0%".
                    ConstraintNote(text: "This adapter reports no per-track percentage.")
                }
            }
        }
    }

    // MARK: Failures

    private var failures: [TrackRow] {
        appState.syncRun.requestedSourceIDs
            .compactMap { appState.syncRun.sources[$0] }
            .flatMap(\.rows)
            .filter { $0.runtimeStatus == "failed" }
    }

    private var failureCard: some View {
        Card(title: "Failures", subtitle: "Pinned above the log so scrollback cannot lose them", flush: true) {
            ForEach(failures) { row in
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 8) {
                        Image(systemName: Severity.error.symbolName)
                            .foregroundStyle(Theme.error)
                        Text(row.title).font(Typography.control).lineLimit(1)
                        Spacer(minLength: 8)
                        MonoValue(text: row.sourceLabel.isEmpty ? row.sourceID : row.sourceLabel)
                    }
                    if let detail = row.failureDetail, !detail.isEmpty {
                        Text(detail)
                            .font(Typography.mono)
                            .foregroundStyle(Theme.textSecondary)
                            .textSelection(.enabled)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .padding(.horizontal, 13)
                .padding(.vertical, 6)
                .overlay(alignment: .bottom) {
                    Rectangle().fill(Theme.separator).frame(height: 0.5)
                }
            }
        }
    }

    // MARK: Per-source sections

    private var orderedSourceIDs: [String] {
        let requested = appState.syncRun.requestedSourceIDs
        let extras = appState.syncRun.sources.keys.filter { !requested.contains($0) }.sorted()
        return requested + extras
    }

    private var sourceSections: some View {
        ForEach(orderedSourceIDs, id: \.self) { sourceID in
            let snapshot = appState.syncRun.sources[sourceID]
            let lifecycle = appState.syncSourceLifecycle(sourceID)
            Card(
                title: sourceID,
                subtitle: snapshot.map(summary) ?? "No events yet",
                flush: true
            ) {
                HStack(spacing: 8) {
                    LifecycleChip(lifecycle: lifecycle, isAnimating: appState.pendingPrompt == nil)
                    Button {
                        toggleExpanded(sourceID)
                    } label: {
                        Image(systemName: isExpanded(sourceID) ? "chevron.down" : "chevron.right")
                    }
                    .buttonStyle(.plain)
                    .help(isExpanded(sourceID) ? "Collapse \(sourceID)" : "Expand \(sourceID)")
                }
            } content: {
                if isExpanded(sourceID) {
                    sourceBody(sourceID, snapshot: snapshot, lifecycle: lifecycle)
                }
            }
        }
    }

    @ViewBuilder
    private func sourceBody(_ sourceID: String, snapshot: SourceSnapshot?, lifecycle: Lifecycle) -> some View {
        if let snapshot {
            let rows = snapshot.rows.filter(rowFilter.includes)
            if rows.isEmpty {
                EmptyStateView(
                    title: sourceID,
                    kind: .empty,
                    detail: "This source reported rows, but none match the \(rowFilter.rawValue) filter."
                )
                .frame(height: 110)
            } else {
                ForEach(rows) { row in
                    trackRow(row)
                }
            }
        } else {
            // C2 — three distinct kinds of nothing, never one grey blank.
            EmptyStateView(
                title: sourceID,
                kind: lifecycle == .queued
                    ? .blocked("Queued — udl plans and runs one source at a time and has not reached this one.")
                    : .notRun(action: "start a run"),
                detail: nil
            )
            .frame(height: 110)
        }
    }

    private func trackRow(_ row: TrackRow) -> some View {
        HStack(spacing: 8) {
            Image(systemName: icon(for: row.runtimeStatus))
                .foregroundStyle(severity(for: row.runtimeStatus).tint)
                .frame(width: 16)
            Text(row.title).font(Typography.control).lineLimit(1)
            Spacer(minLength: 8)
            if row.progressKnown, row.runtimeStatus == "downloading" {
                FrozenProgressView(
                    value: row.progressPercent,
                    total: 100,
                    isFrozen: appState.pendingPrompt != nil
                )
                .frame(width: 90)
            }
            Text(row.statusLabel)
                .font(Typography.mono)
                .foregroundStyle(Theme.textTertiary)
        }
        .padding(.horizontal, 13)
        .frame(height: Metrics.tableRowHeight)
    }

    private func summary(_ source: SourceSnapshot) -> String {
        "\(source.downloadedCount) done · \(source.skippedCount) skipped · \(source.failedCount) failed · \(source.includedCount) in run"
    }

    /// Until the user touches a disclosure, follow udl: the source it is on
    /// while the run is live, then everything that actually produced rows once
    /// the run is over.
    private var defaultExpanded: Set<String> {
        if appState.syncRun.phase.isActive {
            return appState.activeSyncSourceID.map { [$0] } ?? []
        }
        return Set(appState.syncRun.sources.keys)
    }

    private func isExpanded(_ sourceID: String) -> Bool {
        (expanded ?? defaultExpanded).contains(sourceID)
    }

    private func toggleExpanded(_ sourceID: String) {
        var next = expanded ?? defaultExpanded
        if next.contains(sourceID) { next.remove(sourceID) } else { next.insert(sourceID) }
        expanded = next
    }

    // MARK: Activity

    private var activityCard: some View {
        Card(title: "Activity", subtitle: "sync.event · OutputEvent", flush: true) {
            Button {
                activityExpanded.toggle()
            } label: {
                Image(systemName: activityExpanded ? "chevron.down" : "chevron.right")
            }
            .buttonStyle(.plain)
        } content: {
            if !activityExpanded {
                EmptyView()
            } else if appState.syncRun.activity.isEmpty {
                EmptyStateView(
                    title: "Activity",
                    kind: appState.syncRun.phase.isActive ? .working : .empty,
                    detail: appState.syncRun.phase.isActive
                        ? "Waiting for the first sync.event…"
                        : "The run produced no output events."
                )
                .frame(height: 100)
            } else {
                ForEach(Array(appState.syncRun.activity.suffix(80).enumerated()), id: \.offset) { _, event in
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(event.level.uppercased())
                            .font(Typography.monoSmall)
                            .foregroundStyle(activitySeverity(event).tint)
                            .frame(width: 52, alignment: .leading)
                        Text(event.message)
                            .font(Typography.mono)
                            .textSelection(.enabled)
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 13)
                    .padding(.vertical, 2)
                }
            }
        }
    }

    private func activitySeverity(_ event: OutputEvent) -> Severity {
        switch event.level.lowercased() {
        case "error", "fatal": .error
        case "warn", "warning": .warn
        case "info": .info
        default: .idle
        }
    }

    // MARK: Sidebar

    /// C2 — the same sequence the plan surface shows, so the sidebar does not
    /// change shape when udl moves between planning and executing.
    private var sidebarContext: SidebarContext? {
        let ids = appState.syncRun.requestedSourceIDs
        guard !ids.isEmpty else { return nil }
        let items = ids.map { sourceID -> SidebarContextItem in
            let lifecycle = appState.syncSourceLifecycle(sourceID)
            let capability = appState.syncCapability(forSourceID: sourceID)
            return SidebarContextItem(
                id: sourceID,
                title: sourceID,
                subtitle: capability?.adapter,
                sourceType: capability?.sourceType,
                lifecycle: lifecycle,
                unavailableReason: appState.syncRun.sources[sourceID] == nil
                    ? "udl plans one source at a time."
                    : nil
            )
        }
        return SidebarContext(
            title: "Run sources",
            items: items,
            selectedID: expanded?.first ?? appState.activeSyncSourceID,
            note: nil,
            select: { expanded = [$0] }
        )
    }

    // MARK: Inspector

    @ViewBuilder private var inspector: some View {
        InspectorSection(title: "This run") {
            FieldRow("Phase", appState.syncRun.phase.rawValue)
            FieldRow("Mode", appState.syncDryRun ? "Dry run" : "Live run")
            FieldRow("Sources", "\(appState.syncRun.requestedSourceIDs.count)")
            FieldRow("Plan limit", appState.syncUnlimited ? "∞ (0)" : "\(appState.syncPlanLimit)")
            FieldRow("Plan window", appState.syncPlanWindow.label)
            FieldRow("Timeout", appState.syncTimeoutSeconds == 0 ? "None" : "\(appState.syncTimeoutSeconds)s")
            if let runID = appState.syncRun.runID {
                FieldRow(label: "Run ID") { MonoValue(text: runID) }
            }
            if let exitCode = appState.syncRun.exitCode {
                FieldRow("Exit code", "\(exitCode)")
            }
        }

        InspectorSection(title: "Advanced") {
            FieldRow("On existing", appState.syncAskOnExisting.label)
            FieldRow("Scan gaps", appState.syncScanGaps ? "Yes" : "No")
            FieldRow("Preflight", appState.syncNoPreflight ? "Skipped" : "Run")
            FieldRow("Track status", appState.syncTrackStatus.label)
            // C17
            ConstraintNote(text: "Fixed when sync.start was called. Change them on the configure screen and start another run.")
        }

        InspectorSection(title: "Totals") {
            FieldRow("Downloaded", "\(total(\.downloadedCount))")
            FieldRow("Skipped", "\(total(\.skippedCount))")
            FieldRow("Failed", "\(total(\.failedCount))")
            FieldRow("In run", "\(total(\.includedCount))")
        }

        // C16 — the protocol reports no throughput, elapsed time, or byte
        // count, so the mockup's Throughput panel is absent, not estimated.
        InspectorSection(title: "Not available") {
            ConstraintNote(text: "udl reports no elapsed time, transfer rate, or byte count, so none is shown.")
        }
    }

    private func total(_ keyPath: KeyPath<SourceSnapshot, Int>) -> Int {
        appState.syncRun.sources.values.reduce(0) { $0 + $1[keyPath: keyPath] }
    }

    // MARK: Status bar

    private var statusSummary: some View {
        SummaryLine {
            if appState.pendingPrompt != nil {
                Text("Backend paused — waiting for your answer")
                    .foregroundStyle(Theme.accent)
            } else {
                SummaryCount(value: total(\.downloadedCount), noun: "done", severity: .ok)
                Text("·")
                SummaryCount(value: total(\.skippedCount), noun: "skipped")
                Text("·")
                SummaryCount(value: total(\.failedCount), noun: "failed", severity: failures.isEmpty ? .idle : .error)
                if let progress = appState.syncRun.progress?.progress.global, progress.total > 0 {
                    Text("·")
                    SummaryCount(
                        value: max(progress.total - progress.completed, 0),
                        noun: "remaining",
                        severity: .info
                    )
                }
            }
        }
    }

    // MARK: Row vocabulary

    private func icon(for status: String) -> String {
        switch status {
        case "downloaded": "checkmark.circle.fill"
        case "failed": "xmark.octagon.fill"
        case "skipped": "forward.circle.fill"
        case "downloading": "arrow.down.circle.fill"
        default: "circle"
        }
    }

    private func severity(for status: String) -> Severity {
        switch status {
        case "downloaded": .ok
        case "failed": .error
        case "skipped": .warn
        case "downloading": .info
        default: .idle
        }
    }
}
