import XCTest
@testable import UDL

final class JSONRPCConnectionTests: XCTestCase {
    private struct EchoResult: Codable, Sendable, Equatable {
        let value: String
    }

    func testCorrelatesOutOfOrderReplies() async throws {
        let inbound = Pipe()
        let outbound = Pipe()
        let connection = JSONRPCConnection(
            readHandle: inbound.fileHandleForReading,
            writeHandle: outbound.fileHandleForWriting
        )
        await connection.start()

        let first = Task {
            try await connection.callValue("fixture.first", params: JSONValue.object([:]))
        }
        let second = Task {
            try await connection.callValue("fixture.second", params: JSONValue.object([:]))
        }
        let requestA = try await readJSONObject(from: outbound.fileHandleForReading)
        let requestB = try await readJSONObject(from: outbound.fileHandleForReading)
        let requests = [requestA, requestB]
        let firstRequest = try XCTUnwrap(requests.first { $0["method"] as? String == "fixture.first" })
        let secondRequest = try XCTUnwrap(requests.first { $0["method"] as? String == "fixture.second" })

        try writeJSON([
            "jsonrpc": "2.0", "id": secondRequest["id"] as Any,
            "result": ["value": "second"],
        ], to: inbound.fileHandleForWriting)
        try writeJSON([
            "jsonrpc": "2.0", "id": firstRequest["id"] as Any,
            "result": ["value": "first"],
        ], to: inbound.fileHandleForWriting)

        let firstValue = try await first.value
        let secondValue = try await second.value
        XCTAssertEqual(firstValue, .object(["value": .string("first")]))
        XCTAssertEqual(secondValue, .object(["value": .string("second")]))
        try inbound.fileHandleForWriting.close()
        await connection.close()
    }

    func testDisconnectFailsPendingContinuation() async throws {
        let inbound = Pipe()
        let outbound = Pipe()
        let connection = JSONRPCConnection(
            readHandle: inbound.fileHandleForReading,
            writeHandle: outbound.fileHandleForWriting
        )
        await connection.start()
        let pending = Task {
            try await connection.callValue("fixture.pending", params: JSONValue.object([:]))
        }
        _ = try await readJSONObject(from: outbound.fileHandleForReading)
        try inbound.fileHandleForWriting.close()
        do {
            _ = try await pending.value
            XCTFail("pending request unexpectedly succeeded")
        } catch {
            XCTAssertTrue(error.localizedDescription.localizedCaseInsensitiveContains("disconnect"))
        }
    }

    func testNotificationAndUIRequestRoundTrip() async throws {
        let inbound = Pipe()
        let outbound = Pipe()
        let connection = JSONRPCConnection(
            readHandle: inbound.fileHandleForReading,
            writeHandle: outbound.fileHandleForWriting
        )
        await connection.start()

        try writeJSON([
            "jsonrpc": "2.0", "method": "sync.event",
            "params": ["run_id": "run-1"],
        ], to: inbound.fileHandleForWriting)
        var notifications = connection.notifications.makeAsyncIterator()
        let notification = await notifications.next()
        XCTAssertEqual(notification?.method, "sync.event")

        try writeJSON([
            "jsonrpc": "2.0", "id": "server-1", "method": "ui.confirm",
            "params": ["prompt": "Continue?", "default": false],
        ], to: inbound.fileHandleForWriting)
        var requests = connection.uiRequests.makeAsyncIterator()
        let nextRequest = await requests.next()
        let request = try XCTUnwrap(nextRequest)
        XCTAssertEqual(request.kind, .confirm)
        try await connection.respond(to: request, result: .object(["confirmed": .bool(true)]))
        let reply = try await readJSONObject(from: outbound.fileHandleForReading)
        XCTAssertEqual(reply["id"] as? String, "server-1")
        XCTAssertEqual((reply["result"] as? [String: Any])?["confirmed"] as? Bool, true)
        try inbound.fileHandleForWriting.close()
        await connection.close()
    }

    func testProgressIsNewestOnlyWhileLifecycleAndTerminalRemainLossless() async throws {
        let inbound = Pipe()
        let outbound = Pipe()
        let connection = JSONRPCConnection(
            readHandle: inbound.fileHandleForReading,
            writeHandle: outbound.fileHandleForWriting
        )
        await connection.start()

        for percent in [10, 20, 30] {
            try writeJSON([
                "jsonrpc": "2.0", "method": "sync.progress",
                "params": ["run_id": "run-1", "percent": percent],
            ], to: inbound.fileHandleForWriting)
        }
        try writeJSON([
            "jsonrpc": "2.0", "method": "sync.event",
            "params": ["run_id": "run-1", "sequence": 1],
        ], to: inbound.fileHandleForWriting)
        try writeJSON([
            "jsonrpc": "2.0", "method": "run.finished",
            "params": ["run_id": "run-1", "sequence": 2],
        ], to: inbound.fileHandleForWriting)

        // Receiving terminal is a barrier: the reader has already processed
        // every preceding progress frame, without relying on a timed sleep.
        var lifecycle = connection.notifications.makeAsyncIterator()
        let firstLifecycle = await lifecycle.next()
        let terminal = await lifecycle.next()
        XCTAssertEqual(firstLifecycle?.method, "sync.event")
        XCTAssertEqual(terminal?.method, "run.finished")

        var progress = connection.progressNotifications.makeAsyncIterator()
        let nextProgress = await progress.next()
        let latest = try XCTUnwrap(nextProgress)
        XCTAssertEqual(latest.params.objectValue?["percent"]?.intValue, 30)
        try inbound.fileHandleForWriting.close()
        await connection.close()
    }

    func testEveryUIRequestKindRoundTrips() async throws {
        for (offset, method) in ["ui.confirm", "ui.input", "ui.selectRows"].enumerated() {
            let inbound = Pipe()
            let outbound = Pipe()
            let connection = JSONRPCConnection(
                readHandle: inbound.fileHandleForReading,
                writeHandle: outbound.fileHandleForWriting
            )
            await connection.start()
            try writeJSON([
                "jsonrpc": "2.0",
                "id": "server-\(offset)",
                "method": method,
                "params": [:],
            ], to: inbound.fileHandleForWriting)
            var requests = connection.uiRequests.makeAsyncIterator()
            let nextRequest = await requests.next()
            let request = try XCTUnwrap(nextRequest)
            XCTAssertEqual(request.kind.rawValue, method)
            try await connection.respond(
                to: request,
                result: .object(["canceled": .bool(false)])
            )
            let reply = try await readJSONObject(from: outbound.fileHandleForReading)
            XCTAssertEqual(reply["id"] as? String, "server-\(offset)")
            try inbound.fileHandleForWriting.close()
            await connection.close()
        }
    }

    func testCancellationAnswersPromptBeforeCancelingRun() async {
        actor Recorder {
            var values: [String] = []
            func append(_ value: String) { values.append(value) }
        }
        let recorder = Recorder()
        await performOrderedCancellation(
            answerPending: { await recorder.append("prompt") },
            cancelRun: { await recorder.append("run") }
        )
        let values = await recorder.values
        XCTAssertEqual(values, ["prompt", "run"])
    }

    /// Docking the plan out of the sheet must not change the wire ordering:
    /// a pending `ui.selectRows` is still answered before `run.cancel` goes
    /// out, otherwise Go stays blocked on a request nobody will ever reply to.
    func testDockedPlanPromptRepliesBeforeCancelingRun() async throws {
        actor Recorder {
            var values: [String] = []
            func append(_ value: String) { values.append(value) }
        }
        let inbound = Pipe()
        let outbound = Pipe()
        let connection = JSONRPCConnection(
            readHandle: inbound.fileHandleForReading,
            writeHandle: outbound.fileHandleForWriting
        )
        await connection.start()
        try writeJSON([
            "jsonrpc": "2.0", "id": "plan-1", "method": "ui.selectRows",
            "params": [
                "run_id": "run-9",
                "source_id": "sc-likes",
                "rows": [],
                "details": [:],
                "download_order": "newest_first",
                "plan_window": "first",
            ],
        ], to: inbound.fileHandleForWriting)
        var requests = connection.uiRequests.makeAsyncIterator()
        let nextRequest = await requests.next()
        let request = try XCTUnwrap(nextRequest)
        XCTAssertEqual(request.kind, .selectRows)

        let recorder = Recorder()
        await performOrderedCancellation(
            answerPending: {
                try? await connection.respond(
                    to: request,
                    result: .object([
                        "selected_indices": .array([]),
                        "download_order": .string("newest_first"),
                        "canceled": .bool(true),
                        "rebuild": .bool(false),
                        "plan_window": .string("first"),
                    ])
                )
                await recorder.append("prompt")
            },
            cancelRun: { await recorder.append("run") }
        )

        let reply = try await readJSONObject(from: outbound.fileHandleForReading)
        XCTAssertEqual(reply["id"] as? String, "plan-1")
        XCTAssertEqual((reply["result"] as? [String: Any])?["canceled"] as? Bool, true)
        let values = await recorder.values
        XCTAssertEqual(values, ["prompt", "run"])
        try inbound.fileHandleForWriting.close()
        await connection.close()
    }

    private func writeJSON(_ object: [String: Any], to handle: FileHandle) throws {
        var data = try JSONSerialization.data(withJSONObject: object, options: [.sortedKeys])
        data.append(0x0A)
        try handle.write(contentsOf: data)
    }

    private func readJSONObject(from handle: FileHandle) async throws -> [String: Any] {
        // Blocking pipe reads must not occupy Swift's cooperative executor:
        // on a small CI runner the tasks that produce these bytes need it too.
        let data: Data = try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global().async {
                do {
                    var data = Data()
                    while true {
                        let byte = try handle.read(upToCount: 1) ?? Data()
                        if byte.isEmpty { throw JSONRPCConnectionError.disconnected("test pipe EOF") }
                        if byte[0] == 0x0A { break }
                        data.append(byte)
                    }
                    continuation.resume(returning: data)
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
        let object = try JSONSerialization.jsonObject(with: data)
        return try XCTUnwrap(object as? [String: Any])
    }
}
