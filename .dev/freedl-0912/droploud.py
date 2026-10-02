#!/usr/bin/env python3
"""Capture an unlocked Droploud native download without changing browser focus.

Run only after the real required actions have unlocked the exact gate. The normal
Download handler still asks the server for its signed media URL. Its file anchor
and success-page transition are intercepted, then the signed response is saved
without cookies, browser downloads, new tabs, scrolling, or foreground dialogs.
A timed-out native request retains its interception until the delayed response
arrives; inspect the tab before retrying. Runtime provenance contains an expiring
signed URL; keep it outside the checkout.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import time
from urllib.parse import urlsplit
import urllib.request
import uuid

from helium import execute_main

CAPTURE = r'''
if(location.origin!=='https://droploud.com'||location.pathname!==GATE_PATH)
  throw Error('The selected tab is not the exact Droploud gate');
const pane=document.querySelector('.dtr-stage.is-vis');
const button=[...pane?.querySelectorAll('button')||[]].find(b=>b.innerText==='Download');
if(!button||button.disabled||!pane.innerText.includes('Drop unlocked!'))
  throw Error('Complete the real gate requirements first');
if(window._udlDroploudCapturePending)throw Error('Previous native download request is still pending; inspect before retrying');
const anchorClick=HTMLAnchorElement.prototype.click, timer=window.setTimeout;
const state={status:'pending',restore(){
  if(HTMLAnchorElement.prototype.click===interceptAnchor)HTMLAnchorElement.prototype.click=anchorClick;
  if(window.setTimeout===interceptTimer)window.setTimeout=timer;
  if(window._udlDroploudCapturePending===state)delete window._udlDroploudCapturePending;
}};
function interceptAnchor(){
  if(this.download){state.file={url:this.href,name:this.download};state.status='captured';return;}
  return anchorClick.call(this);
}
function interceptTimer(fn,ms,...rest){
  if(state.file&&ms===150&&String(fn).includes('removeChild')&&String(fn).includes('/success')){
    state.restore();return 0;
  }
  return timer(fn,ms,...rest);
}
window[KEY]=state;
window._udlDroploudCapturePending=state;
HTMLAnchorElement.prototype.click=interceptAnchor;
window.setTimeout=interceptTimer;
try{button.click();}catch(error){state.restore();throw error;}return 'native download requested';
'''


def validate_media_url(value):
    url = urlsplit(value)
    if (url.scheme != 'https' or url.username or url.password or not url.hostname
            or not url.hostname.endswith('.r2.cloudflarestorage.com')):
        raise ValueError('Expected the native HTTPS Cloudflare R2 media URL')
    return value


def save_response(url, target):
    """Publish exclusively only after the complete media response has been saved."""
    part = target.parent / ('.' + target.name + '.' + uuid.uuid4().hex + '.part')
    digest = hashlib.sha256()
    total = 0
    try:
        with urllib.request.urlopen(url, timeout=45) as source, part.open('xb') as out:
            validate_media_url(source.geturl())
            kind = source.headers.get('Content-Type', '')
            if not kind.startswith(('audio/', 'application/octet-stream')):
                raise ValueError('Expected audio response, got ' + kind)
            expected = int(source.headers.get('Content-Length', '0'))
            while chunk := source.read(1024 * 1024):
                total += len(chunk)
                digest.update(chunk)
                out.write(chunk)
            out.flush()
            os.fsync(out.fileno())
        if not total or (expected and total != expected):
            raise ValueError('Incomplete native media response')
        os.link(part, target)
        return {'path': str(target), 'size': total, 'sha256': digest.hexdigest(), 'content_type': kind}
    finally:
        part.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--window', type=int, required=True)
    parser.add_argument('--tab', type=int, required=True)
    parser.add_argument('--track-id', type=uuid.UUID, required=True)
    parser.add_argument('--name', required=True, help='Plain media filename in Helium downloads')
    parser.add_argument('--evidence', type=Path, required=True, help='Private JSON outside the checkout')
    args = parser.parse_args()
    if Path(args.name).name != args.name or Path(args.name).suffix.lower() not in {'.wav', '.mp3', '.m4a', '.flac', '.aif', '.aiff'}:
        parser.error('--name must be a plain media filename')
    repo = Path(__file__).resolve().parents[2]
    evidence = args.evidence.expanduser().resolve()
    if evidence.is_relative_to(repo):
        parser.error('--evidence must be outside the checkout (signed URLs are private)')
    if evidence.exists():
        parser.error('evidence already exists; use a fresh provenance path')
    target = Path.home() / 'Downloads/helium-downloads' / args.name
    if target.exists():
        parser.error('output already exists')
    key = '_udlDroploud_' + uuid.uuid4().hex
    expr = json.dumps(key)
    gate_path = '/track/' + str(args.track_id)
    try:
        execute_main(args.window, args.tab,
                     CAPTURE.replace('GATE_PATH', json.dumps(gate_path)).replace('KEY', expr))
        deadline = time.monotonic() + 15
        while True:
            result = execute_main(args.window, args.tab,
                                  'return window[' + expr + ']?.file || null;')
            if result:
                break
            if time.monotonic() >= deadline:
                raise TimeoutError('No native download URL; inspect the gate before retrying')
            time.sleep(.5)
        validate_media_url(result['url'])
        result.update(gate_url='https://droploud.com' + gate_path,
                      capture_method='native unlocked Download handler')
        evidence.parent.mkdir(parents=True, exist_ok=True)
        with evidence.open('x', opener=lambda path, flags: os.open(path, flags, 0o600)) as out:
            out.write(json.dumps(result))
        evidence.chmod(0o600)
        metadata = save_response(result['url'], target)
        result.update(metadata)
        evidence.write_text(json.dumps(result))
        print(json.dumps(metadata))
    finally:
        try:
            execute_main(args.window, args.tab,
                         'const state=window[' + expr + ']; if(state?.status==="captured"){state.restore();delete window[' + expr + '];} return state?.status || null;')
        except Exception:
            pass


if __name__ == '__main__':
    main()
