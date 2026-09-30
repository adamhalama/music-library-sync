#!/usr/bin/env python3
"""Inspect an exact SoundCloud profile/card in an existing background Helium tab.

No navigation or clicks by default. --action explicitly authorizes one action;
a timed-out or ambiguous write is never retried. Comment verification requires
the exact signed-in account permalink and an own visible matching comment.
--allow-focus is required for comments: that action scrolls/focuses the tab
input, so use it only during an explicitly permitted browser interaction slot.
"""
import argparse
import json
import re
import time
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlsplit

from helium import execute_main

RESERVED = {'discover', 'stream', 'you', 'search', 'settings', 'upload', 'terms',
            'pages', 'signin', 'login', 'logout', 'connect', 'oauth', 'api'}


def soundcloud_url(value, segments):
    p = urlsplit(value)
    if p.scheme != 'https' or p.netloc != 'soundcloud.com' or p.query or p.fragment:
        raise ValueError('Expected an exact HTTPS soundcloud.com URL without query or fragment')
    if not re.fullmatch(r'/[A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)?/?', p.path):
        raise ValueError('Expected a canonical public SoundCloud path')
    parts = p.path.strip('/').split('/')
    if len(parts) != segments or any(not re.fullmatch(r'[A-Za-z0-9_-]+', s) for s in parts):
        raise ValueError('Expected a public SoundCloud profile or track path')
    if parts[0].lower() in RESERVED:
        raise ValueError('Not a public SoundCloud profile')
    return '/' + '/'.join(parts)


COMMON = r'''
const cfg=CONFIG;
const path=u=>{try{return new URL(u,location.href).pathname.replace(/\/$/,'')}catch{return ''}};
const visible=e=>!!e&&!!e.getClientRects().length&&getComputedStyle(e).visibility!=='hidden';
if(location.hostname!=='soundcloud.com'||path(location.href)!==cfg.profile)
  throw Error('Existing tab must be at the exact profile URL');
if(document.querySelector('input[type="password"]') ||
 [...document.querySelectorAll('[role="dialog"]')].some(e=>visible(e)&&/sign in|log in/i.test(e.innerText)))
 throw Error('Login dialog requires attention');
const header=document.querySelector('.userInfoBar');
const followButtons=header?[...header.querySelectorAll('button.sc-button-follow')].filter(visible):[];
const following=b=>!!b&&(b.classList.contains('sc-button-selected')||b.getAttribute('aria-pressed')==='true'||/^following$/i.test(b.textContent.trim()));
const matches=cfg.track?[...document.querySelectorAll('a.soundTitle__title')].filter(a=>path(a.href)===cfg.track):[];
const cards=[...new Set(matches.map(a=>a.closest('.soundList__item')).filter(Boolean))];
const card=cards.length===1?cards[0]:null;
const control=s=>card?[...card.querySelectorAll(s)].filter(visible):[];
const selected=b=>!!b&&(b.classList.contains('sc-button-selected')||b.getAttribute('aria-pressed')==='true');
const likes=control('button.sc-button-like'),reposts=control('button.sc-button-repost');
const inputs=control('input.commentForm__input, textarea.commentForm__input');
const input=inputs.length===1?inputs[0]:null;
const ownComment=()=>{
 if(!cfg.account||!cfg.comment)return false;
 return [...(card||document).querySelectorAll('.commentPopover__wrapper, .commentItem')].some(c=>
  [...c.querySelectorAll('a.commentPopover__username, a.commentItem__usernameLink')].some(a=>path(a.href)===cfg.account)&&
  [...c.querySelectorAll('.commentPopover__body, .commentItem__body')].some(b=>b.textContent.trim()===cfg.comment));
};
const count=()=>{const e=card?.querySelector('.sc-ministats-comments span[aria-hidden="true"]');return e?e.textContent.trim():null};
const snapshot=()=>({profile:cfg.profile,track:cfg.track,headerFound:!!header,
 followControls:followButtons.length,following:followButtons.length===1&&following(followButtons[0]),
 matchingCards:cards.length,likeControls:likes.length,liked:likes.length===1&&selected(likes[0]),
 repostControls:reposts.length,reposted:reposts.length===1&&selected(reposts[0]),
 commentInputs:inputs.length,commentInputEmpty:!!input&&!input.value,commentCount:count(),
 ownCommentVisible:ownComment()});
'''

ACTION = r'''
const before=snapshot();
if(cfg.action==='inspect')return before;
const signedIn=[...document.querySelectorAll('a.header__userNavUsernameButton[data-test-id="user-nav-btn"]')];
if(signedIn.length!==1)throw Error('Signed-in account header unavailable');
const requireOne=(items,what)=>{if(items.length!==1)throw Error(what+' control missing or ambiguous');
 const e=items[0];if(e.disabled||e.getAttribute('aria-disabled')==='true')throw Error(what+' disabled');return e};
if(cfg.action==='follow'){
 const b=requireOne(followButtons,'Follow');
 if(following(b))return {before,already:true,clicked:false};
 if(!/^follow$/i.test(b.textContent.trim()))throw Error('Unexpected follow label');
 b.click();return {before,clicked:true};
}
if(cards.length!==1)throw Error('Exact track card missing or ambiguous');
if(cfg.action==='like'||cfg.action==='repost'){
 const b=requireOne(cfg.action==='like'?likes:reposts,cfg.action);
 if(selected(b))return {before,already:true,clicked:false};
 b.click();return {before,clicked:true};
}
if(!cfg.account||!cfg.comment)throw Error('Comment needs exact account and text');
if(ownComment())return {before,already:true,clicked:false};
if(!input)throw Error('Comment input missing or ambiguous');
if(input.value)throw Error('Comment input already contains text; preserve user draft');
const accountLinks=[...document.querySelectorAll('a.header__userNavUsernameButton[data-test-id="user-nav-btn"]')];
if(accountLinks.length!==1||path(accountLinks[0].href)!==cfg.account)
 throw Error('Signed-in account permalink does not match');
input.scrollIntoView({block:'center'});input.focus();
if(document.activeElement!==input)throw Error('Comment input did not receive focus');
const proto=input.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;
Object.getOwnPropertyDescriptor(proto,'value').set.call(input,cfg.comment);
input.dispatchEvent(new Event('input',{bubbles:true}));
input.dispatchEvent(new KeyboardEvent('keyup',{bubbles:true,key:cfg.comment}));
const submit=requireOne(control('.commentForm__submitButton'),'Comment submit');
submit.click();
return {before,clicked:true,submittedOnce:true};
'''


def run(window, tab, cfg, action):
    options = dict(cfg, action=action)
    body = COMMON.replace('CONFIG', json.dumps(options)) + (ACTION if action != 'snapshot' else 'return snapshot();')
    return execute_main(window, tab, body)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--window', type=int, required=True)
    parser.add_argument('--tab', type=int, required=True)
    parser.add_argument('--profile-url', required=True)
    parser.add_argument('--track-url')
    parser.add_argument('--action', choices=['inspect', 'follow', 'like', 'repost', 'comment'], default='inspect')
    parser.add_argument('--account-url', help='Exact signed-in SoundCloud profile URL (required for comment)')
    parser.add_argument('--comment', default='🔥')
    parser.add_argument('--allow-focus', action='store_true', help='Permit comment input scrolling/focus; requires an authorized interaction slot')
    parser.add_argument('--evidence', type=Path, help='New evidence file outside repository')
    args = parser.parse_args()
    try:
        if min(args.window,args.tab)<=0:
            raise ValueError('Window and tab IDs must be positive')
        cfg={'profile':soundcloud_url(args.profile_url,1),
             'track':soundcloud_url(args.track_url,2) if args.track_url else None,
             'account':soundcloud_url(args.account_url,1) if args.account_url else None,
             'comment':args.comment}
        if cfg['track'] and cfg['track'].split('/')[1]!=cfg['profile'].strip('/'):
            raise ValueError('Track must belong to exact profile')
        if args.action in {'like','repost','comment'} and not cfg['track']:
            raise ValueError('Track URL required for track actions')
        if args.action=='comment' and not args.allow_focus:
            raise ValueError('Comment requires --allow-focus during an authorized interaction slot')
        if args.action=='comment' and (not cfg['account'] or not args.comment.strip()):
            raise ValueError('Comment requires exact account URL and nonempty text')
        evidence=args.evidence.expanduser().resolve() if args.evidence else None
        if evidence and (evidence.exists() or evidence.is_relative_to(Path(__file__).resolve().parents[2])):
            raise ValueError('Evidence must be a new file outside the repository')
        result=run(args.window,args.tab,cfg,args.action)
        if args.action=='inspect':
            report={'action':'inspect','state':result}
        else:
            before=result['before']; last=before; verified=bool(result.get('already'))
            deadline=time.monotonic()+15
            while not verified and time.monotonic()<deadline:
                time.sleep(.75)
                last=run(args.window,args.tab,cfg,'snapshot')
                field={'follow':'following','like':'liked','repost':'reposted','comment':'ownCommentVisible'}[args.action]
                verified=last[field]
                if args.action=='comment':
                    verified=verified and last['commentInputEmpty'] and last['commentCount']!=before['commentCount']
            report={'action':args.action,'before':before,'after':last,'clicked':result.get('clicked',False),
                    'verified':verified,'checked_at':datetime.now(timezone.utc).isoformat()}
            if not verified:
                report['status']='ambiguous; inspect before any further action, write not retried'
        if evidence:
            with evidence.open('x',encoding='utf-8') as f:
                json.dump(report,f,indent=2);f.write('\n')
        print(json.dumps(report))
        if args.action!='inspect' and not report['verified']:
            raise SystemExit(2)
    except (ValueError,RuntimeError,json.JSONDecodeError) as error:
        parser.exit(1,f'soundcloud: {error}\n')


if __name__=='__main__':
    main()
