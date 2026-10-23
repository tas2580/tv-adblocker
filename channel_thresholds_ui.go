package main

const channelThresholdHTML = `
  <details id="channel-thresholds" class="settings-advanced channel-thresholds"><summary data-i18n="recognition.channelThresholds"></summary>
    <p id="channel-threshold-hint" class="card-description" data-i18n="recognition.channelThresholdHint"></p>
    <button id="refresh-channel-thresholds" type="button" onclick="refreshChannelThresholds()" data-i18n="recognition.refreshChannels"></button>
    <p id="channel-threshold-message" class="field-hint" role="status"></p>
    <div id="channel-threshold-list"></div>
  </details>
<style>
.channel-threshold-row{display:grid;grid-template-columns:minmax(0,1fr) 150px;align-items:center;gap:1rem;padding:.5rem 0;border-bottom:1px solid var(--border)}
.channel-threshold-row span{overflow-wrap:anywhere}
.channel-thresholds .channel-threshold-row input{margin:0}
@media(max-width:600px){.channel-threshold-row{grid-template-columns:minmax(0,1fr) 110px;gap:.6rem}}
</style>
`

const channelThresholdJS = `
let channelThresholdDraft={},channelThresholdChannels=[],channelThresholdRows=new Map();
let channelThresholdState='idle',channelThresholdError='',channelThresholdRequest=null;

function syncChannelThresholdForm(config){
  channelThresholdDraft={...(config.channel_similarity_minimum||{})};
  channelThresholdRows.clear();
  renderChannelThresholds();
  if(selectedTab==='recognition')void refreshChannelThresholds();
}

function updateChannelThresholdPlaceholders(){
  for(const {input} of channelThresholdRows.values())input.placeholder=$('similarity_minimum').value;
}

function renderChannelThresholds(){
  const box=$('channel-threshold-list'),rows=[],seen=new Set();
  for(const channel of channelThresholdChannels){
    if(!channel.url||seen.has(channel.url))continue;
    seen.add(channel.url);
    let row=channelThresholdRows.get(channel.url);
    if(!row){
      const label=document.createElement('label'),name=document.createElement('span'),input=document.createElement('input');
      label.className='channel-threshold-row';
      input.type='number';input.min='0';input.max='1';input.step='0.01';
      input.setAttribute('aria-describedby','channel-threshold-hint');
      input.value=channelThresholdDraft[channel.url]??'';
      input.addEventListener('input',()=>{
        input.setCustomValidity('');
        if(input.validity.badInput)channelThresholdDraft[channel.url]=NaN;
        else if(input.value==='')delete channelThresholdDraft[channel.url];
        else channelThresholdDraft[channel.url]=Number(input.value);
      });
      label.append(name,input);
      row={label,name,input};
      channelThresholdRows.set(channel.url,row);
    }
    row.name.textContent=channel.name||t('recognition.unnamedChannel');
    row.input.setAttribute('aria-label',t('recognition.channelSimilarity',{name:row.name.textContent}));
    rows.push(row.label);
  }
  box.replaceChildren(...rows);
  updateChannelThresholdPlaceholders();
  $('refresh-channel-thresholds').disabled=channelThresholdState==='loading';
  const key=channelThresholdState==='loading'?'common.loading':channelThresholdState==='error'?'recognition.channelsFailed':rows.length?'':'recognition.noChannels';
  const message=$('channel-threshold-message');
  message.textContent=key?t(key,{error:channelThresholdError}):'';
  message.hidden=!key;
}

async function refreshChannelThresholds(){
  if(channelThresholdRequest)return channelThresholdRequest;
  channelThresholdState='loading';
  renderChannelThresholds();
  channelThresholdRequest=(async()=>{
    try{
      const channels=await api('/api/playlist');
      channelThresholdChannels=Array.isArray(channels)?channels:[];
      channelThresholdState='ready';
      channelThresholdError='';
    }catch(error){
      channelThresholdState='error';channelThresholdError=error.message;
    }finally{
      channelThresholdRequest=null;
      renderChannelThresholds();
    }
  })();
  return channelThresholdRequest;
}

function collectChannelThresholds(){
  for(const [url,value] of Object.entries(channelThresholdDraft)){
    if(!Number.isFinite(value)||value<=0||value>1){
      const input=channelThresholdRows.get(url)?.input;
      if(input){$('channel-thresholds').open=true;input.setCustomValidity(t('recognition.channelSimilarityInvalid'));input.reportValidity()}
      throw Error(t('recognition.channelSimilarityInvalid'));
    }
  }
  return {...channelThresholdDraft};
}
`
