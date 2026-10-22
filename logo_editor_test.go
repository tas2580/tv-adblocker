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
