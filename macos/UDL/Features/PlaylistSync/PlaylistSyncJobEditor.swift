import SwiftUI

struct PlaylistSyncJobEditor: View {
    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss

    let existing: PlaylistSyncJob?
    @State private var id: String
    @State private var rekordboxName: String
    @State private var rekordboxID: String
    @State private var navidromeName: String
    @State private var navidromeID: String
    @State private var isSaving = false

    init(existing: PlaylistSyncJob? = nil) {
        self.existing = existing
        _id = State(initialValue: existing?.id ?? "")
        _rekordboxName = State(initialValue: existing?.rekordbox.playlist ?? "")
        _rekordboxID = State(initialValue: existing?.rekordbox.playlistID ?? "")
        _navidromeName = State(initialValue: existing?.navidrome.playlist ?? "")
        _navidromeID = State(initialValue: existing?.navidrome.playlistID ?? "")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(existing == nil ? "Add playlist sync job" : "Edit playlist sync job")
                .font(Typography.sectionTitle)
            Form {
                Section("Pair") {
                    TextField("Stable job ID", text: $id)
                        .constrained(by: existing == nil ? nil : "The job ID names its binding state file and cannot be changed here.")
                    if existing != nil {
                        ConstraintNote(text: "Changing either selector invalidates the saved binding and requires a fresh plan. It never rewrites a provider ID into playlists.yaml.")
                    }
                }
                Section("Rekordbox") {
                    TextField("Playlist name", text: $rekordboxName)
                    TextField("Playlist ID (optional)", text: $rekordboxID)
                }
                Section("Navidrome") {
                    TextField("Playlist name", text: $navidromeName)
                    TextField("Playlist ID (optional)", text: $navidromeID)
                }
                ConstraintNote(text: "Both names are required because either side may become a new destination. IDs are optional stable selectors. Credentials and last-run state are never stored here.")
            }
            .formStyle(.grouped)

            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                if isSaving { ProgressView().controlSize(.small) }
                Button("Save pair") { save() }
                    .buttonStyle(.borderedProminent)
                    .constrained(by: saveConstraint)
            }
        }
        .padding(20)
        .frame(width: 570, height: 500)
    }

    private var saveConstraint: String? {
        if isSaving { return "Writing playlists.yaml…" }
        if id.trimmed.isEmpty || rekordboxName.trimmed.isEmpty || navidromeName.trimmed.isEmpty {
            return "A stable ID and both playlist names are required."
        }
        return nil
    }

    private func save() {
        isSaving = true
        let job = PlaylistSyncJob(
            id: id.trimmed,
            rekordbox: PlaylistSyncSelector(
                playlist: rekordboxName.trimmed,
                playlistID: rekordboxID.trimmed.isEmpty ? nil : rekordboxID.trimmed
            ),
            navidrome: PlaylistSyncSelector(
                playlist: navidromeName.trimmed,
                playlistID: navidromeID.trimmed.isEmpty ? nil : navidromeID.trimmed
            )
        )
        Task {
            let saved = await appState.savePlaylistSyncJob(job)
            isSaving = false
            if saved { dismiss() }
        }
    }
}
