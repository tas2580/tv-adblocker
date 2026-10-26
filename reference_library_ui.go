package main

const referenceLibraryJS = `
let referenceLibraryRequest=0;
const referenceCards=new Map();
function renderReferenceData(box,ref){
  box.replaceChildren();
  const data=ref.data;
  if(!data){box.textContent=t('logos.dataUnknown');return}
  const list=document.createElement('dl');list.className='reference-data';
  function row(label,value){
    const line=document.createElement('div'),term=document.createElement('dt'),description=document.createElement('dd');
    term.textContent=t(label)+': ';description.textContent=value;line.append(term,description);list.append(line);return description;
  }
  const availability=state=>t('logos.data.'+(state||'missing'));
  if(ref.automatic)row('detection.autoReferences',t('detection.validation',{rate:Math.round((ref.validation_rate||0)*100),date:ref.validated_at||'–'}));
  row('logos.dataImage',availability(data.image));
  row('logos.dataSource',t('logos.source.'+data.source));
  row('logos.dataMask',availability(data.mask));
  row('logos.dataAutomaticMask',availability(data.automatic_mask));
  row('logos.dataShape',t(data.shape_usable?'logos.shapeUsable':'logos.shapeUnusable'));
  const color=data.color||{state:'inactive'},estimate=color.estimate;
  let colorText=t('logos.color.'+color.state);
  if(color.state==='learning')colorText=t(color.samples<color.minimum_samples?'logos.colorProgress':'logos.colorMoreBackgrounds',{samples:color.samples,minimum:color.minimum_samples});
  if(color.state==='ready'&&estimate)colorText=t('logos.colorReady',{r:Math.round(estimate.rgb[0]),g:Math.round(estimate.rgb[1]),b:Math.round(estimate.rgb[2]),confidence:Math.round(estimate.confidence*100),samples:estimate.samples});
  const colorValue=row('logos.dataColor',colorText);
  if(color.state==='ready'&&estimate){const swatch=document.createElement('span');swatch.className='reference-color-swatch';swatch.setAttribute('aria-hidden','true');swatch.style.backgroundColor='rgb('+estimate.rgb.map(v=>Math.round(v)).join(',')+')';colorValue.prepend(swatch)}
  box.append(list);
  let hint='';
  if(data.image!=='available')hint='logos.retakeImage';
  else if(data.mask==='invalid')hint='logos.retakeMask';
  else if(!data.shape_usable)hint='logos.retakeShape';
  else if(data.mask==='generated')hint='logos.saveMaskHint';
  if(hint){const note=document.createElement('p');note.className='reference-hint';note.textContent=t(hint);box.append(note)}
}
function createReferenceCard(){
  const card=document.createElement('article');card.className='reference';
  const open=document.createElement('button');open.type='button';open.className='reference-open';open.setAttribute('aria-label',t('logos.showPosition'));
  const image=document.createElement('img');image.alt=t('logos.referenceAlt');open.append(image);
  const label=document.createElement('small'),data=document.createElement('div'),remove=document.createElement('button'),toggle=document.createElement('button');remove.type='button';toggle.type='button';toggle.hidden=true;
  card.append(open,label,data,toggle,remove);
  return {card,open,image,label,data,remove,toggle};
}
async function refreshReferences(){
  const request=++referenceLibraryRequest,stream=currentActiveURL;
  try{
    let refs=await api('/api/references');
    if(request!==referenceLibraryRequest||stream!==currentActiveURL)return;
    refs=Array.isArray(refs)?refs:[];
    const box=$('references');
    if(!refs.length){referenceCards.clear();box.textContent=t('logos.noReferences');return}
    if(!referenceCards.size)box.replaceChildren();
    const ids=new Set(refs.map(ref=>ref.id));
    for(const [id,elements] of referenceCards){if(!ids.has(id)){elements.card.remove();referenceCards.delete(id)}}
    for(const ref of refs){
      let elements=referenceCards.get(ref.id);
      if(!elements){elements=createReferenceCard();referenceCards.set(ref.id,elements);box.append(elements.card)}
      const {card,open,image,label,data,remove,toggle}=elements;
      card.dataset.referenceId=ref.id;open.onclick=()=>selectReference(ref);open.setAttribute('aria-label',t('logos.showPosition'));
      const source='/api/references/'+encodeURIComponent(ref.id)+'.bmp';
      if(image.getAttribute('src')!==source)image.src=source;
      image.alt=t('logos.referenceAlt');label.textContent=ref.created_at;label.title=ref.stream_name||ref.stream_url;
      renderReferenceData(data,ref);
      toggle.hidden=!ref.automatic;toggle.textContent=t(ref.disabled?'detection.enableReference':'detection.disableReference');toggle.onclick=async()=>{try{await api('/api/references/'+encodeURIComponent(ref.id),{method:'PATCH',body:JSON.stringify({disabled:!ref.disabled})});await refreshReferences()}catch(e){alert(e.message)}};
      remove.textContent=t('actions.delete');
      remove.onclick=async()=>{
        if(!confirm(t('alerts.deleteReference')))return;
        try{await api('/api/references/'+encodeURIComponent(ref.id),{method:'DELETE'});if(selectedReferenceID===ref.id){selectedReferenceID='';closeLogoMask()}await status();await refreshReferences()}catch(e){alert(e.message)}
      };
    }
  }catch(e){if(request!==referenceLibraryRequest||stream!==currentActiveURL)return;referenceCards.clear();$('references').textContent=t('logos.referencesFailed',{error:e.message})}
}
`
