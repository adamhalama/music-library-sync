import SwiftUI

/// `rekordbox.config.write` — the paths and mappings sheet, unchanged in
/// behaviour from the pre-redesign editor and moved out of `RekordboxView` so
/// that file is one screen rather than two.
///
/// This edits `rekordbox.yaml`, never a plan. The plan is opaque and
/// checksummed (C9) and nothing on this sheet can reach it.
struct RekordboxConfigEditor: View {
    @EnvironmentObject private var appState: AppState
    @Environment(\.dismiss) private var dismiss
    @State var config: RekordboxConfig
    @State private var mappingID = ""
    @State private var musicFolder = ""
    @State private var rekordboxFolder = ""
    @State private var saveError: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Rekordbox configuration").font(Typography.sectionTitle)
                Text("rekordbox.config.write re-marshals the whole file canonically.")
                    .font(Typography.caption)
                    .foregroundStyle(Theme.textSecondary)
            }

            Form {
                TextField("Database directory", text: $config.defaults.dbDir)
                TextField("Backup directory", text: $config.defaults.backupDir)
                TextField(
                    "Python binary",
                    text: Binding(
                        get: { config.defaults.pythonBin ?? "" },
                        set: { config.defaults.pythonBin = $0.isEmpty ? nil : $0 }
                    )
                )
                Section("Folder mappings") {
                    ForEach(config.sync.folders) { mapping in
                        LabeledContent(
                            mapping.id,
                            value: "\(mapping.musicFolder ?? mapping.musicFolderID ?? "—") → \(mapping.rekordboxFolder ?? mapping.rekordboxFolderID ?? "—")"
                        )
                    }
                    TextField("Mapping ID", text: $mappingID)
                    TextField("Music folder", text: $musicFolder)
                    TextField("Rekordbox folder", text: $rekordboxFolder)
                    Button("Add mapping") {
                        config.sync.folders.append(RekordboxFolderMapping(
                            id: mappingID, musicFolder: musicFolder, musicFolderID: nil,
                            rekordboxFolder: rekordboxFolder, rekordboxFolderID: nil,
                            playlistNameMap: nil, includePlaylists: nil, excludePlaylists: nil,
                            onMissingTracks: "fail", createFolders: nil, createPlaylists: nil
                        ))
                        mappingID = ""; musicFolder = ""; rekordboxFolder = ""
                    }
                    .constrained(
                        by: (mappingID.isEmpty || musicFolder.isEmpty || rekordboxFolder.isEmpty)
                            ? "A mapping needs an id, a Music folder and a Rekordbox folder."
                            : nil
                    )
                }
            }
            .formStyle(.grouped)

            if let saveError {
                Callout(title: saveError, severity: .error)
            }
            ConstraintNote(
                text: "Changing these paths does not change an existing plan. Regenerate the plan afterwards so its checksum covers the new inputs."
            )

            HStack {
                Button("Cancel", role: .cancel) { dismiss() }
                Spacer()
                Button("Save canonical config") {
                    Task {
                        if await appState.saveRekordboxConfig(config) {
                            dismiss()
                        } else {
                            saveError = "rekordbox.yaml was not written. The backend rejected these values."
                        }
                    }
                }
                .buttonStyle(.borderedProminent)
            }
        }
        .padding(24)
        .frame(width: 720, height: 620)
    }
}
