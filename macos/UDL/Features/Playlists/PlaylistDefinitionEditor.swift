import SwiftUI

/// Add or edit one entry in `playlists.yaml`.
///
/// `playlists.saveDefinition` upserts by ID, so the same sheet does both. The
/// two handoff fields are edited here rather than on the inspector's handoff
/// rows, because they are stored config, not a per-session choice: the
/// inspector's "Map…" button opens this.
struct PlaylistDefinitionEditor: View {
    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss

    /// `nil` adds; a value edits that definition in place.
    let existing: PlaylistDefinition?

    @State private var id: String
    @State private var name: String
    @State private var providerName: String
    @State private var providerID: String
    @State private var freeDLJob: String
    @State private var rekordboxTarget: String
    @State private var isSaving = false

    init(existing: PlaylistDefinition? = nil) {
        self.existing = existing
        _id = State(initialValue: existing?.id ?? "")
        _name = State(initialValue: existing?.name ?? "")
        _providerName = State(initialValue: existing?.providerPlaylist ?? "")
        _providerID = State(initialValue: existing?.providerPlaylistID ?? "")
        _freeDLJob = State(initialValue: existing?.defaultFreeDLJob ?? "")
        _rekordboxTarget = State(initialValue: existing?.defaultRekordboxTarget ?? "")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(existing == nil ? "Add playlist definition" : "Edit playlist definition")
                .font(Typography.sectionTitle)

            Form {
                Section("Identity") {
                    TextField("Stable ID", text: $id)
                        .disabled(existing != nil)
                        .help(existing == nil
                              ? "Used for the snapshot file name; must be unique."
                              : "The ID names the snapshot file on disk, so it is fixed once a definition exists.")
                    if existing != nil {
                        ConstraintNote(
                            text: "The ID names this playlist's snapshot file, so changing it here would orphan the cached snapshot rather than rename it."
                        )
                    }
                    TextField("Display name", text: $name)
                }

                Section("Apple Music") {
                    TextField("Playlist name", text: $providerName)
                    TextField("Persistent ID (optional)", text: $providerID)
                    ConstraintNote(
                        text: "Saving this definition writes playlists.yaml only. It does not contact Music.app and does not create a snapshot; refresh does that, explicitly."
                    )
                }

                Section("Hand off to") {
                    TextField("Free DL job ID", text: $freeDLJob, prompt: Text("none"))
                    TextField("Rekordbox job or mapping ID", text: $rekordboxTarget, prompt: Text("none"))
                    ConstraintNote(
                        text: "These are the definition's default_freedl_job and default_rekordbox_target. udl does not validate that the referenced job exists, so a typo shows up as an unresolved handoff, not a save error."
                    )
                }
            }
            .formStyle(.grouped)

            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                if isSaving { ProgressView().controlSize(.small) }
                Button("Save") { save() }
                    .buttonStyle(.borderedProminent)
                    .constrained(by: isSaving
                                 ? "Writing playlists.yaml…"
                                 : (isValid ? nil : "An id, a display name, and either a Music playlist name or a persistent ID are required."))
                    .help("Writes this definition into playlists.yaml. Music.app is not contacted.")
            }
        }
        .padding(20)
        .frame(width: 560, height: 560)
    }

    private var isValid: Bool {
        !id.trimmed.isEmpty && !name.trimmed.isEmpty
            && !(providerName.trimmed.isEmpty && providerID.trimmed.isEmpty)
    }

    private func save() {
        isSaving = true
        let definition = PlaylistDefinition(
            id: id.trimmed,
            name: name.trimmed,
            provider: existing?.provider ?? "apple_music",
            providerPlaylist: providerName.trimmed.isEmpty ? nil : providerName.trimmed,
            providerPlaylistID: providerID.trimmed.isEmpty ? nil : providerID.trimmed,
            defaultFreeDLJob: freeDLJob.trimmed.isEmpty ? nil : freeDLJob.trimmed,
            defaultRekordboxTarget: rekordboxTarget.trimmed.isEmpty ? nil : rekordboxTarget.trimmed
        )
        Task {
            let saved = await appState.savePlaylistDefinition(definition)
            isSaving = false
            if saved { dismiss() }
        }
    }
}

extension String {
    var trimmed: String { trimmingCharacters(in: .whitespacesAndNewlines) }
}
