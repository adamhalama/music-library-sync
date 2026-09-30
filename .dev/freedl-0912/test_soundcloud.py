#!/usr/bin/env python3
"""Offline SoundCloud action guards; jsdom uses the userscript test dependency.

Run: NODE_PATH=/tmp/udl-userscript-tests/node_modules python3 test_soundcloud.py
No browser is launched or contacted.
"""
import contextlib
import io
import json
import subprocess
import sys
import unittest
from unittest.mock import patch

import soundcloud

FIXTURE = '''
<a class="header__userNavUsernameButton" data-test-id="user-nav-btn" href="/test-account"></a>
<div class="userInfoBar"><button class="sc-button-follow">Follow</button></div>
<li class="soundList__item" id="other"><a class="soundTitle__title" href="/artist/other">Other</a>
<button class="sc-button-like">Like</button></li>
<li class="soundList__item" id="exact"><a class="soundTitle__title" href="/artist/exact">Exact</a>
<button class="sc-button-like">Like</button><button class="sc-button-repost">Repost</button>
<input class="commentForm__input"><button class="commentForm__submitButton">Send</button>
<span class="sc-ministats-comments"><span aria-hidden="true">20</span></span></li>
'''
HARNESS = r'''
const{JSDOM}=require('jsdom');const fs=require('node:fs');
const args=JSON.parse(fs.readFileSync(0,'utf8'));
const d=new JSDOM(args.html,{url:'https://soundcloud.com/artist',runScripts:'outside-only'});
const w=d.window;w.Element.prototype.getClientRects=function(){return[1]};
const events=[];w.Element.prototype.scrollIntoView=function(){events.push('scroll')};
for(const b of w.document.querySelectorAll('button'))b.addEventListener('click',()=>events.push(b.closest('li')?.id+':'+b.className));
const input=w.document.querySelector('#exact input');
input.addEventListener('focus',()=>events.push('focus'));
input.addEventListener('input',()=>events.push('input:'+input.value));
input.addEventListener('keyup',e=>events.push('keyup:'+e.key));
let result,error;try{result=w.eval('(function(){'+args.body+'})()')}catch(e){error=e.message}
console.log(JSON.stringify({result,error,events,input:input.value}));
'''


class SoundCloudGuards(unittest.TestCase):
    def evaluate(self, action, html=FIXTURE):
        cfg={'profile':'/artist','track':'/artist/exact','account':'/test-account',
             'comment':'🔥','action':action}
        body=soundcloud.COMMON.replace('CONFIG',json.dumps(cfg))+soundcloud.ACTION
        proc=subprocess.run(['node','-e',HARNESS],input=json.dumps({'html':html,'body':body}),
                            text=True,capture_output=True,check=True)
        return json.loads(proc.stdout)

    def test_inspect_does_not_click_focus_or_scroll(self):
        r=self.evaluate('inspect')
        self.assertEqual(r['events'],[])
        self.assertEqual(r['result']['matchingCards'],1)
        self.assertEqual(r['result']['commentCount'],'20')

    def test_like_targets_only_exact_card(self):
        r=self.evaluate('like')
        self.assertEqual(r['events'],['exact:sc-button-like'])
        self.assertTrue(r['result']['clicked'])

    def test_duplicate_card_refuses_action(self):
        r=self.evaluate('like',FIXTURE+'<li class="soundList__item"><a class="soundTitle__title" href="/artist/exact">Duplicate</a></li>')
        self.assertIn('ambiguous',r['error'])
        self.assertEqual(r['events'],[])

    def test_existing_own_comment_prevents_duplicate(self):
        comment='<div class="commentPopover__wrapper"><a class="commentPopover__username" href="/test-account">Me</a><p class="commentPopover__body">🔥</p></div>'
        r=self.evaluate('comment',FIXTURE.replace('</li>\n',comment+'</li>\n'))
        self.assertTrue(r['result']['already'])
        self.assertFalse(r['result']['clicked'])
        self.assertEqual(r['events'],[])

    def test_own_comment_on_other_track_does_not_suppress_action(self):
        comment='<div class="commentPopover__wrapper"><a class="commentPopover__username" href="/test-account">Me</a><p class="commentPopover__body">🔥</p></div>'
        r=self.evaluate('comment',FIXTURE.replace('</li>',comment+'</li>',1))
        self.assertTrue(r['result']['submittedOnce'])
        self.assertEqual(r['events'][-1],'exact:commentForm__submitButton')

    def test_comment_focuses_fills_and_submits_once(self):
        r=self.evaluate('comment')
        self.assertEqual(r['events'],['scroll','focus','input:🔥','keyup:🔥','exact:commentForm__submitButton'])
        self.assertTrue(r['result']['submittedOnce'])
        self.assertEqual(r['input'],'🔥')

    def test_comment_preserves_user_draft(self):
        r=self.evaluate('comment',FIXTURE.replace('class="commentForm__input"','class="commentForm__input" value="draft"'))
        self.assertIn('preserve user draft',r['error'])
        self.assertEqual(r['input'],'draft')
        self.assertEqual(r['events'],[])

    def test_comment_requires_focus_optin_before_browser_call(self):
        argv=['soundcloud.py','--window','1','--tab','2','--profile-url','https://soundcloud.com/artist',
              '--track-url','https://soundcloud.com/artist/exact','--account-url','https://soundcloud.com/test-account',
              '--action','comment']
        with patch.object(sys,'argv',argv),patch.object(soundcloud,'execute_main') as browser,contextlib.redirect_stderr(io.StringIO()) as err:
            with self.assertRaises(SystemExit) as raised:soundcloud.main()
        self.assertEqual(raised.exception.code,1)
        self.assertIn('--allow-focus',err.getvalue())
        browser.assert_not_called()

    def test_reject_noncanonical_or_sensitive_urls(self):
        for url in ['https://soundcloud.com@evil.test/user','https://soundcloud.com/user?token=x',
                    'https://soundcloud.com/search','https://soundcloud.com//user',
                    'https://soundcloud.com/user/../other','http://soundcloud.com/user']:
            with self.subTest(url=url),self.assertRaises(ValueError):soundcloud.soundcloud_url(url,1)


if __name__=='__main__':
    unittest.main()
