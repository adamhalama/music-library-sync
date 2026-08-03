import XCTest
import SwiftUI
@testable import UDL

/// Rules the redesign states in prose and that nothing else can enforce: a
/// failure belongs on the screen that caused it, and a disabled control states
/// its reason. Both are properties of how the sources are written, so they are
/// checked by reading the sources. Each closed a class of defect that had
/// already shipped once.
final class SourceRuleTests: XCTestCase {
    /// `macos/UDL`, derived from this file's own location so the test works
    /// from any checkout without a hardcoded path.
    private var appSourceRoot: URL {
        URL(fileURLWithPath: #filePath)      // .../macos/UDLTests/WireModelTests.swift
            .deletingLastPathComponent()     // .../macos/UDLTests
            .deletingLastPathComponent()     // .../macos
            .appending(path: "UDL")
    }

    private func source(_ relativePath: String) throws -> [String] {
        let url = appSourceRoot.appending(path: relativePath)
        return try String(contentsOf: url, encoding: .utf8).components(separatedBy: "\n")
    }

    private func swiftFiles() throws -> [(name: String, lines: [String])] {
        let keys: [URLResourceKey] = [.isRegularFileKey]
        guard let walker = FileManager.default.enumerator(
            at: appSourceRoot,
            includingPropertiesForKeys: keys
        ) else { return [] }
        var files: [(String, [String])] = []
        for case let url as URL in walker where url.pathExtension == "swift" {
            let text = try String(contentsOf: url, encoding: .utf8)
            files.append((url.lastPathComponent, text.components(separatedBy: "\n")))
        }
        return files
    }

    /// Returns the name of the `func` each matching line sits inside.
    private func enclosingFunctions(of lines: [String], matching needle: String) -> Set<String> {
        var current = "<file scope>"
        var found: Set<String> = []
        for line in lines {
            if let name = Self.functionName(in: line) { current = name }
            if line.contains(needle) { found.insert(current) }
        }
        return found
    }

    private static func functionName(in line: String) -> String? {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        guard let range = trimmed.range(of: "func ") else { return nil }
        // Only declarations, not calls: `func` must open the declaration.
        let prefix = trimmed[trimmed.startIndex..<range.lowerBound]
        guard prefix.allSatisfy({ $0.isLetter || $0 == " " || $0 == "@" }) else { return nil }
        let rest = trimmed[range.upperBound...]
        guard let paren = rest.firstIndex(of: "(") else { return nil }
        return String(rest[rest.startIndex..<paren])
    }

    /// The blocking modal is for session-level failures no screen owns. A
    /// failure a screen's own action caused belongs on that screen — Finish
    /// Setup once failed with its reason recorded only where the Config screen
    /// would render it, so onboarding looked untouched.
    func testOnlySessionLevelFailuresReachTheModalAlert() throws {
        let allowed: Set<String> = [
            "refreshPlanPrompt",   // a `ui.selectRows` frame the app cannot decode
            "start",               // the backend would not launch
            "restart",             // the backend would not come back
            "answerPrompt",        // the reply the blocked backend is waiting for
            "answerPlanSelection", // ditto, when it cannot even be encoded
            "cancelPrompt",        // cancellation reply to the same blocked backend
        ]
        let lines = try source("Model/AppState.swift")
        let actual = enclosingFunctions(of: lines, matching: "alertMessage = ")
        XCTAssertEqual(
            actual.subtracting(allowed),
            [],
            "These raise a modal but are a screen's own failure; route them to that screen's status."
        )
    }

    /// Failure mode 1 in PLAN.md: a `.disabled(true)` with no stated reason is a
    /// defect, and a `.help(_)` tooltip is not a stated reason. `.constrained(by:)`
    /// is the sanctioned way to disable a control, because it states the reason
    /// as part of disabling. A bare `.disabled(` is allowed only where the reason
    /// is on screen by other means, and each of those is named here.
    func testEveryDisabledControlStatesItsReasonOnScreen() throws {
        /// file name → the bare `.disabled(` sites it is allowed to contain, and
        /// why the reason is visible without a note.
        let allowed: [String: (count: Int, because: String)] = [
            // The primitives that implement the rule.
            "ConstraintNote.swift": (1, "this *is* `.constrained(by:)`"),
            // The step renders its own unavailability reason as its subtitle.
            "FreeDLPhaseBar.swift": (1, "the reason is the step's subtitle"),
            // Menu items cannot host a note; each mirrors an on-screen control
            // that carries one.
            "UDLApp.swift": (2, "menu commands mirroring on-screen actions"),
            // Toolbar buttons that swap their label for a spinner and the word
            // for what is running — the reason is the label.
            "DoctorView.swift": (1, "the toolbar button reads “Running checks…”"),
            "CredentialsView.swift": (1, "the toolbar button reads “Reloading…”"),
            "HomeView.swift": (1, "the toolbar button reads “Running checks…”"),
            "PlaylistsView.swift": (1, "toolbar; the run is named in the status bar"),
            // Move up / move down state their reason in one note under the row.
            "ConfigSourceForm.swift": (2, "one note under the button row"),
            // Per-row toggles: one note above the table, not one per row.
            "FreeDLView.swift": (2, "one note above the table (phase 4 lesson)"),
            // Buttons in a horizontal row whose shared note sits beside or under
            // it; `.constrained(by:)` stacks vertically and would break the row.
            "SourceEditorView.swift": (1, "the missing-requirement note is beside it"),
            "OnboardingView.swift": (1, "the note is beside it in the status bar"),
            "FreeDLJobForm.swift": (2, "one “nothing to save” note under the row"),
            "PlaylistDefinitionEditor.swift": (1, "the ID note is under the field"),
            "RekordboxView.swift": (3, "one note per strip, not one per button"),
        ]

        var offenders: [String] = []
        for file in try swiftFiles() {
            let bare = file.lines.filter {
                $0.contains(".disabled(") && !$0.contains("constrained(by:")
            }.count
            guard bare > 0 else { continue }
            let budget = allowed[file.name]?.count ?? 0
            if bare > budget {
                offenders.append("\(file.name): \(bare) bare .disabled( sites, \(budget) allowed")
            }
        }
        XCTAssertEqual(
            offenders.sorted(),
            [],
            "Disabling without an adjacent stated reason is failure mode 1. Use .constrained(by:)."
        )
    }
}

final class WireModelTests: XCTestCase {
    func testFinishedWireLifecycleMapsToDone() {
        XCTAssertEqual(Lifecycle(wire: "finished"), .done)
        XCTAssertEqual(Lifecycle(wire: "canceled"), .canceled)
        XCTAssertEqual(Lifecycle(wire: "not_run"), .notRun)
    }

    func testEveryPromptKindHasTheCanonicalCancellationReply() {
        XCTAssertEqual(
            UIRequestKind.confirm.canceledResult,
            .object(["confirmed": .bool(false), "canceled": .bool(true)])
        )
        XCTAssertEqual(
            UIRequestKind.input.canceledResult,
            .object(["value": .string(""), "canceled": .bool(true)])
        )
        XCTAssertEqual(
            UIRequestKind.selectRows.canceledResult,
            .object([
                "selected_indices": .array([]),
                "download_order": .string("oldest_first"),
                "canceled": .bool(true),
                "rebuild": .bool(false),
                "plan_window": .string("first"),
            ])
        )
    }

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
            .appending(path: "internal/agent/testdata/protocol_v2_golden.json")
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
          "future":"shape"
        }
        """.data(using: .utf8)!
        let event = try JSONDecoder.agent.decode(SyncEventNotification.self, from: payload)
        XCTAssertEqual(event.source.rows.first?.planClass, "known_gap")
        XCTAssertEqual(event.source.rows.first?.runtimeStatus, "downloaded")
        XCTAssertEqual(event.source.downloadedCount, 1)
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

    /// C17 — the parameters the GUI used to hardcode now come from AppState.
    /// This pins the wire spelling, including the `ask_on_existing_set` flag
    /// that makes "udl decides" distinguishable from "never ask".
    func testSyncStartParamsEncodeSurfacedAdvancedOptions() throws {
        let params = SyncStartParams(
            sourceIDs: ["sc-likes"],
            dryRun: true,
            timeoutSeconds: 0,
            plan: true,
            planLimit: 0,
            planWindow: .latest,
            planWindowBySource: ["sc-likes": .latest],
            downloadOrderBySource: ["sc-likes": .oldestFirst],
            askOnExisting: true,
            askOnExistingSet: true,
            scanGaps: true,
            noPreflight: true,
            trackStatus: .names
        )
        let encoded = try JSONSerialization.jsonObject(
            with: JSONEncoder.agent.encode(params)
        ) as? [String: Any]
        XCTAssertEqual(encoded?["plan_limit"] as? Int, 0)
        XCTAssertEqual(encoded?["plan_window"] as? String, "latest")
        XCTAssertEqual(encoded?["ask_on_existing"] as? Bool, true)
        XCTAssertEqual(encoded?["ask_on_existing_set"] as? Bool, true)
        XCTAssertEqual(encoded?["scan_gaps"] as? Bool, true)
        XCTAssertEqual(encoded?["no_preflight"] as? Bool, true)
        XCTAssertEqual(encoded?["track_status"] as? String, "names")
    }

    /// `SyncDefaults` is the only place a sync default is written. A second copy
    /// is how `dryRun` once ended up claiming one value in a comment and another
    /// in the initialiser, so this pins the constants themselves — including
    /// C1's `dryRun: true`, without which the sidebar route offers a live run.
    func testSyncDefaultsAreTheDocumentedC1AndC17Values() throws {
        XCTAssertTrue(SyncDefaults.dryRun)
        XCTAssertFalse(SyncDefaults.unlimited)
        XCTAssertEqual(SyncDefaults.planLimit, 50)
        XCTAssertEqual(SyncDefaults.timeoutSeconds, 0)
        XCTAssertEqual(SyncDefaults.planWindow, .first)
        XCTAssertEqual(SyncDefaults.askOnExisting, .backendDefault)
        XCTAssertFalse(SyncDefaults.scanGaps)
        XCTAssertFalse(SyncDefaults.noPreflight)
        XCTAssertEqual(SyncDefaults.trackStatus, .off)

        // The wire payload a default-state run sends. `ask_on_existing_set:
        // false` is what leaves the choice to udl, reproducing pre-C17 behaviour.
        let params = SyncStartParams(
            sourceIDs: ["sc-likes"],
            dryRun: SyncDefaults.dryRun,
            timeoutSeconds: SyncDefaults.timeoutSeconds,
            plan: true,
            planLimit: SyncDefaults.unlimited ? 0 : SyncDefaults.planLimit,
            planWindow: SyncDefaults.planWindow,
            planWindowBySource: [:],
            downloadOrderBySource: [:],
            askOnExisting: SyncDefaults.askOnExisting.value,
            askOnExistingSet: SyncDefaults.askOnExisting.isSet,
            scanGaps: SyncDefaults.scanGaps,
            noPreflight: SyncDefaults.noPreflight,
            trackStatus: SyncDefaults.trackStatus
        )
        let encoded = try JSONSerialization.jsonObject(
            with: JSONEncoder.agent.encode(params)
        ) as? [String: Any]
        XCTAssertEqual(encoded?["dry_run"] as? Bool, true)
        XCTAssertEqual(encoded?["plan_limit"] as? Int, 50)
        XCTAssertEqual(encoded?["plan_window"] as? String, "first")
        XCTAssertEqual(encoded?["ask_on_existing_set"] as? Bool, false)
        XCTAssertEqual(encoded?["track_status"] as? String, "none")
    }

    /// The initialisers and `resetSyncAdvanced()` are two routes to the same
    /// values; both must read `SyncDefaults` rather than repeating it.
    @MainActor
    func testSyncStateStartsAtAndResetsToTheDefaults() {
        let state = AppState()
        XCTAssertEqual(state.syncDryRun, SyncDefaults.dryRun)
        XCTAssertEqual(state.syncUnlimited, SyncDefaults.unlimited)
        XCTAssertEqual(state.syncPlanLimit, SyncDefaults.planLimit)
        XCTAssertEqual(state.syncTimeoutSeconds, SyncDefaults.timeoutSeconds)
        XCTAssertEqual(state.syncPlanWindow, SyncDefaults.planWindow)
        XCTAssertEqual(state.syncAskOnExisting, SyncDefaults.askOnExisting)
        XCTAssertEqual(state.syncScanGaps, SyncDefaults.scanGaps)
        XCTAssertEqual(state.syncNoPreflight, SyncDefaults.noPreflight)
        XCTAssertEqual(state.syncTrackStatus, SyncDefaults.trackStatus)

        state.syncPlanWindow = .latest
        state.syncAskOnExisting = .ask
        state.syncScanGaps = true
        state.syncNoPreflight = true
        state.syncTrackStatus = .names
        state.resetSyncAdvanced()
        XCTAssertEqual(state.syncPlanWindow, SyncDefaults.planWindow)
        XCTAssertEqual(state.syncAskOnExisting, SyncDefaults.askOnExisting)
        XCTAssertEqual(state.syncScanGaps, SyncDefaults.scanGaps)
        XCTAssertEqual(state.syncNoPreflight, SyncDefaults.noPreflight)
        XCTAssertEqual(state.syncTrackStatus, SyncDefaults.trackStatus)
    }

    func testSyncCancellationRequestedBeforeRunIDIsSentExactlyOnceWhenIDArrives() {
        var run = SyncRunState(phase: .starting, requestedSourceIDs: ["source-a"])

        XCTAssertTrue(run.requestCancellation())
        XCTAssertEqual(run.phase, .canceling)
        XCTAssertTrue(run.cancelRequested)
        XCTAssertTrue(run.terminalMessage?.contains("Stopping") == true)
        XCTAssertFalse(run.beginCancellationRequest(), "no request can be sent before the run ID exists")
        XCTAssertFalse(run.requestCancellation(), "repeated Stop must be idempotent")

        run.registerRunID("run-1")
        XCTAssertEqual(run.phase, .canceling, "a late start response must not revive the run")
        XCTAssertTrue(run.beginCancellationRequest())
        XCTAssertFalse(run.beginCancellationRequest(), "only one cancellation request may be in flight")

        run.acknowledgeCancellation()
        XCTAssertTrue(run.cancelAcknowledged)
        XCTAssertFalse(run.beginCancellationRequest(), "acknowledgement prevents a duplicate send")
    }

    func testSyncCancellationFailureReturnsToRetryableRunningState() {
        var run = SyncRunState(runID: "run-1", phase: .running)
        XCTAssertTrue(run.requestCancellation())
        XCTAssertTrue(run.beginCancellationRequest())

        run.failCancellation("connection remained healthy")
        XCTAssertEqual(run.phase, .running)
        XCTAssertFalse(run.cancelRequested)
        XCTAssertFalse(run.cancelRequestInFlight)
        XCTAssertFalse(run.cancelAcknowledged)
        XCTAssertTrue(run.terminalMessage?.contains("Try Stop again") == true)

        XCTAssertTrue(run.requestCancellation(), "failed cancellation must be retryable")
        XCTAssertTrue(run.beginCancellationRequest())
    }

    func testSyncCancellationWatchdogCannotOverwriteTerminalState() {
        var canceling = SyncRunState(runID: "run-1", phase: .canceling, cancelRequested: true)
        canceling.markStillStopping()
        XCTAssertTrue(canceling.stillStopping)
        XCTAssertTrue(canceling.terminalMessage?.contains("Still stopping") == true)

        var canceled = SyncRunState(runID: "run-1", phase: .canceled, exitCode: 130, cancelRequested: true)
        canceled.markStillStopping()
        XCTAssertFalse(canceled.stillStopping)
        XCTAssertNil(canceled.terminalMessage)
    }

    func testLateCancellationResponseCannotOverwriteTerminalOutcome() {
        var canceled = SyncRunState(
            runID: "run-1",
            phase: .canceled,
            terminalMessage: "Sync canceled. Completed tracks remain saved.",
            exitCode: 130,
            cancelRequested: true,
            cancelRequestInFlight: true
        )

        canceled.acknowledgeCancellation()
        XCTAssertEqual(canceled.terminalMessage, "Sync canceled. Completed tracks remain saved.")

        canceled.failCancellation("late reply")
        XCTAssertEqual(canceled.phase, .canceled)
        XCTAssertEqual(canceled.terminalMessage, "Sync canceled. Completed tracks remain saved.")
    }

    func testTerminalCancellationFinalizesRetainedSourceLifecycles() {
        let rows = [PlanRow(
            index: 1,
            remoteID: "remote-1",
            remoteURL: "https://example.test/1",
            title: "One",
            status: "missing_new",
            toggleable: true,
            selectedByDefault: true
        )]
        let details = PlanSourceDetails(
            sourceID: "draft",
            sourceType: "soundcloud",
            adapter: "scdl",
            url: "https://example.test",
            targetDir: "/tmp/target",
            stateFile: "/tmp/state",
            planLimit: 1,
            planWindow: .first,
            dryRun: true
        )
        var run = SyncRunState(phase: .running)
        run.installPlan(SelectRowsParams(
            runID: "run-1",
            sourceID: "draft",
            rows: rows,
            details: details,
            downloadOrder: .oldestFirst,
            planWindow: .first
        ), selectedIndices: [1])
        run.installPlan(SelectRowsParams(
            runID: "run-1",
            sourceID: "accepted",
            rows: rows,
            details: details,
            downloadOrder: .oldestFirst,
            planWindow: .first
        ), selectedIndices: [1])
        run.acceptPlan(sourceID: "accepted")
        run.phase = .canceled

        run.finalizeSourceTables()

        XCTAssertEqual(run.sourceTables["draft"]?.lifecycle, "not_run")
        XCTAssertEqual(run.sourceTables["accepted"]?.lifecycle, "canceled")
    }

    func testRunFinishedExitCodesMapToDistinctTerminalStates() {
        let cases: [(Int, SyncRunPhase, String)] = [
            (0, .succeeded, "completed successfully"),
            (4, .dependencyFailure, "exit code 4"),
            (5, .partialFailure, "exit code 5"),
            (130, .canceled, "Completed tracks remain saved"),
            (1, .failed, "exit code 1"),
        ]
        for (exitCode, expectedPhase, expectedMessage) in cases {
            var run = SyncRunState(runID: "run-1", phase: .running)
            run.finish(exitCode: exitCode, error: nil)
            XCTAssertEqual(run.exitCode, exitCode)
            XCTAssertEqual(run.phase, expectedPhase)
            XCTAssertTrue(run.terminalMessage?.contains(expectedMessage) == true)
        }

        var explicit = SyncRunState(runID: "run-2", phase: .running)
        explicit.finish(exitCode: 1, error: "backend detail")
        XCTAssertEqual(explicit.terminalMessage, "backend detail")
    }

    /// The defaults must reproduce exactly what the GUI sent before C17, so
    /// surfacing the controls does not silently change behaviour.
    func testAskOnExistingDefaultMatchesThePreviousHardcodedValues() {
        XCTAssertEqual(AskOnExistingPolicy.backendDefault.isSet, false)
        XCTAssertEqual(AskOnExistingPolicy.backendDefault.value, false)
        XCTAssertEqual(AskOnExistingPolicy.never.isSet, true)
        XCTAssertEqual(AskOnExistingPolicy.never.value, false)
        XCTAssertEqual(AskOnExistingPolicy.ask.isSet, true)
        XCTAssertEqual(AskOnExistingPolicy.ask.value, true)
        XCTAssertEqual(TrackStatusMode.off.rawValue, "none")
    }

    /// A locked row must always be able to say why, because the plan surface
    /// answers the click instead of swallowing it.
    func testEveryPlanRowStatusHasALabelAndALockReason() throws {
        for status in ["missing_new", "missing_known_gap", "already_downloaded", "future_status"] {
            let payload = """
            {
              "index": 0,
              "remote_id": "r1",
              "remote_url": "https://example.test/1",
              "title": "Track",
              "status": "\(status)",
              "toggleable": false,
              "selected_by_default": false
            }
            """.data(using: .utf8)!
            let row = try JSONDecoder.agent.decode(PlanRow.self, from: payload)
            XCTAssertFalse(row.statusLabel.isEmpty)
            XCTAssertFalse(row.lockReason.isEmpty)
        }
    }

    func testPlanQueueProjectionMatchesCanonicalOldestAndNewestSlots() {
        func row(_ index: Int, toggleable: Bool = true) -> PlanRow {
            PlanRow(
                index: index,
                remoteID: "track-\(index)",
                remoteURL: "",
                title: "Track \(index)",
                status: toggleable ? "missing_new" : "already_downloaded",
                toggleable: toggleable,
                selectedByDefault: toggleable
            )
        }
        let rows = [row(1), row(2, toggleable: false), row(3), row(4), row(5)]
        let selected: Set<Int> = [1, 2, 3, 5]

        let oldest = PlanQueueProjection(rows: rows, selectedIndices: selected, downloadOrder: .oldestFirst)
        XCTAssertEqual(rows.map { oldest.executionSlot(forSourceIndex: $0.index) }, [3, 0, 2, 0, 1])

        let newest = PlanQueueProjection(rows: rows, selectedIndices: selected, downloadOrder: .newestFirst)
        XCTAssertEqual(rows.map { newest.executionSlot(forSourceIndex: $0.index) }, [1, 0, 2, 0, 3])
    }

    func testPlanQueueProjectionRenumbersSelectedRowsWithoutReorderingSourceRows() {
        let rows = (1...4).map { index in
            PlanRow(
                index: index,
                remoteID: "track-\(index)",
                remoteURL: "",
                title: "Track \(index)",
                status: "missing_new",
                toggleable: true,
                selectedByDefault: true
            )
        }
        let initial = PlanQueueProjection(rows: rows, selectedIndices: [1, 2, 3, 4], downloadOrder: .oldestFirst)
        let toggled = PlanQueueProjection(rows: rows, selectedIndices: [1, 3, 4], downloadOrder: .oldestFirst)

        XCTAssertEqual(rows.map(\.index), [1, 2, 3, 4])
        XCTAssertEqual(rows.map { initial.executionSlot(forSourceIndex: $0.index) }, [4, 3, 2, 1])
        XCTAssertEqual(rows.map { toggled.executionSlot(forSourceIndex: $0.index) }, [3, 0, 2, 1])
    }

    func testUnifiedSourceTableKeepsIdentityAndOrderWhenAccepted() {
        let rows = (1...4).map { index in
            PlanRow(
                index: index, remoteID: "track-\(index)", remoteURL: "",
                title: "Track \(index)", status: "missing_new",
                toggleable: true, selectedByDefault: true
            )
        }
        let params = SelectRowsParams(
            runID: "run-1", sourceID: "source-a", rows: rows,
            details: PlanSourceDetails(
                sourceID: "source-a", sourceType: "soundcloud", adapter: "scdl",
                url: "https://example.test", targetDir: "/music", stateFile: "/state",
                planLimit: 50, planWindow: .first, dryRun: false
            ),
            downloadOrder: .oldestFirst, planWindow: .first
        )
        var run = SyncRunState(phase: .running, requestedSourceIDs: ["source-a"])
        run.installPlan(params, selectedIndices: [1, 3, 4])
        let before = try! XCTUnwrap(run.sourceTables["source-a"])
        let identities = before.rows.map(\.id)
        XCTAssertEqual(before.rows.map(\.executionSlot), [3, 0, 2, 1])

        run.acceptPlan(sourceID: "source-a")
        let accepted = try! XCTUnwrap(run.sourceTables["source-a"])
        XCTAssertTrue(accepted.accepted)
        XCTAssertEqual(accepted.rows.map(\.id), identities)
        XCTAssertEqual(accepted.rows.map(\.index), [1, 2, 3, 4])

        run.updateDraft(sourceID: "source-a", selectedIndices: [2], downloadOrder: .newestFirst)
        let locked = try! XCTUnwrap(run.sourceTables["source-a"])
        XCTAssertEqual(locked.selectedIndices, [1, 3, 4], "accepted mutation must be ignored")
        XCTAssertEqual(locked.downloadOrder, .oldestFirst)
    }

    func testUnifiedSourceTableMergesCanonicalSnapshotAndProgressByRowIdentity() {
        func track(_ index: Int, slot: Int, status: String) -> TrackRow {
            TrackRow(
                sourceID: "source-a", sourceLabel: "source-a", remoteID: "track-\(index)",
                title: "Track \(index)", index: index, executionSlot: slot,
                toggleable: true, planStatus: "missing_new", planClass: "new",
                selected: true, runScope: "included", runtimeStatus: status,
                statusLabel: status, failureDetail: nil,
                progressKnown: status == "downloading", progressPercent: status == "downloading" ? 25 : 0
            )
        }
        var run = SyncRunState(phase: .running, requestedSourceIDs: ["source-a", "source-b"])
        run.mergeSnapshot(
            sourceID: "source-a",
            snapshot: SourceSnapshot(
                lifecycle: "running", confirmed: true,
                rows: [track(1, slot: 2, status: "queued"), track(2, slot: 1, status: "downloading")],
                activity: []
            )
        )
        run.mergeProgressRow(sourceID: "source-a", row: track(2, slot: 1, status: "downloaded"))

        let table = try! XCTUnwrap(run.sourceTables["source-a"])
        XCTAssertEqual(table.rows.map(\.index), [1, 2])
        XCTAssertEqual(table.rows[1].runtimeStatus, "downloaded")
        XCTAssertEqual(table.rows[1].executionSlot, 1)
        XCTAssertEqual(run.requestedSourceIDs, ["source-a", "source-b"], "sidebar history order must remain stable")
    }

    func testMultiSourceFocusPrioritizesInputThenExplicitChoiceThenActiveWork() {
        XCTAssertEqual(
            SyncSourceFocus.target(
                current: "source-a", selectionIsExplicit: true,
                pendingInput: "source-b", active: "source-c"
            ),
            "source-b",
            "a source needing input must override even an explicit sidebar choice"
        )
        XCTAssertEqual(
            SyncSourceFocus.target(
                current: "source-a", selectionIsExplicit: true,
                pendingInput: nil, active: "source-c"
            ),
            "source-a",
            "active work must not steal an explicit sidebar choice"
        )
        XCTAssertEqual(
            SyncSourceFocus.target(
                current: "source-a", selectionIsExplicit: false,
                pendingInput: nil, active: "source-c"
            ),
            "source-c"
        )
        XCTAssertEqual(
            SyncSourceFocus.target(
                current: "source-a", selectionIsExplicit: false,
                pendingInput: nil, active: nil
            ),
            "source-a"
        )
    }

    func testProtocolV2ProgressDecodesCanonicalRowAndIsIgnoredAfterCancel() throws {
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
        let notification = try JSONDecoder.agent.decode(SyncProgressNotification.self, from: payload)
        XCTAssertEqual(notification.row?.index, 8)
        XCTAssertEqual(notification.row?.executionSlot, 1)

        var running = SyncRunState(runID: "run-1", phase: .running)
        XCTAssertTrue(running.applyProgress(notification))
        XCTAssertEqual(running.progress?.track.progressPercent, 73)

        var canceling = SyncRunState(runID: "run-1", phase: .canceling, cancelRequested: true)
        XCTAssertFalse(canceling.applyProgress(notification))
        XCTAssertNil(canceling.progress)
        XCTAssertTrue(canceling.sourceTables.isEmpty)
    }

    /// Home and Doctor both route a failing check through `DoctorFix`, so the
    /// same check can never lead to two different screens — and a check with no
    /// recognisable target falls back to Doctor rather than guessing.
    func testDoctorFixRoutingIsSharedAndFallsBackToDetails() {
        func check(_ name: String, _ detail: String, status: String = "warning") -> DoctorCheck {
            DoctorCheck(name: name, severity: .warn, status: status, detail: detail, remediation: nil)
        }
        XCTAssertEqual(DoctorFix(check("auth", "Deezer ARL is missing")), .credentials)
        XCTAssertEqual(DoctorFix(check("auth", "SoundCloud client ID not in Keychain")), .credentials)
        XCTAssertEqual(DoctorFix(check("rekordbox", "Rekordbox is running")), .rekordbox)
        XCTAssertEqual(DoctorFix(check("config", "config.yaml has no sources")), .config)
        XCTAssertEqual(DoctorFix(check("filesystem", "archive drift detected")), .none)
        XCTAssertNil(DoctorFix(check("filesystem", "archive drift detected")).destination)
        XCTAssertEqual(DoctorFix(check("filesystem", "archive drift detected")).shortActionLabel, "Details")
        // A passing check never offers a fix, whatever words it happens to use.
        XCTAssertEqual(DoctorFix(check("auth", "Deezer ARL is available", status: "ok")), .none)
    }

    /// The "Used by" column has no protocol field. It is derived from
    /// `sources.capabilities` using the same adapter rules `internal/doctor`
    /// applies when it decides which credential checks to emit.
    func testCredentialConsumersMirrorTheBackendAdapterRules() {
        func source(_ id: String, _ type: String, _ adapter: String) -> SourceCapability {
            SourceCapability(
                sourceID: id, sourceType: type, adapter: adapter,
                supportsPlan: true, supportsPlanWindow: false, supportsDownloadOrder: true,
                defaultPlanWindow: .first, defaultDownloadOrder: .newestFirst
            )
        }
        let sources = [
            source("sc-likes", "soundcloud", "scdl"),
            source("sc-free", "soundcloud", "scdl-freedl"),
            source("sp-deemix", "spotify", "deemix"),
            source("sp-spotdl", "spotify", "spotdl"),
        ]
        XCTAssertEqual(
            credentialConsumers(.soundCloudClientID, sources: sources),
            ["sc-likes", "sc-free"]
        )
        XCTAssertEqual(credentialConsumers(.deemixARL, sources: sources), ["sp-deemix"])
        XCTAssertEqual(credentialConsumers(.spotifyApp, sources: sources), ["sp-deemix"])
        XCTAssertTrue(credentialConsumers(.deemixARL, sources: []).isEmpty)
    }

    /// C13 — when something outside Keychain supplies the running value, the
    /// row must never offer a plain "Replace", which would imply that writing
    /// the Keychain entry changes what the backend uses.
    func testExternalOverrideIsMarkedAndNeverOfferedAPlainReplace() {
        func status(health: String, storage: String) -> CredentialStatus {
            CredentialStatus(
                kind: .deemixARL, title: "Deezer ARL", health: health,
                storageSource: storage, summary: "", lastFailureKind: nil, lastFailureMessage: nil
            )
        }
        let env = status(health: "external_override", storage: "env")
        XCTAssertTrue(env.isExternallyOverridden)
        XCTAssertEqual(env.actionLabel, "Move to Keychain")
        XCTAssertEqual(env.storageLabel, "environment variable")

        let spotdl = status(health: "available", storage: "spotdl_config")
        XCTAssertTrue(spotdl.isExternallyOverridden)
        XCTAssertEqual(spotdl.actionLabel, "Move to Keychain")

        let keychain = status(health: "available", storage: "keychain")
        XCTAssertFalse(keychain.isExternallyOverridden)
        XCTAssertEqual(keychain.actionLabel, "Replace")
        XCTAssertEqual(keychain.storageLabel, "macOS Keychain")

        let missing = status(health: "missing", storage: "")
        XCTAssertEqual(missing.actionLabel, "Add…")
        XCTAssertEqual(missing.storageLabel, "not stored")
        XCTAssertFalse(missing.environmentVariables.isEmpty)
    }

    // MARK: - Phase 6: Free DL (C7, C8)

    /// C8 — three different things a Free DL cell can mean. `pending`,
    /// `matching` and `probing` are "still checking"; `not_found` is a real
    /// answer. Rendering both as an empty cell is the ambiguity this rules out.
    func testFreeDLRowDistinguishesStillCheckingFromNothingFound() {
        func row(local: String?, probe: String?) -> FreeDLPlanRow {
            var payload: [String: JSONValue] = [
                "index": .number(0),
                "remote_id": .string("1"),
                "title": .string("Track"),
                "local_quality": .object(["lossless": .bool(false)]),
                "free_dl_probe": .object(probe.map { ["status": JSONValue.string($0)] } ?? [:]),
                "selectable": .bool(true),
                "selected": .bool(true),
            ]
            if let local { payload["local_state"] = .string(local) }
            let data = try! JSONEncoder.agent.encode(JSONValue.object(payload))
            return try! JSONDecoder.agent.decode(FreeDLPlanRow.self, from: data)
        }
        for state in ["pending", "matching", "probing"] {
            XCTAssertFalse(row(local: state, probe: nil).localResolved, state)
        }
        XCTAssertTrue(row(local: "not_found", probe: nil).localResolved)
        XCTAssertEqual(row(local: "not_found", probe: nil).localLabel, "not in library")
        XCTAssertFalse(row(local: "matched", probe: nil).freeDLResolved)
        XCTAssertTrue(row(local: "matched", probe: "no_free_dl").freeDLResolved)
        XCTAssertEqual(row(local: "matched", probe: "no_free_dl").freeDLLabel, "no free DL")
    }

    /// C8 — a row is emitted before its probe returns, so `selectable: false`
    /// on a streaming row means "not decided yet". Calling it Blocked there is
    /// a claim udl has not made.
    func testStreamingRowIsCheckingNotBlocked() {
        func row(selectable: Bool, probe: String?, skip: String?) -> FreeDLPlanRow {
            var payload: [String: JSONValue] = [
                "index": .number(0),
                "remote_id": .string("1"),
                "title": .string("Track"),
                "local_quality": .object(["lossless": .bool(false)]),
                "free_dl_probe": .object(probe.map { ["status": JSONValue.string($0)] } ?? [:]),
                "selectable": .bool(selectable),
                "selected": .bool(selectable),
            ]
            if let skip { payload["skip_reason"] = .string(skip) }
            let data = try! JSONEncoder.agent.encode(JSONValue.object(payload))
            return try! JSONDecoder.agent.decode(FreeDLPlanRow.self, from: data)
        }
        let streaming = row(selectable: false, probe: nil, skip: nil)
        XCTAssertTrue(streaming.isStillResolving)
        XCTAssertEqual(streaming.statusLabel, "Checking")
        XCTAssertTrue(streaming.lockReason.contains("not finished checking"))

        // `pending` is the same state, spelled by recomputeSelectable.
        XCTAssertTrue(row(selectable: false, probe: nil, skip: "pending").isStillResolving)
        // A probe can land available before the source plan says whether the
        // row is toggleable. No reason has been written, so nothing is known.
        XCTAssertTrue(row(selectable: false, probe: "available", skip: nil).isStillResolving)

        let resolved = row(selectable: false, probe: "no_free_dl", skip: "no_free_dl")
        XCTAssertFalse(resolved.isStillResolving)
        XCTAssertEqual(resolved.statusLabel, "No free DL")
        XCTAssertEqual(resolved.lockReason, "This track has no Free DL link on SoundCloud.")

        let have = row(selectable: false, probe: "available", skip: "already-present")
        XCTAssertEqual(have.statusLabel, "Have")
        XCTAssertEqual(have.statusSeverity, .idle)

        let upgrade = row(selectable: true, probe: "available", skip: nil)
        XCTAssertEqual(upgrade.statusLabel, "Upgrade")
        XCTAssertFalse(upgrade.isStillResolving)
    }

    /// Quality is the mockup's colour-coded monospace cell. Under 320 kbps is
    /// the low tint; a probe error says so instead of reading as absent.
    func testFreeDLQualitySummaryNamesProbeFailureRatherThanRenderingBlank() {
        func quality(_ payload: [String: JSONValue]) -> FreeDLQuality {
            let data = try! JSONEncoder.agent.encode(JSONValue.object(payload))
            return try! JSONDecoder.agent.decode(FreeDLQuality.self, from: data)
        }
        let lossless = quality(["codec": .string("flac"), "lossless": .bool(true)])
        XCTAssertEqual(lossless.summary, "flac lossless")
        XCTAssertEqual(lossless.severity, .ok)
        let low = quality(["codec": .string("mp3"), "lossless": .bool(false), "effective_bitrate": .number(192_000)])
        XCTAssertEqual(low.summary, "mp3 192k")
        XCTAssertEqual(low.severity, .warn)
        XCTAssertEqual(quality(["lossless": .bool(false)]).summary, "—")
        XCTAssertEqual(quality(["lossless": .bool(false), "error": .string("no stream")]).summary, "probe failed")
    }

    /// C7 — four RPCs, four steps, and a step whose backend prerequisite does
    /// not exist is never silently clickable.
    func testFreeDLPhasesNameTheirOwnRPC() {
        XCTAssertEqual(FreeDLPhase.allCases.count, 4)
        XCTAssertEqual(FreeDLPhase.plan.method, "freedl.plan.start")
        XCTAssertEqual(FreeDLPhase.capture.method, "freedl.capture.start")
        XCTAssertEqual(FreeDLPhase.promotionPlan.method, "freedl.promotionPlan.build")
        XCTAssertEqual(FreeDLPhase.promote.method, "freedl.promote.apply")
        XCTAssertEqual(Set(FreeDLPhase.allCases.map(\.method)).count, 4)
        XCTAssertEqual(FreeDLPhase.allCases.map(\.number), [1, 2, 3, 4])
    }

    // MARK: - Phase 6: Rekordbox (C9, C10)

    /// C9 — the presentation reads the plan; it never becomes the plan. The
    /// value handed to `rekordbox.apply` must stay the exact `JSONValue` the
    /// backend sent, `omitempty` gaps and all.
    func testRekordboxPlanPresentationKeepsTheSentValueVerbatim() throws {
        let payload = """
        {
          "version": "v1",
          "generated_at": "2026-08-01T15:04:00Z",
          "mode": "mirror",
          "music_playlist": {"name": "Friday Warmup", "track_count": 3},
          "rekordbox_playlist": {"name": "UDL/Warmup"},
          "rekordbox_db_dir": "/db",
          "backup_dir": "/backups",
          "summary": {"music_total": 3, "matched_by_path": 2, "missing_in_rekordbox": 1, "will_add": 1, "will_move": 1},
          "rows": [
            {"music_index": 1, "artist": "A", "title": "One", "duration": "3:20", "path": "/a.aiff", "match_status": "matched", "action": "add"},
            {"music_index": 2, "artist": "B", "title": "Two", "duration": "4:01", "path": "/b.aiff", "match_status": "missing", "action": "skip"}
          ],
          "checksum_sha256": "63be0000000000000000000000000000000000000000000000000000000910ff",
          "future_field": {"kept": true}
        }
        """
        let value = try JSONDecoder.agent.decode(JSONValue.self, from: Data(payload.utf8))
        let plan = try XCTUnwrap(RekordboxPlanPresentation(value))
        // The unknown field survives, because the value is never re-encoded
        // from the Swift model.
        XCTAssertEqual(plan.value.objectValue?["future_field"]?.objectValue?["kept"]?.boolValue, true)
        XCTAssertEqual(plan.title, "Friday Warmup → UDL/Warmup")
        XCTAssertEqual(plan.missing, 1)
        XCTAssertEqual(plan.changeCount, 2)
        XCTAssertEqual(plan.blockers.count, 1)
        XCTAssertEqual(plan.rows.first?.duration, "3:20")
        XCTAssertTrue(plan.shortChecksum.hasPrefix("63be"))
    }

    /// A folder plan carries no top-level summary; its counts are the sum of
    /// its per-operation summaries rather than a zero it never stated.
    func testRekordboxFolderPlanSumsPerOperationSummaries() throws {
        let payload = """
        {
          "version": "v1-folder",
          "operations": [
            {"summary": {"music_total": 2, "will_add": 1}, "rows": [
              {"music_index": 1, "title": "One", "match_status": "matched", "action": "add"}]},
            {"summary": {"music_total": 3, "will_add": 2}, "rows": [
              {"music_index": 1, "title": "Two", "match_status": "matched", "action": "add"}]}
          ],
          "checksum_sha256": "abc"
        }
        """
        let value = try JSONDecoder.agent.decode(JSONValue.self, from: Data(payload.utf8))
        let plan = try XCTUnwrap(RekordboxPlanPresentation(value))
        XCTAssertEqual(plan.musicTotal, 5)
        XCTAssertEqual(plan.willAdd, 3)
        XCTAssertEqual(plan.rows.count, 2)
    }

    /// C10 — the primary action always names its blocker, and a blocker the
    /// protocol cannot report before an attempt is only ever set from a real
    /// backend refusal.
    func testRekordboxApplyGateNamesEveryBlocker() throws {
        func plan(missing: Int, checksum: String = "abc") throws -> RekordboxPlanPresentation {
            let rows = (0..<missing).map {
                #"{"music_index": \#($0), "title": "T", "match_status": "missing", "action": "skip"}"#
            }.joined(separator: ",")
            let payload = #"{"version":"v1","summary":{"will_add":2},"rows":[\#(rows)],"checksum_sha256":"\#(checksum)"}"#
            let value = try JSONDecoder.agent.decode(JSONValue.self, from: Data(payload.utf8))
            return try XCTUnwrap(RekordboxPlanPresentation(value))
        }

        let noPlan = RekordboxApplyGate(plan: nil, obstacle: nil, isPlanning: false)
        XCTAssertEqual(noPlan.actionLabel, "Generate plan")
        XCTAssertEqual(noPlan.action, .generatePlan)

        let blocked = RekordboxApplyGate(plan: try plan(missing: 3), obstacle: nil, isPlanning: false)
        XCTAssertEqual(blocked.actionLabel, "3 tracks missing — resolve to apply")
        XCTAssertEqual(blocked.action, .showBlockedRows)
        XCTAssertTrue(blocked.blocksDryRun)
        XCTAssertNotNil(blocked.reason)

        let noChecksum = RekordboxApplyGate(plan: try plan(missing: 0, checksum: ""), obstacle: nil, isPlanning: false)
        XCTAssertEqual(noChecksum.actionLabel, "Regenerate plan — no checksum")
        XCTAssertEqual(noChecksum.action, .generatePlan)

        let ready = RekordboxApplyGate(plan: try plan(missing: 0), obstacle: nil, isPlanning: false)
        XCTAssertEqual(ready.actionLabel, "Apply 2 changes…")
        XCTAssertEqual(ready.action, .confirmApply)
        XCTAssertFalse(ready.blocksDryRun)
        XCTAssertNil(ready.reason)

        // A live refusal outranks the plan's own arithmetic.
        let running = RekordboxApplyGate(
            plan: try plan(missing: 0),
            obstacle: RekordboxObstacle(backendMessage: "Rekordbox is running; close Rekordbox before using this command (rekordbox)"),
            isPlanning: false
        )
        XCTAssertEqual(running.actionLabel, "Rekordbox is open — quit it to apply")
        XCTAssertEqual(running.action, .confirmApply)

        let drift = RekordboxApplyGate(
            plan: try plan(missing: 0),
            obstacle: RekordboxObstacle(backendMessage: "plan checksum mismatch"),
            isPlanning: false
        )
        XCTAssertEqual(drift.actionLabel, "Regenerate plan")
        XCTAssertEqual(drift.action, .generatePlan)
        XCTAssertEqual(drift.pillTitle, "Plan drifted")
    }

    /// The obstacle classifier reads the real backend sentences, not invented
    /// ones. These strings come from `internal/rekordbox/playlistsync/plan.go`.
    func testRekordboxObstacleClassifiesRealBackendMessages() {
        XCTAssertEqual(
            RekordboxObstacle(backendMessage: "Rekordbox is running; close Rekordbox before using this command (rekordbox)"),
            .rekordboxRunning("Rekordbox is running; close Rekordbox before using this command (rekordbox)")
        )
        if case .rekordboxRunning = RekordboxObstacle(backendMessage: "Rekordbox database sidecar exists while RB should be closed: /db/master.db-wal") {} else {
            XCTFail("a stale sidecar is the same blocker as a running process")
        }
        XCTAssertTrue(RekordboxObstacle(backendMessage: "plan checksum mismatch").requiresRegeneration)
        if case .partialMirrorRefused = RekordboxObstacle(backendMessage: "plan has 3 missing Rekordbox tracks; v1 refuses partial mirror apply") {} else {
            XCTFail("a refused partial mirror is not a generic failure")
        }
        // An unrecognised message never invents an action label.
        XCTAssertNil(RekordboxObstacle(backendMessage: "python bridge exited 1").actionLabel)
    }

    // MARK: Phase 7 — Playlists and Config

    /// `missing_local` is `omitempty` on the wire, so a track udl found on disk
    /// arrives with the key absent. Treating absent as "unknown" would put a
    /// warning pill on every healthy row.
    func testAbsentMissingLocalMeansTheFileWasFound() throws {
        let payload = """
        {
          "version": 1,
          "playlist_id": "friday",
          "name": "Friday warmup",
          "provider": "apple_music",
          "provider_playlist": "Friday warmup",
          "refreshed_at": "2026-08-01T10:00:00Z",
          "checksum_sha256": "abc123",
          "tracks": [
            {"index": 1, "title": "Present", "path": "/music/present.aiff"},
            {"index": 2, "title": "Gone", "missing_local": true}
          ]
        }
        """.data(using: .utf8)!
        let snapshot = try JSONDecoder.agent.decode(PlaylistSnapshot.self, from: payload)
        XCTAssertFalse(snapshot.tracks[0].isMissingLocally)
        XCTAssertTrue(snapshot.tracks[1].isMissingLocally)
        XCTAssertEqual(snapshot.missingLocally, 1)
    }

    /// The duration field is `duration of t as text` from Music.app's
    /// AppleScript: seconds as a real in the *system* locale. Playlists and
    /// Rekordbox read the same field, so they share one parser, and an
    /// unparseable value falls back to the raw string rather than to a number
    /// udl never reported.
    func testMusicDurationIsSharedAndNeverInventsATime() {
        XCTAssertEqual(MusicDuration.label("266.029"), "4:26")
        XCTAssertEqual(MusicDuration.label("266,029"), "4:26")
        XCTAssertEqual(MusicDuration.label("59"), "0:59")
        XCTAssertEqual(MusicDuration.label(nil), "—")
        XCTAssertEqual(MusicDuration.label(""), "—")
        XCTAssertEqual(MusicDuration.label("unknown"), "unknown")
        // The Rekordbox row must resolve through the same helper.
        let row = RekordboxPlanRowView(.object([
            "music_index": .number(3),
            "title": .string("Atalantis"),
            "duration": .string("266,029"),
            "match_status": .string("matched"),
            "action": .string("add")
        ]))
        XCTAssertEqual(row?.durationLabel, MusicDuration.label("266,029"))
    }

    /// C11 — a refresh that fails or is canceled must be visually distinct from
    /// one that succeeded, and must say the previous snapshot survived. The
    /// message alone cannot carry that; the severity and the flag do.
    func testPlaylistStatusMarksEveryOutcomeThatPreservedTheSnapshot() {
        let refreshed = AppState.PlaylistStatus(message: "Snapshot refreshed: +2, −0, 41 unchanged.", severity: .ok)
        XCTAssertFalse(refreshed.preservedPreviousSnapshot)

        let canceled = AppState.PlaylistStatus(
            message: "Refresh canceled. The previous valid snapshot was preserved.",
            severity: .warn,
            preservedPreviousSnapshot: true
        )
        let failed = AppState.PlaylistStatus(
            message: "Refresh failed. The previous valid snapshot was preserved.",
            severity: .error,
            preservedPreviousSnapshot: true
        )
        XCTAssertNotEqual(canceled.severity, failed.severity, "cancel and failure are not the same outcome")
        for status in [canceled, failed] {
            XCTAssertTrue(status.preservedPreviousSnapshot)
            XCTAssertTrue(status.message.contains("preserved"))
        }
    }

    /// C14 — a `*bool` policy field has three states on the wire. A `Toggle`
    /// would turn every unset key into an explicit `false` on the first save.
    func testTriStateBoolRoundTripsUnsetSeparatelyFromFalse() throws {
        XCTAssertEqual(TriStateBool(nil), .unset)
        XCTAssertEqual(TriStateBool(false), .no)
        XCTAssertEqual(TriStateBool(true), .yes)
        XCTAssertNil(TriStateBool.unset.value)
        XCTAssertEqual(TriStateBool.no.value, false)

        // And unset must stay absent through an encode, not become `false`.
        let policy = SourceSyncPolicy(breakOnExisting: true, askOnExisting: nil, localIndexCache: false)
        let encoded = try JSONEncoder.agent.encode(policy)
        let rawObject = try JSONSerialization.jsonObject(with: encoded)
        let object = try XCTUnwrap(rawObject as? [String: Any])
        XCTAssertEqual(object["break_on_existing"] as? Bool, true)
        XCTAssertEqual(object["local_index_cache"] as? Bool, false)
        XCTAssertNil(object["ask_on_existing"], "unset must not be written as false")
    }

    /// The local checks exist to block Save and to attribute a problem to one
    /// sidebar row before a save is attempted. They must attribute, not just
    /// list.
    func testConfigValidationAttributesProblemsToTheirSource() {
        let config = MainConfig(
            version: 1,
            defaults: MainConfigDefaults(
                stateDir: "", archiveFile: "archive.txt", threads: 1,
                continueOnError: true, commandTimeoutSeconds: 900
            ),
            sources: [
                source(id: "likes", url: "https://soundcloud.com/x/likes", targetDir: "/music"),
                source(id: "likes", url: "https://soundcloud.com/y/likes", targetDir: "/music"),
                source(id: "spot", url: "", targetDir: "")
            ],
            rekordbox: nil
        )
        let issues = ConfigValidation.issues(in: config)
        XCTAssertTrue(issues.contains { $0.message.contains("state_dir") && $0.sourceID == nil })
        XCTAssertTrue(issues.contains { $0.message.contains("Duplicate source id") && $0.sourceID == "likes" })
        XCTAssertTrue(issues.contains { $0.message.contains("no url") && $0.sourceID == "spot" })
        XCTAssertTrue(issues.contains { $0.message.contains("no target_dir") && $0.sourceID == "spot" })
        XCTAssertTrue(issues.allSatisfy { $0.origin == .local })

        let clean = MainConfig(
            version: 1,
            defaults: MainConfigDefaults(
                stateDir: "/state", archiveFile: "archive.txt", threads: 1,
                continueOnError: true, commandTimeoutSeconds: 900
            ),
            sources: [source(id: "likes", url: "https://soundcloud.com/x/likes", targetDir: "/music")],
            rekordbox: nil
        )
        XCTAssertTrue(ConfigValidation.issues(in: clean).isEmpty)
    }

    /// `config.Validate` requires `state_file` for both source types the editor
    /// can create. Without this rule the first Finish Setup was refused by the
    /// backend with a message no screen rendered.
    func testASourceWithoutAStateFileIsRejectedBeforeItIsSent() {
        let config = MainConfig(
            version: 1,
            defaults: MainConfigDefaults(
                stateDir: "/state", archiveFile: "archive.txt", threads: 1,
                continueOnError: true, commandTimeoutSeconds: 900
            ),
            sources: [
                source(id: "likes", url: "https://soundcloud.com/x/likes", targetDir: "/music", stateFile: nil),
                source(id: "blank", url: "https://soundcloud.com/y/likes", targetDir: "/music", stateFile: "  ")
            ],
            rekordbox: nil
        )
        let issues = ConfigValidation.issues(in: config)
        XCTAssertTrue(issues.contains { $0.message.contains("state_file") && $0.sourceID == "likes" })
        XCTAssertTrue(issues.contains { $0.message.contains("state_file") && $0.sourceID == "blank" })
    }

    /// Go marshals a nil slice or map as `null`. `DefaultEmpty` is the one place
    /// that is handled, so this pins its three behaviours: null decodes empty, a
    /// present value still decodes, and encoding emits a bare collection so a
    /// wrapped field is invisible on the way back out.
    func testDefaultEmptyDecodesNullAndEncodesABareCollection() throws {
        struct Probe: Codable, Equatable {
            let name: String
            @DefaultEmpty var rows: [String]
            @DefaultEmpty var index: [String: Int]
        }

        let null = try JSONDecoder.agent.decode(
            Probe.self,
            from: #"{"name": "n", "rows": null, "index": null}"#.data(using: .utf8)!
        )
        XCTAssertTrue(null.rows.isEmpty)
        XCTAssertTrue(null.index.isEmpty)

        let absent = try JSONDecoder.agent.decode(
            Probe.self,
            from: #"{"name": "n"}"#.data(using: .utf8)!
        )
        XCTAssertTrue(absent.rows.isEmpty)
        XCTAssertTrue(absent.index.isEmpty)

        let present = try JSONDecoder.agent.decode(
            Probe.self,
            from: #"{"name": "n", "rows": ["a"], "index": {"a": 1}}"#.data(using: .utf8)!
        )
        XCTAssertEqual(present.rows, ["a"])
        XCTAssertEqual(present.index, ["a": 1])

        // The memberwise initialiser must still take the bare collection, or
        // wrapping a field would rewrite every construction site.
        let built = Probe(name: "n", rows: ["a"], index: ["a": 1])
        let encoded = try JSONSerialization.jsonObject(
            with: JSONEncoder.agent.encode(built)
        ) as? [String: Any]
        XCTAssertEqual(encoded?["rows"] as? [String], ["a"])
        XCTAssertEqual(encoded?["index"] as? [String: Int], ["a": 1])
    }

    /// The fresh-install case: every collection the app reads at startup arrives
    /// `null` at once. Before `DefaultEmpty`, `playlists.config.read` alone was
    /// enough to raise a modal "playlist cache could not load" on first launch.
    func testEveryStartupResultDecodesWithAllCollectionsNull() throws {
        func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
            try JSONDecoder.agent.decode(type, from: json.data(using: .utf8)!)
        }

        let initialize = try decode(InitializeResult.self, """
        {"protocol_version": 1,
         "build": {"version": "dev", "commit": "abc", "date": "now"},
         "methods": null, "working_dir": "/tmp", "config_paths": null,
         "feature_config_paths": null, "capabilities": null}
        """)
        XCTAssertTrue(initialize.methods.isEmpty)
        XCTAssertTrue(initialize.configPaths.isEmpty)
        XCTAssertTrue(initialize.featureConfigPaths.isEmpty)
        XCTAssertTrue(initialize.capabilities.isEmpty)

        let doctor = try decode(DoctorResult.self, """
        {"checks": null, "effective_path": "/usr/bin",
         "resolved_dependencies": null, "exit_code": 0}
        """)
        XCTAssertTrue(doctor.checks.isEmpty)
        XCTAssertTrue(doctor.resolvedDependencies.isEmpty)

        let credentials = try decode(CredentialsListResult.self, #"{"credentials": null}"#)
        let sources = try decode(SourceCapabilitiesResult.self, #"{"sources": null}"#)
        let playlistList = try decode(PlaylistListResult.self, #"{"playlists": null}"#)
        let providerList = try decode(ProviderPlaylistListResult.self, #"{"playlists": null}"#)
        let playlistConfig = try decode(PlaylistConfig.self, #"{"version": 1, "playlists": null}"#)
        XCTAssertTrue(credentials.credentials.isEmpty)
        XCTAssertTrue(sources.sources.isEmpty)
        XCTAssertTrue(playlistList.playlists.isEmpty)
        XCTAssertTrue(providerList.playlists.isEmpty)
        XCTAssertTrue(playlistConfig.playlists.isEmpty)

        let freeDL = try decode(FreeDLConfig.self, """
        {"version": 1,
         "defaults": {"plan_limit": 50, "download_order": "oldest_first",
                      "target_format": "auto", "min_match_score": 72,
                      "ambiguity_gap": 8, "replace_limit": 0,
                      "command_timeout_seconds": 900},
         "jobs": null}
        """)
        XCTAssertTrue(freeDL.jobs.isEmpty)

        let rekordbox = try decode(RekordboxSyncConfig.self, #"{"folders": null, "jobs": null}"#)
        XCTAssertTrue(rekordbox.folders.isEmpty)
        XCTAssertTrue(rekordbox.jobs.isEmpty)

        let inspect = try decode(RekordboxInspectResult.self, #"{"playlists": null, "contents": null}"#)
        XCTAssertTrue(inspect.playlists.isEmpty)
        XCTAssertTrue(inspect.contents.isEmpty)

        let config = try decode(MainConfig.self, """
        {"version": 1,
         "defaults": {"state_dir": "/state", "archive_file": "archive.txt",
                      "threads": 1, "continue_on_error": true,
                      "command_timeout_seconds": 900},
         "sources": null}
        """)
        XCTAssertTrue(config.sources.isEmpty)

        let onboarding = try decode(OnboardingState.self, """
        {"reason": "no_sources", "auto_started": true, "config_path": "/c.yaml",
         "config_context_label": "/c.yaml", "detail_lines": null,
         "defaults": {"state_dir": "/state", "archive_file": "archive.txt",
                      "threads": 1, "continue_on_error": true,
                      "command_timeout_seconds": 900}}
        """)
        XCTAssertTrue(onboarding.detailLines.isEmpty)

        // A source udl planned to nothing still asks for a selection.
        let prompt = try decode(SelectRowsParams.self, """
        {"run_id": "r", "source_id": "s", "rows": null,
         "details": {"source_id": "s", "source_type": "soundcloud",
                     "adapter": "scdl", "url": "https://x.test",
                     "target_dir": "/m", "state_file": "/s",
                     "plan_limit": 50, "plan_window": "first", "dry_run": true},
         "download_order": "newest_first", "plan_window": "first"}
        """)
        XCTAssertTrue(prompt.rows.isEmpty)

        let snapshot = try decode(SourceSnapshot.self, """
        {"lifecycle": "done", "confirmed": true, "rows": null, "activity": null}
        """)
        XCTAssertTrue(snapshot.rows.isEmpty)
        XCTAssertTrue(snapshot.activity.isEmpty)
    }

    /// A duplicate cannot reuse the id: the id names the state file, so two
    /// sources sharing one would be two sources udl cannot tell apart.
    func testDuplicatingASourceTakesAFreeID() {
        let original = source(id: "likes", url: "https://soundcloud.com/x/likes", targetDir: "/music")
        let first = original.duplicated(existingIDs: ["likes"])
        XCTAssertEqual(first.id, "likes-copy")
        XCTAssertEqual(first.url, original.url)
        let second = original.duplicated(existingIDs: ["likes", "likes-copy"])
        XCTAssertEqual(second.id, "likes-copy-2")
    }

    /// C14 — `-32003` from `config.writeFile` is not a save error. Nothing was
    /// written, and the two SHAs the backend reported are what the conflict
    /// state offers the user a choice between.
    func testConfigWriteConflictCarriesBothSHAs() throws {
        let payload = """
        {
          "path": "/Users/x/.config/udl/config.yaml",
          "expected_content_sha256": "aaaa1111",
          "actual_content_sha256": "bbbb2222"
        }
        """.data(using: .utf8)!
        let data = try JSONDecoder.agent.decode(JSONValue.self, from: payload)
        let object = try XCTUnwrap(data.objectValue)
        let conflict = AppState.ConfigConflict(
            path: try XCTUnwrap(object["path"]?.stringValue),
            expectedSHA256: try XCTUnwrap(object["expected_content_sha256"]?.stringValue),
            actualSHA256: try XCTUnwrap(object["actual_content_sha256"]?.stringValue)
        )
        XCTAssertNotEqual(conflict.expectedSHA256, conflict.actualSHA256)
        XCTAssertEqual(conflict.path, "/Users/x/.config/udl/config.yaml")
    }

    private func source(
        id: String,
        url: String,
        targetDir: String,
        stateFile: String? = "state.sync.scdl"
    ) -> MainConfigSource {
        MainConfigSource(
            id: id, type: "soundcloud", enabled: true, targetDir: targetDir, url: url,
            stateFile: stateFile,
            sync: SourceSyncPolicy(breakOnExisting: nil, askOnExisting: nil, localIndexCache: nil),
            adapter: SourceAdapter(kind: "scdl", extraArgs: nil, minVersion: nil)
        )
    }
}
