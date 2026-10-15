import AppKit
import SwiftUI

struct OnboardingView: View {
    @EnvironmentObject private var appState: AppState
    @State private var showingSource = false
    @State private var confirmingInvalidReplacement = false
    @State private var source: MainConfigSource?
    @State private var attemptedFinish = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                WorkspaceHeader(
                    title: headline,
                    lede: "Setup can be closed and resumed. No file is written until you finish."
                )
                PathField(label: "Project directory", path: appState.projectDirectory.path)

                if let state = appState.onboarding?.state {
                    Card(title: "Setup state", subtitle: "startup.onboardingState") {
                        StatusPill(
                            title: state.reason.replacingOccurrences(of: "_", with: " "),
                            severity: state.reason == "invalid_config" ? .error : .warn
                        )
                        PathField(label: "Config", path: state.configPath)
                        ForEach(state.detailLines, id: \.self) { line in
                            Text(line)
                                .font(Typography.control)
                                .foregroundStyle(Theme.textSecondary)
                        }
                    }
                }

                Card(title: "1 · Choose context") {
                    HStack {
                        Text("Home is the safe default. Choose a project folder when it contains project-specific config.")
                            .font(Typography.control)
                            .foregroundStyle(Theme.textSecondary)
                        Spacer()
                        Button("Choose folder…") { chooseProjectDirectory() }
                    }
                    .padding(6)
                }

                Card(title: "2 · Check dependencies") {
                    HStack {
                        if appState.doctor != nil {
                            let attention = appState.attention
                            Text("\(attention.doctorErrors) errors · \(attention.doctorWarnings) warnings")
                                .font(Typography.control)
                        } else {
                            Text("Not run yet.")
                                .font(Typography.control)
                                .foregroundStyle(Theme.textSecondary)
                        }
                        Spacer()
                        Button("Run Doctor") { Task { await appState.refreshDoctor() } }
                    }
                    .padding(6)
                }

                Card(title: "3 · Add the first source") {
                    HStack {
                        if let source {
                            Text("\(source.id) · \(source.type) · \(source.adapter.kind)")
                                .font(Typography.control)
                        } else {
                            Text("No source drafted yet.")
                                .font(Typography.control)
                                .foregroundStyle(Theme.textSecondary)
                        }
                        Spacer()
                        Button(source == nil ? "Create source…" : "Edit source…") { showingSource = true }
                    }
                    .padding(6)
                }

                Card(title: "4 · Credentials") {
                    HStack {
                        // C12 — editors never preload an existing secret.
                        ConstraintNote(
                            text: "Credentials are optional during setup. Every editor starts empty; the existing value is never loaded or shown."
                        )
                        Spacer()
                        Button("Open Credentials") { appState.destination = .credentials }
                    }
                    .padding(6)
                }

                // A refused write used to leave this screen looking untouched:
                // `saveMainConfig` records the reason in `configProblems` /
                // `configStatusMessage`, both of which only the Config screen
                // renders. Finishing setup is the one action here, so its
                // failure has to be visible here.
                if !appState.configProblems.isEmpty {
                    Callout(
                        title: "udl refused the configuration. Nothing was written.",
                        detail: appState.configProblems.joined(separator: "\n"),
                        severity: .error
                    )
                } else if attemptedFinish, let message = appState.configStatusMessage {
                    Callout(title: message, severity: .error)
                }

                ConstraintNote(
                    text: "No file is written until you press Finish Setup, which now lives in the status bar with every other primary action."
                )
            }
            .padding(.horizontal, Metrics.contentPaddingHorizontal)
            .padding(.vertical, Metrics.contentPadding)
            .frame(maxWidth: 850, alignment: .leading)
        }
        .workspaceToolbar(title: "Welcome", subtitle: "startup.onboardingState")
        .udlStatusBar {
            SummaryLine {
                Text(steps.completed == steps.total
                     ? "Ready to write the first config file."
                     : "\(steps.completed) of \(steps.total) steps done")
                if let reason = appState.onboarding?.state.reason {
                    Text("·")
                    Text(reason.replacingOccurrences(of: "_", with: " "))
                        .foregroundStyle(Theme.textSecondary)
                }
            }
        } actions: {
            // Failure mode 1 — a disabled primary states its reason on screen,
            // not only in a tooltip nobody hovers.
            if source == nil {
                ConstraintNote(text: "Add a source first; udl will not accept a config with none.")
            }
            Button("Finish Setup") { finish() }
                .buttonStyle(.borderedProminent)
                .disabled(source == nil)
                .help(source == nil
                      ? "Add a source first; udl will not accept a config with none."
                      : "Writes the first config.yaml and restarts the backend against it.")
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

    private func finish() {
        guard let source else { return }
        attemptedFinish = true
        if appState.onboarding?.state.reason == "invalid_config" {
            confirmingInvalidReplacement = true
        } else {
            Task { _ = await appState.createInitialConfig(source: source) }
        }
    }

    /// Only the two steps that gate Finish Setup are counted. Choosing a
    /// project folder and running Doctor are optional, and counting them would
    /// make a ready setup read as incomplete.
    private var steps: (completed: Int, total: Int) {
        var done = 0
        if appState.doctor != nil { done += 1 }
        if source != nil { done += 1 }
        return (done, 2)
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
