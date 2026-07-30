import AppKit
import SwiftUI

struct OnboardingView: View {
    @EnvironmentObject private var appState: AppState
    @State private var showingSource = false
    @State private var confirmingInvalidReplacement = false
    @State private var source: MainConfigSource?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                Text("WELCOME TO UDL").font(.caption.bold().monospaced()).foregroundStyle(.orange)
                Text(headline).font(.largeTitle.bold())
                Text("Project directory: \(appState.projectDirectory.path)")
                    .font(.caption.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)

                if let state = appState.onboarding?.state {
                    GroupBox("Setup state") {
                        VStack(alignment: .leading, spacing: 6) {
                            Text(state.reason.replacingOccurrences(of: "_", with: " ").uppercased())
                                .font(.caption.bold().monospaced())
                            Text("Config: \(state.configPath)").textSelection(.enabled)
                            ForEach(state.detailLines, id: \.self) { Text($0).foregroundStyle(.secondary) }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(6)
                    }
                }

                GroupBox("1 · Choose context") {
                    HStack {
                        Text("Home is the safe default. Choose a project folder when it contains project-specific config.")
                            .foregroundStyle(.secondary)
                        Spacer()
                        Button("Choose folder…") { chooseProjectDirectory() }
                    }
                    .padding(6)
                }

                GroupBox("2 · Check dependencies") {
                    HStack {
                        if let doctor = appState.doctor {
                            Text("\(doctor.checks.filter { $0.severity == .error }.count) errors · \(doctor.checks.filter { $0.severity == .warn }.count) warnings")
                        }
                        Spacer()
                        Button("Run Doctor") { Task { await appState.refreshDoctor() } }
                    }
                    .padding(6)
                }

                GroupBox("3 · Add the first source") {
                    HStack {
                        if let source {
                            Text("\(source.id) · \(source.type) · \(source.adapter.kind)")
                        } else {
                            Text("No source drafted yet.").foregroundStyle(.secondary)
                        }
                        Spacer()
                        Button(source == nil ? "Create source…" : "Edit source…") { showingSource = true }
                    }
                    .padding(6)
                }

                GroupBox("4 · Credentials") {
                    HStack {
                        Text("Credentials are optional during setup. Replacement fields always begin empty.")
                            .foregroundStyle(.secondary)
                        Spacer()
                        Button("Open Credentials") { appState.destination = .credentials }
                    }
                    .padding(6)
                }

                HStack {
                    Text("Setup can be closed and resumed; no file is written until Finish Setup.")
                        .font(.caption).foregroundStyle(.secondary)
                    Spacer()
                    Button("Finish Setup") {
                        guard let source else { return }
                        if appState.onboarding?.state.reason == "invalid_config" {
                            confirmingInvalidReplacement = true
                        } else {
                            Task { _ = await appState.createInitialConfig(source: source) }
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(source == nil)
                }
            }
            .padding(36)
            .frame(maxWidth: 850, alignment: .leading)
        }
        .sheet(isPresented: $showingSource) {
            SourceEditorView(source: source) { source = $0 }
        }
        .confirmationDialog(
            "Replace the invalid configuration?",
            isPresented: $confirmingInvalidReplacement,
            titleVisibility: .visible
        ) {
            Button("Replace with validated config", role: .destructive) {
                guard let source else { return }
                Task { _ = await appState.createInitialConfig(source: source) }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This explicit action canonically replaces the invalid file. Cancel to repair it outside UDL instead.")
        }
    }

    private var headline: String {
        switch appState.onboarding?.state.reason {
        case "invalid_config": "Let’s repair the configuration."
        case "no_sources": "Add your first music source."
        default: "Build a dependable music pipeline."
        }
    }

    private func chooseProjectDirectory() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.directoryURL = appState.projectDirectory
        if panel.runModal() == .OK, let url = panel.url {
            Task { await appState.useProjectDirectory(url) }
        }
    }
}
