#!/usr/bin/env python3
"""Inspect or follow one public Instagram profile in an existing background Helium tab.

The caller supplies the exact profile URL obtained from a gate. This script uses
only Helium's selected-tab JavaScript execution; it never activates a window or
uses mouse/keyboard automation. Without --apply it only navigates and inspects.
"""
import argparse
import json
import re
import time
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlsplit

from helium import execute


RESERVED = {
    'about', 'accounts', 'api', 'challenge', 'developer', 'direct', 'explore',
    'graphql', 'legal', 'login', 'oauth', 'p', 'reel', 'reels', 'stories',
}
PROFILE_SCRIPT = r'''JSON.stringify((()=>{
  const visible=e=>!!e.getClientRects().length && getComputedStyle(e).visibility!=='hidden';
  const label=e=>(e.innerText||e.getAttribute('aria-label')||'').trim().replace(/\s+/g,' ');
  const main=document.querySelector('main');
  const headers=main?[...main.querySelectorAll('header')].filter(visible):[];
  const login=location.pathname.startsWith('/accounts/') ||
    !!document.querySelector('input[type="password"]') ||
    [...document.querySelectorAll('[role="dialog"]')].some(e=>visible(e)&&/\b(log in|sign in)\b/i.test(label(e)));
  const headersInfo=headers.map(h=>({
    text:label(h).slice(0,1200),
    controls:[...h.querySelectorAll('button,[role="button"]')].filter(visible)
      .map(e=>({text:label(e),disabled:!!e.disabled||e.getAttribute('aria-disabled')==='true'}))
  }));
  return {url:location.href,title:document.title,login,hasMain:!!main,
    loadTime:performance.timeOrigin,
    headers:headersInfo};
})())'''

ACTION_SCRIPT = r'''JSON.stringify((()=>{
  const expected=EXPECTED;
  const visible=e=>!!e.getClientRects().length && getComputedStyle(e).visibility!=='hidden';
  const label=e=>(e.innerText||e.getAttribute('aria-label')||'').trim().replace(/\s+/g,' ');
  if(location.href!==URL) throw Error('Profile URL changed before action');
  if(location.pathname.startsWith('/accounts/') || document.querySelector('input[type="password"]') ||
      [...document.querySelectorAll('[role="dialog"]')]
        .some(e=>visible(e)&&/\b(log in|sign in)\b/i.test(label(e))))
    throw Error('Login required');
  const main=document.querySelector('main');
  if(!main) throw Error('Profile main element missing');
  const headers=[...main.querySelectorAll('header')].filter(visible);
  const matches=headers.filter(h=>{
    const text=label(h);
    return new RegExp('(^|\\s)'+expected.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')+'(\\s|$)','i').test(text) &&
      /\bposts?\b/i.test(text) && /\bfollowers?\b/i.test(text) && /\bfollowing\b/i.test(text);
  });
  if(matches.length!==1) throw Error('Expected profile header is missing or ambiguous');
  const buttons=[...matches[0].querySelectorAll('button,[role="button"]')]
    .filter(visible).filter(e=>/^(Follow|Following)$/i.test(label(e)));
  if(buttons.length!==1) throw Error('Profile follow control is missing or ambiguous');
  const button=buttons[0];
  if(label(button).toLowerCase()!=='follow' || button.disabled || button.getAttribute('aria-disabled')==='true')
    throw Error('Profile is no longer ready to follow');
  button.click();
  return {clicked:true,url:location.href,loadTime:performance.timeOrigin};
})())'''


def profile_url(value):
    parts = urlsplit(value)
    if (parts.scheme != 'https' or parts.netloc not in {'instagram.com', 'www.instagram.com'}):
        raise ValueError('Expected an HTTPS instagram.com profile URL')
    if parts.query or parts.fragment or parts.username or parts.password:
        raise ValueError('Profile URL must have no credentials, query, or fragment')
    match = re.fullmatch(r'/([A-Za-z0-9._]{1,30})/?', parts.path)
    if not match:
        raise ValueError('Expected one Instagram username path')
    username = match.group(1)
    if (username.lower() in RESERVED or username.startswith('.') or username.endswith('.')
            or '..' in username):
        raise ValueError('URL is not a public profile username')
    return value, username


def inspect(window, tab, url, username):
    page = json.loads(execute(window, tab, PROFILE_SCRIPT))
    if page['login']:
        return 'login-required', page
    if page['url'] != url:
        return 'loading', page
    matching = []
    for header in page['headers']:
        words = re.findall(r'[A-Za-z0-9._]+', header['text'].lower())
        if (username.lower() in words and re.search(r'\bposts?\b', header['text'], re.I)
                and re.search(r'\bfollowers?\b', header['text'], re.I)
                and re.search(r'\bfollowing\b', header['text'], re.I)):
            matching.append(header)
    if len(matching) > 1:
        return 'ambiguous-header', page
    if not matching:
        return 'loading', page
    controls = [c for c in matching[0]['controls'] if c['text'].lower() in {'follow', 'following'}]
    if not controls:
        return 'missing-control', page
    if len(controls) > 1:
        return 'ambiguous-control', page
    if controls[0]['disabled']:
        return 'disabled-control', page
    return controls[0]['text'].lower(), page


def await_profile(window, tab, url, username, timeout=30, newer_than=None,
                  required_state=None):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        state, last = inspect(window, tab, url, username)
        if newer_than is not None and last['loadTime'] <= newer_than:
            time.sleep(.5)
            continue
        if state in {'follow', 'following'} and (required_state is None or state == required_state):
            return state
        if state == 'missing-control' and required_state == 'following':
            time.sleep(.5)
            continue
        if state in {'login-required', 'ambiguous-header', 'missing-control',
                     'ambiguous-control', 'disabled-control'}:
            raise RuntimeError(f'Instagram profile inspection stopped: {state}')
        time.sleep(.5)
    title = last['title'] if last else ''
    suffix = f'; expected {required_state}' if required_state else ''
    raise RuntimeError(f'Instagram profile did not reach the expected state{suffix} (title: {title!r})')


def save_evidence(path, report):
    target = path.expanduser().resolve()
    repo = Path(__file__).resolve().parents[2]
    if target.is_relative_to(repo):
        raise ValueError('Evidence path must be outside the repository')
    with target.open('x', encoding='utf-8') as output:
        output.write(json.dumps(report, indent=2, sort_keys=True) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--window', type=int, required=True, help='Existing Helium window ID')
    parser.add_argument('--tab', type=int, required=True, help='Existing tab ID in that window')
    parser.add_argument('--url', required=True, help='Exact public profile URL from the gate')
    parser.add_argument('--apply', action='store_true', help='Click Follow if required')
    parser.add_argument('--evidence', type=Path, help='Write action proof outside the repository')
    args = parser.parse_args()
    try:
        url, username = profile_url(args.url)
        if args.window <= 0 or args.tab <= 0:
            raise ValueError('Window and tab IDs must be positive')
        if args.evidence:
            target = args.evidence.expanduser().resolve()
            if target.is_relative_to(Path(__file__).resolve().parents[2]) or target.exists():
                raise ValueError('Evidence path must be a new file outside the repository')
        execute(args.window, args.tab, 'location.href='+json.dumps(url)+"; 'navigation requested'")
        before = await_profile(args.window, args.tab, url, username)
        action = 'already-following' if before == 'following' else 'ready-to-follow'
        verified = before == 'following'
        if args.apply and before == 'follow':
            source = ACTION_SCRIPT.replace('EXPECTED', json.dumps(username)).replace('URL', json.dumps(url))
            result = json.loads(execute(args.window, args.tab, source))
            if not result.get('clicked') or result.get('url') != url:
                raise RuntimeError('Follow click did not return the expected profile URL')
            await_profile(args.window, args.tab, url, username, required_state='following')
            time.sleep(1)
            # A reload distinguishes a persisted follow from an optimistic button update.
            execute(args.window, args.tab, "location.reload(); 'reload requested'")
            await_profile(args.window, args.tab, url, username,
                          newer_than=result['loadTime'], required_state='following')
            action, verified = 'clicked-follow', True
        report = {'profile_url':url, 'username':username, 'window':args.window,
                  'tab':args.tab, 'before':before, 'action':action,
                  'verified_following':verified,
                  'checked_at':datetime.now(timezone.utc).isoformat()}
        if args.evidence:
            save_evidence(args.evidence, report)
        print(json.dumps(report, sort_keys=True))
    except (ValueError, RuntimeError, json.JSONDecodeError) as error:
        parser.exit(1, f'instagram: {error}\n')


if __name__ == '__main__':
    main()
