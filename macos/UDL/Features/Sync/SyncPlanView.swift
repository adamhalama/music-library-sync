import SwiftUI

/// C1/C2/C3 — the plan, docked.
///
/// This is `PlanSelectionPromptView` moved out of a 900×610 sheet and into the
/// Sync workspace. The data and the reply path are identical: the same decoded
/// `SelectRowsParams`, the same `answerPlanSelection` call, the same
/// `initialPlanSelection` / `rememberPlanSelection` overrides across rebuilds.
/// What changes is that the plan is now a place you work — with the sidebar
/// showing where udl is in the sequence, and the status bar showing that the
/// backend is stopped while you look at it.
struct SyncPlanView: View {
    @EnvironmentObject private var appState: AppState
    @EnvironmentObject private var chrome: ShellChrome

    @State private var selected: Set<Int> = []
    @State private var cursor: PlanRow.ID?
    @State private var order: DownloadOrder = .newestFirst
    @State private var window: PlanWindow = .first
    @State private var filter: PlanRowFilter = .all
    @State private var sortOrder = [KeyPathComparator(\PlanRow.index)]
    /// C-principle: a locked row that does nothing when clicked is a defect.
    /// Clicking the lock states the backend's reason here.
    @State private var lockedReason: String?

    var body: some View {
        if let prompt = appState.planPrompt {
            content(prompt)
                .workspaceToolbar(
                    title: prompt.params.sourceID,
                    subtitle: "ui.selectRows · run \(prompt.params.runID)",
                    searchable: true,
                    searchPrompt: "Filter by title or remote ID"
                ) {
                    Picker("Rows", selection: $filter) {
                        ForEach(PlanRowFilter.allCases) { option in
                            Text("\(option.label) \(count(of: option, in: prompt))").tag(option)
                        }
                    }
                    .pickerStyle(.segmented)
                    .frame(width: 300)

                    Button("Stop run", role: .destructive) {
                        Task { await appState.cancelActiveSync() }
                    }
                }
                .workspaceInspector { inspector(prompt) }
                .udlStatusBar {
                    statusSummary(prompt)
                } actions: {
                    Button("Reset to defaults") { resetToDefaults(prompt) }
                    primaryButton(prompt)
                }
                .sidebarContext(sidebarContext)
                .task(id: prompt.id) { load(prompt) }
                .onDisappear { chrome.searchText = "" }
        }
    }

    // MARK: Content

    /// `BoundedContent` gives the stack the definite height a `Table` needs in
    /// order to scroll instead of expanding through the toolbar and the status
    /// bar. See its documentation for why the safe-area inset matters.
    private func content(_ prompt: AppState.PlanPrompt) -> some View {
        BoundedContent {
            VStack(alignment: .leading, spacing: 0) {
                // C3 — Go is blocked on this request. This is the loudest thing
                // on the screen, because a stalled backend must never look like
                // a working one.
                WaitingBanner(
                    detail: "udl planned this source and stopped. Nothing downloads, and no other source is planned, until you continue or stop the run."
                )

                header(prompt)

                if let progress = appState.syncRun.progress {
                    frozenProgress(progress)
                }

                planTable(prompt)

                if let lockedReason {
                    ConstraintNote(text: lockedReason, severity: .warn)
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.vertical, 8)
                }
            }
        }
        .background(shortcuts(prompt))
    }

    private func header(_ prompt: AppState.PlanPrompt) -> some View {
        HStack(alignment: .top, spacing: 12) {
            SourceGlyph(sourceType: prompt.params.details.sourceType)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 8) {
                    Text(prompt.params.sourceID).font(Typography.sectionTitle)
                    LifecycleChip(lifecycle: .needsYou)
                    // C2 — where udl is in the sequence, from the source list
                    // that was actually sent to sync.start.
                    if let position = appState.syncSourcePosition {
                        Text("Source \(position.index) of \(position.total)")
                            .font(Typography.control)
                            .foregroundStyle(Theme.textSecondary)
                    }
                }
                Text("\(prompt.params.details.sourceType) · \(prompt.params.details.adapter) · \(prompt.params.details.dryRun ? "dry run" : "live run")")
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 12)
    }

    /// C3 — the meters render greyed and frozen while the backend is blocked.
    private func frozenProgress(_ snapshot: StructuredProgressSnapshot) -> some View {
        let global = snapshot.progress.global
        return VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text("Whole run").font(Typography.caption).foregroundStyle(Theme.textSecondary)
                Spacer()
                Text(global.total > 0 ? "\(global.completed) of \(global.total) tracks · paused" : "Paused before any download")
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
            }
            FrozenProgressView(
                value: Double(max(global.completed, 0)),
                total: Double(max(global.total, 1)),
                isFrozen: true
            )
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.bottom, 10)
    }

    private func planTable(_ prompt: AppState.PlanPrompt) -> some View {
        Table(rows(prompt), selection: $cursor, sortOrder: $sortOrder) {
            TableColumn("") { row in
                toggle(row)
            }
            .width(30)

            TableColumn("#", value: \.index) { row in
                Text(row.index + 1, format: .number)
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
            }
            .width(38)

            TableColumn("Title", value: \.title) { row in
                Text(row.title)
                    .font(Typography.control)
                    .lineLimit(1)
            }

            TableColumn("Status", value: \.status) { row in
                StatusPill(title: row.statusLabel, severity: row.statusSeverity)
            }
            .width(min: 84, ideal: 96)

            TableColumn("Remote ID") { row in
                MonoValue(text: row.remoteID.isEmpty ? row.remoteURL : row.remoteID)
            }
            .width(min: 120, ideal: 180)
        }
        .tableStyle(.inset(alternatesRowBackgrounds: true))
        // A Table reports the ideal height of every row it holds. Without an
        // explicit flexible frame the enclosing VStack over-commits and a long
        // plan draws straight through the toolbar and the sidebar.
        .frame(maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
        .onChange(of: cursor) { _, _ in lockedReason = nil }
    }

    @ViewBuilder
    private func toggle(_ row: PlanRow) -> some View {
        if row.toggleable {
            Toggle(
                "",
                isOn: Binding(
                    get: { selected.contains(row.index) },
                    set: { value in
                        if value { selected.insert(row.index) } else { selected.remove(row.index) }
                    }
                )
            )
            .labelsHidden()
        } else {
            // A disabled checkbox swallows the click and explains nothing.
            // This one answers the question the click was asking.
            Button {
                lockedReason = row.lockReason
            } label: {
                Image(systemName: "lock.fill")
                    .foregroundStyle(Theme.textTertiary)
            }
            .buttonStyle(.plain)
            .help(row.lockReason)
            .accessibilityLabel("Locked: \(row.lockReason)")
        }
    }

    /// Space toggles the row under the cursor; ⌘A selects every toggleable row.
    private func shortcuts(_ prompt: AppState.PlanPrompt) -> some View {
        ZStack {
            Button("Toggle row") { toggleCursorRow(prompt) }
                .keyboardShortcut(.space, modifiers: [])
            Button("Select all") {
                selected = Set(prompt.params.rows.filter(\.toggleable).map(\.index))
            }
            .keyboardShortcut("a", modifiers: .command)
        }
        .opacity(0)
        .frame(width: 0, height: 0)
        .accessibilityHidden(true)
    }

    // MARK: Inspector

    @ViewBuilder
    private func inspector(_ prompt: AppState.PlanPrompt) -> some View {
        let details = prompt.params.details
        let capability = appState.syncCapability(forSourceID: prompt.params.sourceID)
        let supportsWindow = capability?.supportsPlanWindow ?? (details.adapter == "deemix")
        let supportsOrder = capability?.supportsDownloadOrder ?? true

        InspectorSection(title: "Your reply") {
            Picker("Download order", selection: $order) {
                ForEach(DownloadOrder.allCases) { Text($0.label).tag($0) }
            }
            // C5 — rendered, disabled, and explained; never hidden.
            .constrained(by: supportsOrder ? nil : "\(details.adapter) fixes the download order.")

            Picker("Plan window", selection: $window) {
                ForEach(PlanWindow.allCases) { Text($0.label).tag($0) }
            }
            .constrained(by: supportsWindow ? nil : "\(details.adapter) has no first/latest window.")

            if supportsWindow {
                // C4 — a window change is not a filter. It re-plans the source.
                ConstraintNote(
                    text: "Changing the window re-plans this source and clears your selection.",
                    severity: window == prompt.params.planWindow ? .info : .warn
                )
            }
            FieldRow("Selected", "\(selected.count) of \(prompt.params.rows.count)")
        }

        InspectorSection(title: "Sent with this run") {
            FieldRow("Plan limit", details.planLimit == 0 ? "∞ (0)" : "\(details.planLimit)")
            if details.planLimit == 0 {
                // C6
                ConstraintNote(text: "∞ was sent as plan_limit: 0.")
            }
            FieldRow("Mode", details.dryRun ? "Dry run" : "Live run")
            FieldRow("Run-wide window", appState.syncPlanWindow.label)
            FieldRow("Timeout", appState.syncTimeoutSeconds == 0 ? "None" : "\(appState.syncTimeoutSeconds)s")
            FieldRow("On existing", appState.syncAskOnExisting.label)
            FieldRow("Scan gaps", appState.syncScanGaps ? "Yes" : "No")
            FieldRow("Preflight", appState.syncNoPreflight ? "Skipped" : "Run")
            FieldRow("Track status", appState.syncTrackStatus.label)
            // C17 — these were fixed when sync.start was called and the reply
            // cannot carry them, so they are shown, not offered.
            ConstraintNote(text: "sync.start fixed these for the whole run. Only the order and window above travel back in this reply.")
        }

        InspectorSection(title: "Paths") {
            PathField(label: "Target", path: details.targetDir)
            PathField(label: "State", path: details.stateFile)
            PathField(label: "URL", path: details.url)
        }

        InspectorSection(title: "Columns") {
            // Failure mode 3: the mockup's Artist and Time columns have no
            // protocol source, so they are absent rather than invented.
            ConstraintNote(text: "ui.selectRows sends index, title, status, remote ID and remote URL. There is no artist or duration field, so those columns are not shown.")
        }
    }

    // MARK: Status bar

    private func statusSummary(_ prompt: AppState.PlanPrompt) -> some View {
        SummaryLine {
            Text("Backend paused — waiting for your selection")
                .foregroundStyle(Theme.accent)
            Text("·")
            SummaryCount(value: selected.count, noun: "queued", severity: selected.count > 0 ? .ok : .idle)
            Text("·")
            SummaryCount(value: count(of: .new, in: prompt), noun: "new", severity: .info)
            Text("·")
            SummaryCount(value: count(of: .gaps, in: prompt), noun: "gaps", severity: .warn)
            Text("·")
            SummaryCount(value: count(of: .have, in: prompt), noun: "have", severity: .ok)
        }
    }

    @ViewBuilder
    private func primaryButton(_ prompt: AppState.PlanPrompt) -> some View {
        if window == prompt.params.planWindow {
            Button("Continue") { submit(prompt, rebuild: false) }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
        } else {
            // C4 — once the value differs, the only honest primary action is
            // the one that discards the selection and asks udl again.
            Button("Rebuild plan") { submit(prompt, rebuild: true) }
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.defaultAction)
        }
    }

    // MARK: Sidebar

    /// C2 — the sequence, with exactly one source able to read `Needs you`.
    private var sidebarContext: SidebarContext? {
        let ids = appState.syncRun.requestedSourceIDs
        guard !ids.isEmpty else { return nil }
        let active = appState.activeSyncSourceID
        let items = ids.map { sourceID -> SidebarContextItem in
            let lifecycle = appState.syncSourceLifecycle(sourceID)
            let capability = appState.syncCapability(forSourceID: sourceID)
            return SidebarContextItem(
                id: sourceID,
                title: sourceID,
                subtitle: capability?.adapter,
                sourceType: capability?.sourceType,
                lifecycle: lifecycle,
                unavailableReason: sourceID == active ? nil : "udl plans one source at a time."
            )
        }
        return SidebarContext(
            title: "Run sources",
            items: items,
            selectedID: active,
            note: nil,
            select: { _ in }
        )
    }

    // MARK: Data

    private func rows(_ prompt: AppState.PlanPrompt) -> [PlanRow] {
        let query = chrome.searchText.trimmingCharacters(in: .whitespaces).lowercased()
        return prompt.params.rows
            .filter(filter.includes)
            .filter { row in
                guard !query.isEmpty else { return true }
                return row.title.lowercased().contains(query)
                    || row.remoteID.lowercased().contains(query)
            }
            .sorted(using: sortOrder)
    }

    private func count(of filter: PlanRowFilter, in prompt: AppState.PlanPrompt) -> Int {
        prompt.params.rows.filter(filter.includes).count
    }

    // MARK: Actions

    private func load(_ prompt: AppState.PlanPrompt) {
        // Unchanged from the sheet: the remembered overrides are what survive a
        // rebuild, so they must drive the initial selection every time.
        selected = appState.initialPlanSelection(prompt.params)
        cursor = appState.rememberedPlanCursor(sourceID: prompt.params.sourceID, rows: prompt.params.rows)
        order = prompt.params.downloadOrder
        window = prompt.params.planWindow
        filter = .all
        lockedReason = nil
    }

    private func toggleCursorRow(_ prompt: AppState.PlanPrompt) {
        guard let cursor, let row = prompt.params.rows.first(where: { $0.id == cursor }) else { return }
        guard row.toggleable else {
            lockedReason = row.lockReason
            return
        }
        if selected.contains(row.index) { selected.remove(row.index) } else { selected.insert(row.index) }
    }

    private func resetToDefaults(_ prompt: AppState.PlanPrompt) {
        appState.resetPlanSelection(sourceID: prompt.params.sourceID)
        selected = Set(prompt.params.rows.filter(\.selectedByDefault).map(\.index))
        order = prompt.params.downloadOrder
        window = prompt.params.planWindow
        lockedReason = nil
    }

    private func submit(_ prompt: AppState.PlanPrompt, rebuild: Bool) {
        appState.rememberPlanSelection(
            prompt.params,
            selectedIndices: selected,
            cursor: cursor
        )
        let result = SelectRowsResult(
            selectedIndices: rebuild ? [] : selected.sorted(),
            downloadOrder: order,
            canceled: false,
            rebuild: rebuild,
            planWindow: window
        )
        // answerPlanSelection sets the source's plan window *before* the reply
        // crosses the wire. Do not inline it here.
        Task { await appState.answerPlanSelection(result, request: prompt.request) }
    }
}
