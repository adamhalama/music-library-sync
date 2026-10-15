import SwiftUI

/// Window chrome that outlives any single screen: the inspector toggle and the
/// toolbar search field. Owned by `UDLApp` so the menu commands and the
/// toolbar buttons drive exactly the same state.
@MainActor
final class ShellChrome: ObservableObject {
    @Published var inspectorVisible = true
    @Published var searchText = ""

    func toggleInspector() {
        inspectorVisible.toggle()
    }
}

/// A row in the sidebar's contextual section — the `contextual` slot in
/// `shell.js`. Sources, jobs, and playlists all render through this, so C2's
/// lifecycle chip and its "why can't I click this" reason are expressed once.
struct SidebarContextItem: Identifiable, Equatable {
    let id: String
    var title: String
    var subtitle: String?
    var sourceType: String?
    var lifecycle: Lifecycle?
    /// `nil` means selectable. A non-nil reason renders the row dimmed and
    /// states the reason inline rather than silently ignoring the click.
    var unavailableReason: String?
}

/// The contextual sidebar section supplied by the active screen.
struct SidebarContext: Equatable {
    var title: String
    var items: [SidebarContextItem]
    var selectedID: String?
    var note: String?
    var select: (String) -> Void

    static func == (lhs: SidebarContext, rhs: SidebarContext) -> Bool {
        lhs.title == rhs.title
            && lhs.items == rhs.items
            && lhs.selectedID == rhs.selectedID
            && lhs.note == rhs.note
    }
}

@MainActor
final class SidebarContextStore: ObservableObject {
    @Published var context: SidebarContext?
}

private struct SidebarContextModifier: ViewModifier {
    @EnvironmentObject private var store: SidebarContextStore
    let context: SidebarContext?

    func body(content: Content) -> some View {
        content
            // Publishing from `onAppear` lands inside the same update that
            // created the view, and SwiftUI drops it — the contextual section
            // then never appears for a screen that arrives already populated,
            // such as the docked plan. `onChange(initial:)` is delivered after
            // the body, so the store actually sees it.
            .onChange(of: context, initial: true) { _, updated in store.context = updated }
            // Only clear what this screen actually put there. SwiftUI runs the
            // outgoing screen's `onDisappear` *after* the incoming screen has
            // published, so an unconditional clear here wipes the new screen's
            // section the instant it arrives.
            .onDisappear { if store.context == context { store.context = nil } }
    }
}

extension View {
    /// Publishes this screen's contextual sidebar section. Cleared when the
    /// screen goes away, so the sidebar never shows a stale source list.
    func sidebarContext(_ context: SidebarContext?) -> some View {
        modifier(SidebarContextModifier(context: context))
    }
}
