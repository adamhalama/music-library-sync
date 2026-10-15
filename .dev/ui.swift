import AppKit
// Posts synthetic mouse and keyboard events for driving UDL-Dev.app during
// manual verification. Coordinates are in the flipped global space that
// `screencapture -R` and System Events use, so a point measured on a screenshot
// taken by `app.sh win` can be passed straight in.
//
//   ui click <x> <y>          left click
//   ui move <x> <y>           move the pointer only
//   ui type <text>            type a literal string
//   ui key <keycode> [flags]  press a key; flags: cmd, shift, opt, ctrl

func post(_ e: CGEvent?) {
    e?.post(tap: .cghidEventTap)
    usleep(60_000)
}

func click(_ x: Double, _ y: Double) {
    let p = CGPoint(x: x, y: y)
    post(CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: p, mouseButton: .left))
    post(CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown, mouseCursorPosition: p, mouseButton: .left))
    post(CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp, mouseCursorPosition: p, mouseButton: .left))
}

func flags(_ names: [String]) -> CGEventFlags {
    var f = CGEventFlags()
    for n in names {
        switch n {
        case "cmd": f.insert(.maskCommand)
        case "shift": f.insert(.maskShift)
        case "opt": f.insert(.maskAlternate)
        case "ctrl": f.insert(.maskControl)
        default: break
        }
    }
    return f
}

let args = Array(CommandLine.arguments.dropFirst())
switch args.first {
case "click":
    click(Double(args[1])!, Double(args[2])!)
case "move":
    let p = CGPoint(x: Double(args[1])!, y: Double(args[2])!)
    post(CGEvent(mouseEventSource: nil, mouseType: .mouseMoved, mouseCursorPosition: p, mouseButton: .left))
case "type":
    for scalar in Array(args.dropFirst()).joined(separator: " ").utf16 {
        let down = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true)
        down?.keyboardSetUnicodeString(stringLength: 1, unicodeString: [scalar])
        post(down)
        let up = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false)
        up?.keyboardSetUnicodeString(stringLength: 1, unicodeString: [scalar])
        post(up)
    }
case "key":
    let code = CGKeyCode(UInt16(args[1])!)
    let f = flags(Array(args.dropFirst(2)))
    let down = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: true)
    down?.flags = f
    post(down)
    let up = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: false)
    up?.flags = f
    post(up)
default:
    FileHandle.standardError.write(Data("usage: ui {click|move|type|key} …\n".utf8))
    exit(2)
}
