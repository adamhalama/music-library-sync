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
        let requestA = try readJSONObject(from: outbound.fileHandleForReading)
        let requestB = try readJSONObject(from: outbound.fileHandleForReading)
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

        XCTAssertEqual(try await first.value, .object(["value": .string("first")]))
        XCTAssertEqual(try await second.value, .object(["value": .string("second")]))
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
        _ = try readJSONObject(from: outbound.fileHandleForReading)
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
        let request = try XCTUnwrap(await requests.next())
        XCTAssertEqual(request.kind, .confirm)
        try await connection.respond(to: request, result: .object(["confirmed": .bool(true)]))
        let reply = try readJSONObject(from: outbound.fileHandleForReading)
        XCTAssertEqual(reply["id"] as? String, "server-1")
        XCTAssertEqual((reply["result"] as? [String: Any])?["confirmed"] as? Bool, true)
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
            let request = try XCTUnwrap(await requests.next())
            XCTAssertEqual(request.kind.rawValue, method)
            try await connection.respond(
                to: request,
                result: .object(["canceled": .bool(false)])
            )
            let reply = try readJSONObject(from: outbound.fileHandleForReading)
            XCTAssertEqual(reply["id"] as? String, "server-\(offset)")
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

    private func writeJSON(_ object: [String: Any], to handle: FileHandle) throws {
        var data = try JSONSerialization.data(withJSONObject: object, options: [.sortedKeys])
        data.append(0x0A)
        try handle.write(contentsOf: data)
    }

    private func readJSONObject(from handle: FileHandle) throws -> [String: Any] {
        var data = Data()
        while true {
            let byte = handle.readData(ofLength: 1)
            if byte.isEmpty { throw JSONRPCConnectionError.disconnected("test pipe EOF") }
            if byte[0] == 0x0A { break }
            data.append(byte)
        }
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }
}
