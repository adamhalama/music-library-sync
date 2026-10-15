import XCTest
@testable import UDL

final class SyncTerminalProgressTests: XCTestCase {
    func testBufferedProgressCannotOverwriteTerminalTrackOrRunProgress() throws {
        let notification = try progressNotification()
        for exitCode in [0, 4, 5, 130, 1] {
            var run = SyncRunState(runID: "run-1", phase: .running)
            XCTAssertTrue(run.applyProgress(notification))
            let completed = TrackRow(
                sourceID: "source-a", sourceLabel: "source-a", remoteID: "older", title: "Older",
                index: 8, executionSlot: 1, toggleable: true, planStatus: "missing_known_gap",
                planClass: "known_gap", selected: true, runScope: "included",
                runtimeStatus: "downloaded", statusLabel: "Downloaded", failureDetail: nil,
                progressKnown: true, progressPercent: 100
            )
            run.mergeSnapshot(sourceID: "source-a", snapshot: SourceSnapshot(
                lifecycle: "finished", confirmed: true, rows: [completed], activity: []
            ))
            run.finish(exitCode: exitCode, error: nil)
            // Keep the overall progress untouched as well as the retained row.
            run.progress = nil
            let phase = run.phase
            let message = run.terminalMessage

            XCTAssertFalse(run.applyProgress(notification))
            XCTAssertNil(run.progress)
            XCTAssertEqual(run.phase, phase)
            XCTAssertEqual(run.exitCode, exitCode)
            XCTAssertEqual(run.terminalMessage, message)
            XCTAssertEqual(run.sources["source-a"]?.rows.first?.runtimeStatus, "downloaded")
            XCTAssertEqual(run.sourceTables["source-a"]?.rows.first?.runtimeStatus, "downloaded")
            XCTAssertEqual(run.sourceTables["source-a"]?.rows.first?.progressPercent, 100)
            XCTAssertEqual(run.sourceTables["source-a"]?.lifecycle, "finished")
        }
    }

    func testProgressOnlyAppliesWhileStartingOrRunningWithoutAnExitCode() throws {
        let notification = try progressNotification()
        for phase in [SyncRunPhase.idle, .canceling, .succeeded, .partialFailure,
                      .dependencyFailure, .failed, .canceled] {
            var run = SyncRunState(runID: "run-1", phase: phase)
            XCTAssertFalse(run.applyProgress(notification))
            XCTAssertNil(run.progress)
            XCTAssertTrue(run.sources.isEmpty)
            XCTAssertTrue(run.sourceTables.isEmpty)
        }
        for phase in [SyncRunPhase.starting, .running] {
            var active = SyncRunState(runID: "run-1", phase: phase)
            XCTAssertTrue(active.applyProgress(notification))
            XCTAssertEqual(active.progress?.track.progressPercent, 73)
            var terminal = SyncRunState(runID: "run-1", phase: phase, exitCode: 0)
            XCTAssertFalse(terminal.applyProgress(notification))
            XCTAssertNil(terminal.progress)
        }
    }

    func testTerminalSourceRejectsBufferedProgressWhileOtherSourcesStillRun() throws {
        let notification = try progressNotification()
        for lifecycle in ["finished", "failed", "canceled", "skipped", "not_run", "completed"] {
            var run = SyncRunState(runID: "run-1", phase: .running)
            let row = TrackRow(
                sourceID: "source-a", sourceLabel: "source-a", remoteID: "older", title: "Older",
                index: 8, executionSlot: 1, toggleable: true, planStatus: "missing_known_gap",
                planClass: "known_gap", selected: true, runScope: "included",
                runtimeStatus: "downloaded", statusLabel: "Downloaded", failureDetail: nil,
                progressKnown: true, progressPercent: 100
            )
            run.mergeSnapshot(sourceID: "source-a", snapshot: SourceSnapshot(
                lifecycle: lifecycle, confirmed: true, rows: [row], activity: []
            ))
            run.mergeSnapshot(sourceID: "source-b", snapshot: SourceSnapshot(
                lifecycle: "running", confirmed: true, rows: [], activity: []
            ))
            XCTAssertFalse(run.applyProgress(notification))
            XCTAssertNil(run.progress)
            XCTAssertEqual(run.sources["source-a"]?.lifecycle, lifecycle)
            XCTAssertEqual(run.sourceTables["source-a"]?.lifecycle, lifecycle)
            XCTAssertEqual(run.sources["source-a"]?.rows.first?.runtimeStatus, "downloaded")
            XCTAssertEqual(run.sourceTables["source-a"]?.rows.first?.progressPercent, 100)
            XCTAssertEqual(run.sources["source-b"]?.lifecycle, "running")
            XCTAssertEqual(run.phase, .running)
            XCTAssertNil(run.exitCode)
        }
    }

    func testTerminalSourceTableRejectsProgressEvenWithoutSnapshotOrRow() throws {
        let notification = try progressNotification()
        let rowless = SyncProgressNotification(runID: notification.runID, sourceID: notification.sourceID,
                                               progress: notification.progress, row: nil)
        var run = SyncRunState(runID: "run-1", phase: .running)
        run.sourceTables["source-a"] = SyncSourceTableState(
            sourceID: "source-a", planRows: [], selectedIndices: [], downloadOrder: .oldestFirst,
            planWindow: .first, accepted: true, hasTrackPlan: false, lifecycle: "finished"
        )
        XCTAssertFalse(run.applyProgress(rowless))
        XCTAssertNil(run.progress)
        XCTAssertTrue(run.sources.isEmpty)
    }

    private func progressNotification() throws -> SyncProgressNotification {
        let payload = """
        {
          "run_id":"run-1",
          "source_id":"source-a",
          "progress":{
            "progress":{
              "source":{"id":"source-a","lifecycle":"running","planned_total":2,"item_total":2,"item_index":1,"completed":0},
              "track":{"name":"Older","lifecycle":"downloading","progress_percent":73},
              "global":{"total":2,"completed":0}
            },
            "track":{"name":"Older","progress_known":true,"progress_percent":73,"lifecycle":"downloading"},
            "structured_track_events":true
          },
          "row":{
            "source_id":"source-a","source_label":"source-a","remote_id":"older","title":"Older",
            "index":8,"execution_slot":1,"toggleable":true,"plan_status":"missing_known_gap","plan_class":"known_gap",
            "selected":true,"run_scope":"included","runtime_status":"downloading","status_label":"73%",
            "progress_known":true,"progress_percent":73
          }
        }
        """.data(using: .utf8)!
        return try JSONDecoder.agent.decode(SyncProgressNotification.self, from: payload)    }
}
