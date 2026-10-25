package main

const indexHTML = `<!doctype html><html lang=""><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title data-i18n="app.title"></title><style>
:root{color-scheme:light;--bg:#f3f6fa;--surface:#fff;--surface-alt:#f7f9fc;--text:#172033;--muted:#64748b;--border:#dbe3ee;--accent:#2563eb;--accent-text:#fff;--shadow:0 10px 30px #1e293b12}*{box-sizing:border-box}body.dark{color-scheme:dark;--bg:#0b1220;--surface:#111b2e;--surface-alt:#17233a;--text:#e8eef9;--muted:#9aacc6;--border:#2a3b58;--accent:#60a5fa;--accent-text:#07111f;--shadow:0 12px 32px #0005}body{font:15px system-ui,sans-serif;max-width:1080px;margin:2rem auto;padding:0 1rem 2rem;color:var(--text);background:var(--bg);transition:background .2s,color .2s}h1,h2,h3{letter-spacing:-.02em}h1{margin:0;font-size:1.8rem}h2{margin-top:0;font-size:1.15rem}.app-header{display:flex;align-items:flex-start;justify-content:space-between;gap:1rem;margin-bottom:1.25rem}.app-header p{margin:.35rem 0 0}.header-actions{display:flex;align-items:center;gap:.45rem}.language-select{width:auto;margin:0;padding:.5rem}.tabs{display:flex;gap:.45rem;overflow-x:auto;padding:0 0 .85rem;border-bottom:1px solid var(--border);margin-bottom:1rem}.tab{border:1px solid transparent;background:transparent;color:var(--muted);border-radius:8px;padding:.55rem .75rem;white-space:nowrap}.tab:hover{background:var(--surface-alt);color:var(--text)}.tab.active{background:var(--accent);color:var(--accent-text);font-weight:650}.tab-panel{display:none}.tab-panel.active{display:block}section{background:var(--surface);padding:1.35rem;margin:0;border:1px solid var(--border);border-radius:14px;box-shadow:var(--shadow)}label{display:block;margin:.55rem 0;color:var(--muted);font-size:.9rem;font-weight:600}input,select,textarea{width:100%;box-sizing:border-box;padding:.6rem .65rem;border:1px solid var(--border);border-radius:8px;background:var(--surface-alt);color:var(--text);margin-top:.25rem;font:inherit}input:focus,select:focus,textarea:focus{outline:2px solid var(--accent);outline-offset:1px}.checkbox{display:flex;align-items:center;gap:.55rem}.checkbox input{width:auto;margin:0}button{border:0;border-radius:8px;background:var(--accent);color:var(--accent-text);font:inherit;font-weight:600;padding:.58rem .82rem;margin:.25rem .25rem .25rem 0;cursor:pointer;transition:filter .15s,transform .15s}button:hover{filter:brightness(1.08)}button:active{transform:translateY(1px)}.theme-toggle{flex:0 0 auto;background:var(--surface);color:var(--text);border:1px solid var(--border)}#preview-wrap{position:relative;display:inline-block;max-width:100%;line-height:0}#preview{max-width:100%;border:1px solid var(--border);border-radius:8px;cursor:crosshair;user-select:none;-webkit-user-drag:none;touch-action:none}#selection{position:absolute;display:none;box-sizing:border-box;border:2px solid #ff3b30;background:#ff3b3022;pointer-events:none}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:0 1rem}.references{display:flex;gap:.75rem;flex-wrap:wrap}.reference{border:1px solid var(--border);background:var(--surface-alt);border-radius:9px;padding:.5rem;margin:0;width:160px}.reference img{display:block;width:100%;max-height:86px;object-fit:contain;background:#111;border-radius:5px}.reference small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;margin:.4rem 0;color:var(--muted)}#placeholder-preview{display:none;max-width:320px;max-height:180px;object-fit:contain;border:1px solid var(--border);border-radius:8px;background:#111;margin-top:.75rem}.muted{color:var(--muted)}#status{white-space:pre-wrap;font-family:ui-monospace,SFMono-Regular,Consolas,monospace;line-height:1.55;background:var(--surface-alt);border:1px solid var(--border);border-radius:9px;padding:.85rem;margin-bottom:.8rem}@media(max-width:600px){body{margin:1rem auto}.app-header{align-items:center}.app-header p{display:none}section{padding:1rem}.tabs{margin-left:-1rem;margin-right:-1rem;padding-left:1rem;padding-right:1rem}}</style>
<div class="app-header"><div><h1 data-i18n="app.title"></h1><p class="muted" data-i18n="app.subtitle"></p></div><div class="header-actions"><select id="language-select" class="language-select" data-i18n-aria-label="language.label"><option value="de" data-i18n="language.de"></option><option value="en" data-i18n="language.en"></option></select><button id="theme-toggle" class="theme-toggle" type="button" onclick="toggleTheme()"></button></div></div>
<nav class="tabs" data-i18n-aria-label="tabs.label"><button class="tab active" type="button" data-tab="status" data-i18n="tabs.status"></button><button class="tab" type="button" data-tab="kodi" data-i18n="tabs.kodi"></button><button class="tab" type="button" data-tab="placeholder" data-i18n="tabs.placeholder"></button><button class="tab" type="button" data-tab="logos" data-i18n="tabs.logos"></button><button class="tab" type="button" data-tab="recognition" data-i18n="tabs.recognition"></button></nav>
<style>
[data-panel="status"]{padding:0;overflow:hidden}.status-topline{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:1.4rem 1.6rem;border-bottom:1px solid var(--border)}.status-topline h2{margin:0}.status-badge{display:inline-flex;align-items:center;gap:.5rem;border-radius:99px;background:var(--surface-alt);color:var(--muted);padding:.4rem .75rem;font-size:.8rem;font-weight:650}.status-badge:before{content:"";width:7px;height:7px;border-radius:50%;background:currentColor}.status-badge[data-tone="good"]{background:#e7f6ef;color:#226444}.status-badge[data-tone="warm"]{background:#fff2d9;color:#80530b}.status-badge[data-tone="error"]{background:#fdeceb;color:#a32b31}.dark .status-badge[data-tone="good"]{background:#18392e;color:#99dfb9}.dark .status-badge[data-tone="warm"]{background:#3d311d;color:#f0cd88}.dark .status-badge[data-tone="error"]{background:#42232c;color:#ffb5b9}.status-hero{display:flex;align-items:center;gap:1.35rem;padding:2rem 1.6rem;background:linear-gradient(120deg,var(--surface-alt),var(--surface))}.status-icon{display:grid;place-items:center;flex:0 0 76px;height:76px;border:1px solid var(--border);border-radius:22px;color:var(--accent);background:var(--surface);box-shadow:var(--shadow)}.status-icon svg{width:36px;height:36px}.status-hero-copy{min-width:0}.status-eyebrow{color:var(--muted);font-size:.75rem;font-weight:650;letter-spacing:.1em;text-transform:uppercase;margin:0 0 .55rem}.status-hero h3{font-size:clamp(1.6rem,4vw,2.2rem);line-height:1.15;margin:0;overflow-wrap:anywhere}.status-description{margin:.6rem 0 0;color:var(--muted);line-height:1.6}.status-content{padding:0 1.6rem 1.5rem}.status-cards{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:.8rem;margin:.2rem 0 1.4rem}.status-card{padding:1.1rem;border:1px solid var(--border);border-radius:12px;min-width:0}.status-card dt{font-size:.8rem;color:var(--muted);margin-bottom:.65rem}.status-card dd{margin:0;font-size:1.1rem;font-weight:650;line-height:1.4}.status-card dd.status-time{font-size:1.65rem;letter-spacing:-.03em;font-variant-numeric:tabular-nums;line-height:1.1}.status-card p{font-size:.8rem;color:var(--muted);line-height:1.5;margin:.5rem 0 0}.status-actions{display:flex;flex-wrap:wrap;gap:.5rem}.status-actions button{margin:0}.status-actions .secondary{background:var(--surface-alt);color:var(--text);border:1px solid var(--border)}.status-actions button:disabled{opacity:.5;cursor:wait}.status-notice{background:var(--surface-alt);border-left:3px solid var(--accent);border-radius:6px;padding:.8rem 1rem;margin:0 0 1rem;font-size:.9rem;line-height:1.6;overflow-wrap:anywhere}.status-errors{border-color:#d97706}.status-errors p{margin:0}.status-details{margin-top:1.4rem;padding-top:1rem;border-top:1px solid var(--border)}.status-details summary{cursor:pointer;color:var(--muted);font-size:.85rem;padding:.25rem 0;width:fit-content}.status-details #status{font-size:.8rem;overflow-wrap:anywhere;margin:1rem 0 0}button:focus-visible,summary:focus-visible{outline:2px solid var(--accent);outline-offset:4px}@media(max-width:600px){.app-header{align-items:flex-start;flex-direction:column;gap:.5rem}.app-header h1{font-size:1.55rem}.header-actions{width:100%}.header-actions .language-select{flex:1}.header-actions .theme-toggle{margin:0}.status-topline{padding:1rem;flex-wrap:wrap}.status-hero{padding:1.5rem 1rem;gap:1rem}.status-icon{flex-basis:54px;height:54px;border-radius:16px}.status-icon svg{width:28px;height:28px}.status-content{padding:0 1rem 1rem}.status-cards{grid-template-columns:1fr;gap:.6rem}.status-card{padding:.9rem 1rem}.status-card dt{margin-bottom:.35rem}.status-card dd.status-time{font-size:1.4rem}.status-actions button{flex:1 1 100%}}
.status-programme{margin:.55rem 0 0;font-size:1.05rem;font-weight:600;line-height:1.45;overflow-wrap:anywhere}.status-icon img{width:100%;height:100%;min-height:0;object-fit:contain;padding:8px}.status-icon>[hidden]{display:none}
[data-panel="status"] [hidden]{display:none}.status-kodi-unavailable{margin:0;padding:2.5rem 1rem;text-align:center;font-size:1.25rem;font-weight:650}
</style>
<section class="tab-panel active" data-panel="status">
  <p id="status-kodi-unavailable" class="status-kodi-unavailable" role="status" data-i18n="status.kodiUnavailable" hidden></p>
  <div class="status-topline" data-status-online><h2 data-i18n="status.heading"></h2><span id="status-badge" class="status-badge" role="status" data-i18n="common.loading"></span></div>
  <div class="status-hero" data-status-online>
    <div id="status-channel-icon" class="status-icon" aria-hidden="true"><svg viewBox="0 0 32 32" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="26" height="18" rx="4"/><path d="M11 28h10M16 23v5M13 10l7 4-7 4z"/></svg></div>
    <div class="status-hero-copy"><p class="status-eyebrow" data-i18n="status.now"></p><h3 id="status-channel" data-i18n="common.loading"></h3><p id="status-programme" class="status-programme" hidden></p><p id="status-description" class="status-description"></p></div>
  </div>
  <div class="status-content" data-status-online>
    <dl class="status-cards">
      <div class="status-card"><dt data-i18n="status.playback"></dt><dd id="status-playback">–</dd><p id="status-playback-hint"></p></div>
      <div class="status-card"><dt id="status-elapsed-label" data-i18n="status.duration"></dt><dd id="status-elapsed" class="status-time">–</dd><p data-i18n="status.durationHint"></p></div>
      <div class="status-card"><dt data-i18n="status.detection"></dt><dd id="status-detection">–</dd><p id="status-detection-hint"></p></div>
    </dl>
    <p id="status-notice" class="status-notice" hidden></p>
    <div id="status-errors" class="status-notice status-errors" role="status" hidden></div>
    <div class="status-actions"><button id="detection-toggle" type="button" onclick="toggleDetection()" disabled data-i18n="status.startDetection"></button><button id="play-sender" class="secondary" type="button" hidden onclick="post('/api/play/sender')" data-i18n="status.playSender"></button><button id="play-placeholder" class="secondary" type="button" onclick="post('/api/play/placeholder')" disabled data-i18n="status.playPlaceholder"></button><button id="cancel-program-cooldown" class="secondary" type="button" hidden onclick="post('/api/cancel-program-cooldown')" data-i18n="status.cancelCooldown"></button><button id="resume-automatic" class="secondary" type="button" hidden onclick="post('/api/resume-automatic')" data-i18n="status.resumeAutomatic"></button></div>
` + logoDiagnosticHTML + `
` + debugCaptureHTML + `
    <details class="status-details"><summary data-i18n="status.details"></summary><div id="status" data-i18n="common.loading"></div></details>
  </div>
</section>
<section class="tab-panel settings-panel" data-panel="kodi">
  <div class="status-hero settings-hero">
    <div class="status-icon" aria-hidden="true"><svg viewBox="0 0 32 32" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="26" height="19" rx="4"/><path d="M11 28h10M16 23v5M11 12l4 4 7-7"/></svg></div>
    <div class="status-hero-copy"><p class="status-eyebrow" data-i18n="tabs.kodi"></p><h2 data-i18n="kodi.heading"></h2><p class="status-description" data-i18n="kodi.intro"></p></div>
  </div>
  <div class="settings-body"><div class="settings-grid">
    <fieldset class="settings-card"><legend data-i18n="kodi.connectionHeading"></legend><p class="card-description" data-i18n="kodi.connectionHint"></p>
<label><span data-i18n="kodi.rpcUrl"></span><input id="kodi_url" data-i18n-placeholder="kodi.rpcPlaceholder"></label><div class="grid"><label><span data-i18n="kodi.username"></span><input id="kodi_username" autocomplete="username"></label><label><span data-i18n="kodi.password"></span><input id="kodi_password" type="password" autocomplete="current-password"></label></div>
</fieldset>
    <fieldset class="settings-card"><legend data-i18n="kodi.channelsHeading"></legend><p class="card-description" data-i18n="kodi.channelsHint"></p>
<label><span data-i18n="kodi.m3uUrl"></span><input id="m3u_url" data-i18n-placeholder="kodi.m3uPlaceholder"></label>
</fieldset>
  </div>
  <details class="settings-advanced"><summary data-i18n="kodi.networkHeading"></summary><p class="card-description" data-i18n="kodi.networkHint"></p><div class="grid">
    <label><span data-i18n="kodi.publicBaseUrl"></span><input id="public_base_url" data-i18n-placeholder="kodi.publicBasePlaceholder"></label>
  </div></details></div>
<div class="settings-footer"><p data-i18n="settings.saveHint"></p><div class="status-actions"><button type="button" onclick="saveConfig()" data-i18n="actions.saveConfig"></button><button type="button" class="secondary" onclick="testKodiConnection()" data-i18n="status.testKodi"></button></div></div>
</section>
` + fillerHTML + `
<section class="tab-panel settings-panel" data-panel="logos">
  <div class="status-hero settings-hero">
    <div class="status-icon" aria-hidden="true"><svg viewBox="0 0 32 32" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H7a3 3 0 0 0-3 3v4M21 4h4a3 3 0 0 1 3 3v4M4 21v4a3 3 0 0 0 3 3h4M28 21v4a3 3 0 0 1-3 3h-4"/><circle cx="16" cy="16" r="6"/><path d="m13 16 2 2 4-4"/></svg></div>
    <div class="status-hero-copy"><p class="status-eyebrow" data-i18n="tabs.logos"></p><h2 data-i18n="logos.heading"></h2><p class="status-description" data-i18n="logos.intro"></p></div>
  </div>
  <div class="settings-body">
    <ol class="setup-steps"><li><span>1</span><div data-i18n="logos.stepLoad"></div></li><li><span>2</span><div data-i18n="logos.stepSelect"></div></li><li><span>3</span><div data-i18n="logos.stepSave"></div></li></ol>
    <div class="editor-toolbar"><h3 data-i18n="logos.frameHeading"></h3><button id="refresh-frame" type="button" class="secondary" onclick="refreshFrame()" data-i18n="logos.refreshFrame"></button></div>
    <div class="image-stage logo-stage"><div class="empty-preview"><span class="empty-symbol" aria-hidden="true">▧</span><h3 data-i18n="logos.emptyHeading"></h3><p data-i18n="logos.emptyHint"></p></div><div id="preview-wrap"><img id="preview" draggable="false" data-i18n-alt="logos.previewAlt"><span id="selection"></span></div><div id="preview-loading" class="preview-loading" role="status" hidden><span class="loading-spinner" aria-hidden="true"></span><span data-i18n="logos.loadingFrame"></span></div></div>
    <div class="selection-toolbar"><p id="rect" class="field-hint" role="status"></p><div class="status-actions"><button type="button" onclick="saveReference()" data-i18n="logos.saveReference"></button><button type="button" class="secondary" onclick="optimizeSelection()" data-i18n="logos.optimizeSelection"></button></div></div>
` + logoEditorHTML + `
    <div class="reference-library"><h3 data-i18n="logos.referencesHeading"></h3><p class="card-description" data-i18n="logos.libraryHint"></p><p class="card-description" data-i18n="logos.dataHint"></p><div id="references" class="references muted"></div></div>
  </div>
</section>
<section class="tab-panel settings-panel" data-panel="recognition">
  <div class="status-hero settings-hero">
    <div class="status-icon" aria-hidden="true"><svg viewBox="0 0 32 32" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M5 8h22M5 16h22M5 24h22"/><rect x="10" y="5" width="5" height="6" rx="2" fill="var(--surface)"/><rect x="20" y="13" width="5" height="6" rx="2" fill="var(--surface)"/><rect x="8" y="21" width="5" height="6" rx="2" fill="var(--surface)"/></svg></div>
    <div class="status-hero-copy"><p class="status-eyebrow" data-i18n="tabs.recognition"></p><h2 data-i18n="recognition.heading"></h2><p class="status-description" data-i18n="recognition.intro"></p></div>
  </div>
  <div class="settings-body"><div class="settings-grid">
    <fieldset class="settings-card"><legend data-i18n="recognition.detectionHeading"></legend><p class="card-description" data-i18n="recognition.detectionHint"></p>
<label><span data-i18n="recognition.interval"></span><input id="interval_seconds" type="number" min="1" aria-describedby="interval_seconds-hint"></label><p id="interval_seconds-hint" class="field-hint" data-i18n="recognition.intervalHint"></p><div class="grid"><label><span data-i18n="recognition.missingFrames"></span><input id="missing_frames" type="number" min="1"></label><label><span data-i18n="recognition.presentFrames"></span><input id="present_frames" type="number" min="1"></label></div>
</fieldset>
    <fieldset class="settings-card"><legend data-i18n="recognition.timingHeading"></legend><p class="card-description" data-i18n="recognition.timingHint"></p>
<label><span data-i18n="recognition.cooldown"></span><input id="program_cooldown_seconds" type="number" min="0" aria-describedby="program_cooldown_seconds-hint"></label><p id="program_cooldown_seconds-hint" class="field-hint" data-i18n="recognition.cooldownHint"></p><label><span data-i18n="recognition.minimum"></span><input id="placeholder_min_seconds" type="number" min="0" aria-describedby="placeholder_min_seconds-hint"></label><p id="placeholder_min_seconds-hint" class="field-hint" data-i18n="recognition.minimumHint"></p><label class="checkbox toggle-card"><input id="pause_detection_during_program_lock" type="checkbox" aria-describedby="pause-hint"><span data-i18n="recognition.pauseDuringLock"></span></label><p id="pause-hint" class="field-hint" data-i18n="recognition.pauseHint"></p>
</fieldset>
  </div>
  <details class="settings-advanced"><summary data-i18n="recognition.advancedHeading"></summary><p class="card-description" data-i18n="recognition.advancedHint"></p><div class="grid">
    <label><span data-i18n="recognition.similarity"></span><input id="similarity_minimum" type="number" min="0.1" max="1" step="0.01"></label>
    <label><span data-i18n="recognition.contrast"></span><input id="contrast_strength" type="number" min="0.5" max="3" step="0.1"></label>
  </div><label class="checkbox toggle-card"><input id="debug_enabled" type="checkbox"><span data-i18n="debug.enable"></span></label><p class="field-hint" data-i18n="debug.enableHint"></p></details>
` + channelThresholdHTML + `
  </div>
<div class="settings-footer"><p data-i18n="settings.saveHint"></p><div class="status-actions"><button type="button" onclick="saveConfig()" data-i18n="recognition.save"></button></div></div>
</section>
<dialog id="help-dialog" aria-labelledby="help-title"><div class="help-dialog-header"><h2 id="help-title"></h2><button id="help-close" type="button" data-i18n-aria-label="help.close" data-i18n="help.close"></button></div><div id="help-content" class="help-content"></div></dialog>
<style>
.help-button{display:grid;place-items:center;width:28px;height:28px;min-width:28px;padding:0;margin:0;border:1px solid var(--border);border-radius:50%;background:var(--surface);color:var(--accent);font-size:1rem;line-height:1;box-shadow:none}.help-button:hover{background:var(--surface-alt)}.help-target{position:relative}.help-target>.help-button{position:absolute;top:.8rem;right:.8rem}.help-target.settings-card{padding-right:2.7rem}.help-target.settings-card legend{max-width:calc(100% - 1.8rem)}.help-target.status-card{padding-right:2.7rem}.help-target.source-choice{padding-right:3.5rem}.help-target.mask-workspace,.help-target.reference-library{padding-right:3rem}.help-target .help-button:focus-visible{outline:2px solid var(--accent);outline-offset:3px}.help-details-wrap{position:relative;margin-top:1.4rem}.help-details-wrap>.settings-advanced,.help-details-wrap>.status-details{margin-top:0;padding-right:3.3rem}.help-details-wrap>.help-button{position:absolute;top:.75rem;right:.75rem;z-index:1}.help-inline-row{display:flex;align-items:center;gap:.6rem;margin:.8rem 0 .5rem}.help-inline-row h3{font-size:.95rem;margin:0}.help-inline-row .help-button{margin-left:auto}.help-target.editor-toolbar{padding-right:2.5rem}.help-target.editor-toolbar>.help-button{top:0;right:0}.help-target.reference-library>.help-button{top:1.3rem}.help-target[data-panel="status"]>.help-button{top:1.4rem;right:1.4rem;z-index:1}.help-target[data-panel="status"] .status-topline{padding-right:4.5rem}.help-target[data-panel="status"] .status-kodi-unavailable{padding-right:4rem}#help-dialog{width:min(620px,calc(100% - 2rem));max-height:min(80vh,760px);padding:0;border:1px solid var(--border);border-radius:14px;background:var(--surface);color:var(--text);box-shadow:0 22px 70px #0005}#help-dialog::backdrop{background:#07111f99}.help-dialog-header{display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:1.1rem 1.3rem;border-bottom:1px solid var(--border);position:sticky;top:0;background:var(--surface)}.help-dialog-header h2{margin:0;font-size:1.15rem}.help-dialog-header button{margin:0;background:var(--surface-alt);color:var(--text);border:1px solid var(--border)}.help-content{padding:1.1rem 1.3rem 1.4rem;line-height:1.65;overflow-wrap:anywhere}.help-content p{margin:0 0 .9rem}.help-content p:last-child{margin-bottom:0}@media(max-width:600px){.help-target[data-panel="status"]>.help-button{top:1rem;right:1rem}.help-target[data-panel="status"] .status-topline{padding-right:3.8rem}.help-dialog-header,.help-content{padding-left:1rem;padding-right:1rem}}
.settings-panel{padding:0;overflow:hidden}
.settings-hero{border-bottom:1px solid var(--border)}
.settings-hero h2{font-size:clamp(1.5rem,3.5vw,2rem);line-height:1.2;margin:0}
.settings-body{padding:1.6rem}
.settings-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:1.2rem;align-items:start}
.settings-card{min-width:0;margin:0;padding:1.2rem;border:1px solid var(--border);border-radius:12px}
.settings-card legend{padding:0 .45rem;font-weight:650;font-size:1rem}
.settings-card .grid{grid-template-columns:repeat(2,minmax(0,1fr))}
.settings-card label{color:var(--text);font-size:.85rem;margin:.8rem 0}
.settings-card input,.settings-card select,.settings-card textarea{margin-top:.45rem;font-size:.9rem;font-weight:400}
.settings-panel .grid>label{min-width:0}
.settings-panel textarea{resize:vertical;line-height:1.6}
.card-description,.field-hint{font-size:.85rem;color:var(--muted);line-height:1.65;margin:.3rem 0 1rem}
.field-hint{font-size:.8rem;margin:-.3rem 0 .9rem}
.settings-advanced{margin-top:1.4rem;border:1px solid var(--border);border-radius:12px;padding:1rem 1.2rem;background:var(--surface-alt)}
.settings-advanced summary{cursor:pointer;font-weight:600;font-size:.9rem}
.settings-advanced[open] summary{margin-bottom:1rem}
.settings-advanced input{background:var(--surface)}
.settings-footer{border-top:1px solid var(--border);padding:1.2rem 1.6rem;background:var(--surface-alt);display:flex;align-items:center;justify-content:space-between;gap:1rem;flex-wrap:wrap}
.settings-footer>p{font-size:.8rem;color:var(--muted);margin:0}
.settings-panel .secondary{background:var(--surface-alt);color:var(--text);border:1px solid var(--border)}
.source-choice{display:flex;gap:1.5rem;align-items:center;margin-bottom:1.5rem;padding:1rem 1.2rem;border:1px solid var(--border);border-radius:12px;background:var(--surface-alt)}
.source-choice>label{flex:1;margin:0;color:var(--text)}
.source-choice>p{flex:1;margin:0}
.source-choice select{background:var(--surface)}
.image-stage{display:grid;place-items:center;background:var(--surface-alt);border:1px dashed var(--border);border-radius:12px;overflow:hidden;min-width:0}
.empty-preview{text-align:center;color:var(--muted);padding:1.5rem;max-width:440px;font-size:.85rem;line-height:1.65}
.empty-preview h3{color:var(--text);font-size:1rem;margin:.5rem 0}
.empty-preview p{margin:.4rem 0}
.empty-symbol{display:block;font-size:2.5rem;color:var(--accent);line-height:1.2}
.placeholder-stage{min-height:180px}
.placeholder-stage:has(img[src]) .empty-preview,.logo-stage:has(img[src]) .empty-preview{display:none}
.placeholder-stage #placeholder-preview{width:100%;max-width:100%;max-height:230px;margin:0;border:0;border-radius:0}
.upload-field input{background:var(--surface);font-size:.8rem;padding:.5rem;max-width:100%}
.upload-field input::file-selector-button{border:0;border-radius:5px;background:var(--surface-alt);color:var(--text);font:inherit;font-weight:600;padding:.5rem;margin-right:.7rem;cursor:pointer}
.setup-steps{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:1rem;margin:0 0 1.6rem;padding:0;list-style:none}
.setup-steps li{display:flex;align-items:center;gap:.7rem;font-size:.85rem;color:var(--muted)}
.setup-steps li>span{display:grid;place-items:center;flex:0 0 30px;height:30px;background:var(--surface-alt);border:1px solid var(--border);border-radius:50%;font-weight:650;color:var(--accent)}
.editor-toolbar,.selection-toolbar{display:flex;align-items:center;justify-content:space-between;gap:1rem;flex-wrap:wrap}
.editor-toolbar{margin-bottom:.8rem}
.editor-toolbar h3,.reference-library h3{font-size:1rem;margin:0}
.editor-toolbar button{margin:0}
.editor-toolbar button:disabled{opacity:.6;cursor:wait}
.logo-stage{position:relative;min-height:240px}
.preview-loading:not([hidden]){position:absolute;inset:0;z-index:2;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:1rem;background:var(--surface-alt);color:var(--text);font-size:.9rem;cursor:wait}
.loading-spinner{width:32px;height:32px;border:3px solid var(--border);border-top-color:var(--accent);border-radius:50%;animation:preview-spin .8s linear infinite}
@keyframes preview-spin{to{transform:rotate(360deg)}}
@media(prefers-reduced-motion:reduce){.loading-spinner{animation:none}}
.logo-stage #preview-wrap{display:block;line-height:0;margin:0}
.logo-stage #preview-wrap:not(:has(img[src])){display:none}
.logo-stage #preview{display:block;border:0;border-radius:0;max-height:none}
.selection-toolbar{margin:1rem 0 1.5rem}
.selection-toolbar .field-hint{margin:0;overflow-wrap:anywhere}
.reference-library{border-top:1px solid var(--border);padding-top:1.4rem}
.reference-library .card-description{margin:.5rem 0 1rem}
.references{display:grid;grid-template-columns:repeat(auto-fill,minmax(min(100%,245px),1fr));font-size:.85rem;line-height:1.6}
.references:not(:has(.reference)){display:block;background:var(--surface-alt);border:1px dashed var(--border);padding:1.4rem;text-align:center;border-radius:10px}
.reference{width:auto;min-width:0;padding:.7rem;border-radius:12px}
.reference .reference-open{display:block;width:100%;padding:0;margin:0;border:0;background:transparent}
.reference-data{margin:.7rem 0;font-size:.8rem;color:var(--text);line-height:1.5}
.reference-data>div{margin:.35rem 0;overflow-wrap:anywhere}
.reference-data dt{display:inline;color:var(--muted)}.reference-data dd{display:inline;margin:0}
.reference-hint{font-size:.78rem;line-height:1.5;margin:.7rem 0;color:var(--muted)}
.reference-color-swatch{display:inline-block;width:.85em;height:.85em;border:1px solid var(--border);border-radius:2px;margin-right:.35em;vertical-align:baseline}
.reference:hover,.reference:focus-visible{border-color:var(--accent);outline:2px solid var(--accent);outline-offset:2px}
.reference button{font-size:.8rem;background:var(--surface);color:var(--text);border:1px solid var(--border);margin:0}
.toggle-card{padding:.8rem;border-radius:8px;background:var(--surface-alt);line-height:1.5}
.settings-card .toggle-card input{margin:0;accent-color:var(--accent);flex:0 0 auto;width:18px;height:18px}
@media(max-width:760px){.settings-grid{grid-template-columns:1fr}.settings-footer .status-actions{width:100%}}
@media(max-width:600px){.settings-body{padding:1rem}.settings-card{padding:1rem}.settings-card .grid{grid-template-columns:1fr}.settings-footer{padding:1rem}.settings-advanced{padding:1rem}.settings-advanced .grid{grid-template-columns:minmax(0,1fr)}.source-choice{align-items:stretch;flex-direction:column;gap:.7rem;padding:1rem}.setup-steps{grid-template-columns:1fr;gap:.7rem}.selection-toolbar .status-actions{width:100%}.settings-panel .status-hero{align-items:flex-start}.references{grid-template-columns:repeat(auto-fill,minmax(125px,1fr))}.logo-stage{min-height:200px}}
` + fillerCSS + `
</style>
<script>
let cfg={},rect=null,start=null,formInitialized=false,currentActiveURL='',selectedReferenceID='',messages={},language='en';
let selectedTab='status',previewPageActive=true,previewSessionRequest=Promise.resolve(),previewLoadController=null;
let elapsedClock=null,statusDetailLines=null,statusDetailElapsedIndex=-1;
const previewClientID=Date.now().toString(36)+'-'+Math.random().toString(36).slice(2);
const $=id=>document.getElementById(id),fields=['m3u_url','public_base_url','kodi_url','kodi_username','interval_seconds','missing_frames','present_frames','program_cooldown_seconds','pause_detection_during_program_lock','placeholder_min_seconds','similarity_minimum','contrast_strength','debug_enabled'],supportedLanguages=['de','en'];
function t(key,values={}){let text=messages[key]||key;return text.replace(/\{(\w+)\}/g,(_,name)=>values[name]??'')}
const helpTargets=[
  ['[data-panel="status"]','status.overview'],
  ['[data-panel="status"] .status-card:nth-child(1)','status.playback'],
  ['[data-panel="status"] .status-card:nth-child(2)','status.duration'],
  ['[data-panel="status"] .status-card:nth-child(3)','status.detection'],
  ['[data-panel="status"] .status-content>.status-actions','status.controls','before'],
  ['[data-panel="status"] .logo-diagnostic','status.diagnostic','details'],
  ['[data-panel="status"] .debug-captures','status.debug','details'],
  ['[data-panel="status"] .status-details:not(.logo-diagnostic):not(.debug-captures)','status.details','details'],
  ['[data-panel="kodi"] .settings-card:nth-child(1)','kodi.connection'],
  ['[data-panel="kodi"] .settings-card:nth-child(2)','kodi.channels'],
  ['[data-panel="kodi"] .settings-advanced','kodi.network','details'],
  ['[data-panel="placeholder"] .filler-library','placeholder.source'],
  ['[data-panel="placeholder"] .settings-card:nth-child(1)','placeholder.image'],
  ['[data-panel="placeholder"] .settings-card:nth-child(2)','placeholder.files'],
  ['[data-panel="logos"] .editor-toolbar','logos.editor'],
  ['[data-panel="logos"] .mask-workspace','logos.mask'],
  ['[data-panel="logos"] .reference-library','logos.library'],
  ['[data-panel="recognition"] .settings-card:nth-child(1)','recognition.detection'],
  ['[data-panel="recognition"] .settings-card:nth-child(2)','recognition.timing'],
  ['[data-panel="recognition"] .settings-advanced:not(.channel-thresholds)','recognition.advanced','details'],
  ['[data-panel="recognition"] .channel-thresholds','recognition.channels','details']
];
let helpOpener=null,openHelpKey='';
function renderHelp(key){$('help-title').textContent=t('help.'+key+'.title');let content=$('help-content');content.replaceChildren(...t('help.'+key+'.body').split(/\n\n+/).map(paragraph=>{let p=document.createElement('p');p.textContent=paragraph;return p}))}
function refreshHelpLabels(){document.querySelectorAll('.help-button').forEach(button=>{let label=t('help.open',{topic:t('help.'+button.dataset.help+'.title')});button.setAttribute('aria-label',label);button.title=label});if($('help-dialog').open)renderHelp(openHelpKey)}
function setupHelp(){
  for(let [selector,key,placement] of helpTargets){let target=document.querySelector(selector);if(!target)continue;let button=document.createElement('button');button.type='button';button.className='help-button';button.dataset.help=key;button.textContent='?';button.addEventListener('click',()=>{helpOpener=button;openHelpKey=key;renderHelp(key);$('help-dialog').showModal();$('help-close').focus()});
    if(placement==='details'){let wrapper=document.createElement('div');wrapper.className='help-details-wrap';target.before(wrapper);wrapper.append(target,button)}
    else if(placement==='before'){let row=document.createElement('div');row.className='help-inline-row';let heading=document.createElement('h3');heading.dataset.i18n='help.'+key+'.title';row.append(heading,button);target.before(row)}
    else{target.classList.add('help-target');target.append(button)}
  }
  $('help-close').addEventListener('click',()=>$('help-dialog').close());
  $('help-dialog').addEventListener('close',()=>{helpOpener?.focus();helpOpener=null;openHelpKey=''});
}
function translatePage(){document.querySelectorAll('[data-i18n]').forEach(element=>{let text=t(element.dataset.i18n);if(element.children.length){let node=[...element.childNodes].find(child=>child.nodeType===Node.TEXT_NODE);if(node)node.nodeValue=text;else element.prepend(document.createTextNode(text))}else element.textContent=text});document.querySelectorAll('[data-i18n-placeholder]').forEach(element=>element.placeholder=t(element.dataset.i18nPlaceholder));document.querySelectorAll('[data-i18n-alt]').forEach(element=>element.alt=t(element.dataset.i18nAlt));document.querySelectorAll('[data-i18n-aria-label]').forEach(element=>element.setAttribute('aria-label',t(element.dataset.i18nAriaLabel)));$('rect').textContent=rect?t('logos.selectionSize',{width:rect.w,height:rect.h}):t('logos.noSelection');$('references').textContent=t('common.loading')}
function preferredLanguage(){let languages=navigator.languages?.length?navigator.languages:[navigator.language];return languages.map(value=>value.toLowerCase().split('-')[0]).find(value=>supportedLanguages.includes(value))||'en'}
async function setLanguage(next,manual=false){language=supportedLanguages.includes(next)?next:'en';let response=await fetch('/i18n/'+language+'.json');if(!response.ok)throw Error();messages=await response.json();document.documentElement.lang=language;$('language-select').value=language;translatePage();refreshHelpLabels();applyTheme(localStorage.getItem('tv-logo-monitor-theme')||(matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'));if(manual)localStorage.setItem('tv-logo-monitor-language',language);if(formInitialized){renderFillers();renderChannelThresholds();await status();await refreshReferences()}}
function applyTheme(theme){document.body.classList.toggle('dark',theme==='dark');$('theme-toggle').textContent=t(theme==='dark'?'theme.light':'theme.dark');localStorage.setItem('tv-logo-monitor-theme',theme)}
function toggleTheme(){applyTheme(document.body.classList.contains('dark')?'light':'dark')}
function previewWanted(){return selectedTab==='logos'&&previewPageActive&&!document.hidden}
function syncPreviewSession(){
  if(!previewWanted())previewLoadController?.abort();
  previewSessionRequest=previewSessionRequest.catch(()=>{}).then(()=>api('/api/preview',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({client_id:previewClientID,active:previewWanted()}),keepalive:true}));
  return previewSessionRequest;
}
function selectTab(name){selectedTab=name;document.querySelectorAll('.tab').forEach(tab=>tab.classList.toggle('active',tab.dataset.tab===name));document.querySelectorAll('.tab-panel').forEach(panel=>panel.classList.toggle('active',panel.dataset.panel===name));localStorage.setItem('tv-logo-monitor-tab',name);void syncPreviewSession().catch(()=>{});if(name==='recognition'&&formInitialized)void refreshChannelThresholds();if(name==='logos'&&formInitialized)void refreshReferences()}
function setupUI(){
  setupHelp();
  $('similarity_minimum').addEventListener('input',updateChannelThresholdPlaceholders);
  document.querySelectorAll('.tab').forEach(tab=>tab.addEventListener('click',()=>selectTab(tab.dataset.tab)));
  $('language-select').addEventListener('change',event=>setLanguage(event.target.value,true));
  let tab=localStorage.getItem('tv-logo-monitor-tab');
  if(tab&&document.querySelector('[data-panel="'+tab+'"]'))selectTab(tab);
  setInterval(()=>{if(previewWanted())void syncPreviewSession().catch(()=>{})},5000);
  document.addEventListener('visibilitychange',()=>{void syncPreviewSession().catch(()=>{})});
  window.addEventListener('pagehide',()=>{
    previewPageActive=false;
    previewLoadController?.abort();
    const body=JSON.stringify({client_id:previewClientID,active:false});
    if(!navigator.sendBeacon?.('/api/preview',body))void syncPreviewSession().catch(()=>{});
  });
  window.addEventListener('pageshow',()=>{previewPageActive=true;void syncPreviewSession().catch(()=>{})});
}
function formatSeconds(seconds){let m=Math.floor(seconds/60),s=seconds%60;return m+':'+String(s).padStart(2,'0')}
function syncElapsedClock(status,active){
  const placeholder=['placeholder','manual-placeholder','switching-placeholder'].includes(status.mode);
  if(!active.url&&!placeholder){elapsedClock=null;return}
  const phase=placeholder?'advert':'program';
  const startedAt=placeholder?status.advert_started_at:status.program_started_at;
  const serverSeconds=placeholder?status.advert_elapsed_seconds:status.program_elapsed_seconds;
  if(elapsedClock?.phase===phase&&elapsedClock.startedAt===startedAt)return;
  elapsedClock={phase,startedAt,seconds:startedAt?Math.max(0,serverSeconds||0):0};
}
function renderElapsedClock(){
  if(!elapsedClock)return;
  const time=formatSeconds(elapsedClock.seconds);
  $('status-elapsed').textContent=time;
  if(statusDetailLines){
    statusDetailLines[statusDetailElapsedIndex]=t(elapsedClock.phase==='advert'?'status.advertElapsed':'status.programElapsed',{time});
    $('status').textContent=statusDetailLines.filter(Boolean).join('\n');
  }
}
function tickElapsedClock(){
  if(!elapsedClock)return;
  if(elapsedClock.startedAt)elapsedClock.seconds++;
  renderElapsedClock();
}
async function api(path,opt={}){opt.headers={...(opt.headers||{}),'Accept-Language':language};let r=await fetch(path,opt);if(!r.ok)throw Error(await r.text());return r.headers.get('content-type')?.includes('json')?r.json():r.text()}
function modeName(mode){return t('status.mode.'+(mode||'ready'))}
function renderChannelLogo(active){
  const icon=$('status-channel-icon'),url=active.logo_url||'';
  if(icon.dataset.logo===url)return;
  icon.dataset.logo=url;
  icon.querySelector('img')?.remove();
  const fallback=icon.querySelector('svg');
  fallback.removeAttribute('hidden');
  if(!url)return;
  const img=document.createElement('img');
  img.alt='';img.hidden=true;img.decoding='async';img.referrerPolicy='no-referrer';
  img.onload=()=>{if(img.parentNode!==icon)return;img.hidden=false;fallback.setAttribute('hidden','')};
  img.onerror=()=>{if(img.parentNode!==icon)return;img.remove();fallback.removeAttribute('hidden')};
  icon.append(img);
  img.src=url;
}
function renderProgramme(title){
  const programme=$('status-programme');
  programme.textContent=title||'';
  programme.hidden=!title;
}
function renderStatus(x){
  const s=x.status,active=x.active_channel||{},placeholder=['placeholder','manual-placeholder','switching-placeholder'].includes(s.mode),switching=s.mode?.startsWith('switching-'),enabled=s.running||s.detection_paused;
  const errors=[s.kodi_error?t('status.kodiError',{value:s.kodi_error}):'',s.last_error?t('status.error',{value:s.last_error}):'',s.playlist_error?t('status.playlistError',{value:s.playlist_error}):'',s.debug_error?t('debug.saveError',{error:s.debug_error}):''].filter(Boolean);
  const setup=s.mode==='setup'||(active.url&&!x.reference_count),state=errors.length?'attention':s.manual_placeholder?'manual':!active.url?'waiting':setup?'setup':!enabled?'stopped':s.detection_paused?'paused':'active';
  $('status-badge').textContent=t('status.state.'+state);
  $('status-badge').dataset.tone=errors.length?'error':state==='active'?'good':['manual','setup','paused'].includes(state)?'warm':'neutral';
  $('status-channel').textContent=active.name||t('status.waitingChannel');
  renderChannelLogo(active);
  renderProgramme(active.url?s.kodi_programme:"");
  const description=errors.length?'attention':s.manual_placeholder?'manual':!active.url?'waiting':setup?'setup':!enabled?'stopped':switching?'switching':placeholder?'advert':s.detection_paused?'paused':'watching';
  $('status-description').textContent=t('status.description.'+description);
  $('status-playback').textContent=switching?modeName(s.mode):placeholder?t('status.placeholderPlaying'):active.url?t('status.channelPlaying'):t('status.noPlayback');
  $('status-playback-hint').textContent=placeholder?t(s.manual_placeholder?'status.manualHint':'status.automaticHint'):s.kodi_label||t('status.playbackHint');
  $('status-elapsed-label').textContent=t(placeholder?(s.manual_placeholder?'status.placeholderDuration':'status.advertDuration'):'status.programDuration');
  $('status-elapsed').textContent=active.url||placeholder?formatSeconds((placeholder?s.advert_elapsed_seconds:s.program_elapsed_seconds)||0):'–';
  const detectionLabel=t(setup?'status.state.setup':s.detection_paused?'status.state.paused':!s.running?'status.state.stopped':s.logo_detection?.decision==='uncertain'?'decision.uncertain':s.logo_present==null?'status.checking':s.logo_present?'status.logoFound':'status.logoMissing');
  const score=s.logo_detection?.score;
  $('status-detection').textContent=s.running&&!s.detection_paused&&!setup&&s.logo_present===true&&s.logo_detection?.decision==='present'&&Number.isFinite(score)?t('status.logoFoundPercent',{percent:Math.round(score*100)}):detectionLabel;
  $('status-detection-hint').textContent=t(setup?'status.setupHint':s.detection_paused?'status.pauseHint':!s.running?'status.stoppedHint':'status.referencesHint',{count:x.reference_count||0});
  const notice=s.manual_placeholder?t('status.manualNotice'):s.program_cooldown_active?t('status.cooldownNotice',{time:formatSeconds(s.program_cooldown_remaining||0)}):s.placeholder_min_remaining?t('status.minimumNotice',{time:formatSeconds(s.placeholder_min_remaining)}):'';
  $('status-notice').textContent=notice;$('status-notice').hidden=!notice;
  $('cancel-program-cooldown').hidden=!s.program_cooldown_active;
  renderStatusErrors(errors);
  $('detection-toggle').textContent=t(enabled?'status.stopDetection':'status.startDetection');
  document.querySelectorAll('.status-actions button').forEach(button=>button.disabled=false);
}
function renderStatusErrors(errors){
  const box=$('status-errors');
  if(box.textContent!==errors.join(''))box.replaceChildren(...errors.map(message=>{const p=document.createElement('p');p.textContent=message;return p}));
  box.hidden=!errors.length;
}
function renderStatusError(error){
  elapsedClock=null;
  $('status-badge').textContent=t('status.state.offline');$('status-badge').dataset.tone='error';
  $('status-description').textContent=t('status.offlineHint');
  renderStatusErrors([t('status.offlineHint'),error.message].filter(Boolean));
  document.querySelectorAll('.status-actions button').forEach(button=>button.disabled=true);
}
async function status(syncForm=false){try{let x=await api('/api/status');cfg=x.config;if(syncForm||!formInitialized){for(let f of fields){let k=['kodi_url','kodi_username'].includes(f)?f.slice(5):f;if(cfg[k]!==undefined)['pause_detection_during_program_lock','debug_enabled'].includes(f)?$(f).checked=cfg[k]:$(f).value=cfg[k]}syncFillerForm(cfg,x.filler_timezone);syncChannelThresholdForm(cfg);if(cfg.kodi){$('kodi_url').value=cfg.kodi.url||'';$('kodi_username').value=cfg.kodi.username||''}$('kodi_password').value='';formInitialized=true}let kodiUnavailable=Boolean(x.status.kodi_error);$('status-kodi-unavailable').hidden=!kodiUnavailable;document.querySelectorAll('[data-status-online]').forEach(element=>element.hidden=kodiUnavailable);if(kodiUnavailable){currentActiveURL='';elapsedClock=null;statusDetailLines=null;return}let active=x.active_channel||{},key=active.url||'';if(key!==currentActiveURL){currentActiveURL=key;closeLogoMask();if(captureID)void cancelLogoCapture();refreshReferences()}let s=x.status;syncElapsedClock(s,active);let placeholderActive=['placeholder','manual-placeholder','switching-placeholder'].includes(s.mode),source=t('status.sourceLibrary'),elapsed=placeholderActive?s.advert_elapsed_seconds:s.program_elapsed_seconds,lockState=s.program_cooldown_active?t('status.lockActive',{time:formatSeconds(s.program_cooldown_remaining||0)}):t('status.lockInactive'),detectorActive=s.running||s.detection_paused;$('detection-toggle').textContent=t(s.detection_paused?'status.detectionPaused':s.running?'status.stopDetection':'status.startDetection');$('detection-toggle').dataset.running=String(detectorActive);$('play-sender').hidden=!placeholderActive;$('play-placeholder').hidden=placeholderActive;$('resume-automatic').hidden=!s.manual_placeholder;statusDetailLines=[t('status.channel',{value:active.name||t('status.noChannel')}),t('status.kodiLabel',{value:s.kodi_label||'–'}),t('status.kodiFile',{value:s.kodi_file||'–'}),t('status.placeholderSource',{value:source}),t('status.mode',{value:modeName(s.mode)}),t(placeholderActive?'status.advertElapsed':'status.programElapsed',{time:formatSeconds(elapsed||0)}),t('status.lock',{value:lockState}),t('status.timing',{program:formatSeconds(cfg.program_cooldown_seconds||0),advert:formatSeconds(cfg.placeholder_min_seconds||0)}),t('status.running',{value:t(s.running?'common.yes':'common.no')}),s.detection_paused?t('status.detectionPaused'):'',t('status.logo',{value:s.logo_present===undefined?t('status.unknown'):t(s.logo_present?'common.yes':'common.no')}),t('status.similarity',{value:(s.similarity||0).toFixed(3)}),t('status.matches',{matches:s.matches,misses:s.misses}),s.placeholder_min_remaining?t('status.minimumRemaining',{time:formatSeconds(s.placeholder_min_remaining)}):'',s.kodi_error?t('status.kodiError',{value:s.kodi_error}):'',s.last_error?t('status.error',{value:s.last_error}):'',s.playlist_error?t('status.playlistError',{value:s.playlist_error}):'',t('status.references',{count:x.reference_count||0})];statusDetailElapsedIndex=5;$('status').textContent=statusDetailLines.filter(Boolean).join('\n');renderStatus(x);renderElapsedClock();if($('debug-capture-panel').open)void refreshDebugCaptures();if(previewWanted())void refreshReferences()}catch(e){renderStatusError(e)}}
function collect(){let n={...cfg};n.channel_similarity_minimum=collectChannelThresholds();for(let f of ['m3u_url','public_base_url','interval_seconds','missing_frames','present_frames','program_cooldown_seconds','placeholder_min_seconds','similarity_minimum','contrast_strength'])n[f]=['interval_seconds','missing_frames','present_frames','program_cooldown_seconds','placeholder_min_seconds','similarity_minimum','contrast_strength'].includes(f)?Number($(f).value):$(f).value;n.pause_detection_during_program_lock=$('pause_detection_during_program_lock').checked;n.debug_enabled=$('debug_enabled').checked;n.fillers=structuredClone(fillerDraft);n.fallback_filler_id=fallbackFillerID;n.kodi={...(cfg.kodi||{}),url:$('kodi_url').value,username:$('kodi_username').value,password:$('kodi_password').value};return n}
async function saveConfig(notify=true){try{await api('/api/config',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(collect())});await status(true);await refreshReferences();if(notify)alert(t('alerts.saved'))}catch(e){if(notify)alert(e.message);throw e}}
async function post(p){try{await api(p,{method:'POST'});await status()}catch(e){alert(e.message)}}
async function toggleDetection(){await post($('detection-toggle').dataset.running==='true'?'/api/stop':'/api/start')}
async function testKodiConnection(){try{await saveConfig(false);let result=await api('/api/kodi-test',{method:'POST'});alert(t('alerts.kodiReachable',{endpoint:result.endpoint,result:JSON.stringify(result.result)}))}catch(e){alert(t('alerts.kodiFailed',{error:e.message}))}}
async function refreshFrame(){
  const button=$('refresh-frame');
  if(button.disabled)return;
  const controller=new AbortController();
  previewLoadController=controller;
  button.disabled=true;
  $('preview-loading').hidden=false;
  $('preview-wrap').setAttribute('aria-busy','true');
  try{
    await syncPreviewSession();
    for(let i=0;i<16;i++){
      if(controller.signal.aborted)return;
      let r=await fetch('/api/frame.bmp?'+Date.now(),{signal:controller.signal});
      if(r.ok){
        const img=$('preview'),previous=img.getAttribute('src'),next=URL.createObjectURL(await r.blob());
        img.src=next;
        try{await img.decode()}catch(e){if(previous)img.src=previous;else img.removeAttribute('src');URL.revokeObjectURL(next);throw e}
        if(previous)URL.revokeObjectURL(previous);
        return;
      }
      if(r.status!==404)throw Error(await r.text());
      await new Promise(resolve=>setTimeout(resolve,750));
    }
    throw Error(t('alerts.noFrame'));
  }catch(e){if(!controller.signal.aborted)alert(e.message)}finally{
    previewLoadController=null;
    button.disabled=false;
    $('preview-loading').hidden=true;
    $('preview-wrap').setAttribute('aria-busy','false');
    void status();
  }
}
function previewPoint(e){let r=$('preview').getBoundingClientRect();return{x:(e.clientX-r.left)*1280/r.width,y:(e.clientY-r.top)*720/r.height}}
function showSelection(){if(!rect)return;let img=$('preview'),box=$('selection'),r=img.getBoundingClientRect();box.style.display='block';box.style.left=(rect.x*r.width/1280)+'px';box.style.top=(rect.y*r.height/720)+'px';box.style.width=(rect.w*r.width/1280)+'px';box.style.height=(rect.h*r.height/720)+'px'}
let preview=$('preview');preview.addEventListener('dragstart',e=>e.preventDefault());preview.addEventListener('load',showSelection);preview.addEventListener('pointerdown',e=>{e.preventDefault();preview.setPointerCapture(e.pointerId);start=previewPoint(e);closeLogoMask();rect=null;selectedReferenceID='';$('selection').style.display='none';$('rect').textContent=t('logos.noSelection')});preview.addEventListener('pointermove',e=>{if(!start)return;let end=previewPoint(e);rect={x:Math.round(Math.min(start.x,end.x)),y:Math.round(Math.min(start.y,end.y)),w:Math.round(Math.abs(end.x-start.x)),h:Math.round(Math.abs(end.y-start.y))};showSelection()});preview.addEventListener('pointerup',e=>{if(!start)return;let end=previewPoint(e);rect={x:Math.round(Math.min(start.x,end.x)),y:Math.round(Math.min(start.y,end.y)),w:Math.round(Math.abs(end.x-start.x)),h:Math.round(Math.abs(end.y-start.y))};showSelection();$('rect').textContent=t('logos.selectionSize',{width:rect.w,height:rect.h});start=null})
function selectReference(ref){void openLogoMask(ref.id);rect={...ref.rect};selectedReferenceID=ref.id;showSelection();$('rect').textContent=t('logos.selectionSize',{width:rect.w,height:rect.h});$('preview-wrap').scrollIntoView({behavior:'smooth',block:'center'})}
async function saveReference(){if(!rect)return alert(t('alerts.selectArea'));try{let saved=await api('/api/reference',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(rect)});selectedReferenceID=saved.id;await openLogoMask(saved.id);await status();await refreshReferences();alert(t('alerts.referenceSaved'))}catch(e){alert(e.message)}}
async function optimizeSelection(){if(!rect)return alert(t('alerts.selectArea'));try{rect=await api('/api/optimize-rect',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(rect)});showSelection();$('rect').textContent=t('logos.selectionSize',{width:rect.w,height:rect.h})}catch(e){alert(e.message)}}

` + fillerJS + logoEditorJS + referenceLibraryJS + debugCaptureJS + channelThresholdJS + `
async function bootstrap(){setupUI();try{await setLanguage(localStorage.getItem('tv-logo-monitor-language')||preferredLanguage());await status();setInterval(tickElapsedClock,1000);setInterval(status,3000);await refreshReferences()}catch(e){renderStatusError(e)}}
bootstrap();
</script></html>`
