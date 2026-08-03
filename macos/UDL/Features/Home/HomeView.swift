import SwiftUI

/// The default destination. Outcome-first, and every value on it has a
/// protocol source.
///
/// C16 — the protocol reports no library statistics and no run history, so the
/// mockup's "tracks on disk", "library size", and "recent runs" panels are
/// absent rather than faked or stored app-side.
struct HomeView: View {
    @EnvironmentObject private var appState: AppState

    private var attention: AppState.Attention { appState.attention }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                hero
                cards
                needsAttention
            }
        }
        .workspaceToolbar(
            title: "udl",
            subtitle: appState.initialization?.configPaths.first
        ) {
            Button {
                Task { await appState.refreshDoctor() }
            } label: {
                if appState.isLoadingDoctor {
                    Label { Text("Running checks…") } icon: { ProgressView().controlSize(.small) }
                } else {
                    Label("Re-check", systemImage: "arrow.clockwise")
                }
            }
            .disabled(appState.isLoadingDoctor)
            .help(appState.isLoadingDoctor ? "doctor.run is already running." : "Re-runs doctor.run")
        }
        .workspaceInspector { inspector }
        .udlStatusBar {
            doctorSummary
        } actions: {
            Button("Edit config") { appState.destination = .config }
            Button("Start dry run & plan") {
                Task { await appState.startDryRunPlan() }
            }
            .buttonStyle(.borderedProminent)
            .constrained(by: appState.busyReason)
        }
    }

    // MARK: Hero

    private var hero: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(headline)
                .font(Typography.hero)
                .fixedSize(horizontal: false, vertical: true)
            Text(lede)
                .font(Typography.control)
                .foregroundStyle(Theme.textSecondary)
                .fixedSize(horizontal: false, vertical: true)

            HStack(spacing: 12) {
                // C1 — "Start dry run & plan", not "Review sync plan": there is
                // no plan-preview method, so the plan is produced inside a run.
                Button("Start dry run & plan") {
                    Task { await appState.startDryRunPlan() }
                }
                .buttonStyle(.borderedProminent)
                .constrained(by: appState.busyReason)

                Button("Check system") { appState.destination = .doctor }

                Text("⌘R starts the same dry run")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
            }
            .padding(.top, 9)

            ConstraintNote(
                text: "udl has no plan preview. Planning happens inside a run, one source at a time, and the run is a dry run until you say otherwise."
            )
            .padding(.top, 6)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.top, 22)
        .padding(.bottom, 18)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Theme.separator).frame(height: 0.5)
        }
    }

    private var headline: String {
        if appState.syncSources.isEmpty {
            return "No sync sources configured"
        }
        if attention.syncNeedsYou {
            return "A run is waiting for your selection"
        }
        if attention.syncActive {
            return attention.syncRemaining > 0
                ? "Run in progress — \(attention.syncRemaining) tracks remaining"
                : "Run in progress"
        }
        if attention.hasBlockingWork {
            let blockers = attention.doctorErrors + attention.unhealthyCredentials + attention.rekordboxBlockers
            return "\(blockers) \(blockers == 1 ? "item needs" : "items need") you before a run"
        }
        return "\(appState.syncSources.count) \(appState.syncSources.count == 1 ? "source" : "sources") ready to plan"
    }

    private var lede: String {
        if appState.syncSources.isEmpty {
            return "Add a source in Advanced Config, then start a dry run to see what udl would download."
        }
        if let startup = appState.startupAttention?.attention {
            return startup.summaryText
        }
        if let terminal = appState.syncRun.terminalMessage, appState.syncRun.phase != .idle {
            return terminal
        }
        return "\(attention.doctorPassed) checks passed. Nothing has run in this session yet."
    }

    // MARK: Workflow cards

    private var cards: some View {
        LazyVGrid(
            columns: [GridItem(.flexible(), spacing: 11), GridItem(.flexible(), spacing: 11)],
            spacing: 11
        ) {
            workflowCard(
                destination: .sync,
                title: "Run Sync",
                description: "Download new tracks from your configured sources into the library.",
                pills: syncPills
            )
            workflowCard(
                destination: .freeDL,
                title: "SoundCloud Free DL",
                description: "Find tracks you own in low quality that offer a free lossless download, then promote them.",
                pills: freeDLPills
            )
            workflowCard(
                destination: .rekordbox,
                title: "Rekordbox Sync",
                description: "Mirror a playlist into Rekordbox with a checksummed, fail-closed plan.",
                pills: rekordboxPills
            )
            workflowCard(
                destination: .playlists,
                title: "Playlists",
                description: "Cached Apple Music snapshots you can hand off to Free DL or Rekordbox.",
                pills: playlistPills
            )
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 18)
    }

    private func workflowCard(
        destination: AppState.Destination,
        title: String,
        description: String,
        pills: [(String, Severity)]
    ) -> some View {
        Button {
            appState.destination = destination
        } label: {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: destination.icon)
                    .font(Typography.cardTitle)
                    .foregroundStyle(Theme.textSecondary)
                    .frame(width: 30, height: 30)
                    .background(Theme.fill, in: RoundedRectangle(cornerRadius: 8))
                VStack(alignment: .leading, spacing: 3) {
                    Text(title).font(Typography.cardTitle)
                    Text(description)
                        .font(Typography.caption)
                        .foregroundStyle(Theme.textTertiary)
                        .fixedSize(horizontal: false, vertical: true)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if !pills.isEmpty {
                        HStack(spacing: 6) {
                            ForEach(pills, id: \.0) { pill in
                                StatusPill(title: pill.0, severity: pill.1)
                            }
                        }
                        .padding(.top, 4)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(13)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.window)
            .overlay(
                RoundedRectangle(cornerRadius: 10).stroke(Theme.separator, lineWidth: 0.5)
            )
            .clipShape(RoundedRectangle(cornerRadius: 10))
        }
        .buttonStyle(.plain)
    }

    private var syncPills: [(String, Severity)] {
        var pills: [(String, Severity)] = []
        if attention.syncNeedsYou {
            pills.append(("Needs you", .warn))
        } else if attention.syncActive {
            pills.append(("Running", .info))
        }
        pills.append((
            "\(appState.syncSources.count) \(appState.syncSources.count == 1 ? "source" : "sources")",
            .idle
        ))
        if let notResumed = appState.notResumedMessage(for: .sync), !notResumed.isEmpty {
            pills.append(("Not resumed", .warn))
        }
        return pills
    }

    private var freeDLPills: [(String, Severity)] {
        var pills: [(String, Severity)] = []
        if let jobs = appState.freeDLConfig?.config.jobs {
            let enabled = jobs.filter(\.enabled).count
            pills.append(("\(enabled) of \(jobs.count) jobs enabled", enabled == 0 ? .idle : .ok))
        } else {
            pills.append(("Config not loaded", .idle))
        }
        if attention.freeDLSelectable > 0 {
            pills.append(("\(attention.freeDLSelectable) upgradeable", .info))
        }
        return pills
    }

    private var rekordboxPills: [(String, Severity)] {
        var pills: [(String, Severity)] = []
        if let runtime = appState.rekordboxRuntime?.status {
            pills.append((runtime.healthy ? "Runtime ready" : "Runtime not ready", runtime.healthy ? .ok : .warn))
        } else {
            pills.append(("Runtime unknown", .idle))
        }
        if attention.rekordboxBlockers > 0 {
            pills.append(("\(attention.rekordboxBlockers) apply blockers", .error))
        }
        return pills
    }

    private var playlistPills: [(String, Severity)] {
        var pills: [(String, Severity)] = [(
            "\(appState.playlists.count) \(appState.playlists.count == 1 ? "playlist" : "playlists")",
            .idle
        )]
        if attention.playlistsWithoutSnapshot > 0 {
            pills.append(("\(attention.playlistsWithoutSnapshot) never refreshed", .warn))
        }
        return pills
    }

    // MARK: Needs attention

    private var attentionChecks: [DoctorCheck] {
        (appState.doctor?.checks ?? []).filter {
            let severity = Severity.forCheck($0)
            return severity == .error || severity == .warn
        }
    }

    private var needsAttention: some View {
        Card(title: "Needs attention", subtitle: "The same doctor.run report Check System renders.", flush: true) {
            Button("Open Check System") { appState.destination = .doctor }
                .buttonStyle(.link)
        } content: {
            if appState.doctor == nil {
                EmptyStateView(
                    title: "Check System",
                    kind: appState.isLoadingDoctor ? .working : .notRun(action: "run Check System")
                )
                .frame(height: 120)
            } else if attentionChecks.isEmpty {
                EmptyStateView(
                    title: "Nothing needs you",
                    kind: .empty,
                    detail: "\(attention.doctorPassed) checks passed with no warnings or errors."
                )
                .frame(height: 120)
            } else {
                ForEach(attentionChecks) { check in
                    attentionRow(check)
                }
            }
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.bottom, 20)
    }

    private func attentionRow(_ check: DoctorCheck) -> some View {
        HStack(alignment: .top, spacing: 10) {
            SeverityBadge(severity: Severity.forCheck(check))
            VStack(alignment: .leading, spacing: 2) {
                Text(check.name).font(Typography.control)
                Text(check.detail)
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 8)
            // Routing lives in `DoctorFix` so Home and Doctor can never send
            // the same check to two different screens.
            let fix = DoctorFix(check)
            Button(fix.shortActionLabel) {
                appState.destination = fix.destination ?? .doctor
            }
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Theme.separator).frame(height: 0.5)
        }
    }

    // MARK: Inspector

    @ViewBuilder private var inspector: some View {
        InspectorSection(title: "Sources") {
            FieldRow("Configured", "\(appState.syncSources.count)")
            FieldRow("Plan-capable", "\(appState.syncSources.filter(\.supportsPlan).count)")
            FieldRow("Windowed", "\(appState.syncSources.filter(\.supportsPlanWindow).count)")
            ConstraintNote(text: "From sources.capabilities. udl reports no library size or track count, so neither is shown.")
        }

        InspectorSection(title: "Doctor") {
            FieldRow(label: "Errors") {
                Text("\(attention.doctorErrors)").foregroundStyle(attention.doctorErrors > 0 ? Theme.error : Theme.text)
            }
            FieldRow(label: "Warnings") {
                Text("\(attention.doctorWarnings)").foregroundStyle(attention.doctorWarnings > 0 ? Theme.warn : Theme.text)
            }
            FieldRow("Passed", "\(attention.doctorPassed)")
        }

        InspectorSection(title: "Backend") {
            FieldRow("Status", appState.backend.state.label)
            FieldRow("Version", appState.backendVersion)
            if let build = appState.initialization?.build {
                FieldRow("Commit", String(build.commit.prefix(10)))
            }
        }

        if let initialization = appState.initialization {
            InspectorSection(title: "Paths") {
                PathField(label: "Working dir", path: initialization.workingDir)
                ForEach(initialization.configPaths, id: \.self) { path in
                    PathField(label: "Config", path: path)
                }
                ForEach(initialization.featureConfigPaths.sorted(by: { $0.key < $1.key }), id: \.key) { feature, paths in
                    ForEach(paths, id: \.self) { path in
                        PathField(label: feature, path: path)
                    }
                }
            }
        }
    }

    private var doctorSummary: some View {
        SummaryLine {
            if appState.doctor == nil {
                Text("Check System has not run in this session")
            } else {
                SummaryCount(value: attention.doctorErrors, noun: "errors", severity: attention.doctorErrors > 0 ? .error : .idle)
                Text("·")
                SummaryCount(value: attention.doctorWarnings, noun: "warnings", severity: attention.doctorWarnings > 0 ? .warn : .idle)
                Text("·")
                SummaryCount(value: attention.doctorPassed, noun: "passed", severity: .ok)
            }
        }
    }
}
