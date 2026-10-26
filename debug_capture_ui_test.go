package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDebugCaptureDirectionSelection(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js unavailable")
	}
	for _, language := range []string{"de", "en"} {
		t.Run(language, func(t *testing.T) {
			messages, err := localeFiles.ReadFile("locales/" + language + ".json")
			if err != nil {
				t.Fatal(err)
			}
			prelude := `
const assert=require('node:assert/strict'),messages=` + string(messages) + `,language='` + language + `';
function t(key,values={}){assert(messages[key],'missing translation: '+key);return messages[key].replace(/\{(\w+)\}/g,(_,name)=>{assert(name in values,'missing value: '+name);return String(values[name])})}
class Element{
  constructor(){this.value='';this.textContent='';this.hidden=false;this.children=[]}
  replaceChildren(...children){this.children=children;this.value=children[0]?.value||''}
}
const elements=new Map(),$=id=>{assert(elements.has(id),'missing element: '+id);return elements.get(id)};
for(const match of ` + "`" + debugCaptureHTML + "`" + `.matchAll(/id="([^"]+)"/g))elements.set(match[1],new Element());
const document={createElement:()=>new Element()},logoColorDiagnosticText=()=>'';
$('debug-capture-panel').open=true;$('debug-direction').value='to-placeholder';
const frame={time:'2026-10-26T12:00:00Z',misses:0,matches:3,result:{decision:'present',score:.95,threshold:.8,edge_score:.9,structure_score:.9,coverage:1,reference_id:'ref',rect:{x:2,y:3,w:48,h:36},scale:1}};
const base={time:frame.time,channel:'<b>Test</b>',interval_seconds:2,missing_frames:2,present_frames:3,similarity_minimum:.8,uncertainty_band:.03,frames:[frame]};
let fixtures=[{...base,id:'return',direction:'to-sender'},{...base,id:'advert',direction:'to-placeholder'},{...base,id:'legacy'}],pending=null,requests=[];
async function api(path){
  requests.push(path);
  if(pending){const response=pending;pending=null;return await response}
  if(path==='/api/debug-captures')return structuredClone(fixtures);
  const capture=fixtures.find(c=>path==='/api/debug-captures/'+c.id);assert(capture,'unknown capture: '+path);return structuredClone(capture);
}
`
			checks := `
(async()=>{
  await refreshDebugCaptures();
  assert.deepEqual($('debug-case').children.map(c=>c.value),['advert','legacy']);
  assert.equal(selectedDebugCapture.id,'advert');assert.equal($('debug-content').hidden,false);
  assert($('debug-case').children[0].textContent.includes('<b>Test</b>'),'channel label should stay plain text');
  assert($('debug-case-values').textContent.includes('2'));assert($('debug-case-values').textContent.includes(t('debug.withoutLogo')));
  $('debug-case').value='legacy';await loadDebugCapture('legacy');
  await refreshDebugCaptures();assert.equal($('debug-case').value,'legacy','refresh changed selected case');
  $('debug-direction').value='to-sender';await refreshDebugCaptures();
  assert.deepEqual($('debug-case').children.map(c=>c.value),['return']);assert.equal(selectedDebugCapture.id,'return');
  assert($('debug-case-values').textContent.includes('3'));assert($('debug-case-values').textContent.includes(t('debug.withLogo')));
  assert($('debug-frame-values').textContent.includes('3/3'),'present frame requirement missing');
  assert($('debug-frame-values').textContent.includes('0/2'),'missing frame requirement missing');
  assert.equal($('debug-download').href,'/api/debug-captures/return/download');
  assert.equal($('debug-frame-image').src,'/api/debug-captures/return/frames/0');
  fixtures=fixtures.filter(c=>c.id!=='return');await refreshDebugCaptures();
  assert.equal(selectedDebugCapture,null);assert.equal($('debug-content').hidden,true);
  assert.equal($('debug-direction').hidden,false);assert.equal($('debug-case').children.length,0);
  assert.equal($('debug-message').textContent,t('debug.empty',{direction:t('debug.toSender')}));
  $('debug-direction').value='to-placeholder';await refreshDebugCaptures();
  assert.equal($('debug-content').hidden,false);assert.equal(selectedDebugCapture.id,'advert');
  let resolveOld;pending=new Promise(resolve=>resolveOld=resolve);
  const oldSelection=loadDebugCapture('legacy');
  $('debug-direction').value='to-sender';await refreshDebugCaptures();
  resolveOld({...base,id:'legacy'});await oldSelection;
  assert.equal(selectedDebugCapture,null,'outdated response replaced the new selection');assert.equal($('debug-content').hidden,true);
  fixtures=[{...base,id:'new-return',direction:'to-sender'}];await refreshDebugCaptures();
  assert.equal(selectedDebugCapture.id,'new-return');assert.equal($('debug-message').textContent,'');
  assert.equal($('debug-content').hidden,false);
})().catch(error=>{console.error(error);process.exitCode=1});`
			command := exec.Command(node)
			command.Stdin = strings.NewReader(prelude + debugCaptureJS + checks)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("debug capture selection: %v\n%s", err, output)
			}
		})
	}
}
