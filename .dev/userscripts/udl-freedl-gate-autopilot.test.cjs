const {JSDOM}=require('jsdom');
const fs=require('fs'),assert=require('node:assert/strict');
const path=require('node:path');
const source=fs.readFileSync(path.join(__dirname,'udl-freedl-gate-autopilot.user.js'),'utf8');
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
function page(host,html){
 const dom=new JSDOM(html,{url:`https://${host}/gate/test`,runScripts:'outside-only'}),w=dom.window;
 w.console.log=()=>{};w.UDL_GATE_CONFIG={tickMs:5,minSettleMs:1,budgetMs:3000};
 w.HTMLElement.prototype.getClientRects=function(){return [{}]};
 w.HTMLElement.prototype.getBoundingClientRect=function(){return {left:0,right:100}};
 w.HTMLElement.prototype.scrollIntoView=function(){};
 return {w,dom,start:()=>w.eval(source)};
}
(async()=>{
 // Upcoming slide is still laid out, and must not be clicked.
 {
 const {w,dom,start}=page('hypeddit.com','<button id="downloadProcess">Download</button><div class="email fangate-slider-content current-slide"><input id="email_address"><input id="email_name"><button id="email_to_downloads_next">Share email address</button></div><div class="dw fangate-slider-content upcomming-slide"><a id="gateDownloadButton">Download</a></div>');
 let downloads=0,emails=0;w.document.querySelector('#gateDownloadButton').onclick=()=>downloads++;
 w.document.querySelector('#email_to_downloads_next').onclick=()=>emails++;
 start();await sleep(100);assert.equal(downloads,0);assert.equal(emails,0);assert.equal(w.udlGate.state.status,'needs-email');
 w.udlGate.configure({email:'test@example.invalid',name:'Test'});await sleep(100);
 assert.equal(emails,1);assert.equal(downloads,0);assert.equal(w.document.querySelector('#email_address').value,'test@example.invalid');dom.window.close();
 }
 // Gaterush marks open button done and disables it; pending confirm still waits.
 {
 const {w,dom,start}=page('gaterush.me','<div id="stepStage"><div class="follow-pair"><button data-open data-url="https://example.com/artist">Open</button><button data-confirm disabled>Followed</button></div></div><button id="download" disabled>Download</button>');
 const open=w.document.querySelector('[data-open]'),confirm=w.document.querySelector('[data-confirm]');let confirmed=0;
 open.onclick=()=>{open.classList.add('done');open.disabled=true;confirm.disabled=false};
 confirm.onclick=()=>{confirmed++;confirm.classList.add('done');confirm.disabled=true};
 start();await sleep(100);assert.equal(confirmed,0);assert.equal(w.udlGate.state.status,'needs-follow');
 w.udlGate.confirmFollow('https://example.com/artist','Following observed');await sleep(100);assert.equal(confirmed,1);dom.window.close();
 }
 // List-style button transforms into confirm and must receive a second click only after evidence.
 {
 const {w,dom,start}=page('gaterush.me','<div id="stepStage"><button class="list-btn" data-url="https://example.com/artist">Open</button></div>');
 const btn=w.document.querySelector('.list-btn');let clicks=0;btn.onclick=()=>{clicks++;if(clicks===1)btn.textContent='Done';else{btn.classList.add('done');btn.disabled=true}};
 start();await sleep(100);assert.equal(clicks,1);w.udlGate.confirmFollow('https://example.com/artist','Following observed');await sleep(100);assert.equal(clicks,2);dom.window.close();
 }
 // A verified like cannot satisfy a required repost at the same URL.
 {
 const {w,dom,start}=page('hypeddit.com','<button id="downloadProcess">Download</button><div class="sc fangate-slider-content current-slide"><a class="hype-btn" data-step="like" data-url="https://example.com/track">Like</a><a class="hype-btn" data-step="repost" data-url="https://example.com/track">Repost</a><a class="hype-btn" id="next">Next</a></div>');
 let advanced=0;w.document.querySelector('#next').onclick=()=>advanced++;
 start();await sleep(100);assert.equal(w.udlGate.state.pending.kind,'like');assert.equal(advanced,0);
 w.udlGate.confirmAction('https://example.com/track','like','Unlike visible');await sleep(100);assert.equal(w.udlGate.state.pending.kind,'repost');assert.equal(advanced,0);
 w.udlGate.confirmAction('https://example.com/track','repost','Undo repost visible');await sleep(100);assert.equal(advanced,1);dom.window.close();
 }
 // Entry Download is not a successful download, nor permission to confirm follows.
 {
 const {w,dom,start}=page('mypresskit.info','<button id="entry">Download</button><button id="confirm">I followed</button>');let confirms=0;
 w.document.querySelector('#confirm').onclick=()=>confirms++;start();await sleep(100);assert.notEqual(w.udlGate.state.status,'download-requested');assert.equal(confirms,0);dom.window.close();
 }
 // Reinjection into an already armed Gaterush list must not confirm blindly.
 {
 const {w,dom,start}=page('gaterush.me','<div id="stepStage"><button class="list-btn" data-url="https://example.com/artist" aria-label="Done">Done</button></div>');
 let confirmed=0;const btn=w.document.querySelector('.list-btn');btn.onclick=()=>{confirmed++;btn.classList.add('done');btn.disabled=true};
 start();await sleep(100);assert.equal(confirmed,0);assert.equal(w.udlGate.state.status,'needs-follow');
 w.udlGate.confirmFollow('https://example.com/artist','Following observed');await sleep(100);assert.equal(confirmed,1);dom.window.close();
 }
 // Real Droploud multi-account shape: all evidence required, no foreground popups.
 {
 const {w,dom,start}=page('droploud.com','<div class="dtr-root"><div class="dtr-stage is-vis"><div class="dtr-card-pane dtr-card-pane-step"><h2 class="dtr-card-title">Follow 3 accounts on Instagram</h2><div class="dtr-open-grid"><button>A</button><button>B</button><button>C</button></div><button class="dtr-confirm-btn" disabled>I did it →</button></div></div></div>');
 const urls=['https://instagram.com/a','https://instagram.com/b','https://instagram.com/c'];
 w.__next_f=[[1,'7:'+JSON.stringify({gateData:{steps:[{id:'instagram',type:'instagram_follow',label:'Follow 3 accounts on Instagram',urls}]}})]];
 w.UDL_GATE_CONFIG.backgroundOnly=true;
 let opens=0,confirms=0,armed=0;w.open=()=>{opens++;return {}};
 const confirm=w.document.querySelector('.dtr-confirm-btn');confirm.onclick=()=>confirms++;
 [...w.document.querySelectorAll('.dtr-open-grid button')].forEach((b,i)=>b.onclick=()=>{w.open(urls[i]);if(++armed===3)confirm.disabled=false});
 start();await sleep(100);assert.equal(opens,0);assert.equal(confirms,0);assert.equal(w.udlGate.state.pending.profiles.length,3);assert.equal(w.udlGate.state.openRequests.length,3);
 w.udlGate.confirmFollow(urls[0],'Following');w.udlGate.confirmFollow(urls[1],'Following');await sleep(100);assert.equal(confirms,0);
 w.udlGate.confirmFollow(urls[2],'Following');await sleep(100);assert.equal(confirms,1);assert.equal(opens,0);dom.window.close();
 }
 // MyPressKit's Instagram handler immediately records success: withhold until proof.
 {
 const {w,dom,start}=page('mypresskit.info','<main><div>✓ Follow, Like, Repost &amp; Comment on SoundCloud</div><button id="ig">Follow on Instagram</button><button disabled>Complete all steps to unlock</button></main>');
 const url='https://instagram.com/notpumbaa_/';
 w.__next_f=[[1,JSON.stringify({gate:{id:'45',gateSteps:[{type:'instagram-follow',targetUrl:url}]}})]];
 w.UDL_GATE_CONFIG.backgroundOnly=true;let confirms=0,popups=0;w.open=()=>{popups++;return {}};
 w.document.querySelector('#ig').onclick=()=>{w.open(url);confirms++};
 start();await sleep(100);assert.equal(confirms,0);assert.equal(w.udlGate.state.pending.profile,url);
 w.udlGate.confirmFollow(url,'Following observed');await sleep(100);assert.equal(confirms,1);assert.equal(popups,0);dom.window.close();
 }
 // Combo OAuth reports the native fallback URL, populated comment and exact requirements.
 {
 const {w,dom,start}=page('mypresskit.info','<main><textarea placeholder="Write your comment…"></textarea><button id="combo">Follow, Like, Repost &amp; Comment on SoundCloud</button></main>');
 w.__next_f=[[1,JSON.stringify({gate:{id:'45',trackUrl:'https://soundcloud.com/artist/track',gateSteps:[{type:'soundcloud-comment'}]}})]];
 w.UDL_GATE_CONFIG.backgroundOnly=true;w.UDL_GATE_CONFIG.comment='Nice tune!';let clicks=0;w.document.querySelector('#combo').onclick=()=>clicks++;
 start();await sleep(100);assert.equal(clicks,0);assert.equal(w.udlGate.state.status,'needs-oauth');
 const url=new URL(w.udlGate.state.pending.profile);assert.equal(url.searchParams.get('comment'),'Nice tune!');assert.equal(url.searchParams.get('gate'),'45');assert.equal(url.searchParams.get('combo'),'1');assert.equal(url.searchParams.has('popup'),false);assert.equal(w.document.querySelector('textarea').value,'Nice tune!');dom.window.close();
 }
 // Final enabled Download alone requests the file once.
 {
 const {w,dom,start}=page('mypresskit.info','<main><div>✓ Follow on Instagram</div><button id="download">Download</button></main>');
 let clicks=0;w.document.querySelector('#download').onclick=()=>clicks++;start();await sleep(100);assert.equal(clicks,1);assert.equal(w.udlGate.state.status,'download-requested');dom.window.close();
 }
 // Hidden-tab finite entry fades complete; infinite and exit animations stay untouched.
 {
 const {w,dom,start}=page('gaterush.me','<div class="card" style="opacity:0"><div id="stepStage"><div class="step entering" style="opacity:0"><input id="emailInput"><button data-go>Next</button></div></div></div><div id="unrelated"></div>');
 w.UDL_GATE_CONFIG.backgroundOnly=true;const card=w.document.querySelector('.card'),step=w.document.querySelector('.step');
 let finite=0,infinite=0,unrelated=0,exit=0;
 function animation(target,iterations,opacities,done){return {playState:'running',effect:{target,getComputedTiming:()=>({iterations,endTime:iterations===Infinity?Infinity:500}),getKeyframes:()=>opacities.map(opacity=>({opacity}))},finish(){this.playState='finished';target.style.opacity='1';done()}}}
 w.document.getAnimations=()=>[a,b,c,d,e];
 const a=animation(card,1,[0,1],()=>finite++),b=animation(step,1,[0,1],()=>finite++),c=animation(card,Infinity,[0,1],()=>infinite++),d=animation(w.document.querySelector('#unrelated'),1,[0,1],()=>unrelated++),e=animation(step,1,[1,0],()=>exit++);
 start();await sleep(100);assert.equal(finite,2);assert.equal(infinite,0);assert.equal(unrelated,0);assert.equal(exit,0);assert.equal(w.udlGate.state.status,'needs-email');dom.window.close();
 }
 // Upcoming Hypeddit card remains hidden even when its entry fade is finite.
 {
 const {w,dom,start}=page('hypeddit.com','<button id="downloadProcess">Download</button><div class="fangate-slider-content current-slide"><input id="email_address"></div><div class="fangate-slider-content current-slide upcomming-slide"><button id="gateDownloadButton">Download</button></div>');
 w.UDL_GATE_CONFIG.backgroundOnly=true;let finished=0;
 const future=w.document.querySelector('.upcomming-slide');w.document.getAnimations=()=>[{playState:'running',effect:{target:future,getComputedTiming:()=>({iterations:1,endTime:500}),getKeyframes:()=>[{opacity:0},{opacity:1}]},finish(){finished++}}];
 start();await sleep(100);assert.equal(finished,0);assert.equal(w.udlGate.state.status,'needs-email');dom.window.close();
 }
 console.log('PASS: 12 DOM regression scenarios');
})().catch(e=>{console.error(e);process.exitCode=1});
