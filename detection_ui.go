package main

const detectionSettingsHTML = `
<details class="settings-advanced" id="detection-settings"><summary data-i18n="detection.heading"></summary>
<label><span data-i18n="detection.mode"></span><select id="detection_mode"><option value="classic" data-i18n="detection.classic"></option><option value="compare" data-i18n="detection.compare"></option><option value="hybrid" data-i18n="detection.hybrid"></option></select></label>
<p class="field-hint" data-i18n="detection.modeHint"></p>
<label class="checkbox"><input type="checkbox" id="automatic_references"><span data-i18n="detection.autoReferences"></span></label>
<label class="checkbox"><input type="checkbox" id="sender_learning"><span data-i18n="detection.learning"></span></label>
<p class="field-hint" data-i18n="detection.autoHint"></p>
<label><span data-i18n="detection.channelAuto"></span><select id="channel-auto"><option value="" data-i18n="detection.inherit"></option><option value="true" data-i18n="common.yes"></option><option value="false" data-i18n="common.no"></option></select></label>
<label><span data-i18n="detection.channelLearning"></span><select id="channel-learning"><option value="" data-i18n="detection.inherit"></option><option value="true" data-i18n="common.yes"></option><option value="false" data-i18n="common.no"></option></select></label>
<button type="button" class="secondary" onclick="resetDetectionProfile()" data-i18n="detection.reset"></button>
</details>`

const detectionStatusHTML = `
<details class="settings-advanced" id="detection-diagnostics"><summary data-i18n="detection.diagnostics"></summary>
<p id="detection-summary" role="status"></p><p id="detection-signals"></p><p id="detection-profile"></p>
<p class="field-hint" data-i18n="detection.recordHint"></p>
<label><span data-i18n="detection.purpose"></span><select id="detection-purpose"><option value="validation" data-i18n="detection.validationPurpose"></option><option value="training" data-i18n="detection.trainingPurpose"></option></select></label>
<div class="status-actions"><button id="detection-record-start" type="button" onclick="recordDetection('start')" data-i18n="detection.recordStart" disabled></button><button id="detection-record-stop" type="button" onclick="recordDetection('stop')" data-i18n="detection.recordStop" hidden></button><button type="button" class="secondary" onclick="loadDetectionRecordings()" data-i18n="detection.refresh"></button></div>
<p id="detection-record-state" role="status"></p>
<p id="detection-record-message" role="status"></p>
<label><span data-i18n="detection.recording"></span><select id="detection-recordings" onchange="openDetectionRecording(this.value)"><option value="">–</option></select></label>
<label><span data-i18n="detection.import"></span><input type="file" accept=".json,application/json" onchange="importDetectionRecording(this.files[0])"></label>
<div id="detection-record-editor" hidden>
<label><span data-i18n="detection.sample"></span><input id="detection-sample" type="range" min="0" value="0" oninput="renderDetectionSample()"></label>
<p id="detection-sample-info"></p><img id="detection-sample-image" class="debug-frame-image" alt="" hidden>
<table><thead><tr><th data-i18n="detection.start"></th><th data-i18n="detection.end"></th><th data-i18n="detection.label"></th><th></th></tr></thead><tbody id="detection-labels"></tbody></table>
<div class="status-actions"><button type="button" onclick="addDetectionLabel()" data-i18n="detection.addLabel"></button><button type="button" onclick="saveDetectionLabels()" data-i18n="detection.saveLabels"></button><button type="button" onclick="replayDetectionRecording()" data-i18n="detection.evaluate"></button><a id="detection-download" class="debug-download" download data-i18n="debug.download"></a><button type="button" onclick="downloadDetectionJSON()" data-i18n="detection.downloadJSON"></button></div>
</div>
<p id="detection-acceptance" class="field-hint" data-i18n="detection.pending"></p><div id="detection-report"></div>
</details>`

const detectionJS = `
let detectionChannelDraft={},detectionFormChannel='',detectionRecord=null;
let detectionRecordingState=null,detectionRecordingKnown=false,detectionRecordingPending=false,detectionRecordingVersion=0,detectionRecordingAllowed=false;
function renderDetectionRecordingControls(){
  const running=detectionRecordingState?.state==='recording';
  $('detection-record-start').hidden=running;
  $('detection-record-stop').hidden=!running;
  $('detection-record-start').disabled=detectionRecordingPending||!detectionRecordingKnown||!detectionRecordingAllowed;
  $('detection-record-stop').disabled=detectionRecordingPending||!detectionRecordingKnown;
  $('detection-purpose').disabled=running||detectionRecordingPending;
  if(running&&detectionRecordingState.purpose)$('detection-purpose').value=detectionRecordingState.purpose;
  $('detection-record-state').textContent=running?t('detection.recordingNow'):detectionRecordingState?.state==='ready'?t('detection.recorded'):detectionRecordingState?.error||'';
}
function syncDetectionConfig(config){
  $('detection_mode').value=config.detection_mode||'classic';
  $('automatic_references').checked=config.automatic_references!==false;
  $('sender_learning').checked=config.sender_learning!==false;
  detectionChannelDraft=structuredClone(config.channel_detection||{});detectionFormChannel='';
}
function saveDetectionChannelDraft(){
  if(!detectionFormChannel)return;
  const row={...(detectionChannelDraft[detectionFormChannel]||{})};
  for(const [key,id] of [['automatic_references','channel-auto'],['sender_learning','channel-learning']]){const value=$(id).value;if(value==='')delete row[key];else row[key]=value==='true'}
  if(Object.keys(row).length)detectionChannelDraft[detectionFormChannel]=row;else delete detectionChannelDraft[detectionFormChannel];
}
function collectDetectionConfig(n){saveDetectionChannelDraft();n.detection_mode=$('detection_mode').value;n.automatic_references=$('automatic_references').checked;n.sender_learning=$('sender_learning').checked;n.channel_detection=structuredClone(detectionChannelDraft)}
function renderDetectionStatus(x,recordingVersion=detectionRecordingVersion){
  if(recordingVersion===detectionRecordingVersion&&!detectionRecordingPending){
    detectionRecordingState=x.recording||null;detectionRecordingKnown=true;
    detectionRecordingAllowed=Boolean(x.status.running&&x.active_channel?.url&&['compare','hybrid'].includes(x.config?.detection_mode));
    renderDetectionRecordingControls();
  }
  const key=x.active_channel?.url||'';
  if(key!==detectionFormChannel){saveDetectionChannelDraft();detectionFormChannel=key;const row=detectionChannelDraft[key]||{};for(const [field,id] of [['automatic_references','channel-auto'],['sender_learning','channel-learning']]){$(id).disabled=!key;$(id).value=row[field]===undefined?'':String(row[field])}}
  const d=x.status.detection;
  $('detection-summary').textContent=d?t('detection.summary',{mode:t('detection.'+d.mode),classic:t('detection.state.'+d.classic.state),hybrid:t('detection.state.'+d.hybrid.state),reason:t('detection.reason.'+d.hybrid.reason),remaining:Math.ceil(d.hybrid.remaining_seconds)}):t('detection.noData');
  $('detection-signals').textContent=d?t('detection.signals',{black:t(d.observation.signals.available.black?'common.yes':'common.no'),silence:t(d.observation.signals.available.silence?'common.yes':'common.no'),scene:t(d.observation.signals.available.scene?'common.yes':'common.no')}):'';
  $('detection-profile').textContent=d?t('detection.profile',{breaks:d.profile.breaks,active:t(d.profile.active?'common.yes':'common.no'),duration:Math.round(d.profile.typical_duration_seconds),automatic:t('detection.auto.'+d.automatic_state)}):'';
}
async function resetDetectionProfile(){try{await api('/api/detection/profile',{method:'DELETE'});await status()}catch(e){alert(e.message)}}
async function recordDetection(action){
  if(detectionRecordingPending)return;
  detectionRecordingPending=true;detectionRecordingVersion++;renderDetectionRecordingControls();
  let rec;
  try{
    rec=await api('/api/detection/recording',{method:'POST',body:JSON.stringify({action,purpose:$('detection-purpose').value})});
    detectionRecordingState=rec;detectionRecordingKnown=true;$('detection-record-message').textContent='';
  }catch(e){$('detection-record-message').textContent=e.message}
  finally{detectionRecordingPending=false;detectionRecordingVersion++;renderDetectionRecordingControls()}
  if(rec&&action==='stop'){await loadDetectionRecordings();await openDetectionRecording(rec.id)}
  if(!rec)await status();
}
async function loadDetectionRecordings(){try{const rows=await api('/api/detection/recordings'),select=$('detection-recordings');select.replaceChildren(...[{id:'',channel:'–'},...(Array.isArray(rows)?rows:[])].map(r=>{const o=document.createElement('option');o.value=r.id;o.textContent=r.id?r.channel+' · '+new Date(r.created_at).toLocaleString(language):'–';return o}))}catch(e){$('detection-record-message').textContent=e.message}}
async function openDetectionRecording(id){if(!id)return;try{detectionRecord=await api('/api/detection/recording?id='+encodeURIComponent(id));detectionRecord.local=false;renderDetectionRecording()}catch(e){$('detection-record-message').textContent=e.message}}
async function importDetectionRecording(file){if(!file)return;try{if(file.size>16*1024*1024)throw Error(t('detection.tooLarge'));const rec=JSON.parse(await file.text());if(!Array.isArray(rec.observations)||!rec.settings)throw Error(t('detection.invalid'));detectionRecord=rec;detectionRecord.local=true;renderDetectionRecording()}catch(e){$('detection-record-message').textContent=e.message}}
function renderDetectionRecording(){
  $('detection-record-editor').hidden=false;$('detection-labels').replaceChildren();
  for(const label of detectionRecord.labels||[])addDetectionLabel(label);
  $('detection-sample').max=Math.max(0,detectionRecord.observations.length-1);$('detection-sample').value=0;
  $('detection-download').hidden=detectionRecord.local;$('detection-download').href='/api/detection/recording?id='+encodeURIComponent(detectionRecord.id)+'&download=1';
  renderDetectionSample();
}
function renderDetectionSample(){const o=detectionRecord?.observations[Number($('detection-sample').value)];if(!o){$('detection-sample-info').textContent='';$('detection-sample-image').hidden=true;return}$('detection-sample-info').textContent=t('detection.sampleInfo',{time:o.time.toFixed(1),logo:t('decision.'+o.logo.decision)});const im=$('detection-sample-image');im.hidden=!o.image||detectionRecord.local;if(!im.hidden)im.src='/api/detection/recording?id='+encodeURIComponent(detectionRecord.id)+'&frame='+encodeURIComponent(o.image)}
function addDetectionLabel(label={start:0,end:1,kind:'program'}){
  const row=document.createElement('tr');
  for(const key of ['start','end']){const cell=document.createElement('td'),input=document.createElement('input');input.type='number';input.min='0';input.step='0.1';input.value=label[key];input.dataset.field=key;input.setAttribute('aria-label',t('detection.'+key));cell.append(input);row.append(cell)}
  const cell=document.createElement('td'),select=document.createElement('select');select.dataset.field='kind';select.setAttribute('aria-label',t('detection.label'));for(const kind of ['program','ad']){const option=document.createElement('option');option.value=kind;option.textContent=t('detection.state.'+kind);select.append(option)}select.value=label.kind;cell.append(select);row.append(cell);
  const actions=document.createElement('td'),remove=document.createElement('button');remove.type='button';remove.textContent=t('actions.delete');remove.onclick=()=>row.remove();actions.append(remove);row.append(actions);$('detection-labels').append(row);
}
function collectDetectionLabels(){return [...$('detection-labels').children].map(row=>Object.fromEntries([...row.querySelectorAll('[data-field]')].map(input=>[input.dataset.field,input.dataset.field==='kind'?input.value:Number(input.value)]))).sort((a,b)=>a.start-b.start)}
async function saveDetectionLabels(){try{detectionRecord.labels=collectDetectionLabels();if(!detectionRecord.local)await api('/api/detection/recording?id='+encodeURIComponent(detectionRecord.id),{method:'PUT',body:JSON.stringify({labels:detectionRecord.labels})});$('detection-record-message').textContent=t('detection.labelsSaved')}catch(e){$('detection-record-message').textContent=e.message}}
async function replayDetectionRecording(){try{
  detectionRecord.labels=collectDetectionLabels();const report=await api('/api/detection/replay',{method:'POST',body:JSON.stringify(detectionRecord)}),table=document.createElement('table');
  const heading=document.createElement('tr');for(const key of ['mode','falseSwitches','missedBreaks','adDelay','returnDelay','unreturned']){const cell=document.createElement('th');cell.textContent=t('detection.'+key);heading.append(cell)}table.append(heading);
  const mean=xs=>xs?.length?(xs.reduce((a,b)=>a+b,0)/xs.length).toFixed(1)+' s':'–';
  for(const mode of ['classic','hybrid']){const m=report[mode],row=document.createElement('tr');for(const value of [t('detection.'+mode),m.false_switches+' ('+m.false_switches_per_hour.toFixed(2)+'/h)',m.missed_breaks,mean(m.ad_delays_seconds),mean(m.return_delays_seconds),m.unreturned_breaks]){const cell=document.createElement('td');cell.textContent=String(value);row.append(cell)}table.append(row)}
  $('detection-report').replaceChildren(table);$('detection-acceptance').textContent=t('detection.pending');
}catch(e){$('detection-record-message').textContent=e.message}}
function downloadDetectionJSON(){if(!detectionRecord)return;detectionRecord.labels=collectDetectionLabels();const url=URL.createObjectURL(new Blob([JSON.stringify(detectionRecord,null,2)],{type:'application/json'})),a=document.createElement('a');a.href=url;a.download='observations.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
`
