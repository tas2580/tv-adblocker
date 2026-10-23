package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var fillerBrowserTest = flag.Bool("filler-browser", false, "run filler UI tests in headless Chrome")

func TestFillerUITranslations(t *testing.T) {
	for _, language := range []string{"de", "en"} {
		data, err := localeFiles.ReadFile("locales/" + language + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]string
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatal(err)
		}
		keys := []string{"placeholder.start", "placeholder.end", "placeholder.schedule.always", "placeholder.schedule.annual", "placeholder.schedule.once"}
		for _, re := range []*regexp.Regexp{regexp.MustCompile(`data-i18n="([^"]+)"`), regexp.MustCompile(`\bt\('([^']+)'`)} {
			for _, match := range re.FindAllStringSubmatch(fillerHTML+fillerJS, -1) {
				if match[1] != "placeholder." && match[1] != "placeholder.schedule." {
					keys = append(keys, match[1])
				}
			}
		}
		for _, key := range keys {
			if messages[key] == "" {
				t.Errorf("%s missing %s", language, key)
			}
		}
	}
}

// Uses a real DOM and the complete application, with fetch isolated from the
// user's service and Kodi. Chrome is optional on development/CI machines.
func TestFillerBrowserControls(t *testing.T) {
	if !*fillerBrowserTest {
		t.Skip("run with -args -filler-browser to enable headless Chrome")
	}
	chrome, err := exec.LookPath("google-chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	translations, _ := localeFiles.ReadFile("locales/de.json")
	cfg := defaultConfig()
	cfg.Fillers = []Filler{testFiller("a", FillerSchedule{Mode: "always"}), testFiller("b", FillerSchedule{Mode: "always"})}
	cfg.FallbackFillerID = "a"
	config, _ := json.Marshal(cfg)
	prelude := `
const fixtureMessages=` + string(translations) + `;
let fixtureConfig=` + string(config) + `,fixtureUploads=0,fixtureRejectConfig=false;
window.fetch=async(path,options={})=>{
  let value={};
  if(path.startsWith('/i18n/'))value=fixtureMessages;
  else if(path==='/api/config'){if(fixtureRejectConfig)return new Response('invalid date',{status:400});fixtureConfig=JSON.parse(options.body);value={ok:true}}
  else if(path==='/api/status')value={config:structuredClone(fixtureConfig),status:{mode:'monitoring'},active_channel:{},filler_timezone:'Europe/Berlin'};
  else if(path==='/api/references')value=[];
  else if(path==='/api/placeholder-image'){
    const file=options.body.get('image');if(!file)throw Error('missing uploaded image');
    const id='upload-'+(++fixtureUploads),f={id,name:file.name,source:'uploaded-image',image:'placeholder-image-'+String(fixtureUploads).padStart(32,'0')+'.png',enabled:true,schedule:{mode:'always'}};
    fixtureConfig.fillers.push(f);value={filler:f};
  }
  return new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}});
};
window.alert=message=>{throw Error('Unexpected alert: '+message)};
`
	checks := `
<script>
(async()=>{
  const assert=(condition,message)=>{if(!condition)throw Error(message)};
  for(let i=0;!formInitialized&&i<100;i++)await new Promise(resolve=>setTimeout(resolve,10));
  assert(formInitialized,'application bootstrap failed');selectTab('placeholder');
  const cards=()=>[...document.querySelectorAll('.filler-card')];
  assert(cards().length===2,'initial library not rendered');
  assert($('filler-timezone').textContent.includes('Europe/Berlin'),'timezone missing');
  cards()[0].querySelector('input[type=checkbox]').click();
  assert(!collect().fillers[0].enabled,'checkbox did not change selection');
  let mode=cards()[0].querySelector('select');mode.value='annual';mode.dispatchEvent(new Event('change'));
  let dates=[...cards()[0].querySelectorAll('.filler-annual')];
  for(const [i,month,day] of [[0,'12','15'],[1,'01','06']]){
    dates[i].querySelector('select').value=month;dates[i].querySelector('select').dispatchEvent(new Event('change'));
    dates[i].querySelector('input').value=day;dates[i].querySelector('input').dispatchEvent(new Event('input'));
  }
  assert(collect().fillers[0].schedule.start==='12-15'&&collect().fillers[0].schedule.end==='01-06','annual range serialization failed');
  mode=cards()[1].querySelector('select');mode.value='once';mode.dispatchEvent(new Event('change'));
  const once=[...cards()[1].querySelectorAll('input[type=date]')];
  for(const [i,value] of [[0,'2026-12-01'],[1,'2026-12-26']]){once[i].value=value;once[i].dispatchEvent(new Event('input'))}
  assert(collect().fillers[1].schedule.start==='2026-12-01','one-time range not collected');
  await status();assert(collect().fillers[0].schedule.end==='01-06'&&!collect().fillers[0].enabled,'status polling discarded edits');
  $('kodi_placeholders').value='file:///c.jpg\nfile:///c.jpg\nfile:///a.jpg';addKodiFillers();
  assert(fillerDraft.length===3,'adding Kodi paths did not deduplicate');
  const name=cards()[0].querySelector('input:not([type])');name.value='<b>Winter</b>';name.dispatchEvent(new Event('input'));
  assert($('fallback_filler_id').selectedOptions[0].textContent==='<b>Winter</b>','renaming did not update fallback safely');
  assert(!cards()[0].querySelector('b'),'filler name was interpreted as HTML');
  await saveConfig(false);assert(fixtureConfig.fillers[0].schedule.start==='12-15','save did not persist the date range');
  const data=new DataTransfer();data.items.add(new File(['image-one'],'one.png',{type:'image/png'}));data.items.add(new File(['image-two'],'two.png',{type:'image/png'}));$('placeholder_image_file').files=data.files;
  await uploadPlaceholderImage();
  assert(fixtureUploads===2&&cards().length===5,'multiple uploads did not extend the library');
  assert(document.querySelectorAll('.filler-card img').length===2,'image previews missing');
  cards()[0].querySelector('button').click();assert(fallbackFillerID==='','removing standard did not clear fallback');
  await saveConfig(false);assert(fixtureConfig.fillers.length===4&&fixtureConfig.fallback_filler_id==='','removal was not saved');
  cards()[0].querySelector('input[type=checkbox]').click();const draftBefore=JSON.stringify(fillerDraft);
  fixtureRejectConfig=true;let alerts=0;window.alert=()=>alerts++;$('placeholder_image_file').files=data.files;await uploadPlaceholderImage();
  assert(alerts===1&&JSON.stringify(fillerDraft)===draftBefore,'rejected settings before upload discarded unsaved edits');
  assert(!$('upload-fillers').disabled,'rejected upload left the button disabled');
  document.body.dataset.fillerTest='passed';
})().catch(error=>{document.body.dataset.fillerTest='failed';const output=document.createElement('pre');output.id='filler-test-error';output.textContent=error.stack;document.body.append(output)});
</script>`
	page := strings.Replace(indexHTML, "<script>", "<script>"+prelude+"</script><script>", 1)
	page = strings.Replace(page, "</html>", checks+"</html>", 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "fillers.html")
	if err := os.WriteFile(path, []byte(page), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, chrome, "--headless", "--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu", "--no-first-run", "--user-data-dir="+filepath.Join(dir, "profile"), "--virtual-time-budget=3000", "--dump-dom", "file://"+path)
	var stdout, stderr strings.Builder
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("Chrome: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `data-filler-test="passed"`) {
		re := regexp.MustCompile(`(?s)<pre id="filler-test-error">(.*?)</pre>`)
		errorText := re.FindString(stdout.String())
		if errorText == "" {
			errorText = "browser checks did not finish\n" + stderr.String()
		}
		t.Fatal(errorText)
	}
}
