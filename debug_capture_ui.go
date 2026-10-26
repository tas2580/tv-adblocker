package main

const debugCaptureHTML = `
<details class="settings-advanced status-details debug-captures" id="debug-capture-panel" ontoggle="if(this.open)refreshDebugCaptures()"><summary data-i18n="debug.heading"></summary>
<p class="field-hint" data-i18n="debug.intro"></p>
<label><span data-i18n="debug.direction"></span><select id="debug-direction" onchange="$('debug-content').hidden=true;refreshDebugCaptures()"><option value="to-placeholder" data-i18n="debug.toPlaceholder"></option><option value="to-sender" data-i18n="debug.toSender"></option></select></label>
<p id="debug-message" role="status"></p>
<div id="debug-content" hidden>
  <div class="grid"><label><span data-i18n="debug.case"></span><select id="debug-case" onchange="loadDebugCapture(this.value)"></select></label><label><span data-i18n="debug.frame"></span><select id="debug-frame-select" onchange="renderDebugFrame()"></select></label></div>
  <p id="debug-case-values" class="field-hint"></p><img id="debug-frame-image" alt="" class="debug-frame-image"><pre id="debug-frame-values"></pre>
  <div class="status-actions"><button type="button" class="secondary" onclick="refreshDebugCaptures()" data-i18n="debug.refresh"></button><a id="debug-download" class="debug-download" download data-i18n="debug.download"></a><button type="button" class="secondary" onclick="openDebugSettings()" data-i18n="debug.settings"></button></div>
</div>
</details>
<style>
.debug-captures [hidden]{display:none}.debug-frame-image{display:block;max-width:100%;width:100%;height:auto;margin:1rem 0;border:1px solid var(--border);border-radius:8px}.debug-captures pre{white-space:pre-wrap;overflow-wrap:anywhere;font-size:.85rem}.debug-download{display:inline-flex;align-items:center;padding:.58rem .82rem;border:1px solid var(--border);border-radius:8px;background:var(--surface-alt);color:var(--text);text-decoration:none;font-weight:600}
</style>`

const debugCaptureJS = `
let selectedDebugCapture=null;
let debugCaptureRequest=0;
function openDebugSettings(){selectTab('recognition');document.querySelector('[data-panel="recognition"] .settings-advanced').open=true}
async function refreshDebugCaptures(){
  if(!$('debug-capture-panel').open)return;
  const request=++debugCaptureRequest;
  try{
    const all=await api('/api/debug-captures');if(request!==debugCaptureRequest)return;
    const direction=$('debug-direction').value,captures=all.filter(c=>(c.direction||'to-placeholder')===direction),select=$('debug-case'),previous=select.value;
    select.replaceChildren(...captures.map(c=>{const option=document.createElement('option');option.value=c.id;option.textContent=new Date(c.time).toLocaleString(language)+' · '+(c.channel||'–');return option}));
    if(!captures.length){selectedDebugCapture=null;$('debug-content').hidden=true;$('debug-message').textContent=t('debug.empty',{direction:t(direction==='to-sender'?'debug.toSender':'debug.toPlaceholder')});return}
    select.value=captures.some(c=>c.id===previous)?previous:captures[0].id;
    $('debug-message').textContent='';
    if(!selectedDebugCapture||selectedDebugCapture.id!==select.value){$('debug-content').hidden=true;await loadDebugCapture(select.value)}
    else $('debug-content').hidden=false;
  }catch(e){if(request===debugCaptureRequest)$('debug-message').textContent=e.message}
}
async function loadDebugCapture(id){
  if(!id)return;
  const request=++debugCaptureRequest;
  try{
    const c=await api('/api/debug-captures/'+encodeURIComponent(id));if(request!==debugCaptureRequest)return;selectedDebugCapture=c;
    const select=$('debug-frame-select');select.replaceChildren(...c.frames.map((f,i)=>{const option=document.createElement('option');option.value=String(i);option.textContent=t('debug.frameOption',{number:i+1,time:new Date(f.time).toLocaleTimeString(language)});return option}));
    select.value=String(Math.max(0,c.frames.length-1));
    const toSender=c.direction==='to-sender';
    $('debug-case-values').textContent=t('debug.caseValues',{required:toSender?c.present_frames:c.missing_frames,logo:t(toSender?'debug.withLogo':'debug.withoutLogo'),interval:c.interval_seconds,similarity:c.similarity_minimum.toFixed(2),band:c.uncertainty_band.toFixed(2)});
    $('debug-download').href='/api/debug-captures/'+encodeURIComponent(c.id)+'/download';
    renderDebugFrame();
    $('debug-content').hidden=false;$('debug-message').textContent='';
  }catch(e){if(request===debugCaptureRequest){$('debug-message').textContent=e.message;$('debug-content').hidden=true}}
}
function renderDebugFrame(){
  const c=selectedDebugCapture,index=Number($('debug-frame-select').value),f=c?.frames[index];if(!f)return;
  const r=f.result;
  $('debug-frame-image').src='/api/debug-captures/'+encodeURIComponent(c.id)+'/frames/'+index;
  $('debug-frame-image').alt=t('debug.imageAlt',{number:index+1});
  $('debug-frame-values').textContent=t('debug.frameValues',{decision:t('decision.'+r.decision),score:r.score.toFixed(3),threshold:r.threshold.toFixed(3),edge:r.edge_score.toFixed(3),structure:r.structure_score.toFixed(3),coverage:Math.round(r.coverage*100),reference:r.reference_id||'–',misses:f.misses,missingRequired:c.missing_frames,matches:f.matches,presentRequired:c.present_frames,x:r.rect.x,y:r.rect.y,w:r.rect.w,h:r.rect.h,scale:Math.round(r.scale*100)});
  $('debug-frame-values').textContent+=logoColorDiagnosticText(r);
  if(f.detection)$('debug-frame-values').textContent+='\n'+t('detection.summary',{mode:t('detection.'+f.detection.mode),classic:t('detection.state.'+f.detection.classic.state),hybrid:t('detection.state.'+f.detection.hybrid.state),reason:t('detection.reason.'+f.detection.hybrid.reason),remaining:Math.ceil(f.detection.hybrid.remaining_seconds)});
}
`
