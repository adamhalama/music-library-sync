import SwiftUI

/// The toolbar every workspace shares: a leading title with a monospace
/// subtitle that names the protocol source of what is on screen, then the
/// screen's own trailing controls, then the inspector toggle.
private struct WorkspaceToolbarModifier<Trailing: View>: ViewModifier {
    @EnvironmentObject private var chrome: ShellChrome
    let title: String
    let subtitle: String?
    let isSearchable: Bool
    let searchPrompt: String
    @ViewBuilder let trailing: () -> Trailing

    func body(content: Content) -> some View {
        searchable(content)
            // The window title bar is the leading title slot on macOS; the
            // subtitle names the protocol source of what is on screen.
            .navigationTitle(title)
            .navigationSubtitle(subtitle ?? "")
            .toolbar {
                ToolbarItemGroup(placement: .primaryAction) {
                    trailing()
                    Button {
                        chrome.toggleInspector()
                    } label: {
                        Label("Inspector", systemImage: "sidebar.right")
                    }
                    .help(chrome.inspectorVisible ? "Hide the inspector (⌥⌘I)" : "Show the inspector (⌥⌘I)")
                }
            }
    }

    @ViewBuilder
    private func searchable(_ content: Content) -> some View {
        if isSearchable {
            content.searchable(
                text: $chrome.searchText,
                placement: .toolbar,
                prompt: searchPrompt
            )
        } else {
            content
        }
    }
}

private struct WorkspaceInspectorModifier<Inspector: View>: ViewModifier {
    @EnvironmentObject private var chrome: ShellChrome
    @ViewBuilder let inspector: () -> Inspector

    func body(content: Content) -> some View {
        content.inspector(isPresented: $chrome.inspectorVisible) {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    inspector()
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 14)
                .padding(.vertical, 12)
            }
            .inspectorColumnWidth(
                min: Metrics.inspectorMinWidth,
                ideal: Metrics.inspectorWidth,
                max: Metrics.inspectorMaxWidth
            )
        }
    }
}

extension View {
    func workspaceToolbar<Trailing: View>(
        title: String,
        subtitle: String? = nil,
        searchable: Bool = false,
        searchPrompt: String = "Search",
        @ViewBuilder trailing: @escaping () -> Trailing
    ) -> some View {
        modifier(WorkspaceToolbarModifier(
            title: title,
            subtitle: subtitle,
            isSearchable: searchable,
            searchPrompt: searchPrompt,
            trailing: trailing
        ))
    }

    func workspaceToolbar(
        title: String,
        subtitle: String? = nil,
        searchable: Bool = false,
        searchPrompt: String = "Search"
    ) -> some View {
        workspaceToolbar(
            title: title,
            subtitle: subtitle,
            searchable: searchable,
            searchPrompt: searchPrompt
        ) { EmptyView() }
    }

    /// The per-screen inspector, toggled from the toolbar or ⌥⌘I.
    func workspaceInspector<Inspector: View>(
        @ViewBuilder _ inspector: @escaping () -> Inspector
    ) -> some View {
        modifier(WorkspaceInspectorModifier(inspector: inspector))
    }

    /// The standard scrolling content column.
    func workspaceContent() -> some View {
        frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .background(Theme.window)
    }
}

/// An inspector section: uppercase header plus its rows.
struct InspectorSection<Content: View>: View {
    let title: String
    @ViewBuilder var content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            SectionHeader(title: title)
            content()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
