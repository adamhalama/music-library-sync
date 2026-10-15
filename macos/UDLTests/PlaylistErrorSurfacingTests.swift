import XCTest
@testable import UDL

/// The playlist handler was the one workflow that discarded the backend's error
/// text, which made a whole class of failure — a revoked Automation grant, most
/// notably — invisible from the app. These tests pin the message down.
@MainActor
final class PlaylistErrorSurfacingTests: XCTestCase {
    private let automationError = """
    Music.app automation is not permitted; grant UDL access under System \
    Settings → Privacy & Security → Automation
    """

    private func finished(exitCode: Int, error: String? = nil) -> RunFinishedNotification {
        RunFinishedNotification(runID: "run-1", result: nil, error: error, exitCode: exitCode)
    }

    // MARK: Provider listing

    func testProviderListFailureShowsTheBackendMessage() {
        let state = AppState()
        state.applyPlaylistFinished(finished(exitCode: 1, error: automationError), operation: .providerList)

        let status = try? XCTUnwrap(state.playlistStatus)
        XCTAssertEqual(status?.severity, .error)
        XCTAssertTrue(status?.message.contains("System Settings") == true, status?.message ?? "nil")
        XCTAssertFalse(
            status?.message.contains("without changing any snapshot") == true,
            "the old message replaced a real cause with a non-statement"
        )
    }

    func testProviderListFailureWithoutABackendMessageNamesTheExitCode() {
        let state = AppState()
        state.applyPlaylistFinished(finished(exitCode: 3), operation: .providerList)

        XCTAssertEqual(state.playlistStatus?.severity, .error)
        XCTAssertEqual(
            state.playlistStatus?.message,
            "Provider listing failed with exit code 3."
        )
    }

    /// 130 is a cancellation, not a failure, and must keep reading as one — the
    /// refresh path already treated it that way.
    func testProviderListCancellationStaysAWarning() {
        let state = AppState()
        state.applyPlaylistFinished(finished(exitCode: 130), operation: .providerList)

        XCTAssertEqual(state.playlistStatus?.severity, .warn)
        XCTAssertEqual(state.playlistStatus?.message, "Provider listing canceled.")
        XCTAssertTrue(state.playlistStatus?.preservedPreviousSnapshot == true)
    }

    // MARK: Refresh

    /// C11 — the preservation guarantee survives, but it no longer stands in
    /// for the explanation.
    func testRefreshFailureShowsTheBackendMessageAndStillPreservesTheSnapshot() {
        let state = AppState()
        state.applyPlaylistFinished(
            finished(exitCode: 1, error: automationError),
            operation: .refresh("favorites")
        )

        let message = state.playlistStatus?.message ?? ""
        XCTAssertEqual(state.playlistStatus?.severity, .error)
        XCTAssertTrue(message.contains("System Settings"), message)
        XCTAssertTrue(message.hasSuffix("The previous valid snapshot was preserved."), message)
        XCTAssertTrue(state.playlistStatus?.preservedPreviousSnapshot == true)
    }

    func testRefreshCancellationStaysAWarning() {
        let state = AppState()
        state.applyPlaylistFinished(finished(exitCode: 130), operation: .refresh("favorites"))

        XCTAssertEqual(state.playlistStatus?.severity, .warn)
        XCTAssertEqual(
            state.playlistStatus?.message,
            "Refresh canceled. The previous valid snapshot was preserved."
        )
        XCTAssertTrue(state.playlistStatus?.preservedPreviousSnapshot == true)
    }

    // MARK: Message assembly

    /// A backend message is pasted in front of another sentence, so it has to
    /// terminate. A zero exit code that failed to decode is not a backend
    /// failure and must not claim to be one.
    func testFailureDetailTerminatesTheBackendMessageAndDistinguishesDecodeFailure() {
        XCTAssertEqual(
            AppState.playlistFailureDetail(
                finished(exitCode: 1, error: "no snapshot yet"),
                fallback: "fallback.",
                undecodable: "undecodable."
            ),
            "no snapshot yet."
        )
        XCTAssertEqual(
            AppState.playlistFailureDetail(
                finished(exitCode: 1, error: "no snapshot yet."),
                fallback: "fallback.",
                undecodable: "undecodable."
            ),
            "no snapshot yet."
        )
        XCTAssertEqual(
            AppState.playlistFailureDetail(
                finished(exitCode: 0, error: "  "),
                fallback: "fallback.",
                undecodable: "undecodable."
            ),
            "undecodable."
        )
    }
}
