package main

const logoEditorHTML = `
<div class="mask-workspace">
  <h3 data-i18n="mask.heading"></h3><p class="field-hint" data-i18n="mask.intro"></p>
  <div class="status-actions"><button type="button" id="capture-start" onclick="startLogoCapture()" data-i18n="capture.start"></button><button type="button" id="capture-cancel" class="secondary" hidden onclick="cancelLogoCapture()" data-i18n="capture.cancel"></button></div>
  <p id="capture-progress" role="status" class="field-hint"></p>
  <div id="mask-editor" hidden>
    <p id="mask-source" class="field-hint"></p>
    <div class="mask-canvases"><div><p data-i18n="mask.overlay"></p><canvas id="mask-canvas" aria-label="Logo mask" role="img"></canvas></div><div><p data-i18n="mask.transparent"></p><canvas id="mask-transparent" class="checkerboard" role="img" aria-label="Masked logo"></canvas></div></div>
    <div class="mask-tools"><label><span data-i18n="mask.tool"></span><select id="mask-tool"><option value="include" data-i18n="mask.include"></option><option value="exclude" data-i18n="mask.exclude"></option></select></label><label><span data-i18n="mask.brush"></span><input id="mask-brush" type="range" min="1" max="30" value="4"></label></div>
    <div class="status-actions"><button type="button" onclick="saveEditedMask()" data-i18n="mask.save"></button><button type="button" class="secondary" onclick="resetEditedMask()" data-i18n="mask.reset"></button><button type="button" class="secondary" onclick="undoMaskStroke()" data-i18n="mask.undo"></button></div>
    <p id="mask-feedback" role="status" class="field-hint"></p>
  </div>
</div>
<style>
.mask-workspace{margin:1.4rem 0;padding:1.2rem;border:1px solid var(--border);border-radius:12px}.mask-workspace [hidden],.logo-diagnostic [hidden]{display:none}.mask-canvases{display:flex;flex-wrap:wrap;gap:1.4rem}.mask-canvases>div{max-width:100%;min-width:0}.mask-canvases canvas{display:block;max-width:100%;width:auto;height:auto;border:1px solid var(--border);touch-action:none;image-rendering:pixelated}.mask-canvases #mask-canvas{cursor:crosshair}.mask-tools{display:flex;flex-wrap:wrap;gap:1rem}.mask-tools label{min-width:160px}.checkerboard{background-color:#ddd;background-image:linear-gradient(45deg,#aaa 25%,transparent 25%),linear-gradient(-45deg,#aaa 25%,transparent 25%),linear-gradient(45deg,transparent 75%,#aaa 75%),linear-gradient(-45deg,transparent 75%,#aaa 75%);background-size:16px 16px;background-position:0 0,0 8px,8px -8px,-8px 0}.diagnostic-images{display:flex;flex-wrap:wrap;gap:1rem;margin:1rem 0}.diagnostic-images img{max-width:100%;min-width:120px;max-height:220px;object-fit:contain;image-rendering:pixelated}.logo-diagnostic pre{white-space:pre-wrap;font-size:.85rem}.logo-diagnostic .status-actions{margin-top:.7rem}
</style>`

const logoDiagnosticHTML = `
<details class="settings-advanced status-details logo-diagnostic" id="logo-diagnostic" ontoggle="if(this.open)refreshLogoDiagnostic()"><summary data-i18n="diagnostic.heading"></summary>
<p class="field-hint" data-i18n="diagnostic.intro"></p><p id="diagnostic-message" role="status"></p>
<div id="diagnostic-content" hidden><pre id="diagnostic-values"></pre><div class="diagnostic-images"><img id="diagnostic-frame" alt=""/><img id="diagnostic-mask" class="checkerboard" alt=""/></div>
<div class="status-actions"><button class="secondary" type="button" onclick="refreshLogoDiagnostic()" data-i18n="diagnostic.refresh"></button><button type="button" onclick="exportLogoExample(true)" data-i18n="diagnostic.exportPresent"></button><button type="button" onclick="exportLogoExample(false)" data-i18n="diagnostic.exportMissing"></button></div></div>
</details>`

const logoEditorJS = `
let maskState=null,maskLoadVersion=0,maskDrawing=false,maskLastPoint=null,maskUndo=[],captureID='',captureTimer=null,diagnosticTime='';
function logoError(e){let key=String(e.message||e).trim();return messages[key]||key}
function maskBytes(encoded){return Uint8Array.from(atob(encoded),c=>c.charCodeAt(0))}
function maskBase64(pixels){let s='';for(let i=0;i<pixels.length;i++)s+=String.fromCharCode(pixels[i]);return btoa(s)}
function maskURL(){return '/api/logo-mask?'+(maskState.capture?'capture='+encodeURIComponent(maskState.capture):'id='+encodeURIComponent(maskState.id))}
function closeLogoMask(){maskLoadVersion++;maskState=null;$('mask-editor').hidden=true}
async function openLogoMask(id='',capture=''){
  let version=++maskLoadVersion;
  try{
    let url='/api/logo-mask?'+(capture?'capture='+encodeURIComponent(capture):'id='+encodeURIComponent(id));
    let data=await api(url),image=new Image();image.src=url+'&view=frame&v='+Date.now();await image.decode();
    if(version!==maskLoadVersion)return;
    maskState={id,capture,w:data.w,h:data.h,pixels:maskBytes(data.pixels),automatic:maskBytes(data.automatic),image};maskUndo=[];
    $('mask-editor').hidden=false;$('mask-feedback').textContent='';$('mask-source').textContent=t(capture?'mask.seriesSource':data.source==='series'?'mask.savedSeriesSource':'mask.singleSource');
    $('mask-canvas').setAttribute('aria-label',t('mask.overlay'));$('mask-transparent').setAttribute('aria-label',t('mask.transparent'));
    renderEditedMask();
  }catch(e){if(version===maskLoadVersion)$('capture-progress').textContent=logoError(e)}
}
function renderEditedMask(){
  if(!maskState)return;
  let s=maskState,scale=Math.max(1,Math.min(6,Math.floor(360/Math.max(s.w,s.h))));
  for(let name of ['mask-canvas','mask-transparent']){let c=$(name);c.width=s.w;c.height=s.h;c.style.width=(s.w*scale)+'px'}
  let c=$('mask-canvas'),ctx=c.getContext('2d');ctx.drawImage(s.image,0,0,s.w,s.h);let overlay=ctx.getImageData(0,0,s.w,s.h);
  let transparent=$('mask-transparent').getContext('2d');transparent.drawImage(s.image,0,0,s.w,s.h);let cut=transparent.getImageData(0,0,s.w,s.h);
  for(let i=0;i<s.pixels.length;i++){let a=s.pixels[i]/255*.65;overlay.data[i*4]*=1-a;overlay.data[i*4+1]=overlay.data[i*4+1]*(1-a)+255*a;overlay.data[i*4+2]*=1-a;cut.data[i*4+3]=s.pixels[i]}
  ctx.putImageData(overlay,0,0);transparent.putImageData(cut,0,0);
}
function maskPointer(e){let b=$('mask-canvas').getBoundingClientRect();return{x:(e.clientX-b.left)*maskState.w/b.width,y:(e.clientY-b.top)*maskState.h/b.height}}
function paintMaskPoint(point){
  let s=maskState,r=Number($('mask-brush').value),value=$('mask-tool').value==='include'?255:0;
  for(let y=Math.max(0,Math.floor(point.y-r));y<Math.min(s.h,Math.ceil(point.y+r));y++)for(let x=Math.max(0,Math.floor(point.x-r));x<Math.min(s.w,Math.ceil(point.x+r));x++)if(Math.hypot(x+.5-point.x,y+.5-point.y)<=r)s.pixels[y*s.w+x]=value;
}
function moveMaskBrush(e){if(!maskDrawing||!maskState)return;let point=maskPointer(e),previous=maskLastPoint||point,steps=Math.max(1,Math.ceil(Math.hypot(point.x-previous.x,point.y-previous.y)));for(let i=1;i<=steps;i++)paintMaskPoint({x:previous.x+(point.x-previous.x)*i/steps,y:previous.y+(point.y-previous.y)*i/steps});maskLastPoint=point;renderEditedMask();$('mask-feedback').textContent=t('mask.unsaved')}
$('mask-canvas').addEventListener('pointerdown',e=>{if(!maskState)return;e.preventDefault();maskDrawing=true;maskLastPoint=null;maskUndo.push(maskState.pixels.slice());if(maskUndo.length>20)maskUndo.shift();$('mask-canvas').setPointerCapture(e.pointerId);moveMaskBrush(e)});
$('mask-canvas').addEventListener('pointermove',moveMaskBrush);
for(let event of ['pointerup','pointercancel','lostpointercapture'])$('mask-canvas').addEventListener(event,()=>{maskDrawing=false;maskLastPoint=null});
function resetEditedMask(){if(!maskState)return;maskUndo.push(maskState.pixels.slice());maskState.pixels=maskState.automatic.slice();renderEditedMask();$('mask-feedback').textContent=t('mask.unsaved')}
function undoMaskStroke(){if(!maskState||!maskUndo.length)return;maskState.pixels=maskUndo.pop();renderEditedMask();$('mask-feedback').textContent=t('mask.unsaved')}
async function saveEditedMask(){
  if(!maskState)return;
  let state=maskState;
  try{
    await api(maskURL(),{method:'PUT',headers:{'content-type':'application/json'},body:JSON.stringify({pixels:maskBase64(state.pixels)})});
    if(state.capture){let ref=await api('/api/logo-capture?id='+encodeURIComponent(state.capture),{method:'POST'});captureID='';selectedReferenceID=ref.id;await openLogoMask(ref.id);await refreshReferences();await status()}
    else await refreshReferences();
    $('mask-feedback').textContent=t('mask.saved');
  }catch(e){$('mask-feedback').textContent=logoError(e)}
}
async function startLogoCapture(){
  if(!rect)return alert(t('alerts.selectArea'));
  try{
    await syncPreviewSession();let c=await api('/api/logo-capture',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(rect)});
    captureID=c.id;closeLogoMask();$('capture-start').disabled=true;$('capture-cancel').hidden=false;
    await pollLogoCapture();captureTimer=setInterval(pollLogoCapture,1000);
  }catch(e){$('capture-progress').textContent=logoError(e)}
}
async function pollLogoCapture(){
  let id=captureID;if(!id)return;
  try{
    let c=await api('/api/logo-capture?id='+encodeURIComponent(id));if(captureID!==id)return;
    if(c.state==='recording'){let seconds=Math.max(0,30-Math.floor((Date.now()-Date.parse(c.started))/1000));$('capture-progress').textContent=t('capture.progress',{seconds,count:c.samples});return}
    clearInterval(captureTimer);captureTimer=null;$('capture-start').disabled=false;$('capture-cancel').hidden=true;
    if(c.state==='ready'){$('capture-progress').textContent=t('capture.ready');await openLogoMask('',id)}else{$('capture-progress').textContent=t(c.error||'capture.notReady');captureID=''}
  }catch(e){if(captureID!==id)return;clearInterval(captureTimer);captureID='';$('capture-start').disabled=false;$('capture-cancel').hidden=true;$('capture-progress').textContent=logoError(e)}
}
async function cancelLogoCapture(){let id=captureID;captureID='';clearInterval(captureTimer);captureTimer=null;$('capture-start').disabled=false;$('capture-cancel').hidden=true;$('capture-progress').textContent='';if(maskState?.capture===id)closeLogoMask();if(id)try{await api('/api/logo-capture?id='+encodeURIComponent(id),{method:'DELETE'})}catch(e){$('capture-progress').textContent=logoError(e)}}
function logoColorDiagnosticText(r){
  let text='';
  if(r.logo_color){const c=r.logo_color;text+='\n'+t('diagnostic.logoColor',{r:Math.round(c.rgb[0]),g:Math.round(c.rgb[1]),b:Math.round(c.rgb[2]),confidence:Math.round(c.confidence*100),samples:c.samples})}
  if(r.uncertainty_reason==='logo_color_low_contrast')text+='\n'+t('diagnostic.logoColorLowContrast');
  if(r.uncertainty_reason==='logo_position_pending')text+='\n'+t('diagnostic.logoPositionPending');
  if(r.rejection_reason)text+='\n'+t('diagnostic.shapeRejection',{reason:t('diagnostic.rejection.'+r.rejection_reason)});
  for(const check of r.position_checks||[]){
    const p=check.result,c=check.contrast;
    text+='\n\n'+t('diagnostic.knownPosition',{decision:t('decision.'+p.decision),x:p.rect.x,y:p.rect.y,w:p.rect.w,h:p.rect.h,scale:Math.round(p.scale*100),score:p.score.toFixed(3),threshold:p.threshold.toFixed(3),edge:p.edge_score.toFixed(3),structure:p.structure_score.toFixed(3),coverage:Math.round(p.coverage*100)});
    if(p.rejection_reason)text+='\n'+t('diagnostic.shapeRejection',{reason:t('diagnostic.rejection.'+p.rejection_reason)});
    if(c)text+='\n'+t('diagnostic.contrastCheck',{hidden:Math.round(c.hidden_coverage*100),visible:Math.round(c.visible_coverage*100),supported:Math.round(c.supported_coverage*100),cells:c.hidden_cells,total:c.total_cells,protection:t(c.protected?'diagnostic.protectionActive':'diagnostic.protectionInactive')});
  }
  return text;
}
async function refreshLogoDiagnostic(){
  try{
    let d=await api('/api/logo-diagnostic');diagnosticTime=d.time;let r=d.result;
    $('diagnostic-message').textContent='';$('diagnostic-content').hidden=false;
    $('diagnostic-values').textContent=t('diagnostic.values',{decision:t('decision.'+r.decision),score:r.score.toFixed(3),edge:r.edge_score.toFixed(3),structure:r.structure_score.toFixed(3),coverage:Math.round(r.coverage*100),reference:r.reference_id||'–',x:r.rect.x,y:r.rect.y,w:r.rect.w,h:r.rect.h,scale:Math.round(r.scale*100),threshold:r.threshold.toFixed(2)});
    $('diagnostic-values').textContent+=logoColorDiagnosticText(r);
    $('diagnostic-frame').alt=t('diagnostic.frame');$('diagnostic-mask').alt=t('mask.transparent');
    $('diagnostic-frame').src='/api/logo-diagnostic?view=frame&time='+encodeURIComponent(d.time);$('diagnostic-mask').src='/api/logo-diagnostic?view=mask&time='+encodeURIComponent(d.time);
  }catch(e){$('diagnostic-content').hidden=true;$('diagnostic-message').textContent=logoError(e)}
}
async function exportLogoExample(present){
  try{let r=await fetch('/api/logo-diagnostic?time='+encodeURIComponent(diagnosticTime),{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({present})});if(!r.ok)throw Error(await r.text());let url=URL.createObjectURL(await r.blob()),a=document.createElement('a');a.href=url;a.download='logo-example.zip';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}catch(e){$('diagnostic-message').textContent=logoError(e)}
}
`
