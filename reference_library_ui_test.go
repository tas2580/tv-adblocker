package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestReferenceLibraryAvailabilityAndLiveUpdates(t *testing.T) {
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
const assert=require('node:assert/strict'),messages=` + string(messages) + `;
function t(key,values={}){return (messages[key]||key).replace(/\{(\w+)\}/g,(_,name)=>String(values[name]??''))}
class Element{
  constructor(tag){this.tag=tag;this.children=[];this.style={};this.dataset={};this.attributes={};this.parent=null;this.value=''}
  set textContent(value){this.value=String(value);this.children=[]}get textContent(){return this.value+this.children.map(child=>child.textContent).join('')}
  set innerHTML(value){throw Error('Reference data must not be interpreted as HTML')}
  append(...children){for(const child of children){child.remove();child.parent=this;this.children.push(child)}}
  prepend(child){child.remove();child.parent=this;this.children.unshift(child)}
  replaceChildren(...children){for(const child of this.children)child.parent=null;this.children=[];this.value='';this.append(...children)}
  remove(){if(this.parent){const siblings=this.parent.children;siblings.splice(siblings.indexOf(this),1);this.parent=null}}
  setAttribute(key,value){this.attributes[key]=value}getAttribute(key){return key==='src'?(this.src||null):(this.attributes[key]||null)}
}
const document={createElement:tag=>new Element(tag)},box=new Element('div'),$=()=>box;
let currentActiveURL='test://one',selectedReferenceID='',closed=false,nextResponse=null;
const selected=[],requests=[],confirm=()=>true,alert=message=>{throw Error(message)};
function selectReference(ref){selected.push(ref.id);selectedReferenceID=ref.id}function closeLogoMask(){closed=true}async function status(){}
const good={image:'available',source:'series',mask:'available',automatic_mask:'available',shape_usable:true,color:{state:'ready',samples:8,minimum_samples:6,estimate:{rgb:[253,255,255],confidence:.9,samples:8}}};
const legacy={...structuredClone(good),source:'unknown',mask:'generated',automatic_mask:'missing',color:{state:'inactive',samples:0,minimum_samples:6}};
let fixtures=[{id:'ready',created_at:'<img onerror=unsafe>',stream_name:'Test',data:good},{id:'legacy',data:legacy},{id:'damaged',data:{...structuredClone(good),image:'missing',shape_usable:false,color:{state:'unavailable'}}},{id:'old-payload'}];
async function api(path,options={}){
  requests.push({path,options});
  if(options.method==='DELETE'){fixtures=fixtures.filter(ref=>!path.endsWith('/'+encodeURIComponent(ref.id)));return{ok:true}}
  if(nextResponse){const pending=nextResponse;nextResponse=null;return await pending}
  return structuredClone(fixtures);
}
`
			checks := `
(async()=>{
 await refreshReferences();assert.equal(referenceCards.size,4);
 const ready=referenceCards.get('ready'),old=referenceCards.get('legacy');
 assert(ready.data.textContent.includes('253, 255, 255'));assert(ready.data.textContent.includes('90'));assert(ready.data.textContent.includes(t('logos.shapeUsable')));
 assert.equal(ready.label.textContent,'<img onerror=unsafe>');
 assert(old.data.textContent.includes(t('logos.data.generated')));assert(old.data.textContent.includes(t('logos.saveMaskHint')));assert(old.data.textContent.includes(t('logos.color.inactive')));
 assert(referenceCards.get('damaged').data.textContent.includes(t('logos.retakeImage')));
 assert.equal(referenceCards.get('old-payload').data.textContent,t('logos.dataUnknown'));
 ready.open.onclick();assert.deepEqual(selected,['ready']);
 fixtures[1].data.color={state:'learning',samples:3,minimum_samples:6};await refreshReferences();
 assert.equal(referenceCards.get('ready').open,ready.open,'refresh replaced the focused preview button');
 assert.equal(referenceCards.get('ready').remove,ready.remove,'refresh replaced the delete button');
 assert(old.data.textContent.includes(t('logos.colorProgress',{samples:3,minimum:6})));
 fixtures[1].data.color.samples=8;await refreshReferences();assert(old.data.textContent.includes(t('logos.colorMoreBackgrounds',{samples:8})));
 fixtures[1].data.mask='invalid';await refreshReferences();assert(old.data.textContent.includes(t('logos.retakeMask')));
 fixtures[1].data.mask='available';fixtures[1].data.shape_usable=false;await refreshReferences();assert(old.data.textContent.includes(t('logos.retakeShape')));
 await ready.remove.onclick();assert(!referenceCards.has('ready'));assert(closed);assert.equal(selectedReferenceID,'');
 assert(requests.some(request=>request.path==='/api/references/ready'&&request.options.method==='DELETE'));
 let resolveOld;nextResponse=new Promise(resolve=>resolveOld=resolve);const outdated=refreshReferences();
 currentActiveURL='test://two';fixtures=[{id:'new-channel',data:good}];await refreshReferences();
 resolveOld([{id:'wrong-channel',data:good}]);await outdated;
 assert(referenceCards.has('new-channel'));assert(!referenceCards.has('wrong-channel'));
 fixtures=[];await refreshReferences();assert.equal(referenceCards.size,0);assert.equal(box.textContent,t('logos.noReferences'));
})().catch(error=>{console.error(error);process.exitCode=1});`
			command := exec.Command(node)
			command.Stdin = strings.NewReader(prelude + referenceLibraryJS + checks)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("reference library interaction: %v\n%s", err, output)
			}
		})
	}
}
