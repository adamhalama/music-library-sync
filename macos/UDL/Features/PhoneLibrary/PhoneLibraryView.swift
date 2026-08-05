import SwiftUI

/// The Phone Library workspace: install Navidrome, run it as a UDL-owned
/// service, create the account, generate the smart playlists, migrate Apple
/// Music favorites, and hand the LAN address to Amperfy.
///
/// Every disabled control states its reason beside itself through
/// `.constrained(by:)`, and every failure lands in this screen's
/// `phoneLibraryStatus` rather than a session-level alert.
struct PhoneLibraryView: View {
    @EnvironmentObject private var appState: AppState

    @State private var password = ""
    @State private var confirmingSetupApply = false
    @State private var confirmingFavoriteApply = false
    @State private var confirmingInstall = false
    @State private var showingPlanContents = false

    var body: some View {
        content
            .workspaceToolbar(
                title: "Phone Library",
                subtitle: "navidrome.status · navidrome.setup · navidrome.favorites"
            ) {
                if appState.phoneLibraryRunID != nil {
                    Button("Cancel", role: .destructive) {
                        Task { await appState.cancelPhoneLibraryOperation() }
                    }
                } else {
                    Button {
                        Task { await appState.refreshPhoneLibraryStatus() }
                    } label: {
                        Label("Refresh status", systemImage: "arrow.clockwise")
                    }
                    .help("Reads dependency, service, account, library, and backup state. Changes nothing.")
                }
            }
            .workspaceInspector { inspector }
            .udlStatusBar { statusSummary } actions: { statusActions }
            .task {
                if appState.navidromeStatus == nil {
                    await appState.loadPhoneLibrary()
                }
            }
            .confirmationDialog(
                "Install Navidrome with Homebrew?",
                isPresented: $confirmingInstall,
                titleVisibility: .visible
            ) {
                Button("Install") { Task { await appState.installNavidrome() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("This runs `brew install navidrome` and changes your machine. Nothing in your music library is touched.")
            }
            .confirmationDialog(
                "Apply this setup plan?",
                isPresented: $confirmingSetupApply,
                titleVisibility: .visible
            ) {
                Button("Apply setup") { Task { await appState.applyNavidromeSetup() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("UDL re-verifies the plan checksum, refuses to overwrite any file it does not manage, writes the managed config and LaunchAgent, and restarts the service. Your audio files are never modified.")
            }
            .confirmationDialog(
                "Import Apple Music favorites?",
                isPresented: $confirmingFavoriteApply,
                titleVisibility: .visible
            ) {
                Button("Back up and import") { Task { await appState.applyNavidromeFavorites() } }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("UDL backs up the Navidrome database, stars only exact path matches, and verifies parity. Apple Music is read-only throughout; if the import is interrupted, only the stars this attempt added are undone.")
            }
            .sheet(isPresented: $showingPlanContents) {
                if let plan = appState.navidromeSetupPlan {
                    PhoneLibraryPlanContentsSheet(plan: plan)
                }
            }
    }

    // MARK: Content

    private var content: some View {
        BoundedContent {
            VStack(alignment: .leading, spacing: 14) {
                if let notResumed = appState.notResumedMessage(for: .phoneLibrary) {
                    Callout(title: notResumed, severity: .warn)
                }
                // A failure caused by an action on this screen is reported here,
                // next to the step that caused it.
                if let status = appState.phoneLibraryStatus, status.severity != .info {
                    Callout(title: status.message, severity: status.severity)
                }
                if let recovery = appState.navidromeFavoriteResult?.recoveryCommand {
                    Callout(
                        title: "The favorite import could not be fully undone.",
                        detail: recovery,
                        severity: .error
                    )
                }
                header
                checklist
                dependencyCard
                serviceCard
                accountCard
                setupCard
                playlistsCard
                favoritesCard
                connectionCard
            }
            .padding(.horizontal, Metrics.contentPaddingHorizontal)
            .padding(.vertical, 14)
        }
    }

    private var header: some View {
        WorkspaceHeader(
            title: "Serve this Mac's library to your iPhone",
            lede: "Navidrome runs on this Mac as a UDL-managed service and Amperfy downloads the library for offline playback. v1 is trusted home Wi-Fi over plain HTTP — never port-forward it."
        )
    }

    // MARK: Checklist

    private var checklist: some View {
        let progress = appState.phoneLibraryProgress
        return Card(
            title: "Setup",
            subtitle: "\(progress.completed) of \(progress.total) steps complete"
        ) {
            ForEach(progress.steps) { step in
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Image(systemName: symbol(for: step.state))
                        .foregroundStyle(tint(for: step.state))
                    Text(step.title)
                        .font(Typography.control)
                        .foregroundStyle(step.isDone ? Theme.textSecondary : Theme.text)
                    Spacer(minLength: 0)
                }
                if case .blocked(let reason) = step.state {
                    ConstraintNote(text: reason, severity: .warn)
                        .padding(.leading, 22)
                }
            }
        }
    }

    private func symbol(for state: PhoneLibraryStep.State) -> String {
        switch state {
        case .done: "checkmark.circle.fill"
        case .current: "circle.dashed"
        case .blocked: "exclamationmark.triangle.fill"
        case .pending: "circle"
        }
    }

    private func tint(for state: PhoneLibraryStep.State) -> Color {
        switch state {
        case .done: Severity.ok.tint
        case .current: Theme.accent
        case .blocked: Severity.warn.tint
        case .pending: Theme.textTertiary
        }
    }

    // MARK: Dependency

    private var dependencyCard: some View {
        let dependency = appState.navidromeStatus?.dependency
        return Card(title: "Navidrome", subtitle: "Installed natively through Homebrew") {
            FieldRow("Homebrew", dependency.map { $0.homebrewInstalled ? "installed" : "not installed" } ?? "unknown")
            FieldRow("Navidrome", dependency?.version ?? (dependency?.installed == true ? "unknown version" : "not installed"))
            FieldRow("Minimum required", dependency?.minimumVersion ?? "0.63.2")
            if let path = dependency?.binaryPath, !path.isEmpty {
                FieldRow("Binary", path)
            }
            ForEach(dependency?.problems ?? [], id: \.self) { problem in
                ConstraintNote(text: problem, severity: .warn)
            }
            HStack(spacing: 8) {
                Button(dependency?.installed == true ? "Repair or upgrade…" : "Install Navidrome…") {
                    confirmingInstall = true
                }
                .buttonStyle(.borderedProminent)
                .constrained(by: installReason)
            }
        }
    }

    private var installReason: String? {
        if appState.isPhoneLibraryBusy { return busyReason }
        guard let dependency = appState.navidromeStatus?.dependency else {
            return "Phone Library state has not loaded yet."
        }
        if !dependency.homebrewInstalled {
            return "Homebrew is required. Install it from https://brew.sh first."
        }
        return nil
    }

    // MARK: Service

    private var serviceCard: some View {
        let service = appState.navidromeStatus?.service
        return Card(title: "Service", subtitle: "com.jaa.udl.navidrome") {
            HStack(spacing: 8) {
                StatusPill(title: service?.label ?? "Unknown", severity: service?.severity ?? .idle)
                if let pid = service?.pid, pid > 0 {
                    Text("PID \(pid)")
                        .font(Typography.monoSmall)
                        .foregroundStyle(Theme.textTertiary)
                }
                Spacer(minLength: 0)
            }
            if let local = service?.localURL, !local.isEmpty {
                FieldRow("Local", local)
            }
            if let log = service?.logPath, !log.isEmpty {
                FieldRow("Log", log)
            }
            ForEach(service?.problems ?? [], id: \.self) { problem in
                ConstraintNote(text: problem, severity: .warn)
            }
            HStack(spacing: 8) {
                Button("Start") { Task { await appState.controlNavidromeService("start") } }
                Button("Stop") { Task { await appState.controlNavidromeService("stop") } }
                Button("Restart") { Task { await appState.controlNavidromeService("restart") } }
                if let local = service?.localURL, let url = URL(string: local) {
                    Button("Open Local Server") { NSWorkspace.shared.open(url) }
                }
                Spacer(minLength: 0)
            }
            .constrained(by: serviceControlReason)
        }
    }

    private var serviceControlReason: String? {
        if appState.isPhoneLibraryBusy { return busyReason }
        guard let service = appState.navidromeStatus?.service else {
            return "Phone Library state has not loaded yet."
        }
        if service.state == "not_installed" {
            return "The managed service is not installed yet. Apply the setup plan below first."
        }
        if !service.owned {
            return "A Navidrome LaunchAgent exists that UDL does not manage. UDL will not modify it."
        }
        return nil
    }

    // MARK: Account

    private var accountCard: some View {
        let status = appState.navidromeStatus
        return Card(
            title: "Account",
            subtitle: "One shared account for UDL and Amperfy, so favorites and playlists refer to the same user"
        ) {
            FieldRow("Username", status?.username?.isEmpty == false ? status!.username! : "not set")
            FieldRow("Password", status?.passwordStored == true ? "saved in macOS Keychain" : "not saved")
            FieldRow("Server reachable", status?.reachable == true ? "yes" : "no")
            if let version = status?.server.serverVersion, !version.isEmpty {
                FieldRow("Server version", version)
            }

            ConstraintNote(
                text: "Create the first admin account at the local server address, then save the same password here. UDL stores it only in macOS Keychain.",
                symbol: "person.badge.key"
            )
            HStack(spacing: 8) {
                // The field starts empty and is never preloaded with the stored
                // secret: a preloaded masked buffer makes a pasted replacement
                // append to the old value invisibly.
                SecureField("Navidrome account password", text: $password)
                    .textFieldStyle(.roundedBorder)
                    .frame(maxWidth: 320)
                Button(status?.passwordStored == true ? "Replace" : "Save") {
                    Task {
                        await appState.savePhoneLibraryPassword(password)
                        password = ""
                    }
                }
                Spacer(minLength: 0)
            }
            .constrained(by: passwordSaveReason)
        }
    }

    private var passwordSaveReason: String? {
        if appState.isPhoneLibraryBusy { return busyReason }
        if (appState.navidromeStatus?.username ?? "").isEmpty {
            return "Set the account username in the Navidrome config before saving its password."
        }
        return nil
    }

    // MARK: Setup plan

    private var setupCard: some View {
        Card(title: "Managed setup", subtitle: "Every file and service change, previewed before anything is written") {
            if let plan = appState.navidromeSetupPlan {
                FieldRow("Checksum", plan.shortChecksum)
                FieldRow("Changes", "\(plan.changeCount)")
                ForEach(plan.newDirectories) { directory in
                    FieldRow("create directory", directory.path)
                }
                ForEach(plan.changedFiles) { file in
                    FieldRow(file.action, file.path)
                }
                if plan.service.action != "none" {
                    FieldRow("service", plan.service.action)
                }
                ForEach(plan.warnings, id: \.self) { warning in
                    ConstraintNote(text: warning, severity: .warn)
                }
                ForEach(plan.blockers, id: \.self) { blocker in
                    ConstraintNote(text: blocker, severity: .error, symbol: "exclamationmark.triangle")
                }
            } else {
                Text("No plan yet. Planning inspects the system and writes nothing.")
                    .font(Typography.control)
                    .foregroundStyle(Theme.textSecondary)
            }
            HStack(spacing: 8) {
                Button("Build setup plan") { Task { await appState.planNavidromeSetup() } }
                    .constrained(by: appState.isPhoneLibraryBusy ? busyReason : nil)
                Button("Show file contents…") { showingPlanContents = true }
                    .constrained(by: appState.navidromeSetupPlan == nil ? "Build a plan first." : nil)
                Button("Apply setup…") { confirmingSetupApply = true }
                    .buttonStyle(.borderedProminent)
                    .constrained(by: setupApplyReason)
                Spacer(minLength: 0)
            }
        }
    }

    private var setupApplyReason: String? {
        if appState.isPhoneLibraryBusy { return busyReason }
        guard let plan = appState.navidromeSetupPlan else {
            return "Build a setup plan first; apply only ever runs a plan you have seen."
        }
        if plan.checksum.isEmpty {
            return "The plan carries no checksum, so apply would be refused."
        }
        if !plan.blockers.isEmpty {
            return plan.blockers.joined(separator: " ")
        }
        if plan.changeCount == 0 {
            return "The managed setup already matches this plan; there is nothing to apply."
        }
        return nil
    }

    // MARK: Playlists

    private var playlistsCard: some View {
        let status = appState.navidromeStatus
        return Card(
            title: "Smart playlists",
            subtitle: "All Music · HARD BOUNCE · Favourites, newest-first by creation time"
        ) {
            ForEach(status?.managedPlaylists ?? []) { playlist in
                FieldRow(playlist.name, "\(playlist.trackCount) tracks")
            }
            if (status?.managedPlaylists ?? []).isEmpty {
                Text("Navidrome has not imported the managed playlists yet.")
                    .font(Typography.control)
                    .foregroundStyle(Theme.textSecondary)
            }
            if let refresh = appState.navidromePlaylistRefresh {
                ForEach(refresh.warnings, id: \.self) { warning in
                    ConstraintNote(text: warning, severity: .warn)
                }
            }

            genreDerivation

            HStack(spacing: 8) {
                Button("Refresh playlists") { Task { await appState.refreshNavidromePlaylists() } }
                    .constrained(by: serverReason)
                Button("Derive HARD BOUNCE genres") { Task { await appState.deriveNavidromeGenres() } }
                    .constrained(by: serverReason)
                Spacer(minLength: 0)
            }
        }
    }

    @ViewBuilder private var genreDerivation: some View {
        if let derivation = appState.navidromeGenreDerivation {
            Divider()
            Text("Derived from “\(derivation.sourcePlaylist)”: \(derivation.matchedCount) of \(derivation.sourceTrackCount) tracks matched.")
                .font(Typography.control)
            ForEach(derivation.genres, id: \.self) { genre in
                Toggle(genre, isOn: Binding(
                    get: { appState.navidromeGenreApproval[genre] ?? true },
                    set: { appState.navidromeGenreApproval[genre] = $0 }
                ))
                .toggleStyle(.checkbox)
            }
            if !derivation.unmatchedPaths.isEmpty {
                ConstraintNote(
                    text: "\(derivation.unmatchedPaths.count) source track(s) have no Navidrome match and contributed no genre. Rescan the library if that is unexpected.",
                    severity: .warn
                )
            }
            if !derivation.genrelessPaths.isEmpty {
                ConstraintNote(
                    text: "\(derivation.genrelessPaths.count) matched track(s) carry no genre tag, so they cannot be covered by a genre rule.",
                    severity: .warn
                )
            }
            if !derivation.outsideLibrary.isEmpty {
                ConstraintNote(
                    text: "\(derivation.outsideLibrary.count) source track(s) live outside the configured music directory."
                )
            }
            HStack(spacing: 8) {
                Button("Save approved genres") { Task { await appState.saveNavidromeGenres() } }
                    .buttonStyle(.borderedProminent)
                    .constrained(by: saveGenresReason)
                Spacer(minLength: 0)
            }
        }
    }

    private var saveGenresReason: String? {
        if appState.isPhoneLibraryBusy { return busyReason }
        guard let derivation = appState.navidromeGenreDerivation else { return "Derive the genres first." }
        let approved = derivation.genres.filter { appState.navidromeGenreApproval[$0] ?? true }
        if approved.isEmpty {
            return "Approve at least one genre. An empty allowlist would make HARD BOUNCE match nothing."
        }
        return nil
    }

    // MARK: Favorites

    private var favoritesCard: some View {
        Card(
            title: "Apple Music favorites",
            subtitle: "One-way import. Apple Music is only ever read."
        ) {
            if let plan = appState.navidromeFavoritePlan {
                FieldRow("Checksum", plan.shortChecksum)
                FieldRow("Apple favorites", "\(plan.counts.sourceTotal)")
                FieldRow("Will star", "\(plan.counts.matched)")
                FieldRow("Already starred", "\(plan.counts.alreadyStarred)")
                FieldRow("Outside library", "\(plan.counts.outsideLibrary)")
                FieldRow("Missing", "\(plan.counts.missing)")
                FieldRow("Ambiguous", "\(plan.counts.ambiguous)")
                FieldRow("Metadata-only (never applied)", "\(plan.counts.metadataOnly)")
                ForEach(plan.warnings, id: \.self) { warning in
                    ConstraintNote(text: warning, severity: .warn)
                }
                ForEach(plan.blockers, id: \.self) { blocker in
                    ConstraintNote(text: blocker, severity: .error, symbol: "exclamationmark.triangle")
                }
                if !plan.excludedRows.isEmpty {
                    Divider()
                    SectionHeader(title: "Skipped rows")
                    ForEach(plan.excludedRows.prefix(20)) { row in
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            StatusPill(title: row.statusLabel, severity: row.severity)
                            VStack(alignment: .leading, spacing: 1) {
                                Text(row.artist.isEmpty ? row.title : "\(row.artist) — \(row.title)")
                                    .font(Typography.control)
                                if !row.detail.isEmpty {
                                    Text(row.detail)
                                        .font(Typography.caption)
                                        .foregroundStyle(Theme.textSecondary)
                                }
                            }
                            Spacer(minLength: 0)
                        }
                    }
                    if plan.excludedRows.count > 20 {
                        ConstraintNote(text: "\(plan.excludedRows.count - 20) more skipped row(s) not shown.")
                    }
                }
            } else {
                Text("No migration plan yet. Planning reads both libraries and changes nothing.")
                    .font(Typography.control)
                    .foregroundStyle(Theme.textSecondary)
            }

            if let result = appState.navidromeFavoriteResult {
                Divider()
                if let backup = result.backupPath {
                    FieldRow("Backup", backup)
                }
                FieldRow("Newly starred", "\(result.newlyStarred.count)")
                FieldRow("Parity verified", result.parityVerified ? "yes" : "no")
                if !result.compensated.isEmpty {
                    ConstraintNote(
                        text: "\(result.compensated.count) star(s) added by the interrupted attempt were rolled back. Stars that existed before are untouched.",
                        severity: .warn
                    )
                }
            }

            HStack(spacing: 8) {
                Button("Build migration plan") { Task { await appState.planNavidromeFavorites() } }
                    .constrained(by: serverReason)
                Button("Import favorites…") { confirmingFavoriteApply = true }
                    .buttonStyle(.borderedProminent)
                    .constrained(by: favoriteApplyReason)
                Spacer(minLength: 0)
            }
        }
    }

    private var favoriteApplyReason: String? {
        if appState.isPhoneLibraryBusy { return busyReason }
        guard let plan = appState.navidromeFavoritePlan else {
            return "Build a migration plan first; apply only ever runs a plan you have seen."
        }
        if plan.checksum.isEmpty {
            return "The plan carries no checksum, so apply would be refused."
        }
        if !plan.blockers.isEmpty {
            return plan.blockers.joined(separator: " ")
        }
        if plan.counts.matched == 0 {
            return "Every favorite is already starred or is excluded; there is nothing to import."
        }
        return nil
    }

    // MARK: Connection

    private var connectionCard: some View {
        let connection = appState.phoneConnection
        return Card(title: "Connect Amperfy", subtitle: "Home Wi-Fi only") {
            if connection.isReady {
                FieldRow("Server", connection.serverURL)
                FieldRow("Username", connection.username)
                FieldRow("Approximate download", connection.approximateLibrarySize)
            }
            ConstraintNote(
                text: "In Amperfy, add a Subsonic/Ampache server using this address and the shared account, then download the All Music playlist for offline playback.",
                symbol: "iphone.and.arrow.forward"
            )
            ConstraintNote(
                text: "iOS asks for Local Network permission the first time Amperfy reaches this Mac. Allow it, or the server will look unreachable.",
                symbol: "wifi"
            )
            ConstraintNote(
                text: "This address is reachable on your home network only. Never forward this port on your router.",
                severity: .warn,
                symbol: "lock.shield"
            )
            HStack(spacing: 8) {
                Button("Copy address") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(connection.serverURL, forType: .string)
                }
                .constrained(by: connection.isReady
                    ? nil
                    : "The LAN address is known once the service is running and the account is set.")
                Spacer(minLength: 0)
            }
        }
    }

    // MARK: Shared reasons

    private var busyReason: String? {
        appState.phoneLibraryOperation.map { "\($0.label) Wait for it to finish or cancel it." }
    }

    private var serverReason: String? {
        if appState.isPhoneLibraryBusy { return busyReason }
        guard let status = appState.navidromeStatus else {
            return "Phone Library state has not loaded yet."
        }
        if !status.passwordStored {
            return "Save the account password first; UDL needs it to read the server."
        }
        if !status.reachable {
            return "The server is not reachable. Start the service and check the account."
        }
        return nil
    }

    // MARK: Chrome

    @ViewBuilder private var inspector: some View {
        let status = appState.navidromeStatus
        VStack(alignment: .leading, spacing: 14) {
            InspectorSection(title: "Paths") {
                FieldRow("Music", status?.musicDir ?? "—")
                FieldRow("Data", status?.dataDir ?? "—")
                FieldRow("Playlists", status?.playlistsDir ?? "—")
                if let config = appState.navidromeConfig?.path {
                    FieldRow("Config", config)
                }
            }
            InspectorSection(title: "Library") {
                FieldRow("Tracks", "\(status?.libraryTracks ?? 0)")
                FieldRow("Scanning", status?.scanning == true ? "yes" : "no")
            }
            InspectorSection(title: "Backups") {
                FieldRow("Retained", "\(status?.backupCount ?? 0)")
                if let latest = status?.latestBackup {
                    FieldRow("Latest", latest.path)
                }
                Button("Create backup now") { Task { await appState.createNavidromeBackup() } }
                    .constrained(by: appState.isPhoneLibraryBusy ? busyReason : nil)
            }
            if let problems = status?.problems, !problems.isEmpty {
                InspectorSection(title: "Problems") {
                    ForEach(problems, id: \.self) { problem in
                        ConstraintNote(text: problem, severity: .warn)
                    }
                }
            }
            Spacer(minLength: 0)
        }
    }

    @ViewBuilder private var statusSummary: some View {
        if let operation = appState.phoneLibraryOperation {
            Text(operation.label)
        } else if let status = appState.phoneLibraryStatus {
            Text(status.message)
        } else {
            let progress = appState.phoneLibraryProgress
            Text("\(progress.completed) of \(progress.total) setup steps complete")
        }
    }

    @ViewBuilder private var statusActions: some View {
        if appState.phoneLibraryRunID != nil {
            Button("Cancel", role: .destructive) {
                Task { await appState.cancelPhoneLibraryOperation() }
            }
        }
    }
}

/// The exact bytes the setup plan would write, so apply is never a leap of
/// faith about file contents.
struct PhoneLibraryPlanContentsSheet: View {
    let plan: NavidromeSetupPlanPresentation
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Files this plan would write")
                .font(Typography.sectionTitle)
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    ForEach(plan.files) { file in
                        VStack(alignment: .leading, spacing: 4) {
                            Text("\(file.action) · \(file.path)")
                                .font(Typography.control)
                                .fontWeight(.semibold)
                            Text(file.content)
                                .font(Typography.monoSmall)
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(8)
                                .background(Theme.sunken, in: RoundedRectangle(cornerRadius: Metrics.controlRadius))
                        }
                    }
                }
            }
            HStack {
                Spacer()
                Button("Done") { dismiss() }
                    .keyboardShortcut(.defaultAction)
            }
        }
        .padding(16)
        .frame(width: 720, height: 560)
    }
}
