@_exported import Darwin
@_exported import Foundation

/// The Command Line Tools SDK does not ship the XCTest Swift module. This
/// narrow compatibility surface lets the existing XCTest source files execute
/// unchanged in the development runner; Xcode continues to use real XCTest.
open class XCTestCase {
    public required init() {}
}

public enum XCTFailureRecorder {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var failures: [String] = []

    public static var failureCount: Int {
        lock.withLock { failures.count }
    }

    public static func record(
        _ message: String,
        file: StaticString = #filePath,
        line: UInt = #line
    ) {
        lock.withLock { failures.append("\(file):\(line): \(message)") }
    }

    public static func failures(since index: Int) -> [String] {
        lock.withLock { Array(failures.dropFirst(index)) }
    }
}

private struct XCTUnwrapFailure: Error, CustomStringConvertible {
    let description: String
}

public func XCTFail(
    _ message: @autoclosure () -> String = "",
    file: StaticString = #filePath,
    line: UInt = #line
) {
    let detail = message()
    XCTFailureRecorder.record(detail.isEmpty ? "failed" : detail, file: file, line: line)
}

public func XCTAssertTrue(
    _ expression: @autoclosure () -> Bool,
    _ message: @autoclosure () -> String = "",
    file: StaticString = #filePath,
    line: UInt = #line
) {
    guard !expression() else { return }
    let detail = message()
    XCTFailureRecorder.record(detail.isEmpty ? "expected true" : detail, file: file, line: line)
}

public func XCTAssertFalse(
    _ expression: @autoclosure () -> Bool,
    _ message: @autoclosure () -> String = "",
    file: StaticString = #filePath,
    line: UInt = #line
) {
    guard expression() else { return }
    let detail = message()
    XCTFailureRecorder.record(detail.isEmpty ? "expected false" : detail, file: file, line: line)
}

public func XCTAssertNil<T>(
    _ expression: @autoclosure () -> T?,
    _ message: @autoclosure () -> String = "",
    file: StaticString = #filePath,
    line: UInt = #line
) {
    guard expression() != nil else { return }
    let detail = message()
    XCTFailureRecorder.record(detail.isEmpty ? "expected nil" : detail, file: file, line: line)
}

public func XCTAssertNotNil<T>(
    _ expression: @autoclosure () -> T?,
    _ message: @autoclosure () -> String = "",
    file: StaticString = #filePath,
    line: UInt = #line
) {
    guard expression() == nil else { return }
    let detail = message()
    XCTFailureRecorder.record(detail.isEmpty ? "expected non-nil" : detail, file: file, line: line)
}

public func XCTAssertEqual<T: Equatable>(
    _ expression1: @autoclosure () -> T,
    _ expression2: @autoclosure () -> T,
    _ message: @autoclosure () -> String = "",
    file: StaticString = #filePath,
    line: UInt = #line
) {
    let lhs = expression1()
    let rhs = expression2()
    guard lhs != rhs else { return }
    let detail = message()
    XCTFailureRecorder.record(
        detail.isEmpty ? "\(String(describing: lhs)) is not equal to \(String(describing: rhs))" : detail,
        file: file,
        line: line
    )
}

public func XCTAssertNotEqual<T: Equatable>(
    _ expression1: @autoclosure () -> T,
    _ expression2: @autoclosure () -> T,
    _ message: @autoclosure () -> String = "",
    file: StaticString = #filePath,
    line: UInt = #line
) {
    let lhs = expression1()
    let rhs = expression2()
    guard lhs == rhs else { return }
    let detail = message()
    XCTFailureRecorder.record(
        detail.isEmpty ? "\(String(describing: lhs)) is equal to \(String(describing: rhs))" : detail,
        file: file,
        line: line
    )
}

public func XCTUnwrap<T>(
    _ expression: @autoclosure () -> T?,
    _ message: @autoclosure () -> String = "",
    file: StaticString = #filePath,
    line: UInt = #line
) throws -> T {
    if let value = expression() { return value }
    let supplied = message()
    let detail = supplied.isEmpty ? "expected non-nil value" : supplied
    XCTFailureRecorder.record(detail, file: file, line: line)
    throw XCTUnwrapFailure(description: detail)
}
