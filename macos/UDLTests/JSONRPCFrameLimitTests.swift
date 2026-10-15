import Foundation
import XCTest
@testable import UDL

final class JSONRPCFrameLimitTests: XCTestCase {
    func testExactlyMaximumPayloadExcludesNewlineFromLimit() async throws {
        var bytes = notification(method: "fixture.maximum", size: JSONRPCConnection.maximumFrameBytes)
        bytes.append(0x0A)
        let methods = try await receivedMethods(bytes)
        XCTAssertEqual(methods, ["fixture.maximum"])
    }

    func testCoalescedLegalFramesMayExceedLimitTogether() async throws {
        // A regular file makes availableData return both frames together,
        // avoiding any dependence on how a pipe splits writes into reads.
        let size = JSONRPCConnection.maximumFrameBytes / 2 + 128
        var bytes = notification(method: "fixture.first", size: size)
        bytes.append(0x0A)
        bytes.append(notification(method: "fixture.second", size: size))
        bytes.append(0x0A)
        XCTAssertTrue(bytes.count > JSONRPCConnection.maximumFrameBytes)
        let methods = try await receivedMethods(bytes)
        XCTAssertEqual(methods, ["fixture.first", "fixture.second"])
    }

    func testOversizedCompleteFrameFailsPendingCall() async throws {
        var bytes = notification(method: "fixture.oversized", size: JSONRPCConnection.maximumFrameBytes + 1)
        bytes.append(0x0A)
        try await assertFrameTooLarge(bytes)
    }

    func testOversizedUnfinishedRemainderFailsPendingCall() async throws {
        // Consuming the preceding legal frame must not bypass the remainder
        // check, even when the oversized payload has no final newline.
        var bytes = notification(method: "fixture.first", size: 128)
        bytes.append(0x0A)
        bytes.append(notification(method: "fixture.oversized", size: JSONRPCConnection.maximumFrameBytes + 1))
        try await assertFrameTooLarge(bytes)
    }

    private func notification(method: String, size: Int) -> Data {
        let prefix = "{\"jsonrpc\":\"2.0\",\"method\":\"\(method)\",\"params\":\""
        let suffix = "\"}"
        let padding = String(repeating: "x", count: size - prefix.utf8.count - suffix.utf8.count)
        let frame = Data((prefix + padding + suffix).utf8)
        XCTAssertEqual(frame.count, size)
        return frame
    }

    private func receivedMethods(_ bytes: Data) async throws -> [String] {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: url) }
        try bytes.write(to: url)
        let input = try FileHandle(forReadingFrom: url)
        let output = Pipe()
        let connection = JSONRPCConnection(readHandle: input, writeHandle: output.fileHandleForWriting)
        await connection.start()
        var methods: [String] = []
        for await notification in connection.notifications {
            methods.append(notification.method)
        }
        await connection.close()
        return methods
    }

    private func assertFrameTooLarge(_ bytes: Data) async throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: url) }
        try bytes.write(to: url)
        let input = try FileHandle(forReadingFrom: url)
        let output = Pipe()
        let connection = JSONRPCConnection(readHandle: input, writeHandle: output.fileHandleForWriting)
        let pending = Task {
            try await connection.callValue("fixture.pending", params: JSONValue.object([:]))
        }
        // Wait for the outbound request so the continuation is registered
        // before the regular-file reader can encounter the oversized frame.
        _ = await Task.detached { output.fileHandleForReading.availableData }.value
        await connection.start()
        do {
            _ = try await pending.value
            XCTFail("Oversized frame unexpectedly accepted")
        } catch JSONRPCConnectionError.frameTooLarge {
            // Both complete and unfinished oversized payloads must report the
            // limit error, not merely disconnect at the fixture's EOF.
        } catch {
            XCTFail("Expected frameTooLarge, got \(error)")
        }
        await connection.close()
    }
}
