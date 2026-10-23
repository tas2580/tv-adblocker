package main

const fillerHTML = `
<section class="tab-panel settings-panel" data-panel="placeholder">
  <div class="status-hero settings-hero">
    <div class="status-icon" aria-hidden="true">▧</div>
    <div class="status-hero-copy"><p class="status-eyebrow" data-i18n="tabs.placeholder"></p><h2 data-i18n="placeholder.heading"></h2><p class="status-description" data-i18n="placeholder.intro"></p></div>
  </div>
  <div class="settings-body">
    <div class="settings-grid">
      <fieldset class="settings-card"><legend data-i18n="placeholder.imageHeading"></legend><p class="card-description" data-i18n="placeholder.imageHint"></p>
        <label class="upload-field"><span data-i18n="placeholder.selectImage"></span><input id="placeholder_image_file" type="file" multiple accept="image/jpeg,image/png,image/gif,image/webp,image/bmp"></label>
        <button id="upload-fillers" type="button" class="secondary" onclick="uploadPlaceholderImage()" data-i18n="placeholder.upload"></button>
      </fieldset>
      <fieldset class="settings-card"><legend data-i18n="placeholder.filesHeading"></legend><p class="card-description" data-i18n="placeholder.filesHint"></p>
        <label><span data-i18n="placeholder.kodiFiles"></span><textarea id="kodi_placeholders" rows="3" data-i18n-placeholder="placeholder.kodiFilesPlaceholder"></textarea></label>
        <button type="button" class="secondary" onclick="addKodiFillers()" data-i18n="placeholder.addFiles"></button>
      </fieldset>
    </div>
    <div class="filler-library">
      <h3 data-i18n="placeholder.library"></h3><p class="card-description" data-i18n="placeholder.rules"></p>
      <p id="filler-timezone" class="field-hint"></p>
      <label><span data-i18n="placeholder.fallback"></span><select id="fallback_filler_id" onchange="fallbackFillerID=this.value"></select></label>
      <p class="field-hint" data-i18n="placeholder.fallbackHint"></p>
      <div id="filler-list" class="filler-list"></div>
    </div>
  </div>
  <div class="settings-footer"><p data-i18n="settings.saveHint"></p><div class="status-actions"><button type="button" onclick="saveConfig()" data-i18n="placeholder.save"></button></div></div>
</section>`

const fillerCSS = `
.filler-library{margin-top:1.5rem}.filler-list{display:grid;gap:1rem;margin-top:1.2rem}
.filler-card{border:1px solid var(--border);border-radius:12px;padding:1rem;min-width:0;background:var(--surface)}
.filler-card-header{display:flex;gap:1rem;align-items:flex-start}.filler-card-header>div{flex:1;min-width:0}
.filler-card img{width:110px;height:75px;object-fit:contain;border-radius:6px;background:var(--surface-alt)}
.filler-card .filler-target{display:block;overflow-wrap:anywhere;color:var(--muted);font-size:.8rem;margin:.5rem 0}
.filler-card .filler-enable{display:flex;align-items:center;gap:.5rem}.filler-enable input{width:auto}
.filler-dates{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:1rem}
.filler-annual{display:flex;gap:.5rem}.filler-annual input{width:5rem}.filler-annual select{flex:1;min-width:0}
@media(max-width:600px){.filler-dates{grid-template-columns:1fr}.filler-card-header{flex-wrap:wrap}.filler-card img{width:85px;height:60px}}
`

const fillerJS = `
let fillerDraft=[],fallbackFillerID='',fillerTimezone='';
function syncFillerForm(config,timezone){fillerDraft=structuredClone(config.fillers||[]);fallbackFillerID=config.fallback_filler_id||'';fillerTimezone=timezone||'';renderFillers()}
function fillerLabel(key,input){const label=document.createElement('label'),span=document.createElement('span');span.textContent=t(key);label.append(span,input);return label}
function renderFillerFallback(){
  const select=$('fallback_filler_id'),none=document.createElement('option');none.value='';none.textContent=t('placeholder.noFallback');
  select.replaceChildren(none,...fillerDraft.map(f=>{const option=document.createElement('option');option.value=f.id;option.textContent=f.name||f.target||f.image;return option}));select.value=fallbackFillerID;
}
function fillerDateControl(f,key){
  const schedule=f.schedule;
  if(schedule.mode==='once'){
    const input=document.createElement('input');input.type='date';input.value=schedule[key]||'';input.oninput=()=>schedule[key]=input.value;
    return fillerLabel('placeholder.'+key,input);
  }
  const label=document.createElement('div'),title=document.createElement('p'),row=document.createElement('div'),day=document.createElement('input'),month=document.createElement('select');
  title.textContent=t('placeholder.'+key);row.className='filler-annual';
  day.type='number';day.min='1';day.max='31';day.placeholder=t('placeholder.day');day.setAttribute('aria-label',t('placeholder.'+key)+' · '+t('placeholder.day'));
  month.setAttribute('aria-label',t('placeholder.'+key)+' · '+t('placeholder.month'));
  const empty=document.createElement('option');empty.value='';empty.textContent=t('placeholder.month');month.append(empty);
  for(let number=1;number<=12;number++){const option=document.createElement('option');option.value=String(number).padStart(2,'0');option.textContent=new Intl.DateTimeFormat(language,{month:'long'}).format(new Date(2000,number-1,1));month.append(option)}
  const value=(schedule[key]||'').split('-');month.value=value[0]||'';day.value=value[1]||'';
  const update=()=>{schedule[key]=month.value+'-'+String(day.value).padStart(2,'0')};day.oninput=update;month.onchange=update;
  row.append(day,month);label.append(title,row);return label;
}
function renderFillers(){
  renderFillerFallback();$('filler-timezone').textContent=t('placeholder.timezone',{zone:fillerTimezone});
  const list=$('filler-list');list.replaceChildren();
  if(!fillerDraft.length){const p=document.createElement('p');p.textContent=t('placeholder.emptyLibrary');list.append(p);return}
  for(const f of fillerDraft){
    const card=document.createElement('article'),header=document.createElement('div'),content=document.createElement('div'),enabled=document.createElement('input'),name=document.createElement('input'),target=document.createElement('small');
    card.className='filler-card';header.className='filler-card-header';
    if(f.source==='uploaded-image'){const image=document.createElement('img');image.src='/placeholder-image/'+encodeURIComponent(f.image);image.alt=f.name||'';image.loading='lazy';header.append(image)}
    enabled.type='checkbox';enabled.checked=f.enabled;enabled.onchange=()=>f.enabled=enabled.checked;const toggle=fillerLabel('placeholder.enabled',enabled);toggle.className='filler-enable';
    name.value=f.name||'';name.oninput=()=>{f.name=name.value;renderFillerFallback()};
    target.className='filler-target';target.textContent=f.source==='uploaded-image'?f.image:f.target;
    content.append(toggle,fillerLabel('placeholder.name',name),target);header.append(content);
    const remove=document.createElement('button');remove.type='button';remove.className='secondary';remove.textContent=t('placeholder.remove');remove.onclick=()=>{fillerDraft=fillerDraft.filter(entry=>entry.id!==f.id);if(fallbackFillerID===f.id)fallbackFillerID='';renderFillers()};header.append(remove);card.append(header);
    const mode=document.createElement('select');for(const value of ['always','annual','once']){const option=document.createElement('option');option.value=value;option.textContent=t('placeholder.schedule.'+value);mode.append(option)}mode.value=f.schedule.mode;
    mode.onchange=()=>{f.schedule={mode:mode.value,start:'',end:''};renderFillers()};card.append(fillerLabel('placeholder.schedule',mode));
    if(f.schedule.mode!=='always'){const dates=document.createElement('div');dates.className='filler-dates';dates.append(fillerDateControl(f,'start'),fillerDateControl(f,'end'));card.append(dates)}
    list.append(card);
  }
}
function addKodiFillers(){
  const entries=[...new Set($('kodi_placeholders').value.split(/\r?\n/).map(value=>value.trim()).filter(Boolean))];
  for(const target of entries){if(fillerDraft.some(f=>f.source==='kodi-local'&&f.target===target))continue;const id=globalThis.crypto?.randomUUID?.()||'kodi-'+Date.now().toString(36)+'-'+Math.random().toString(36).slice(2);fillerDraft.push({id,name:target,source:'kodi-local',target,enabled:true,schedule:{mode:'always',start:'',end:''}});if(!fallbackFillerID)fallbackFillerID=id}
  $('kodi_placeholders').value='';renderFillers();
}
async function uploadPlaceholderImage(){
  const input=$('placeholder_image_file'),files=[...input.files];if(!files.length)return alert(t('alerts.selectImage'));
  input.disabled=true;$('upload-fillers').disabled=true;
  let saved=false;
  try{await saveConfig(false);saved=true;for(const file of files){const form=new FormData();form.append('image',file);await api('/api/placeholder-image',{method:'POST',body:form})}input.value=''}catch(e){alert(e.message)}finally{if(saved)await status(true);input.disabled=false;$('upload-fillers').disabled=false}
}
`
