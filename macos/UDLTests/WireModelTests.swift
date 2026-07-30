import XCTest
import SwiftUI
@testable import UDL

final class WireModelTests: XCTestCase {
    func testUnknownFieldsAndEnumValuesDecode() throws {
        let payload = """
        {
          "checks": [{
            "name": "future",
            "severity": "future_severity",
            "status": "ok",
            "detail": "still decodes",
            "future_field": true
          }],
          "effective_path": "/usr/bin",
          "resolved_dependencies": {},
          "exit_code": 0,
          "future_top_level": "ignored"
        }
        """.data(using: .utf8)!
        let result = try JSONDecoder.agent.decode(DoctorResult.self, from: payload)
        XCTAssertEqual(result.checks.first?.severity, .unknown)
        XCTAssertEqual(result.checks.first?.detail, "still decodes")
    }

    func testOnboardingAcceptsNullDetailLinesFromGo() throws {
        let payload = """
        {
          "needed": false,
          "state": {
            "reason": "first_run",
            "auto_started": false,
            "config_path": "/tmp/udl.yaml",
            "config_context_label": "/tmp/udl.yaml",
            "detail_lines": null,
            "defaults": {
              "state_dir": "/tmp/state",
              "archive_file": "archive.txt",
              "threads": 1,
              "continue_on_error": false,
              "command_timeout_seconds": 60
            }
          }
        }
        """.data(using: .utf8)!
        let result = try JSONDecoder.agent.decode(OnboardingResult.self, from: payload)
        XCTAssertEqual(result.state.detailLines, [])
    }

    func testRekordboxConfigAcceptsNullEmptySyncCollectionsFromGo() throws {
        let payload = """
        {
          "path": {
            "path": "/tmp/rekordbox.yaml",
            "kind": "new-user",
            "exists": false
          },
          "config": {
            "version": 1,
            "defaults": {
              "db_dir": "~/Library/Pioneer/rekordbox",
              "backup_dir": "~/Music/rb-library-export",
              "mode": "mirror",
              "create_folders": true,
              "create_playlists": true
            },
            "sync": {
              "folders": null,
              "jobs": null
            }
          },
          "content": "version: 1\\nsync: {}\\n"
        }
        """.data(using: .utf8)!
        let result = try JSONDecoder.agent.decode(RekordboxConfigResult.self, from: payload)
        XCTAssertEqual(result.config.sync.folders.count, 0)
        XCTAssertEqual(result.config.sync.jobs.count, 0)
    }

    func testGoGoldenMethodInventoryMatchesSwift() throws {
        let testFile = URL(fileURLWithPath: #filePath)
        let repository = testFile
            .deletingLastPathComponent()
            .deletingLastPathComponent()
            .deletingLastPathComponent()
        let fixtureURL = repository
            .appending(path: "internal/agent/testdata/protocol_v1_golden.json")
        let data = try Data(contentsOf: fixtureURL)
        let value = try JSONDecoder.agent.decode(JSONValue.self, from: data)
        guard case .object(let root) = value,
              case .array(let methods) = root["methods"] else {
            return XCTFail("invalid Go protocol fixture")
        }
        let names = methods.compactMap { item -> String? in
            guard case .string(let value) = item else { return nil }
            return value
        }
        XCTAssertEqual(names, AgentMethod.allCases.map(\.rawValue))
    }

    func testRedactionRemovesSecretPatterns() {
        let secret = String(repeating: "a", count: 192)
        let redacted = redactedDiagnostic(
            "authorization: Bearer token-value client_secret=hidden arl=\(secret)"
        )
        XCTAssertFalse(redacted.contains("token-value"))
        XCTAssertFalse(redacted.contains("hidden"))
        XCTAssertFalse(redacted.contains(secret))
    }

    func testSelectRowsRequestAndResultRoundTrip() throws {
        let requestJSON = """
        {
          "run_id":"run-1",
          "source_id":"spotify",
          "rows":[
            {"index":1,"remote_id":"one","remote_url":"https://example.test/one","title":"One","status":"missing_new","toggleable":true,"selected_by_default":true},
            {"index":2,"remote_id":"two","remote_url":"https://example.test/two","title":"Two","status":"already_downloaded","toggleable":false,"selected_by_default":false}
          ],
          "details":{"source_id":"spotify","source_type":"spotify","adapter":"deemix","url":"https://example.test/list","target_dir":"/music","state_file":"/state","plan_limit":50,"plan_window":"latest","dry_run":false},
          "download_order":"newest_first",
          "plan_window":"latest",
          "future_field":"ignored"
        }
        """.data(using: .utf8)!
        let request = try JSONDecoder.agent.decode(SelectRowsParams.self, from: requestJSON)
        XCTAssertEqual(request.rows.filter(\.selectedByDefault).map(\.index), [1])
        XCTAssertFalse(request.rows[1].toggleable)

        let reply = SelectRowsResult(
            selectedIndices: [1],
            downloadOrder: .oldestFirst,
            canceled: false,
            rebuild: true,
            planWindow: .first
        )
        let encoded = try JSONEncoder.agent.encode(reply)
        let decoded = try JSONDecoder.agent.decode(SelectRowsResult.self, from: encoded)
        XCTAssertEqual(decoded.selectedIndices, [1])
        XCTAssertEqual(decoded.downloadOrder, .oldestFirst)
        XCTAssertTrue(decoded.rebuild)
        XCTAssertEqual(decoded.planWindow, .first)
    }

    func testSyncEventUsesServerRunStateWithoutReclassification() throws {
        let payload = """
        {
          "run_id":"run-1",
          "event":{"timestamp":"2026-07-30T12:00:00Z","level":"info","event":"track.done","source_id":"source-a","message":"done"},
          "source":{"lifecycle":"running","confirmed":true,"rows":[{"source_id":"source-a","source_label":"Source A","remote_id":"one","title":"One","index":1,"execution_slot":0,"toggleable":true,"plan_status":"missing_known_gap","plan_class":"known_gap","selected":true,"run_scope":"included","runtime_status":"downloaded","status_label":"downloaded","progress_known":true,"progress_percent":100}],"activity":[]},
          "progress":{
            "progress":{
              "source":{"id":"source-a","lifecycle":"running","planned_total":1,"item_total":1,"item_index":1,"completed":1},
              "track":{"name":"One","lifecycle":"done","progress_percent":100},
              "global":{"total":1,"completed":1}
            },
            "track":{"name":"One","progress_known":true,"progress_percent":100,"lifecycle":"done"},
            "structured_track_events":true,
            "future":"shape"
          }
        }
        """.data(using: .utf8)!
        let event = try JSONDecoder.agent.decode(SyncEventNotification.self, from: payload)
        XCTAssertEqual(event.source.rows.first?.planClass, "known_gap")
        XCTAssertEqual(event.source.rows.first?.runtimeStatus, "downloaded")
        XCTAssertEqual(event.source.downloadedCount, 1)
        XCTAssertEqual(event.progress.progress.global.completed, 1)
        XCTAssertEqual(event.progress.track.progressPercent, 100)
    }

    func testARLReplacementDoesNotAppendHiddenExistingValue() {
        let replacement = String(repeating: "a", count: 96)
        let saved = credentialReplacementValue(submitted: replacement)
        XCTAssertEqual(saved.count, 96)
        XCTAssertEqual(saved, replacement)
        XCTAssertNotEqual(saved, replacement + replacement)
    }

    func testDoctorPresentationStates() {
        func report(status: String, exitCode: Int) -> DoctorResult {
            DoctorResult(
                checks: [DoctorCheck(
                    name: "fixture", severity: .info, status: status,
                    detail: "fixture", remediation: nil
                )],
                effectivePath: "/usr/bin",
                resolvedDependencies: [:],
                exitCode: exitCode
            )
        }
        XCTAssertEqual(doctorPresentationState(report(status: "ok", exitCode: 0)), .healthy)
        XCTAssertEqual(doctorPresentationState(report(status: "warning", exitCode: 0)), .warning)
        XCTAssertEqual(doctorPresentationState(report(status: "blocked", exitCode: 4)), .blocked)
    }

    func testConfigSourceCRUDAndReorder() {
        func source(_ id: String) -> MainConfigSource {
            MainConfigSource(
                id: id, type: "soundcloud", enabled: true,
                targetDir: "/tmp/\(id)", url: "https://example.test/\(id)",
                stateFile: "\(id).state",
                sync: SourceSyncPolicy(
                    breakOnExisting: true, askOnExisting: false, localIndexCache: true
                ),
                adapter: SourceAdapter(kind: "scdl", extraArgs: [], minVersion: nil)
            )
        }
        var sources = [source("one"), source("two")]
        sources.append(source("three"))
        sources[1].targetDir = "/tmp/edited"
        sources.move(fromOffsets: IndexSet(integer: 2), toOffset: 0)
        sources.removeAll { $0.id == "one" }
        XCTAssertEqual(sources.map(\.id), ["three", "two"])
        XCTAssertEqual(sources.last?.targetDir, "/tmp/edited")
    }

    @MainActor
    func testPlanSelectionAndCursorSurviveRebuildByStableRemoteID() {
        func row(index: Int, id: String, selected: Bool) -> PlanRow {
            PlanRow(
                index: index,
                remoteID: id,
                remoteURL: "https://example.test/\(id)",
                title: id,
                status: "missing_new",
                toggleable: true,
                selectedByDefault: selected
            )
        }
        func params(rows: [PlanRow]) -> SelectRowsParams {
            SelectRowsParams(
                runID: "run-1",
                sourceID: "source-a",
                rows: rows,
                details: PlanSourceDetails(
                    sourceID: "source-a",
                    sourceType: "spotify",
                    adapter: "deemix",
                    url: "https://example.test/list",
                    targetDir: "/music",
                    stateFile: "/state",
                    planLimit: 50,
                    planWindow: .latest,
                    dryRun: false
                ),
                downloadOrder: .newestFirst,
                planWindow: .latest
            )
        }

        let state = AppState()
        let original = params(rows: [
            row(index: 0, id: "one", selected: true),
            row(index: 1, id: "two", selected: false),
        ])
        state.rememberPlanSelection(
            original,
            selectedIndices: [1],
            cursor: "two"
        )

        let rebuilt = params(rows: [
            row(index: 7, id: "two", selected: false),
            row(index: 8, id: "one", selected: true),
            row(index: 9, id: "three", selected: true),
        ])
        XCTAssertEqual(state.initialPlanSelection(rebuilt), [7, 9])
        XCTAssertEqual(
            state.rememberedPlanCursor(sourceID: "source-a", rows: rebuilt.rows),
            "two"
        )
    }

    func testRekordboxBlockerPresentationHandlesOneAndMultipleRows() {
        func row(_ index: Int, status: String, action: String) -> JSONValue {
            .object([
                "music_index": .number(Double(index)),
                "music_persistent_id": .string("id-\(index)"),
                "artist": .string("Artist \(index)"),
                "title": .string("Track \(index)"),
                "path": .string("/music/\(index).m4a"),
                "match_status": .string(status),
                "action": .string(action),
            ])
        }
        func plan(_ rows: [JSONValue]) -> RekordboxPlanPresentation {
            let value = JSONValue.object([
                "checksum_sha256": .string("fixture"),
                "version": .string("1"),
                "rows": .array(rows),
                "summary": .object(["music_total": .number(Double(rows.count))]),
            ])
            return try! XCTUnwrap(RekordboxPlanPresentation(value))
        }

        XCTAssertEqual(plan([row(1, status: "missing", action: "skip")]).blockers.count, 1)
        let multiple = plan([
            row(1, status: "missing", action: "skip"),
            row(2, status: "ambiguous_path", action: "skip"),
            row(3, status: "duplicate_path", action: "skip"),
            row(4, status: "matched_path", action: "keep"),
        ])
        XCTAssertEqual(multiple.blockers.map(\.title), ["Track 1", "Track 2", "Track 3"])
    }
}
