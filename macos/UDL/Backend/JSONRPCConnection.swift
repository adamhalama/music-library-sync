import Foundation

enum JSONRPCConnectionError: LocalizedError, Sendable {
    case disconnected(String)
    case frameTooLarge
    case malformedFrame(String)
    case remote(code: Int, message: String, data: JSONValue?)
    case canceled

    var errorDescription: String? {
        switch self {
        case .disconnected(let detail): "Backend disconnected: \(detail)"
        case .frameTooLarge: "Backend sent a message larger than 8 MiB."
        case .malformedFrame(let detail): "Backend protocol decode failed: \(detail)"
        case .remote(_, let message, _): message
        case .canceled: "Request canceled."
        }
    }
}

private struct RPCErrorPayload: Codable, Sendable {
    let code: Int
    let message: String
    let data: JSONValue?
}

private struct IncomingEnvelope: Codable, Sendable {
    let jsonrpc: String
    let id: JSONValue?
    let method: String?
    let params: JSONValue?
    let result: JSONValue?
    let error: RPCErrorPayload?
}

private struct OutgoingRequest<Params: Encodable>: Encodable {
    let jsonrpc = "2.0"
    let id: String
    let method: String
    let params: Params
}

private struct OutgoingResponse: Encodable {
    let jsonrpc = "2.0"
    let id: JSONValue
    let result: JSONValue?
    let error: RPCErrorPayload?
}

actor JSONRPCConnection {
    static let maximumFrameBytes = 8 << 20

    private let readHandle: FileHandle
    private let writeHandle: FileHandle
    private var readerTask: Task<Void, Never>?
    private var nextID: UInt64 = 0
    private var pending: [String: CheckedContinuation<JSONValue, Error>] = [:]
    private var closed = false

    nonisolated let notifications: AsyncStream<AgentNotification>
    /// High-frequency progress is newest-only. Lossless lifecycle and terminal
    /// notifications continue through `notifications`.
    nonisolated let progressNotifications: AsyncStream<AgentNotification>
    nonisolated let uiRequests: AsyncStream<UIRequest>
    private let notificationContinuation: AsyncStream<AgentNotification>.Continuation
    private let progressNotificationContinuation: AsyncStream<AgentNotification>.Continuation
    private let uiContinuation: AsyncStream<UIRequest>.Continuation

    init(readHandle: FileHandle, writeHandle: FileHandle) {
        self.readHandle = readHandle
        self.writeHandle = writeHandle

        var notificationContinuation: AsyncStream<AgentNotification>.Continuation!
        notifications = AsyncStream { notificationContinuation = $0 }
        self.notificationContinuation = notificationContinuation

        var progressNotificationContinuation: AsyncStream<AgentNotification>.Continuation!
        progressNotifications = AsyncStream(bufferingPolicy: .bufferingNewest(1)) {
            progressNotificationContinuation = $0
        }
        self.progressNotificationContinuation = progressNotificationContinuation

        var uiContinuation: AsyncStream<UIRequest>.Continuation!
        uiRequests = AsyncStream { uiContinuation = $0 }
        self.uiContinuation = uiContinuation
    }

    func start() {
        guard readerTask == nil else { return }
        let handle = readHandle
        readerTask = Task.detached(priority: .userInitiated) { [weak self] in
            var buffer = Data()
            while !Task.isCancelled {
                let chunk = handle.availableData
                if chunk.isEmpty {
                    await self?.disconnect(reason: "protocol EOF")
                    return
                }
                buffer.append(chunk)
                if buffer.count > Self.maximumFrameBytes {
                    await self?.fail(JSONRPCConnectionError.frameTooLarge)
                    return
                }
                while let newline = buffer.firstIndex(of: 0x0A) {
                    let frame = Data(buffer[..<newline])
                    buffer.removeSubrange(...newline)
                    guard !frame.isEmpty else { continue }
                    await self?.consume(frame)
                }
            }
        }
    }

    func call<Params: Encodable & Sendable, Result: Decodable & Sendable>(
        _ method: AgentMethod,
        params: Params,
        as resultType: Result.Type = Result.self
    ) async throws -> Result {
        let value = try await callValue(method.rawValue, params: params)
        let data = try JSONEncoder.agent.encode(value)
        return try JSONDecoder.agent.decode(Result.self, from: data)
    }

    func callValue<Params: Encodable & Sendable>(_ method: String, params: Params) async throws -> JSONValue {
        guard !closed else { throw JSONRPCConnectionError.disconnected("connection is closed") }
        nextID += 1
        let id = "c-\(nextID)"
        let request = OutgoingRequest(id: id, method: method, params: params)
        let payload = try JSONEncoder.agent.encode(request)

        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                pending[id] = continuation
                do {
                    try writeFrame(payload)
                } catch {
                    pending.removeValue(forKey: id)
                    continuation.resume(throwing: error)
                }
            }
        } onCancel: {
            Task { await self.cancelPending(id) }
        }
    }

    func respond(to request: UIRequest, result: JSONValue) throws {
        try writeResponse(OutgoingResponse(id: request.id, result: result, error: nil))
    }

    func respondError(to request: UIRequest, code: Int, message: String) throws {
        try writeResponse(OutgoingResponse(
            id: request.id, result: nil,
            error: RPCErrorPayload(code: code, message: message, data: nil)
        ))
    }

    func close(reason: String = "closed by application") {
        readerTask?.cancel()
        readerTask = nil
        try? writeHandle.close()
        try? readHandle.close()
        disconnect(reason: reason)
    }

    private func writeResponse(_ response: OutgoingResponse) throws {
        try writeFrame(JSONEncoder.agent.encode(response))
    }

    private func writeFrame(_ payload: Data) throws {
        guard payload.count <= Self.maximumFrameBytes else {
            throw JSONRPCConnectionError.frameTooLarge
        }
        guard !closed else {
            throw JSONRPCConnectionError.disconnected("connection is closed")
        }
        var framed = payload
        framed.append(0x0A)
        try writeHandle.write(contentsOf: framed)
    }

    private func consume(_ frame: Data) {
        do {
            let message = try JSONDecoder.agent.decode(IncomingEnvelope.self, from: frame)
            guard message.jsonrpc == "2.0" else {
                throw JSONRPCConnectionError.malformedFrame("jsonrpc must be 2.0")
            }
            if let method = message.method {
                if let id = message.id, let kind = UIRequestKind(rawValue: method) {
                    uiContinuation.yield(UIRequest(id: id, kind: kind, params: message.params ?? .object([:])))
                } else if method == "sync.progress" {
                    progressNotificationContinuation.yield(AgentNotification(method: method, params: message.params ?? .object([:])))
                } else {
                    notificationContinuation.yield(AgentNotification(method: method, params: message.params ?? .object([:])))
                }
                return
            }
            guard let id = message.id, case .string(let key) = id,
                  let continuation = pending.removeValue(forKey: key) else {
                return
            }
            if let error = message.error {
                continuation.resume(throwing: JSONRPCConnectionError.remote(
                    code: error.code, message: error.message, data: error.data
                ))
            } else {
                continuation.resume(returning: message.result ?? .null)
            }
        } catch {
            fail(JSONRPCConnectionError.malformedFrame(redactedDiagnostic(error.localizedDescription)))
        }
    }

    private func cancelPending(_ id: String) {
        pending.removeValue(forKey: id)?.resume(throwing: JSONRPCConnectionError.canceled)
    }

    private func fail(_ error: Error) {
        closed = true
        let continuations = pending.values
        pending.removeAll()
        continuations.forEach { $0.resume(throwing: error) }
        notificationContinuation.finish()
        progressNotificationContinuation.finish()
        uiContinuation.finish()
        readerTask?.cancel()
        readerTask = nil
    }

    private func disconnect(reason: String) {
        guard !closed else { return }
        fail(JSONRPCConnectionError.disconnected(reason))
    }
}

extension JSONEncoder {
    static var agent: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }
}

extension JSONDecoder {
    static var agent: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let value = try decoder.singleValueContainer().decode(String.self)
            let fractional = ISO8601DateFormatter()
            fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = fractional.date(from: value) {
                return date
            }
            let standard = ISO8601DateFormatter()
            standard.formatOptions = [.withInternetDateTime]
            if let date = standard.date(from: value) {
                return date
            }
            throw DecodingError.dataCorruptedError(
                in: try decoder.singleValueContainer(),
                debugDescription: "Invalid RFC 3339 timestamp"
            )
        }
        return decoder
    }
}

func redactedDiagnostic(_ text: String) -> String {
    var value = text
    let patterns = [
        #"(?i)(authorization:\s*bearer\s+)[^\s"]+"#,
        #"(?i)(client_secret["=: ]+)[^\s",}]+"#,
        #"(?i)(arl["=: ]+)[^\s",}]+"#,
        #"\b[0-9a-fA-F]{192}\b"#,
    ]
    for pattern in patterns {
        value = value.replacingOccurrences(
            of: pattern, with: "$1<redacted>", options: .regularExpression
        )
    }
    return value
}
