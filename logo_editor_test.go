package main

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestLogoEditorAssetsAndTranslations(t *testing.T) {
	for _, language := range []string{"de", "en"} {
		data, err := localeFiles.ReadFile("locales/" + language + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var translations map[string]string
		if err = json.Unmarshal(data, &translations); err != nil {
			t.Fatal(err)
		}
		for _, match := range regexp.MustCompile(`data-i18n="([^"]+)"`).FindAllStringSubmatch(logoEditorHTML+logoDiagnosticHTML, -1) {
			if translations[match[1]] == "" {
				t.Errorf("%s missing %s", language, match[1])
			}
		}
	}
	for _, id := range []string{"mask-editor", "logo-diagnostic", "capture-progress"} {
		if strings.Count(indexHTML, `id="`+id+`"`) != 1 {
			t.Errorf("missing or duplicated %s", id)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js unavailable; browser JavaScript checks skipped")
	}
	start, end := strings.Index(indexHTML, "<script>"), strings.Index(indexHTML, "</script>")
	command := exec.Command(node, "--check")
	command.Stdin = strings.NewReader(indexHTML[start+len("<script>") : end])
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("JavaScript syntax: %v\n%s", err, output)
	}
}

func TestMaskEditorBrushUndoAndSave(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js unavailable")
	}
	prelude := `
const assert=require('node:assert/strict');
let elements=new Map(),requests=[];
function $(id){if(!elements.has(id)){let element={style:{},hidden:true,value:'',width:16,height:16,handlers:{},textContent:'',addEventListener(name,fn){this.handlers[name]=fn},setAttribute(){},setPointerCapture(){},getBoundingClientRect(){return{left:0,top:0,width:16,height:16}},getContext(){return{drawImage(){},getImageData:()=>({data:new Uint8ClampedArray(this.width*this.height*4).fill(128)}),putImageData(){}}}};elements.set(id,element)}return elements.get(id)}
const messages={},t=key=>key;
global.Image=class{async decode(){}};
let selectedReferenceID='',rect={x:0,y:0,w:16,h:16};
async function api(path,options={}){requests.push({path,options});if(options.method==='PUT')return{ok:true};return{w:16,h:16,pixels:Buffer.alloc(256).toString('base64'),automatic:Buffer.alloc(256,128).toString('base64')}}
async function refreshReferences(){}async function status(){}async function syncPreviewSession(){}
`
	checks := `
(async()=>{
 await openLogoMask('test');assert.equal($('mask-editor').hidden,false);assert.equal(maskState.pixels.length,256);
 $('mask-brush').value='2';$('mask-tool').value='include';
 $('mask-canvas').handlers.pointerdown({clientX:8,clientY:8,pointerId:1,preventDefault(){}});
 $('mask-canvas').handlers.pointerup();assert(maskState.pixels.some(v=>v===255));
 undoMaskStroke();assert(maskState.pixels.every(v=>v===0));
 resetEditedMask();assert(maskState.pixels.every(v=>v===128));
 $('mask-tool').value='exclude';$('mask-canvas').handlers.pointerdown({clientX:8,clientY:8,pointerId:2,preventDefault(){}});$('mask-canvas').handlers.pointerup();assert(maskState.pixels.some(v=>v===0));
 await saveEditedMask();let saved=requests.find(r=>r.options.method==='PUT');assert(saved);assert.equal(Buffer.from(JSON.parse(saved.options.body).pixels,'base64').length,256);assert.equal($('mask-feedback').textContent,'mask.saved');
 closeLogoMask();assert.equal(maskState,null);assert.equal($('mask-editor').hidden,true);
})().catch(error=>{console.error(error);process.exitCode=1});`
	command := exec.Command(node)
	command.Stdin = strings.NewReader(prelude + logoEditorJS + checks)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("mask editor interaction: %v\n%s", err, output)
	}
}

func TestLogoContrastDiagnosticRendering(t *testing.T) {
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
function t(key,values={}){assert(messages[key],'missing translation: '+key);return messages[key].replace(/\{(\w+)\}/g,(_,name)=>{assert(name in values,'missing value: '+name);return String(values[name])})}
const elements=new Map();function $(id){if(!elements.has(id))elements.set(id,{value:'0',textContent:'',hidden:false,addEventListener(){}});return elements.get(id)}
`
			checks := `
const shape={decision:'missing',rect:{x:264,y:60,w:86,h:35},scale:1,score:0,threshold:.5,edge_score:.493,structure_score:0,coverage:.419,rejection_reason:'insufficient_contour_coverage'};
const result={...shape,decision:'uncertain',uncertainty_reason:'logo_color_low_contrast',logo_color:{rgb:[224,225,219],confidence:.93,samples:8},position_checks:[{result:shape,contrast:{hidden_coverage:.6,visible_coverage:.38,supported_coverage:.98,hidden_cells:13,total_cells:16,protected:true}}]};
let text=logoColorDiagnosticText(result);assert(text.includes('264, 60'));assert(text.includes('60'));assert(text.includes('98'));assert(text.includes('13/16'));assert(text.includes(t('diagnostic.rejection.insufficient_contour_coverage')));assert(text.includes(t('diagnostic.protectionActive')));assert(!text.includes('undefined'));
selectedDebugCapture={id:'case',missing_frames:3,frames:[{time:'2026-10-25T21:38:58Z',result,misses:0,matches:0}]};renderDebugFrame();assert($('debug-frame-values').textContent.includes(text));
// Old recordings have neither position checks nor contrast details.
const legacy={...shape};delete legacy.rejection_reason;assert.equal(logoColorDiagnosticText(legacy),'');
result.position_checks[0].contrast.protected=false;assert(logoColorDiagnosticText(result).includes(t('diagnostic.protectionInactive')));
for(const reason of ['outside_frame','insufficient_features','insufficient_edge_energy','insufficient_spatial_support','similarity_below_threshold','similarity_uncertainty_band']){assert(logoColorDiagnosticText({...legacy,rejection_reason:reason}).includes(t('diagnostic.rejection.'+reason)))}
`
			command := exec.Command(node)
			command.Stdin = strings.NewReader(prelude + logoEditorJS + debugCaptureJS + checks)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("diagnostic rendering: %v\n%s", err, output)
			}
		})
	}
}
