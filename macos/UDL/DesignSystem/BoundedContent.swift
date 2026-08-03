import SwiftUI

/// The container every screen that hosts a `Table` must use.
///
/// A `Table` reports the ideal height of every row it holds and does not shrink
/// to a smaller proposal. Left unbounded inside the split view's detail column
/// a long table makes the column outgrow the window: rows draw straight through
/// the toolbar, the status bar falls off the bottom, and AppKit squeezes the
/// sidebar to nothing. A definite height is what a `Table` needs in order to
/// scroll instead of expand.
///
/// `GeometryReader` supplies that height, but its `size` spans the whole frame
/// including the safe area the 46pt status bar occupies — so the insets have to
/// come back off, or whatever is stacked below the table is drawn underneath
/// the status bar and clipped.
struct BoundedContent<Content: View>: View {
    var alignment: Alignment = .top
    @ViewBuilder var content: () -> Content

    var body: some View {
        GeometryReader { proxy in
            content()
                .frame(
                    width: proxy.size.width,
                    height: max(
                        proxy.size.height - proxy.safeAreaInsets.top - proxy.safeAreaInsets.bottom,
                        0
                    ),
                    alignment: alignment
                )
        }
    }
}
