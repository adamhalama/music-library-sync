import SwiftUI

/// Configuration is the only pre-run screen. Once a run starts, one persistent
/// source-order table remains mounted from planning through terminal results.
struct SyncView: View {
    @EnvironmentObject private var appState: AppState

    var body: some View {
        if appState.syncRun.phase.isActive || appState.syncRun.exitCode != nil || !appState.syncRun.sourceTables.isEmpty {
            SyncUnifiedWorkspaceView()
        } else {
            SyncConfigureView()
        }
    }
}

private enum UnifiedSyncFilter: String, CaseIterable, Identifiable {
    case all
    case inRun = "in run"
    case remaining
    case downloaded
    case skipped
    case failed
    case have
    case new
    case gaps

    var id: String { rawValue }

    func includes(_ row: TrackRow) -> Bool {
        switch self {
        case .all: true
        case .inRun: row.runScope == "included"
        case .remaining: row.runScope == "included" && ["idle", "queued", "downloading"].contains(row.runtimeStatus)
        case .downloaded: row.runtimeStatus == "downloaded"
        case .skipped: row.runtimeStatus == "skipped"
        case .failed: row.runtimeStatus == "failed"
        case .have: row.runScope == "locked"
        case .new: row.planClass == "new"
        case .gaps: row.planClass == "gap" || row.planClass == "known_gap"
        }
    }
}

/// The accepted plan is the runtime table. It is never replaced by a second
/// row presentation, and browsing state remains available while mutation is
/// locked.
private struct SyncUnifiedWorkspaceView: View {
    @EnvironmentObject private var appState: AppState
    @EnvironmentObject private var chrome: ShellChrome

    @State private var filter: UnifiedSyncFilter = .all
    @State private var cursor: TrackRow.ID?
    @State private var sortOrder = [KeyPathComparator(\TrackRow.index)]
    @State private var confirmingStop = false

    private var sourceID: String? {
        if let selected = appState.selectedSyncSourceID { return selected }
        if let active = appState.activeSyncSourceID { return active }
        return appState.syncRun.requestedSourceIDs.first
    }

    private var table: SyncSourceTableState? {
        sourceID.flatMap { appState.syncRun.sourceTables[$0] }
    }

    var body: some View {
        BoundedContent {
            VStack(alignment: .leading, spacing: 0) {
                header
                if let message = appState.syncRun.terminalMessage {
                    Callout(title: message, severity: terminalSeverity)
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.bottom, 8)
                }
                if appState.planPrompt?.params.sourceID == sourceID {
                    WaitingBanner(
                        detail: "udl is paused on this source. Review the queue and Continue, rebuild the window, or Stop the run."
                    )
                    .padding(.horizontal, Metrics.contentPaddingHorizontal)
                    .padding(.bottom, 8)
                } else if let table, !isMutable(table) {
                    ConstraintNote(text: table.accepted
                        ? "Accepted queue locked — selection, order, window, reset, and rebuild controls cannot change during this run."
                        : "This run is no longer waiting for a selection. The retained table is read-only.")
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.bottom, 8)
                }
                tableBody
            }
        }
        .workspaceToolbar(
            title: sourceID ?? "Run Sync",
            subtitle: appState.syncRun.runID.map { "protocol v2 · run \($0)" } ?? "sync.start",
            searchable: true,
            searchPrompt: "Filter by title or remote ID"
        ) {
            Picker("Rows", selection: $filter) {
                ForEach(filters) { Text($0.rawValue.capitalized).tag($0) }
            }
            .frame(width: 170)
            if appState.syncRun.phase.isActive {
                Button("Stop", role: .destructive) { confirmingStop = true }
            }
        }
        .workspaceInspector { inspector }
        .udlStatusBar { statusSummary } actions: { actions }
        .sidebarContext(sidebarContext)
        .confirmationDialog("Stop this run?", isPresented: $confirmingStop) {
            Button("Stop run", role: .destructive) {
                Task { await appState.cancelActiveSync() }
            }
            Button("Keep running", role: .cancel) {}
        } message: {
            Text("The track downloading now is discarded. Tracks already finished stay on disk and stay recorded in the state file, so a later run resumes from here.")
        }
        .onDisappear { chrome.searchText = "" }
        .onChange(of: table?.accepted) { _, accepted in
            filter = accepted == true ? .all : .all
        }
    }

    private var header: some View {
        HStack(alignment: .top, spacing: 10) {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) {
                    Text(sourceID ?? "Preparing run").font(Typography.sectionTitle)
                    LifecycleChip(lifecycle: sourceID.map(appState.syncSourceLifecycle) ?? .queued)
                    if let sourceID, let position = appState.syncSourcePosition(for: sourceID) {
                        Text("Source \(position.index) of \(position.total)")
                            .font(Typography.control)
                            .foregroundStyle(Theme.textSecondary)
                    }
                }
                Text(table.map { table in
                    if isMutable(table) { return "Select tracks in source order. Queue positions update immediately." }
                    if table.accepted { return "Source order stays fixed; run # is the authoritative execution order." }
                    return "The run ended before this queue was accepted; the source-order table is retained read-only."
                } ?? "Preparing the source table.")
                    .font(Typography.control)
                    .foregroundStyle(Theme.textSecondary)
            }
            Spacer(minLength: 0)
            if let progress = appState.syncRun.progress?.track, !progress.name.isEmpty {
                Text("\(progress.name) · \(Int(progress.progressPercent))%")
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
                    .lineLimit(1)
            }
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 12)
    }

    @ViewBuilder private var tableBody: some View {
        if let table, !table.rows.isEmpty {
            Table(filteredRows(table), selection: $cursor, sortOrder: $sortOrder) {
                TableColumn("") { row in selectionControl(row, table: table) }.width(30)
                TableColumn("#", value: \.index) { row in
                    Text(row.index, format: .number).font(Typography.mono).foregroundStyle(Theme.textTertiary)
                }.width(38)
                TableColumn("Status", value: \.statusLabel) { row in
                    StatusPill(title: row.statusLabel, severity: severity(row))
                }.width(min: 90, ideal: 110)
                TableColumn("Class", value: \.planClass) { row in
                    Text(row.planClass).font(Typography.mono).foregroundStyle(Theme.textTertiary)
                }.width(min: 54, ideal: 68)
                TableColumn("Run") { row in
                    Text(row.executionSlot > 0 ? "run #\(row.executionSlot)" : "—")
                        .font(Typography.mono)
                        .foregroundStyle(row.executionSlot > 0 ? Theme.accent : Theme.textTertiary)
                }.width(min: 60, ideal: 72)
                TableColumn("Title", value: \.title) { row in
                    Text(row.title).font(Typography.control).lineLimit(1)
                }
                TableColumn("Remote ID", value: \.remoteID) { row in MonoValue(text: row.remoteID) }
                    .width(min: 110, ideal: 170)
            }
            .tableStyle(.inset(alternatesRowBackgrounds: true))
            .frame(maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
        } else if let table, !table.hasTrackPlan {
            EmptyStateView(
                title: sourceID ?? "Source",
                kind: .empty,
                detail: "This adapter exposes no track plan. Source-level lifecycle and activity still update here."
            )
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            EmptyStateView(
                title: sourceID ?? "Preparing source",
                kind: appState.syncRun.phase.isActive
                    ? .blocked("Queued — udl has not reached this source yet.")
                    : .notRun(action: "configure another run"),
                detail: nil
            )
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    @ViewBuilder private func selectionControl(_ row: TrackRow, table: SyncSourceTableState) -> some View {
        if isMutable(table) && row.toggleable {
            Toggle("", isOn: Binding(
                get: { appState.syncRun.sourceTables[table.sourceID]?.selectedIndices.contains(row.index) == true },
                set: { selected in
                    var indices = appState.syncRun.sourceTables[table.sourceID]?.selectedIndices ?? []
                    if selected { indices.insert(row.index) } else { indices.remove(row.index) }
                    appState.setSyncTableSelection(sourceID: table.sourceID, selectedIndices: indices)
                }
            )).labelsHidden()
        } else {
            Image(systemName: row.runScope == "locked" ? "lock.fill" : row.selected ? "checkmark.circle.fill" : "circle")
                .foregroundStyle(row.selected ? Theme.accent : Theme.textTertiary)
                .accessibilityLabel(table.accepted ? "Queue accepted and locked" : "Row locked")
        }
    }

    private func filteredRows(_ table: SyncSourceTableState) -> [TrackRow] {
        let query = chrome.searchText.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return table.rows
            .filter(filter.includes)
            .filter { query.isEmpty || $0.title.lowercased().contains(query) || $0.remoteID.lowercased().contains(query) }
            .sorted(using: sortOrder)
    }

    private var filters: [UnifiedSyncFilter] {
        table?.accepted == true
            ? [.all, .inRun, .remaining, .downloaded, .skipped, .failed, .have]
            : [.all, .new, .gaps, .have]
    }

    @ViewBuilder private var inspector: some View {
        if let table {
            let capability = appState.syncCapability(forSourceID: table.sourceID)
            InspectorSection(title: "Queue") {
                Picker("Download order", selection: Binding(
                    get: { appState.syncRun.sourceTables[table.sourceID]?.downloadOrder ?? table.downloadOrder },
                    set: { appState.setSyncTableDownloadOrder(sourceID: table.sourceID, order: $0) }
                )) { ForEach(DownloadOrder.allCases) { Text($0.label).tag($0) } }
                .constrained(by: table.accepted
                    ? "The accepted queue fixes download order for this run."
                    : !isMutable(table) ? "This run is no longer waiting for a queue reply."
                    : capability?.supportsDownloadOrder == false ? "This adapter fixes download order." : nil)

                Picker("Plan window", selection: Binding(
                    get: { appState.syncRun.sourceTables[table.sourceID]?.planWindow ?? table.planWindow },
                    set: { appState.setSyncTablePlanWindow(sourceID: table.sourceID, window: $0) }
                )) { ForEach(PlanWindow.allCases) { Text($0.label).tag($0) } }
                .constrained(by: table.accepted
                    ? "The accepted queue fixes the plan window for this run."
                    : !isMutable(table) ? "This run is no longer waiting for a queue reply."
                    : capability?.supportsPlanWindow == false ? "This adapter exposes no plan window." : nil)

                FieldRow("Selected", "\(table.selectedIndices.count) of \(table.rows.count)")
                FieldRow("Lifecycle", table.lifecycle)
            }
        }
        InspectorSection(title: "Run") {
            FieldRow("Phase", appState.syncRun.phase.rawValue)
            FieldRow("Mode", appState.syncDryRun ? "Dry run" : "Live run")
            if let exit = appState.syncRun.exitCode { FieldRow("Exit code", "\(exit)") }
        }
    }

    @ViewBuilder private var actions: some View {
        if let table, !table.accepted,
           let prompt = appState.planPrompt, prompt.params.sourceID == table.sourceID {
            Button("Reset to defaults") {
                appState.resetPlanSelection(sourceID: table.sourceID)
                appState.setSyncTableSelection(
                    sourceID: table.sourceID,
                    selectedIndices: Set(table.planRows.filter(\.selectedByDefault).map(\.index))
                )
                appState.setSyncTableDownloadOrder(sourceID: table.sourceID, order: prompt.params.downloadOrder)
                appState.setSyncTablePlanWindow(sourceID: table.sourceID, window: prompt.params.planWindow)
            }
            Button(table.planWindow == prompt.params.planWindow ? "Continue" : "Rebuild plan") {
                submit(table, prompt: prompt)
            }
            .buttonStyle(.borderedProminent)
            .keyboardShortcut(.defaultAction)
        } else if appState.syncRun.phase.isActive {
            Button("Stop run", role: .destructive) { confirmingStop = true }
        } else {
            Button("Configure another run") { appState.resetSyncRun() }
                .buttonStyle(.borderedProminent)
        }
    }

    private func submit(_ table: SyncSourceTableState, prompt: AppState.PlanPrompt) {
        let rebuild = table.planWindow != prompt.params.planWindow
        appState.rememberPlanSelection(prompt.params, selectedIndices: table.selectedIndices, cursor: cursor)
        let result = SelectRowsResult(
            selectedIndices: rebuild ? [] : table.selectedIndices.sorted(),
            downloadOrder: table.downloadOrder,
            canceled: false,
            rebuild: rebuild,
            planWindow: table.planWindow
        )
        Task { await appState.answerPlanSelection(result, request: prompt.request) }
    }

    private func isMutable(_ table: SyncSourceTableState) -> Bool {
        !table.accepted
            && appState.syncRun.phase.isActive
            && appState.planPrompt?.params.sourceID == table.sourceID
    }

    private var sidebarContext: SidebarContext? {
        let ids = appState.syncRun.requestedSourceIDs
        guard !ids.isEmpty else { return nil }
        return SidebarContext(
            title: "Run sources",
            items: ids.map { id in
                let capability = appState.syncCapability(forSourceID: id)
                return SidebarContextItem(
                    id: id, title: id, subtitle: capability?.adapter,
                    sourceType: capability?.sourceType,
                    lifecycle: appState.syncSourceLifecycle(id),
                    unavailableReason: nil
                )
            },
            selectedID: sourceID,
            note: nil,
            select: appState.selectSyncSource
        )
    }

    private var statusSummary: some View {
        SummaryLine {
            if let table {
                SummaryCount(value: table.rows.filter { $0.runScope == "included" }.count, noun: "in run", severity: .info)
                Text("·")
                SummaryCount(value: table.rows.filter { $0.runtimeStatus == "downloaded" }.count, noun: "downloaded", severity: .ok)
                Text("·")
                SummaryCount(value: table.rows.filter { $0.runtimeStatus == "failed" }.count, noun: "failed", severity: .error)
            } else {
                Text("Waiting for source plan or lifecycle event").foregroundStyle(Theme.textSecondary)
            }
        }
    }

    private func severity(_ row: TrackRow) -> Severity {
        switch row.runtimeStatus {
        case "downloaded": .ok
        case "failed": .error
        case "skipped": .warn
        case "downloading": .info
        default: row.runScope == "included" ? .info : .idle
        }
    }

    private var terminalSeverity: Severity {
        switch appState.syncRun.phase {
        case .succeeded: .ok
        case .failed, .partialFailure, .dependencyFailure: .error
        default: .warn
        }
    }
}

/// The pre-run surface. C1: there is no plan preview, so the only thing this
/// screen can do is choose sources and options and start a run.
struct SyncConfigureView: View {
    @EnvironmentObject private var appState: AppState

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                WorkspaceHeader(
                    title: "Run Sync",
                    // C1 — the plan lives inside a run and nowhere else.
                    lede: "udl has no plan preview. Planning happens inside a run, one source at a time, and the backend waits for your selection at each source. A dry run resolves every track and reports the outcome without writing files or touching the state file."
                )

                if let notResumed = appState.notResumedMessage(for: .sync) {
                    // C15 — a restart never replays mutating requests.
                    Callout(title: notResumed, severity: .warn)
                }

                Card(title: "Sources", subtitle: "sources.capabilities", flush: true) {
                    if appState.syncSources.isEmpty {
                        EmptyStateView(
                            title: "No sources",
                            kind: .empty,
                            detail: "sources.capabilities returned no configured sync sources."
                        )
                        .frame(height: 140)
                    } else {
                        ForEach(appState.syncSources) { source in
                            sourceRow(source)
                        }
                    }
                }

                if let validation = appState.syncValidationMessage {
                    Callout(title: validation, severity: .warn)
                }
            }
            .padding(.horizontal, Metrics.contentPaddingHorizontal)
            .padding(.vertical, Metrics.contentPadding)
        }
        .workspaceToolbar(title: "Run Sync", subtitle: "sync.start · plan: true")
        .workspaceInspector { inspector }
        .udlStatusBar {
            statusSummary
        } actions: {
            Button("Check system") { appState.destination = .doctor }
            Button(startLabel) { Task { await appState.startSync() } }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
        }
        .task {
            // Startup preloads this list. Avoid an immediate duplicate RPC
            // while the navigation view is still reconciling its controls.
            if appState.syncSources.isEmpty {
                await appState.loadSyncSources()
            }
        }
    }

    /// C1 — the button says which kind of run it starts, and dry run is the
    /// default, so the plan is always reachable through a reversible action.
    private var startLabel: String {
        appState.syncDryRun ? "Start dry run & plan" : "Start sync & plan"
    }

    private var selectedCount: Int {
        appState.syncSources.filter { appState.syncSourceOptions[$0.id]?.selected == true }.count
    }

    private func sourceRow(_ source: SourceCapability) -> some View {
        let options = appState.syncSourceOptions[source.id]
        return HStack(spacing: 12) {
            Toggle(
                "",
                isOn: Binding(
                    get: { options?.selected ?? false },
                    set: { appState.setSourceSelected(source.id, $0) }
                )
            )
            .labelsHidden()
            SourceGlyph(sourceType: source.sourceType)
            VStack(alignment: .leading, spacing: 1) {
                Text(source.sourceID).font(Typography.control).fontWeight(.medium)
                Text("\(source.sourceType) · \(source.adapter)")
                    .font(Typography.monoSmall)
                    .foregroundStyle(Theme.textTertiary)
            }
            Spacer(minLength: 8)

            if !source.supportsPlan {
                // C5 — a source that cannot be planned still runs; it just
                // never asks. Saying so beats a mystery missing prompt.
                ConstraintNote(text: "\(source.adapter) has no plan step, so this source never asks.")
            }

            // C5 — unsupported controls stay visible, disabled, and explained.
            Picker(
                "Window",
                selection: Binding(
                    get: { options?.planWindow ?? source.defaultPlanWindow },
                    set: { appState.setSourcePlanWindow(source.id, $0) }
                )
            ) {
                ForEach(PlanWindow.allCases) { Text($0.label).tag($0) }
            }
            .frame(width: 150)
            .constrained(by: source.supportsPlanWindow ? nil : "\(source.adapter) has no first/latest window.")

            Picker(
                "Order",
                selection: Binding(
                    get: { options?.downloadOrder ?? source.defaultDownloadOrder },
                    set: { appState.setSourceDownloadOrder(source.id, $0) }
                )
            ) {
                ForEach(DownloadOrder.allCases) { Text($0.label).tag($0) }
            }
            .frame(width: 180)
            .constrained(by: source.supportsDownloadOrder ? nil : "\(source.adapter) fixes the download order.")
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Theme.separator).frame(height: 0.5)
        }
    }

    @ViewBuilder private var inspector: some View {
        InspectorSection(title: "Run") {
            Toggle("Dry run", isOn: $appState.syncDryRun)
            Toggle("Unlimited plan", isOn: $appState.syncUnlimited)
            Stepper(
                "Plan limit: \(appState.syncUnlimited ? "∞" : String(appState.syncPlanLimit))",
                value: $appState.syncPlanLimit,
                in: 1...10_000
            )
            .constrained(by: appState.syncUnlimited ? "Unlimited is on, so no limit is sent." : nil)
            if appState.syncUnlimited {
                // C6 — unlimited is encoded as plan limit 0 on the wire.
                ConstraintNote(text: "∞ is sent as plan_limit: 0.")
            }
            FieldRow(label: "Timeout") {
                TextField("", value: $appState.syncTimeoutSeconds, format: .number)
                    .textFieldStyle(.roundedBorder)
                    .frame(width: 76)
            }
            ConstraintNote(text: "Timeout is in seconds; 0 means no timeout.")
        }

        // C17 — every one of these is in SyncStartParams and used to be
        // hardcoded in AppState.startSync(), which meant the GUI decided
        // silently on the user's behalf.
        InspectorSection(title: "Advanced") {
            Picker("Plan window", selection: $appState.syncPlanWindow) {
                ForEach(PlanWindow.allCases) { Text($0.label).tag($0) }
            }
            ConstraintNote(text: "The run-wide default. A per-source window above overrides it.")

            Picker("On existing files", selection: $appState.syncAskOnExisting) {
                ForEach(AskOnExistingPolicy.allCases) { Text($0.label).tag($0) }
            }
            ConstraintNote(text: "“udl decides” sends ask_on_existing_set: false and leaves the choice to the backend.")

            Toggle("Scan for gaps", isOn: $appState.syncScanGaps)
            Toggle("Skip preflight", isOn: $appState.syncNoPreflight)

            Picker("Existing-track status", selection: $appState.syncTrackStatus) {
                ForEach(TrackStatusMode.allCases) { Text($0.label).tag($0) }
            }
            ConstraintNote(text: "Spotify + deemix only; other adapters ignore track_status.")

            Button("Reset advanced to udl defaults") { appState.resetSyncAdvanced() }
                .buttonStyle(.link)
        }

        InspectorSection(title: "Sources") {
            FieldRow("Configured", "\(appState.syncSources.count)")
            FieldRow("Selected", "\(selectedCount)")
            FieldRow("Plan-capable", "\(appState.syncSources.filter(\.supportsPlan).count)")
            // C2 — set expectations before the run rather than after it stalls.
            ConstraintNote(text: "udl plans one source at a time; each source asks for its selection in turn and the backend waits.")
        }
    }

    private var statusSummary: some View {
        SummaryLine {
            SummaryCount(value: selectedCount, noun: "selected", severity: selectedCount > 0 ? .ok : .warn)
            Text("·")
            SummaryCount(value: appState.syncSources.count, noun: "configured")
            Text("·")
            Text(appState.syncDryRun ? "Dry run — nothing is written" : "Live run — files are written")
                .foregroundStyle(appState.syncDryRun ? Theme.textSecondary : Theme.warn)
        }
    }
}
