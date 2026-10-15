import AppKit
import SwiftUI

/// A real `Settings` scene (⌘,). Settings is no longer a sidebar destination:
/// nothing here is a workflow.
struct SettingsView: View {
    var body: some View {
        TabView {
            GeneralSettingsView()
                .tabItem { Label("General", systemImage: "gearshape") }
            BackendSettingsView()
                .tabItem { Label("Backend", systemImage: "terminal") }
        }
        .frame(width: 520, height: 380)
    }
}

private struct GeneralSettingsView: View {
    @EnvironmentObject private var appState: AppState

    var body: some View {
        Form {
            Section("Working directory") {
                Text(appState.projectDirectory.path)
                    .font(Typography.monoSmall)
                    .foregroundStyle(Theme.textSecondary)
                    .textSelection(.enabled)
                HStack {
                    Button("Choose…") { chooseDirectory() }
                    Spacer()
                }
                ConstraintNote(
                    text: "Changing the working directory restarts the backend. Nothing in flight is resumed or replayed."
                )
            }

            Section("Appearance") {
                ConstraintNote(
                    text: "UDL follows the system light and dark appearance and your system accent color. There is no in-app theme."
                )
            }
        }
        .formStyle(.grouped)
    }

    private func chooseDirectory() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.directoryURL = appState.projectDirectory
        guard panel.runModal() == .OK, let url = panel.url else { return }
        Task { await appState.useProjectDirectory(url) }
    }
}

private struct BackendSettingsView: View {
    @EnvironmentObject private var appState: AppState

    var body: some View {
        Form {
            Section("Agent") {
                FieldRow("Status", appState.backend.state.label)
                FieldRow("Version", appState.backendVersion)
                if let build = appState.initialization?.build {
                    FieldRow("Commit", build.commit)
                    FieldRow("Built", build.date)
                }
                if !appState.backend.backendPath.isEmpty {
                    PathField(label: "Executable", path: appState.backend.backendPath)
                }
                HStack {
                    Button("Restart Backend") { Task { await appState.restart() } }
                    Spacer()
                }
            }

            if let initialization = appState.initialization {
                Section("Configuration paths") {
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
        .formStyle(.grouped)
    }
}
