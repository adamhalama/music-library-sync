import AppKit
import SwiftUI

@main
struct UDLApp: App {
    @StateObject private var appState = AppState()
    @StateObject private var chrome = ShellChrome()

    var body: some Scene {
        WindowGroup {
            AppShellView()
                .environmentObject(appState)
                .environmentObject(chrome)
                .frame(minWidth: 1080, minHeight: 640)
                .task { await appState.start() }
                .alert("UDL", isPresented: Binding(
                    get: { appState.alertMessage != nil },
                    set: { if !$0 { appState.alertMessage = nil } }
                )) {
                    Button("Dismiss") { appState.alertMessage = nil }
                    if appState.alertOffersAutomationSettings {
                        Button("Open Automation Settings") { appState.openAutomationSettings() }
                    }
                    Button("Restart Backend") { Task { await appState.restart() } }
                } message: {
                    Text(appState.alertMessage ?? "")
                }
                // C1 — the plan prompt is docked into the Sync workspace, so
                // only confirm and masked-input prompts stay real modals.
                .sheet(item: $appState.modalPrompt) { _ in
                    PendingPromptView()
                        .environmentObject(appState)
                }
        }
        .commands {
            CommandGroup(replacing: .appTermination) {
                Button("Quit UDL") {
                    Task {
                        await appState.shutdown()
                        NSApplication.shared.terminate(nil)
                    }
                }
                .keyboardShortcut("q")
            }
            CommandGroup(after: .toolbar) {
                Button("Toggle Sidebar") {
                    NSApp.keyWindow?.firstResponder?.tryToPerform(
                        #selector(NSSplitViewController.toggleSidebar(_:)),
                        with: nil
                    )
                }
                .keyboardShortcut("s", modifiers: [.command, .option])

                Button(chrome.inspectorVisible ? "Hide Inspector" : "Show Inspector") {
                    chrome.toggleInspector()
                }
                .keyboardShortcut("i", modifiers: [.command, .option])

                Divider()

                ForEach(Array(AppState.Destination.workflows.enumerated()), id: \.element) { index, destination in
                    Button(destination.rawValue) { appState.destination = destination }
                        .keyboardShortcut(
                            KeyEquivalent(Character("\(index + 1)")),
                            modifiers: .command
                        )
                }
            }
            CommandMenu("Run") {
                // C1 — the plan lives inside a run, so the default action is a
                // dry run whose per-source prompt is the plan surface.
                Button("Start Dry Run & Plan") {
                    Task { await appState.startDryRunPlan() }
                }
                .keyboardShortcut("r", modifiers: .command)
                .disabled(appState.isAnythingRunning)

                Button("Cancel Running Work") {
                    Task { await appState.cancelActiveWork() }
                }
                .keyboardShortcut(".", modifiers: .command)
                .disabled(!appState.isAnythingRunning)

                Divider()

                Button("Restart Backend") {
                    Task { await appState.restart() }
                }
            }
        }

        Settings {
            SettingsView()
                .environmentObject(appState)
        }
    }
}
