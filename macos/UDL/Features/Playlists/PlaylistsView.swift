import SwiftUI

/// C11 — the cache-first playlist browser.
///
/// Opening this screen reads `playlists.list` and `playlists.config.read` and
/// nothing else; neither method contacts a provider. Every provider read — a
/// snapshot refresh or Apple Music discovery — is an explicit action, and every
/// outcome that did not replace the snapshot says so in those words.
struct PlaylistsView: View {
    @EnvironmentObject private var appState: AppState
    @EnvironmentObject private var chrome: ShellChrome

    @State private var selection: String?
    @State private var trackFilter = PlaylistTrackFilter.all
    @State private var editing: PlaylistEditorTarget?
    @State private var showingProvider = false
    @State private var confirmingRefresh = false

    var body: some View {
        content
            .sidebarContext(sidebarContext)
            .workspaceToolbar(
                title: "Playlists",
                subtitle: "playlists.list · cache-first",
                searchable: true,
                searchPrompt: "Filter by artist, track or album"
            ) {
                Button { editing = .add } label: { Label("Add playlist", systemImage: "plus") }
                Button {
                    showingProvider = true
                    Task { await appState.discoverProviderPlaylists() }
                } label: {
                    Label("Music playlists", systemImage: "music.note")
                }
                .disabled(appState.playlistActiveRunID != nil)
                .help(appState.busyReason
                      ?? "Asks Music.app for its playlist list. This is the only button on this screen that contacts Music without refreshing a snapshot.")
                Button { Task { await appState.loadPlaylists() } } label: {
                    Label("Reload cache", systemImage: "arrow.clockwise")
                }
                .help("Re-reads the cached snapshots from disk. Music.app is not contacted.")
            }
            .workspaceInspector { inspector }
            .udlStatusBar { statusSummary } actions: { statusActions }
            .task {
                if appState.playlistConfig == nil {
                    await appState.loadPlaylists()
                }
                if let requested = appState.consumeRequestedPlaylistID(),
                   appState.playlists.contains(where: { $0.id == requested }) {
                    selection = requested
                } else if selection == nil {
                    selection = appState.playlists.first?.id
                }
            }
            .onDisappear { chrome.searchText = "" }
            .sheet(item: $editing) { target in
                PlaylistDefinitionEditor(existing: target.definition)
                    .environmentObject(appState)
            }
            .sheet(isPresented: $showingProvider) { providerSheet }
            .confirmationDialog(
                "Refresh this snapshot from \(selectedProviderName)?",
                isPresented: $confirmingRefresh,
                titleVisibility: .visible
            ) {
                Button("Refresh from \(selectedProviderName)") {
                    if let id = selection { Task { await appState.refreshPlaylist(id) } }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("udl reads the current \(selectedProviderName) playlist and replaces the cached snapshot only after the complete result validates. A failed or canceled refresh keeps the snapshot that is on screen now.")
            }
    }

    private var selectedRow: PlaylistListRow? {
        appState.playlists.first { $0.id == selection }
    }

    private var selectedProviderName: String {
        selectedRow?.definition.providerDisplayName ?? "provider"
    }

    // MARK: Content

    private var content: some View {
        BoundedContent {
            VStack(alignment: .leading, spacing: 0) {
                if let notResumed = appState.notResumedMessage(for: .playlists) {
                    Callout(title: notResumed, severity: .warn)
                        .padding(.horizontal, Metrics.contentPaddingHorizontal)
                        .padding(.top, 12)
                }
                if let row = selectedRow {
                    snapshotHead(row)
                    snapshotBody(row)
                    footer
                } else {
                    EmptyStateView(
                        title: appState.playlists.isEmpty ? "No playlist definitions" : "Choose a playlist",
                        kind: .empty,
                        detail: appState.playlists.isEmpty
                            ? "playlists.yaml defines no playlists yet. Add one, then refresh it once to cache its first snapshot."
                            : "Pick a playlist in the sidebar."
                    )
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
        }
    }

    /// The mockup's `.snapshot-head`: what this playlist is, when it was last
    /// read from Music, and the row filter.
    private func snapshotHead(_ row: PlaylistListRow) -> some View {
        HStack(alignment: .bottom, spacing: 14) {
            VStack(alignment: .leading, spacing: 3) {
                Text(row.definition.name).font(Typography.sectionTitle).lineLimit(1)
                Text(subtitle(row))
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textTertiary)
                    .lineLimit(1)
            }
            Spacer(minLength: 12)
            Picker("Tracks", selection: $trackFilter) {
                ForEach(PlaylistTrackFilter.allCases) { option in
                    Text("\(option.label) \(count(of: option, in: row))").tag(option)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .frame(width: 200)
            .constrained(by: row.snapshot == nil ? "There is no cached snapshot to filter yet." : nil)
            .help("Missing shows only tracks whose local file was absent when this snapshot was taken.")
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.separator).frame(height: 0.5) }
    }

    private func subtitle(_ row: PlaylistListRow) -> String {
        let target = row.definition.providerPlaylist
            ?? row.definition.providerPlaylistID
            ?? "no Music playlist named yet"
        guard let snapshot = row.snapshot else {
            return "\(row.definition.provider) · \(target) · no cached snapshot"
        }
        return "\(row.definition.provider) · \(target) · cached \(snapshot.refreshedAt.formatted(date: .abbreviated, time: .shortened))"
    }

    @ViewBuilder private func snapshotBody(_ row: PlaylistListRow) -> some View {
        if let error = row.snapshotError {
            // A snapshot file that exists but will not parse is not the same as
            // no snapshot: udl found something and refused to trust it.
            EmptyStateView(
                title: "Cached snapshot is unreadable",
                kind: .failed(error),
                detail: "udl found a snapshot file for this playlist and could not read it. Refreshing replaces it; nothing else on this screen touches it."
            )
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if let snapshot = row.snapshot {
            let rows = visibleTracks(snapshot)
            if rows.isEmpty {
                EmptyStateView(
                    title: "No matching tracks",
                    kind: .empty,
                    detail: trackFilter == .missingLocally && chrome.searchText.isEmpty
                        ? "Every track in this cached snapshot was on disk when it was taken."
                        : "No track in the cached snapshot matches the current filter and search."
                )
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                trackTable(rows)
            }
        } else {
            EmptyStateView(
                title: "No cached snapshot",
                kind: .notRun(action: "refresh once to read this playlist from \(row.definition.providerDisplayName) and cache it"),
                detail: "Opening a playlist never contacts its provider, so nothing is fetched until you refresh. Nothing is on disk for this definition yet."
            )
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func trackTable(_ tracks: [PlaylistTrack]) -> some View {
        Table(tracks) {
            TableColumn("#") { track in
                Text(track.index, format: .number)
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
            }
            .width(38)
            TableColumn("Artist") { track in
                Text(track.artist?.isEmpty == false ? track.artist! : "—")
                    .font(Typography.control)
                    .foregroundStyle(track.isMissingLocally ? Theme.textTertiary : Theme.text)
                    .lineLimit(1)
            }
            .width(min: 110, ideal: 150)
            TableColumn("Track / local path") { track in
                VStack(alignment: .leading, spacing: 1) {
                    Text(track.title)
                        .font(Typography.control)
                        .foregroundStyle(track.isMissingLocally ? Theme.textTertiary : Theme.text)
                        .lineLimit(1)
                    Text(track.path?.isEmpty == false ? track.path! : "no local file recorded")
                        .font(Typography.monoSmall)
                        .foregroundStyle(Theme.textTertiary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            TableColumn("Album") { track in
                Text(track.album?.isEmpty == false ? track.album! : "—")
                    .font(Typography.control)
                    .foregroundStyle(Theme.textSecondary)
                    .lineLimit(1)
            }
            .width(min: 90, ideal: 132)
            TableColumn("Time") { track in
                // The same locale-formatted AppleScript real Rekordbox parses.
                Text(MusicDuration.label(track.duration))
                    .font(Typography.mono)
                    .foregroundStyle(Theme.textTertiary)
            }
            .width(58)
            TableColumn("Local") { track in
                StatusPill(
                    title: track.isMissingLocally ? "Missing" : "On disk",
                    severity: track.isMissingLocally ? .error : .ok
                )
            }
            .width(min: 86, ideal: 96)
        }
        .tableStyle(.inset(alternatesRowBackgrounds: true))
        .frame(maxWidth: .infinity, minHeight: 0, maxHeight: .infinity)
    }

    /// C11's persistent statement, plus whatever the last Music-touching
    /// operation actually did.
    private var footer: some View {
        VStack(alignment: .leading, spacing: 8) {
            Callout(
                title: "Cached snapshot — opening never contacts \(selectedProviderName).",
                detail: "\(selectedProviderName) is read only when you refresh, and a refresh that fails or is canceled keeps the snapshot you are looking at.",
                severity: .info
            )
            if let status = appState.playlistStatus {
                Callout(
                    title: status.message,
                    detail: status.preservedPreviousSnapshot
                        ? "Nothing on disk changed. The snapshot on screen is still the last valid one."
                        : nil,
                    severity: status.severity
                )
            }
        }
        .padding(.horizontal, Metrics.contentPaddingHorizontal)
        .padding(.vertical, 10)
    }

    // MARK: Filtering

    private func visibleTracks(_ snapshot: PlaylistSnapshot) -> [PlaylistTrack] {
        let query = chrome.searchText.trimmed.lowercased()
        return snapshot.tracks.filter { track in
            guard trackFilter != .missingLocally || track.isMissingLocally else { return false }
            guard !query.isEmpty else { return true }
            return [track.artist, track.title, track.album, track.path]
                .compactMap { $0?.lowercased() }
                .contains { $0.contains(query) }
        }
    }

    private func count(of filter: PlaylistTrackFilter, in row: PlaylistListRow) -> Int {
        guard let snapshot = row.snapshot else { return 0 }
        switch filter {
        case .all: return snapshot.tracks.count
        case .missingLocally: return snapshot.missingLocally
        }
    }

    // MARK: Sidebar

    private var sidebarContext: SidebarContext {
        let binding = $selection
        return SidebarContext(
            title: "Snapshots",
            items: appState.playlists.map { row in
                SidebarContextItem(
                    id: row.id,
                    title: row.definition.name,
                    subtitle: snapshotLabel(row),
                    sourceType: row.definition.provider,
                    lifecycle: lifecycle(row)
                )
            },
            selectedID: selection,
            note: appState.playlists.isEmpty
                ? "playlists.yaml defines no playlists yet."
                : "Snapshots never refresh on open. A failed or canceled refresh preserves the last valid cache.",
            select: { binding.wrappedValue = $0 }
        )
    }

    private func lifecycle(_ row: PlaylistListRow) -> Lifecycle {
        if row.snapshotError != nil { return .failed }
        if appState.playlistActiveRunID != nil && row.id == selection { return .running }
        return row.snapshot == nil ? .notRun : .done
    }

    private func snapshotLabel(_ row: PlaylistListRow) -> String {
        if row.snapshotError != nil { return "unreadable snapshot" }
        guard let snapshot = row.snapshot else { return "never refreshed" }
        let missing = snapshot.missingLocally
        return missing > 0
            ? "\(snapshot.tracks.count) cached · \(missing) missing"
            : "\(snapshot.tracks.count) cached tracks"
    }

    // MARK: Inspector

    @ViewBuilder private var inspector: some View {
        InspectorSection(title: "Snapshot") {
            if let row = selectedRow, let snapshot = row.snapshot {
                FieldRow("Tracks", "\(snapshot.tracks.count)")
                FieldRow(label: "Missing locally") {
                    StatusPill(
                        title: "\(snapshot.missingLocally)",
                        severity: snapshot.missingLocally > 0 ? .warn : .ok,
                        showsDot: false
                    )
                }
                FieldRow("Last refreshed", snapshot.refreshedAt.formatted(date: .abbreviated, time: .shortened))
                FieldRow("Snapshot version", "\(snapshot.version)")
                PathField(label: "checksum_sha256", path: snapshot.checksumSHA256)
                ConstraintNote(
                    text: "Read from the cached JSON on disk. \(row.definition.providerDisplayName) has not been contacted in this session unless you refreshed."
                )
                ConstraintNote(
                    text: "\"Missing locally\" is what udl saw when this snapshot was taken. A file deleted since then still reads as on disk until the next refresh."
                )
            } else if selectedRow?.snapshotError != nil {
                ConstraintNote(
                    text: "The snapshot file exists but could not be read, so it reports no track count, no timestamp and no checksum.",
                    severity: .error,
                    symbol: "exclamationmark.octagon"
                )
            } else {
                ConstraintNote(text: "No cached snapshot for this playlist yet. Nothing is read until you refresh.")
            }
        }

        if let definition = selectedRow?.definition {
            InspectorSection(title: "Send snapshot to") {
                handoff(
                    title: "SoundCloud Free DL",
                    symbol: "sparkle.magnifyingglass",
                    target: definition.defaultFreeDLJob,
                    emptyLabel: "No job mapped",
                    destination: .freeDL
                )
                handoff(
                    title: "Rekordbox Sync",
                    symbol: "square.stack.3d.up",
                    target: definition.defaultRekordboxTarget,
                    emptyLabel: "No target mapped",
                    destination: .rekordbox,
                    playlistID: definition.id
                )
                ConstraintNote(
                    text: "These are the definition's stored defaults in playlists.yaml. Opening a workflow from here navigates to it; it does not start a run."
                )
            }

            InspectorSection(title: "Definition") {
                FieldRow("ID", definition.id)
                FieldRow("Provider", definition.provider)
                FieldRow("Provider playlist", definition.providerPlaylist ?? "—")
                FieldRow("Persistent ID", definition.providerPlaylistID ?? "—")
                Button("Edit playlist definition…") { editing = .edit(definition) }
                    .frame(maxWidth: .infinity)
            }
        }

        InspectorSection(title: "Feature config") {
            if let config = appState.playlistConfig {
                PathField(label: "playlists.yaml", path: config.path)
                FieldRow("Definitions", "\(config.config.playlists.count)")
            } else {
                ConstraintNote(text: "playlists.config.read has not returned yet.")
            }
        }

        InspectorSection(title: "Not available") {
            // C16 in this screen's terms: the mockup's export button had no
            // protocol behind it.
            ConstraintNote(
                text: "There is no export: playlists.list reports no snapshot file path and udl has no export method, so a \"save a copy\" button would be writing a file this app invented rather than one udl produced."
            )
        }
    }

    private func handoff(
        title: String,
        symbol: String,
        target: String?,
        emptyLabel: String,
        destination: AppState.Destination,
        playlistID: String? = nil
    ) -> some View {
        HStack(spacing: 9) {
            Image(systemName: symbol)
                .foregroundStyle(target == nil ? Theme.textTertiary : Theme.accent)
                .frame(width: 18)
            VStack(alignment: .leading, spacing: 1) {
                Text(title).font(Typography.control)
                Text(target ?? emptyLabel)
                    .font(Typography.monoSmall)
                    .foregroundStyle(Theme.textTertiary)
                    .lineLimit(1)
            }
            Spacer(minLength: 6)
            if target == nil {
                Button("Map…") {
                    if let definition = selectedRow?.definition { editing = .edit(definition) }
                }
            } else {
                Button("Open") {
                    if destination == .rekordbox, let playlistID {
                        appState.openRekordboxPlaylist(playlistID)
                    } else {
                        appState.destination = destination
                    }
                }
                .constrained(by: playlistID != nil && selectedRow?.snapshot == nil
                    ? "Refresh this snapshot before sending it to Rekordbox."
                    : nil)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: Provider sheet

    private var providerSheet: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Apple Music playlists").font(Typography.sectionTitle)
            ConstraintNote(
                text: "This list is fetched only by this explicit action, and it changes no snapshot. Opening Playlists never contacts Music.app."
            )
            if appState.providerPlaylists.isEmpty {
                EmptyStateView(
                    title: "Music playlists",
                    kind: appState.playlistActiveRunID == nil
                        ? .empty
                        : .working,
                    detail: appState.playlistActiveRunID == nil
                        ? "Music returned no playlists."
                        : nil
                )
                .frame(minHeight: 260)
            } else {
                List(appState.providerPlaylists) { item in
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(item.name).font(Typography.control)
                            if item.smart == true {
                                Text("Smart playlist")
                                    .font(Typography.caption)
                                    .foregroundStyle(Theme.textTertiary)
                            }
                        }
                        Spacer()
                        Text("\(item.trackCount) tracks").font(Typography.mono)
                    }
                }
            }
            HStack {
                if appState.playlistActiveRunID != nil {
                    Button("Cancel", role: .destructive) {
                        Task { await appState.cancelPlaylistOperation() }
                    }
                }
                Spacer()
                Button("Done") { showingProvider = false }
                    .buttonStyle(.borderedProminent)
            }
        }
        .padding(24)
        .frame(width: 620, height: 520)
    }

    // MARK: Status bar

    private var statusSummary: some View {
        SummaryLine {
            if let row = selectedRow, let snapshot = row.snapshot {
                SummaryCount(value: snapshot.tracks.count, noun: "cached tracks", severity: .idle)
                Text("·")
                SummaryCount(
                    value: snapshot.missingLocally,
                    noun: "missing locally",
                    severity: snapshot.missingLocally > 0 ? .warn : .ok
                )
                Text("·")
                Text(appState.playlistActiveRunID == nil ? "cache intact" : "reading Music…")
                    .foregroundStyle(Theme.textSecondary)
            } else if selectedRow != nil {
                Text("No cached snapshot")
                    .foregroundStyle(Theme.warn)
                Text("· refresh required")
                    .foregroundStyle(Theme.textSecondary)
            } else {
                SummaryCount(value: appState.playlists.count, noun: "playlists", severity: .idle)
            }
            if appState.attention.playlistsWithoutSnapshot > 0 {
                Text("·")
                SummaryCount(
                    value: appState.attention.playlistsWithoutSnapshot,
                    noun: "without a snapshot",
                    severity: .warn
                )
            }
            if trackFilter != .all {
                Text("·")
                Text("Filtered to \(trackFilter.label.lowercased()).").foregroundStyle(Theme.accent)
            }
        }
    }

    @ViewBuilder private var statusActions: some View {
        if appState.playlistActiveRunID != nil {
            Button("Cancel refresh", role: .destructive) {
                Task { await appState.cancelPlaylistOperation() }
            }
            .help("Canceling keeps the snapshot that is on screen. udl replaces a snapshot only after a complete refresh validates.")
        } else {
            Button("Refresh from \(selectedProviderName)…") { confirmingRefresh = true }
                .buttonStyle(.borderedProminent)
                .constrained(by: selection == nil ? "Select a playlist to refresh." : appState.busyReason)
                .help("Reads this playlist from \(selectedProviderName) and replaces the cached snapshot only on success.")
        }
    }
}

/// The mockup's All / Missing segmented control.
enum PlaylistTrackFilter: String, CaseIterable, Identifiable {
    case all
    case missingLocally

    var id: String { rawValue }

    var label: String {
        switch self {
        case .all: "All"
        case .missingLocally: "Missing"
        }
    }
}

/// One sheet does both add and edit, because `playlists.saveDefinition`
/// upserts by ID.
enum PlaylistEditorTarget: Identifiable {
    case add
    case edit(PlaylistDefinition)

    var id: String {
        switch self {
        case .add: "<add>"
        case .edit(let definition): definition.id
        }
    }

    var definition: PlaylistDefinition? {
        switch self {
        case .add: nil
        case .edit(let definition): definition
        }
    }
}

extension PlaylistTrack {
    /// `missing_local` is omitted rather than sent as `false` for a track udl
    /// found, so absence means present.
    var isMissingLocally: Bool { missingLocal == true }
}

extension PlaylistSnapshot {
    var missingLocally: Int { tracks.filter(\.isMissingLocally).count }
}
