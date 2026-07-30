import SwiftUI

@main
struct UDLApp: App {
    @StateObject private var appState = AppState()

    var body: some Scene {
        WindowGroup {
            DashboardView()
                .environmentObject(appState)
                // The Playlists workspace has a 260-point cache column and a
                // 580-point detail column in addition to the main sidebar.
                .frame(minWidth: 1120, minHeight: 640)
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
                .sheet(item: $appState.pendingPrompt) { _ in
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
        }
    }
}
