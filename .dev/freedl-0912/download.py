#!/usr/bin/env python3
"""Save an authorized same-origin media response through an owned Helium tab.

Uses the site's normal authenticated download endpoint; never exports cookies.
Binary chunks stay between Helium and Python rather than model-visible output.
Only completed files are published, exclusively, in Helium's download directory.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import time
import uuid
from urllib.parse import urlsplit
from helium import execute


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--window', type=int, required=True)
    p.add_argument('--tab', type=int, required=True)
    p.add_argument('--url', required=True)
    p.add_argument('--name', required=True)
    p.add_argument('--expected-bytes', type=int)
    a = p.parse_args()
    directory = Path.home()/'Downloads/helium-downloads'
    if Path(a.name).name != a.name or Path(a.name).suffix.lower() not in {'.wav','.mp3','.m4a','.flac','.aiff','.aif'}:
        p.error('name must be a plain media filename')
    target = directory/a.name
    if target.exists():
        p.error('output already exists')
    origin = execute(a.window, a.tab, 'location.origin')
    url = urlsplit(a.url)
    if url.scheme != 'https' or origin != url.scheme+'://'+url.netloc:
        p.error('download must be HTTPS and same-origin with the selected tab')
    key = '_udlFile_'+uuid.uuid4().hex
    expr = json.dumps(key)
    part = directory/('.'+a.name+'.'+uuid.uuid4().hex+'.part')
    try:
        source = '''window[KEY]={state:'loading'};
          fetch(URL,{signal:AbortSignal.timeout(180000)}).then(async r=>{
            if(!r.ok)throw Error('HTTP '+r.status);
            const type=r.headers.get('content-type')||'';
            if(/text\/|json|html/i.test(type))throw Error('Expected media, got '+type);
            const bytes=new Uint8Array(await r.arrayBuffer());
            window[KEY]={state:'ready',bytes,size:bytes.length,type};
          }).catch(e=>window[KEY]={state:'error',error:String(e)});'fetch started';'''
        execute(a.window,a.tab,source.replace('KEY',expr).replace('URL',json.dumps(a.url)))
        deadline=time.monotonic()+200
        while True:
            raw=execute(a.window,a.tab,'JSON.stringify((({bytes,...meta})=>meta)(window['+expr+']))')
            meta=json.loads(raw)
            if meta['state']=='error':raise RuntimeError(meta['error'])
            if meta['state']=='ready':break
            if time.monotonic()>deadline:raise TimeoutError('Browser media fetch timed out')
            time.sleep(1)
        if a.expected_bytes is not None and meta['size']!=a.expected_bytes:
            raise ValueError('Media size differs from verified response headers')
        digest=hashlib.sha256()
        with part.open('xb') as out:
            for offset in range(0,meta['size'],262144):
                js='''(()=>{const b=window[KEY].bytes.subarray(START,END);let s='';
                  for(let i=0;i<b.length;i+=8192)s+=String.fromCharCode(...b.subarray(i,i+8192));return btoa(s);})()'''
                encoded=execute(a.window,a.tab,js.replace('KEY',expr).replace('START',str(offset)).replace('END',str(offset+262144)))
                chunk=base64.b64decode(encoded,validate=True)
                if len(chunk)!=min(262144,meta['size']-offset):raise ValueError('Short browser transfer')
                out.write(chunk);digest.update(chunk)
            out.flush();os.fsync(out.fileno())
        os.link(part,target)
        print(json.dumps({'path':str(target),'size':meta['size'],'content_type':meta['type'],'sha256':digest.hexdigest()}))
    finally:
        part.unlink(missing_ok=True)
        try:execute(a.window,a.tab,'delete window['+expr+']; "released"')
        except Exception:pass


if __name__=='__main__':
    main()
