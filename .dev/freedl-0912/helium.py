#!/usr/bin/env python3
"""Operate an explicitly selected Helium tab without mouse/keyboard or activation.

Requires Helium's View > Developer > Allow JavaScript from Apple Events setting.
This helper never changes that setting or creates/activates a window. Gate pages
can themselves open OAuth windows or save dialogs; stop automation if the user
needs uninterrupted foreground work. Keep runtime config outside the repo.
"""
import argparse
import json
import subprocess
import uuid
from urllib.parse import urlsplit, urlunsplit
from pathlib import Path

SCRIPT = '''on run argv
    set windowID to (item 1 of argv) as integer
    set tabID to (item 2 of argv) as integer
    set sourceText to item 3 of argv
    tell application "Helium"
        return execute tab id tabID of window id windowID javascript sourceText
    end tell
end run'''


def execute(window, tab, source):
    try:
        result = subprocess.run(['osascript', '-e', SCRIPT, str(window), str(tab), source],
                                capture_output=True, text=True, timeout=20)
    except subprocess.TimeoutExpired:
        # The script can include private runtime form values; never echo argv.
        raise RuntimeError('Helium tab did not respond within 20 seconds; inspect state before retrying actions') from None
    if result.returncode:
        raise RuntimeError(result.stderr.strip())
    return result.stdout.strip()


def execute_main(window, tab, body):
    """Run in the page's own context; Apple Events otherwise uses an isolated world.

    A temporary DOM result node avoids exposing results to browser console logs.
    Existing page CSP nonce is used for the authorized automation script.
    Body is JavaScript function content: use return for a serializable result.
    """
    key = 'udl-result-' + uuid.uuid4().hex
    script = '''(()=>{const output=document.getElementById(KEY);
      try { output.textContent=JSON.stringify({value:(()=>{BODY})()}); }
      catch(error) { output.textContent=JSON.stringify({error:String(error)}); }
    })();'''.replace('KEY', json.dumps(key)).replace('BODY', body)
    wrapper = '''(()=>{const output=document.createElement('span');output.id=KEY;output.hidden=true;
      const script=document.createElement('script');
      script.nonce=[...document.scripts].find(s=>s.nonce)?.nonce||'';
      script.textContent=SOURCE;document.documentElement.append(output,script);
      const result=output.textContent;script.remove();output.remove();return result;})()'''
    raw = execute(window, tab, wrapper.replace('KEY', json.dumps(key)).replace('SOURCE', json.dumps(script)))
    if not raw:
        raise RuntimeError('Page did not execute automation script (check CSP/Trusted Types)')
    result = json.loads(raw)
    if 'error' in result:
        raise RuntimeError(result['error'])
    return result.get('value')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--window', type=int)
    parser.add_argument('--tab', type=int)
    sub = parser.add_subparsers(dest='action', required=True)
    sub.add_parser('tabs')
    sub.add_parser('snapshot')
    sub.add_parser('stop')
    evaluate = sub.add_parser('eval')
    evaluate.add_argument('script', type=Path)
    evaluate_main = sub.add_parser('eval-main', help='Execute function body in the page context')
    evaluate_main.add_argument('script', type=Path)
    navigate = sub.add_parser('navigate', help='Navigate only the selected tab, without activation')
    navigate.add_argument('url')
    inject = sub.add_parser('inject')
    inject.add_argument('--config', type=Path, help='Private JSON with email/name, never committed')
    args = parser.parse_args()
    if args.action == 'tabs':
        source = '''JSON.stringify(Application("Helium").windows().map(w => ({
            window:w.id(), tabs:w.tabs().map(t => ({tab:t.id(),title:t.title(),url:t.url()}))
        })))'''
        result = subprocess.run(['osascript', '-l', 'JavaScript', '-e', source],
                                capture_output=True, text=True, timeout=20, check=True)
        windows = json.loads(result.stdout)
        for window in windows:
            for tab in window['tabs']:
                parts = urlsplit(tab['url'])
                # OAuth callbacks can carry credentials in either component.
                tab['url'] = urlunsplit((parts.scheme, parts.netloc, parts.path,
                                        '[redacted]' if parts.query else '',
                                        '[redacted]' if parts.fragment else ''))
        print(json.dumps(windows))
        return
    if args.window is None or args.tab is None:
        parser.error('--window and --tab are required; inspect tabs first')
    if args.action == 'eval-main':
        print(json.dumps(execute_main(args.window, args.tab, args.script.read_text())))
        return
    if args.action == 'navigate':
        if not args.url.startswith(('https://', 'http://')):
            parser.error('navigate requires an HTTP(S) URL')
        print(execute(args.window, args.tab, 'location.href='+json.dumps(args.url)+"; 'navigation requested'"))
        return
    if args.action == 'snapshot':
        source = '''JSON.stringify({url:location.href,title:document.title,
            text:document.body.innerText.slice(0,20000),
            controls:[...document.querySelectorAll('button,a,input')]
              .filter(e=>e.getClientRects().length && getComputedStyle(e).visibility!=='hidden')
              .map(e=>({tag:e.tagName,id:e.id,text:(e.innerText||e.getAttribute('aria-label')||'').slice(0,150),
                type:e.type,href:e.tagName==='A'?e.href:undefined,disabled:e.disabled})),
            autopilot:window.udlGate?.inspect?.()})'''
    elif args.action == 'stop':
        print(execute_main(args.window, args.tab, "window.udlGate?.stop?.(); return 'stop requested';"))
        return
    elif args.action == 'eval':
        source = args.script.read_text()
    else:
        location = execute(args.window, args.tab, 'location.hostname')
        if location.removeprefix('www.') not in {'gaterush.me','hypeddit.com','droploud.com','mypresskit.info'}:
            parser.error('inject only supports the four known gate hosts')
        config = json.loads(args.config.read_text()) if args.config else {}
        source = 'window.UDL_GATE_CONFIG = '+json.dumps(config)+';\n'
        source += (Path(__file__).resolve().parents[1]/'userscripts'/'udl-freedl-gate-autopilot.user.js').read_text()
        source += "\nreturn 'injected';"
        print(execute_main(args.window, args.tab, source))
        return
    print(execute(args.window, args.tab, source))


if __name__ == '__main__':
    main()
