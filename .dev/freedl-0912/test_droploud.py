#!/usr/bin/env python3
"""Regressions for delayed native responses and signed download URL validation."""
import json
import subprocess
import tempfile
import unittest

import droploud


class DroploudTests(unittest.TestCase):
    def test_requires_https_native_media_host(self):
        for value in ('http://test.r2.cloudflarestorage.com/a',
                      'https://evil.example/a',
                      'https://test.r2.cloudflarestorage.com@evil.example/a'):
            with self.subTest(value=value), self.assertRaises(ValueError):
                droploud.validate_media_url(value)
        valid = 'https://media.r2.cloudflarestorage.com/native.wav?X-Amz-Signature=example'
        self.assertEqual(droploud.validate_media_url(valid), valid)

    def test_late_native_response_cannot_escape_to_browser_ui(self):
        setup = r'''
const assert=require('node:assert/strict');
let escaped=0,navigated=0;
global.window=global;
global.location={origin:'https://droploud.com',pathname:'/track/test'};
global.HTMLAnchorElement=class{click(){escaped++}};
const original=HTMLAnchorElement.prototype.click;
const button={innerText:'Download',disabled:false,click(){
  setTimeout(()=>{
    const a=new HTMLAnchorElement();a.download='YOKAI';
    a.href='https://media.r2.cloudflarestorage.com/test.wav';a.click();
    window.setTimeout(()=>{document.body.removeChild(a);navigated++;const path='/success';},150);
  },40);
}};
global.document={querySelector(){return {innerText:'Drop unlocked!',querySelectorAll(){return [button]}}},
  body:{removeChild(){}}};
'''
        capture = droploud.CAPTURE.replace('GATE_PATH', json.dumps('/track/test')).replace('KEY', json.dumps('_capture'))
        assertions = r'''
assert.equal(window._udlDroploudCapturePending.status,'pending');
// A Python polling timeout must leave pending interception installed.
const state=window._capture;
if(state?.status==='captured'){state.restore();delete window._capture;}
assert.notEqual(HTMLAnchorElement.prototype.click,original);
assert.throws(()=>{CAPTURE_AGAIN},/Previous native download request is still pending/);
setTimeout(()=>{
  assert.equal(window._capture.status,'captured');
  assert.equal(escaped,0);assert.equal(navigated,0);
  assert.equal(HTMLAnchorElement.prototype.click,original);
  assert.equal(window._udlDroploudCapturePending,undefined);
},80);
'''.replace('CAPTURE_AGAIN', capture)
        with tempfile.NamedTemporaryFile('w', suffix='.cjs') as script:
            script.write(setup + '\n(()=>{' + capture + '})();\n' + assertions)
            script.flush()
            subprocess.run(['node', script.name], check=True, timeout=5)


if __name__ == '__main__':
    unittest.main()
