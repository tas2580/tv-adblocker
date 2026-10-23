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

var channelThresholdBrowserTest = flag.Bool("channel-threshold-browser", false, "run channel threshold UI tests in headless Chrome")

func TestChannelThresholdTranslations(t *testing.T) {
	for _, language := range []string{"de", "en"} {
		data, err := localeFiles.ReadFile("locales/" + language + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]string
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatal(err)
		}
		for _, re := range []*regexp.Regexp{regexp.MustCompile(`data-i18n="([^"]+)"`), regexp.MustCompile(`['"]((?:recognition|common)\.[a-zA-Z]+)['"]`)} {
			for _, match := range re.FindAllStringSubmatch(channelThresholdHTML+channelThresholdJS, -1) {
				if messages[match[1]] == "" {
					t.Errorf("%s missing %s", language, match[1])
				}
			}
		}
	}
}

func TestChannelThresholdBrowserControls(t *testing.T) {
	if !*channelThresholdBrowserTest {
		t.Skip("run with -args -channel-threshold-browser to enable headless Chrome")
	}
	chrome, err := exec.LookPath("google-chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	de, _ := localeFiles.ReadFile("locales/de.json")
	en, _ := localeFiles.ReadFile("locales/en.json")
	cfg := defaultConfig()
	cfg.ChannelSimilarityMinimum = map[string]float64{"http://tv.test/a": .72, "http://tv.test/absent": .6}
	config, _ := json.Marshal(cfg)
	prelude := `
const fixtureMessages={de:` + string(de) + `,en:` + string(en) + `};
let fixtureConfig=` + string(config) + `,fixtureRejectPlaylist=false,fixtureConfigWrites=0;
let fixtureChannels=[{name:'<b>Same name</b>',url:'http://tv.test/a'},{name:'Same name',url:'http://tv.test/b'},{name:'Duplicate',url:'http://tv.test/a'}];
window.fetch=async(path,options={})=>{
  let value={};
  if(path.startsWith('/i18n/'))value=fixtureMessages[path.includes('/de.')?'de':'en'];
  else if(path==='/api/config'){fixtureConfigWrites++;fixtureConfig=JSON.parse(options.body);value={ok:true}}
  else if(path==='/api/status')value={config:structuredClone(fixtureConfig),status:{mode:'monitoring'},active_channel:{}};
  else if(path==='/api/playlist'){if(fixtureRejectPlaylist)return new Response('test playlist unavailable',{status:502});value=fixtureChannels}
  else if(path==='/api/references')value=[];
  return new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}});
};
window.alert=message=>{throw Error('Unexpected alert: '+message)};
`
	checks := `
<script>
(async()=>{
  const assert=(condition,message)=>{if(!condition)throw Error(message)};
  const edit=(input,value)=>{input.value=value;input.dispatchEvent(new Event('input'))};
  const inputs=()=>[...document.querySelectorAll('#channel-threshold-list input')];
  for(let i=0;!formInitialized&&i<100;i++)await new Promise(resolve=>setTimeout(resolve,10));
  assert(formInitialized,'application bootstrap failed');
  selectTab('recognition');
  assert($('refresh-channel-thresholds').disabled,'loading state missing');
  await refreshChannelThresholds();
  $('channel-thresholds').querySelector('summary').click();
  assert(inputs().length===2,'channels not loaded or duplicate URL not deduplicated');
  assert(!document.querySelector('#channel-threshold-list b'),'channel name interpreted as HTML');
  assert(inputs()[0].value==='0.72'&&inputs()[1].value==='','override or inherited value missing');
  assert(inputs()[1].placeholder==='0.8','global default placeholder missing');
  edit(inputs()[0],'0.76');
  await status();
  assert(inputs()[0].value==='0.76'&&collect().channel_similarity_minimum['http://tv.test/a']===0.76,'status polling discarded edits');
  edit($('similarity_minimum'),'0.88');
  assert(inputs()[1].placeholder==='0.88','placeholder did not follow edited global default');
  for(const value of ['0','-0.1','1.01']){
    edit(inputs()[1],value);
    let rejected=false;
    try{await saveConfig(false)}catch(error){rejected=error.message===t('recognition.channelSimilarityInvalid')}
    assert(rejected&&fixtureConfigWrites===0,'invalid threshold saved: '+value);
  }
  edit(inputs()[1],'1');
  assert(collect().channel_similarity_minimum['http://tv.test/b']===1,'valid upper boundary rejected');
  edit(inputs()[1],'');
  fixtureRejectPlaylist=true;
  await refreshChannelThresholds();
  assert($('channel-threshold-message').textContent.includes('test playlist unavailable'),'playlist error missing');
  assert(inputs()[0].value==='0.76'&&inputs()[1].value==='','failed refresh discarded edits');
  fixtureRejectPlaylist=false;
  await refreshChannelThresholds();
  await setLanguage('en',true);
  assert(inputs()[0].getAttribute('aria-label').includes('Similarity threshold'),'dynamic labels not translated');
  assert(inputs()[0].value==='0.76','language change discarded edits');
  await saveConfig(false);
  assert(fixtureConfig.channel_similarity_minimum['http://tv.test/a']===0.76&&fixtureConfig.channel_similarity_minimum['http://tv.test/absent']===0.6,'saving lost configured values');
  assert(!('http://tv.test/b' in fixtureConfig.channel_similarity_minimum),'empty value persisted as a threshold');
  await refreshChannelThresholds();
  edit(inputs()[0],'');
  await status();
  assert(!('http://tv.test/a' in collect().channel_similarity_minimum),'polling restored cleared override');
  await saveConfig(false);
  await refreshChannelThresholds();
  assert(inputs()[0].value===''&&inputs()[0].placeholder==='0.88','clearing did not restore the default');
  edit(inputs()[0],'0.69');
  fixtureChannels=[{name:'Same name',url:'http://tv.test/b'}];
  await refreshChannelThresholds();
  assert(collect().channel_similarity_minimum['http://tv.test/a']===0.69,'removing a channel from the list discarded its edit');
  fixtureChannels=[];
  await refreshChannelThresholds();
  assert(inputs().length===0&&$('channel-threshold-message').textContent===t('recognition.noChannels'),'empty playlist state missing');
  await saveConfig(false);
  assert(fixtureConfig.channel_similarity_minimum['http://tv.test/a']===0.69,'saving an empty list discarded overrides');
  document.body.dataset.channelThresholdTest='passed';
})().catch(error=>{document.body.dataset.channelThresholdTest='failed';const output=document.createElement('pre');output.id='channel-threshold-test-error';output.textContent=error.stack;document.body.append(output)});
</script>`
	page := strings.Replace(indexHTML, "<script>", "<script>"+prelude+"</script><script>", 1)
	page = strings.Replace(page, "</html>", checks+"</html>", 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "channel-thresholds.html")
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
	if !strings.Contains(stdout.String(), `data-channel-threshold-test="passed"`) {
		errorText := regexp.MustCompile(`(?s)<pre id="channel-threshold-test-error">(.*?)</pre>`).FindString(stdout.String())
		if errorText == "" {
			errorText = "browser checks did not finish\n" + stderr.String()
		}
		t.Fatal(errorText)
	}
}
