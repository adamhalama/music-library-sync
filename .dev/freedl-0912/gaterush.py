#!/usr/bin/env python3
"""Route Gaterush's normal OAuth handler through two owned background Helium tabs.

Uses the gate's own save-comment request and verified provider callback. It never
asserts social completion or changes server gate state itself. Existing GATERUSH
SoundCloud access must be authorized. Sign-in/consent is inspected separately.
"""
import argparse
import json
import time
from helium import execute, execute_main


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('action', choices=['start', 'finish'])
    p.add_argument('--gate-window', type=int, required=True)
    p.add_argument('--gate-tab', type=int, required=True)
    p.add_argument('--auth-window', type=int, required=True)
    p.add_argument('--auth-tab', type=int, required=True)
    a = p.parse_args()
    gate = (a.gate_window, a.gate_tab)
    auth = (a.auth_window, a.auth_tab)
    if gate == auth:
        p.error('Gate and authentication tabs must differ')
    if a.action == 'start':
        execute_main(*gate, '''
          if(location.origin!=='https://gaterush.me'||!window.__GATE__) throw Error('Not a Gaterush gate');
          const button=document.querySelector('.btn-soundcloud[data-go]');
          if(!button || button.disabled) throw Error('SoundCloud step is not current');
          window.udlGate?.stop();
          window._udlAuthBridge={slug:window.__GATE__.slug,requests:[]};
          const original=window.open;
          window.open=function(url){
            let href=String(url||'about:blank');
            const location={get href(){return href},set href(value){href=String(value);window._udlAuthBridge.requests.push(href)}};
            if(href!=='about:blank')window._udlAuthBridge.requests.push(href);
            window._udlAuthPopup={closed:false,location,focus(){},close(){this.closed=true},opener:window};
            return window._udlAuthPopup;
          };
          try { const input=document.querySelector('#commentInput');if(input&&!input.value)input.value='🔥';button.click(); }
          finally { window.open=original; }
          return 'normal gate OAuth handler started';
        ''')
        for _ in range(20):
            bridge = execute_main(*gate, 'return window._udlAuthBridge;')
            if bridge['requests']:
                url = bridge['requests'][-1]
                expected = 'https://gaterush.me/auth/soundcloud?slug='+bridge['slug']
                if url != expected:
                    raise RuntimeError('Unexpected OAuth destination; inspect before navigation')
                execute(*auth, 'location.href='+json.dumps(url)+"; 'background navigation requested'")
                print(json.dumps({'status':'auth-started','slug':bridge['slug']}))
                return
            time.sleep(.5)
        raise RuntimeError('Gate did not produce an OAuth URL; inspect its error message')
    bridge = execute_main(*gate, 'return window._udlAuthBridge;')
    if not bridge:
        raise RuntimeError('No pending gate authorization')
    result = execute(*auth, '''JSON.stringify({origin:location.origin,path:location.pathname,
      title:document.title,body:document.body.innerText,
      scripts:[...document.scripts].map(s=>s.textContent)})''')
    page = json.loads(result)
    slug = bridge['slug']
    success = (page['origin']=='https://gaterush.me' and page['path']=='/callback/soundcloud'
               and page['title']=='Authenticating...' and not page['body'].strip()
               and any('sc-auth' in s and 'sc-auth-error' not in s and json.dumps(slug) in s
                       for s in page['scripts']))
    if not success:
        print(json.dumps({'status':'needs-authentication','title':page['title'],'path':page['path']}))
        return
    execute_main(*gate, 'window._udlAuthPopup?.close();window.postMessage({type:"sc-auth",slug:'+json.dumps(slug)+'},location.origin);return true;')
    print(json.dumps({'status':'verified-callback-delivered','slug':slug}))


if __name__ == '__main__':
    main()
