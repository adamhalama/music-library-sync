import SwiftUI

/// NOVEL UI/DESIGN — two explicit directional actions feed one checksummed
/// row-level mirror preview. The view deliberately reuses the established app
/// shell, contextual sidebar, table, callout, inspector, status, confirmation,
/// and constrained-control patterns.
struct PlaylistSyncView: View {
    @EnvironmentObject private var appState: AppState
    @EnvironmentObject private var chrome: ShellChrome

    @State private var selection: String?
    @State private var rowFilter = PlaylistSyncRowFilter.all
    @State private var editing: PlaylistSyncEditorTarget?
    @State private var confirmingApply = false

    var body: some View {
        content
            .sidebarContext(sidebarContext)
            .workspaceToolbar(
                title: "Playlist Sync",
                subtitle: "explicit direction · exact real paths",
                searchable: true,
                searchPrompt: "Filter by artist, track, path or action"
            ) {
                Button { editing = .add } label: { Label("Add pair", systemImage: "plus") }
                Button { if let job { editing = .edit(job) } } label: { Label("Edit pair", systemImage: "pencil") }
                    .constrained(by: job == nil ? "Choose a configured pair first." : nil)
                Button { Task { await appState.loadPlaylistSync() } } label: { Label("Reload jobs", systemImage: "arrow.clockwise") }
                    .constrained(by: isRunning ? "A playlist sync operation is running." : nil)
            }
            .workspaceInspector { inspector }
            .udlStatusBar { statusSummary } actions: { statusActions }
            .task {
                if appState.playlistConfig == nil { await appState.loadPlaylists() }
                await appState.loadPlaylistSync()
                if selection == nil { selection = appState.playlistSyncJobs.first?.id }
            }
            .onChange(of: appState.playlistSyncJobs.map(\.id)) { _, ids in
                if selection == nil || !ids.contains(selection ?? "") { selection = ids.first }
            }
            .onDisappear { chrome.searchText = "" }
            .sheet(item: $editing) { target in
                PlaylistSyncJobEditor(existing: target.job).environmentObject(appState)
            }
            .confirmationDialog(
                applyConfirmationTitle,
                isPresented: $confirmingApply,
                titleVisibility: .visible
            ) {
                Button("Back up and replace \(plan?.destination.name ?? "destination")") {
                    Task { await appState.applyPlaylistSync() }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(applyConfirmationDetail)
            }
    }

    private var jobRow: PlaylistSyncInspectRow? { appState.playlistSyncJobs.first { $0.id == selection } }
    private var job: PlaylistSyncJob? { jobRow?.job }
    private var plan: PlaylistSyncPlan? { appState.playlistSyncPlan?.plan }
    private var isRunning: Bool { appState.playlistSyncOperation != nil }
    private var planBelongsToSelection: Bool { plan?.jobID == selection }

    private var content: some View {
        BoundedContent {
            VStack(alignment: .leading, spacing: 0) {
                if let notResumed = appState.notResumedMessage(for: .playlistSync) {
                    Callout(title: notResumed, severity: .warn)
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.top, 12)
                }
                if let job {
                    directionBar(job)
                    planBar
                    planBody
                } else {
                    EmptyStateView(
                        title: appState.playlistSyncJobs.isEmpty ? "No paired playlists" : "Choose a pair",
                        kind: .empty,
                        detail: appState.playlistSyncJobs.isEmpty
                            ? "Add a pair in playlists.yaml or with Add pair. Planning reads both providers but does not write either one."
                            : "Choose a configured pair from the sidebar."
                    )
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
        }
    }

    private func directionBar(_ job: PlaylistSyncJob) -> some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(job.id).font(Typography.sectionTitle)
                Text("Rekordbox \(job.rekordbox.playlist) ⇄ Navidrome \(job.navidrome.playlist)")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
            }
            Spacer(minLength: 12)
            Button("Send Rekordbox to phone") {
                Task { await appState.planPlaylistSync(jobID: job.id, direction: "rekordbox-to-navidrome") }
            }
            .buttonStyle(.borderedProminent)
            .constrained(by: isRunning ? "A playlist sync operation is already running." : nil)
            Button("Bring phone edits to Rekordbox") {
                Task { await appState.planPlaylistSync(jobID: job.id, direction: "navidrome-to-rekordbox") }
            }
            .constrained(by: isRunning ? "A playlist sync operation is already running." : nil)
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 12)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.separator).frame(height: 0.5) }
    }

    private var planBar: some View {
        HStack(spacing: 12) {
            StatusPill(title: planStatusTitle, severity: planSeverity)
            VStack(alignment: .leading, spacing: 2) {
                Text(planTitle).font(Typography.cardTitle).lineLimit(1)
                Text(planSubtitle)
                    .font(Typography.monoSmall)
                    .foregroundStyle(Theme.textTertiary)
                    .lineLimit(1)
                    .textSelection(.enabled)
            }
            Spacer(minLength: 12)
            Picker("Rows", selection: $rowFilter) {
                ForEach(PlaylistSyncRowFilter.allCases) { option in
                    Text("\(option.label) \(rowCount(option))").tag(option)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .frame(width: 330)
            .constrained(by: planBelongsToSelection ? nil : "Generate a plan for this pair to filter its rows.")
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 10)
        .background(Theme.sunken)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.separator).frame(height: 0.5) }
    }

    @ViewBuilder private var planBody: some View {
        if let plan, planBelongsToSelection {
            VStack(alignment: .leading, spacing: 0) {
                Table(filteredRows(plan)) {
                    TableColumn("Action") { row in
                        StatusPill(title: row.actionLabel, severity: severity(row.action))
                    }.width(min: 72, ideal: 86)
                    TableColumn("Track") { row in
                        VStack(alignment: .leading, spacing: 1) {
                            Text(row.title).font(Typography.control).lineLimit(1)
                            Text(row.artist ?? "Unknown artist").font(Typography.caption).foregroundStyle(Theme.textTertiary).lineLimit(1)
                        }
                    }
                    TableColumn("Path / reason") { row in
                        Text(row.blocker ?? row.normalizedPath)
                            .font(Typography.monoSmall)
                            .foregroundStyle(row.blocker == nil ? Theme.textSecondary : Theme.error)
                            .lineLimit(2)
                    }
                }
                if !plan.blockers.isEmpty {
                    Callout(
                        title: "\(plan.blockers.count) blocker(s) prevent apply",
                        detail: "udl refuses a partial mirror. Resolve every blocker and generate a fresh plan; no row can be skipped.",
                        severity: .error
                    )
                    .padding(.horizontal, Metrics.contentPaddingHorizontal)
                    .padding(.vertical, 10)
                }
            }
        } else {
            EmptyStateView(
                title: isRunning ? "Reading both providers" : "Choose a direction",
                kind: isRunning ? .working : .notRun(action: "generate a plan"),
                detail: "The source controls exact membership and order. Planning checks every track by normalized real path and writes nothing."
            )
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func filteredRows(_ plan: PlaylistSyncPlan) -> [PlaylistSyncPlanRow] {
        let query = chrome.searchText.trimmed.lowercased()
        return plan.rows.filter { row in
            let actionMatches = rowFilter == .all || row.action == rowFilter.rawValue
            let searchMatches = query.isEmpty || [row.artist, row.title, row.album, row.normalizedPath, row.blocker, row.action]
                .compactMap { $0 }.joined(separator: " ").lowercased().contains(query)
            return actionMatches && searchMatches
        }
    }

    private func rowCount(_ filter: PlaylistSyncRowFilter) -> Int {
        guard let plan, planBelongsToSelection else { return 0 }
        return filter == .all ? plan.rows.count : plan.rows.filter { $0.action == filter.rawValue }.count
    }

    private var sidebarContext: SidebarContext? {
        let items = appState.playlistSyncJobs.map { row in
            SidebarContextItem(
                id: row.id,
                title: row.id,
                subtitle: row.state.map { "last \($0.lastDirection)" } ?? (row.stateError ?? "never verified"),
                sourceType: "navidrome"
            )
        }
        return SidebarContext(
            title: "Paired playlists",
            items: items,
            selectedID: selection,
            note: "Direction is chosen for every run. A verified apply replaces the destination's complete membership and order.",
            select: { selection = $0 }
        )
    }

    @ViewBuilder private var inspector: some View {
        if let plan, planBelongsToSelection {
            InspectorSection(title: "Plan summary") {
                FieldRow("Direction", directionLabel(plan.direction))
                FieldRow("Source", "\(plan.source.name) · \(plan.summary.sourceTotal)")
                FieldRow("Destination", plan.destinationCreate ? "\(plan.destination.name) · create" : "\(plan.destination.name) · \(plan.summary.destinationTotal)")
                FieldRow("Final", "\(plan.summary.finalTotal)")
                FieldRow("Add / remove", "+\(plan.summary.willAdd) / −\(plan.summary.willRemove)")
                FieldRow("Move / keep", "\(plan.summary.willMove) / \(plan.summary.willKeep)")
                FieldRow("Checksum", String(plan.checksumSHA256.prefix(12)))
            }
            if !plan.blockers.isEmpty {
                InspectorSection(title: "Blockers") {
                    ForEach(plan.blockers, id: \.self) { ConstraintNote(text: $0) }
                }
            }
            InspectorSection(title: "Apply contract") {
                FieldRow("Source IDs", "\(plan.preconditions.sourceProviderIDs.count)")
                FieldRow("Destination IDs", "\(plan.preconditions.destinationProviderIDs.count)")
                FieldRow("Exact path matches", "\(plan.preconditions.matched.count)")
                FieldRow("Backup", plan.destination.provider == "navidrome" ? "Navidrome database" : "Rekordbox database directory")
                ConstraintNote(text: "Apply re-reads both ordered memberships and every exact path match. Any change invalidates this plan before backup.")
            }
        } else if let row = jobRow {
            InspectorSection(title: "Pair status") {
                FieldRow("Rekordbox", row.job.rekordbox.playlist)
                FieldRow("Navidrome", row.job.navidrome.playlist)
                FieldRow("Rekordbox ID", row.state?.rekordbox.playlistID ?? row.job.rekordbox.playlistID ?? "Resolve on plan")
                FieldRow("Navidrome ID", row.state?.navidrome.playlistID ?? row.job.navidrome.playlistID ?? "Resolve on plan")
                FieldRow("Last direction", row.state.map { directionLabel($0.lastDirection) } ?? "Never")
                FieldRow("Last verified", row.state?.lastVerifiedAt.formatted(date: .abbreviated, time: .shortened) ?? "Never")
                FieldRow("Parity", row.state.map { "Verified · \($0.finalPathChecksum.prefix(12))" } ?? "Not verified")
                if let error = row.stateError { ConstraintNote(text: error) }
            }
        }
    }

    private var statusSummary: some View {
        HStack(spacing: 8) {
            if isRunning { ProgressView().controlSize(.small) }
            Text(appState.playlistSyncStatus?.message ?? "Choose a pair and direction. Nothing changes until a reviewed plan is applied.")
                .font(Typography.caption)
                .foregroundStyle(Theme.textSecondary)
                .lineLimit(2)
        }
    }

    @ViewBuilder private var statusActions: some View {
        if isRunning {
            Button("Cancel") { Task { await appState.cancelPlaylistSync() } }
        } else {
            Button("Discard plan") { appState.discardPlaylistSyncPlan() }
                .constrained(by: plan == nil ? "There is no plan in this session." : nil)
            Button("Dry-run apply") { Task { await appState.applyPlaylistSync(dryRun: true) } }
                .constrained(by: applyConstraint)
            Button((plan?.summary.willRemove ?? 0) > 0 ? "Review replacement…" : "Apply mirror…") { confirmingApply = true }
                .buttonStyle(.borderedProminent)
                .constrained(by: applyConstraint)
        }
    }

    private var applyConstraint: String? {
        guard let plan, planBelongsToSelection else { return "Generate a plan for the selected pair first." }
        if !plan.blockers.isEmpty { return "Resolve all \(plan.blockers.count) blockers and generate a fresh plan." }
        return nil
    }

    private var planStatusTitle: String {
        guard let plan, planBelongsToSelection else { return isRunning ? "Reading" : "No plan" }
        return plan.blockers.isEmpty ? (plan.summary.willAdd + plan.summary.willRemove + plan.summary.willMove == 0 ? "No changes" : "Ready") : "Blocked"
    }

    private var planSeverity: Severity {
        guard let plan, planBelongsToSelection else { return isRunning ? .info : .idle }
        return plan.blockers.isEmpty ? .ok : .error
    }

    private var planTitle: String {
        guard let plan, planBelongsToSelection else { return "No plan for this pair" }
        return "\(directionLabel(plan.direction)): \(plan.source.name) → \(plan.destination.name)"
    }

    private var planSubtitle: String {
        guard let plan, planBelongsToSelection else { return "Planning reads live provider state and writes a checksummed file." }
        return "plan \(plan.version) · \(plan.generatedAt.formatted()) · checksum \(plan.checksumSHA256.prefix(12))"
    }

    private var applyConfirmationTitle: String {
        guard let plan else { return "Apply playlist mirror?" }
        return "Replace \(plan.destination.name) with \(plan.summary.finalTotal) tracks?"
    }

    private var applyConfirmationDetail: String {
        guard let plan else { return "" }
        let creation = plan.destinationCreate ? "The destination will be created." : "The destination has \(plan.summary.destinationTotal) tracks now."
        return "\(creation) \(plan.summary.willRemove) track(s) will be removed. udl revalidates both sides, creates the mandatory \(plan.destination.provider) backup, replaces membership and order, then reads it back before reporting success."
    }

    private func directionLabel(_ direction: String) -> String {
        direction == "rekordbox-to-navidrome" ? "Send Rekordbox to phone" : "Bring phone edits to Rekordbox"
    }

    private func severity(_ action: String) -> Severity {
        switch action {
        case "add": .ok
        case "remove": .warn
        case "move": .info
        case "blocked": .error
        default: .idle
        }
    }
}

private enum PlaylistSyncEditorTarget: Identifiable {
    case add
    case edit(PlaylistSyncJob)
    var id: String {
        switch self { case .add: "add"; case .edit(let job): "edit:\(job.id)" }
    }
    var job: PlaylistSyncJob? {
        switch self { case .add: nil; case .edit(let job): job }
    }
}
