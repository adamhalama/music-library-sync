import SwiftUI

struct PlaylistsView: View {
    @EnvironmentObject private var appState: AppState
    @State private var selection: String?
    @State private var showingDefinition = false
    @State private var showingProvider = false

    var body: some View {
        HSplitView {
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    Text("PLAYLIST CACHE").font(.caption.bold().monospaced()).foregroundStyle(.orange)
                    Spacer()
                    Button { showingDefinition = true } label: { Image(systemName: "plus") }
                    Button { showingProvider = true; Task { await appState.discoverProviderPlaylists() } } label: {
                        Image(systemName: "music.note")
                    }
                    Button { Task { await appState.loadPlaylists() } } label: { Image(systemName: "arrow.clockwise") }
                }
                List(selection: $selection) {
                    ForEach(Array(appState.playlists.enumerated()), id: \.offset) { _, row in
                        VStack(alignment: .leading, spacing: 3) {
                            Text(row.definition.name)
                            Text(snapshotLabel(row))
                                .font(.caption.monospaced())
                                .foregroundStyle(row.snapshotError == nil ? Color.secondary : Color.red)
                        }
                        .tag(row.id)
                    }
                }
            }
            .padding(20)
            .frame(minWidth: 260, idealWidth: 310)

            detail
                .frame(minWidth: 580)
        }
        .task {
            if appState.playlistConfig == nil {
                await appState.loadPlaylists()
            }
            selection = selection ?? appState.playlists.first?.id
        }
        .sheet(isPresented: $showingDefinition) {
            PlaylistDefinitionEditor()
                .environmentObject(appState)
        }
        .sheet(isPresented: $showingProvider) {
            VStack(alignment: .leading, spacing: 14) {
                Text("Apple Music playlists").font(.title2.bold())
                Text("This list is fetched only by this explicit action. Opening Playlists remains offline.")
                    .foregroundStyle(.secondary)
                List(appState.providerPlaylists) { item in
                    HStack {
                        VStack(alignment: .leading) {
                            Text(item.name)
                            if item.smart == true { Text("Smart playlist").font(.caption).foregroundStyle(.secondary) }
                        }
                        Spacer()
                        Text("\(item.trackCount) tracks").font(.caption.monospaced())
                    }
                }
                if appState.playlistActiveRunID != nil {
                    Button("Cancel", role: .destructive) { Task { await appState.cancelPlaylistOperation() } }
                }
                Button("Done") { showingProvider = false }
            }
            .padding(24)
            .frame(width: 620, height: 520)
        }
    }

    @ViewBuilder private var detail: some View {
        if let row = appState.playlists.first(where: { $0.id == selection }) {
            VStack(alignment: .leading, spacing: 16) {
                HStack {
                    VStack(alignment: .leading) {
                        Text(row.definition.name).font(.largeTitle.bold())
                        Text("\(row.definition.provider) · \(row.definition.providerPlaylist ?? row.definition.providerPlaylistID ?? "unresolved")")
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    if appState.playlistActiveRunID != nil {
                        Button("Cancel refresh", role: .destructive) {
                            Task { await appState.cancelPlaylistOperation() }
                        }
                    } else {
                        Button("Refresh from Music…") {
                            Task { await appState.refreshPlaylist(row.id) }
                        }
                        .buttonStyle(.borderedProminent)
                    }
                }
                Text("Snapshots are cache-first. Music is contacted only when you explicitly refresh.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                if let error = row.snapshotError {
                    ContentUnavailableView("Snapshot is invalid", systemImage: "exclamationmark.triangle", description: Text(error))
                } else if let snapshot = row.snapshot {
                    HStack {
                        Text("\(snapshot.tracks.count) tracks")
                        Text("Refreshed \(snapshot.refreshedAt.formatted())")
                        Spacer()
                        Text(String(snapshot.checksumSHA256.prefix(12))).font(.caption.monospaced())
                    }
                    .foregroundStyle(.secondary)
                    Table(snapshot.tracks) {
                        TableColumn("#") { Text(String($0.index)) }.width(36)
                        TableColumn("Title", value: \.title)
                        TableColumn("Artist") { Text($0.artist ?? "—") }
                        TableColumn("Local") {
                            Label($0.missingLocal == true ? "Missing" : "Present",
                                  systemImage: $0.missingLocal == true ? "exclamationmark.circle" : "checkmark.circle")
                                .foregroundStyle($0.missingLocal == true ? .orange : .green)
                        }
                        .width(100)
                    }
                } else {
                    ContentUnavailableView("No saved snapshot", systemImage: "externaldrive.badge.questionmark",
                                           description: Text("Refresh explicitly to create the first snapshot."))
                }
                if let status = appState.playlistStatusMessage {
                    Text(status).foregroundStyle(.secondary)
                }
            }
            .padding(28)
        } else {
            ContentUnavailableView("Choose a playlist", systemImage: "music.note.list")
        }
    }

    private func snapshotLabel(_ row: PlaylistListRow) -> String {
        if row.snapshotError != nil { return "invalid snapshot" }
        if let snapshot = row.snapshot { return "\(snapshot.tracks.count) cached tracks" }
        return "snapshot missing"
    }
}

private struct PlaylistDefinitionEditor: View {
    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss
    @State private var id = ""
    @State private var name = ""
    @State private var providerName = ""
    @State private var providerID = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Add playlist definition").font(.title2.bold())
            TextField("Stable ID", text: $id)
            TextField("Display name", text: $name)
            TextField("Music playlist name", text: $providerName)
            TextField("Music persistent ID (optional)", text: $providerID)
            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                Button("Save") {
                    let definition = PlaylistDefinition(
                        id: id, name: name, provider: "apple_music",
                        providerPlaylist: providerName, providerPlaylistID: providerID,
                        defaultFreeDLJob: nil, defaultRekordboxTarget: nil
                    )
                    Task {
                        if await appState.savePlaylistDefinition(definition) { dismiss() }
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(id.trimmingCharacters(in: .whitespaces).isEmpty ||
                          name.trimmingCharacters(in: .whitespaces).isEmpty ||
                          (providerName.trimmingCharacters(in: .whitespaces).isEmpty &&
                           providerID.trimmingCharacters(in: .whitespaces).isEmpty))
            }
        }
        .textFieldStyle(.roundedBorder)
        .padding(24)
        .frame(width: 520)
    }
}
