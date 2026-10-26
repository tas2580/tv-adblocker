package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var detectionBrowserTest = flag.Bool("detection-browser", false, "run multi-signal detection controls in headless Chrome")

func TestDetectionBrowserControls(t *testing.T) {
	if !*detectionBrowserTest {
		t.Skip("run with -args -detection-browser")
	}
	chrome, err := exec.LookPath("google-chrome")
	if err != nil {
		t.Skip("Chrome unavailable")
	}
	de, _ := localeFiles.ReadFile("locales/de.json")
	en, _ := localeFiles.ReadFile("locales/en.json")
	prelude := `
const fixtureMessages={de:` + string(de) + `,en:` + string(en) + `};
let channel={name:'Test',url:'http://test/channel'},fixtureConfig={detection_mode:'compare',interval_seconds:2,missing_frames:3,present_frames:3,similarity_minimum:.8,contrast_strength:1.5,kodi:{}},writes=[],recording={id:'20261026T200000.000000000Z',settings:{interval_seconds:2,missing_frames:3,present_frames:3},observations:[{time:0,logo:{decision:'present'}}],labels:[]};
let currentRecording=null,rejectRecordingAction=false;
window.fetch=async(path,options={})=>{
 let value={};
 if(path.startsWith('/i18n/'))value=fixtureMessages[path.includes('/de.')?'de':'en'];
 else if(path==='/api/status')value={config:structuredClone(fixtureConfig),status:{mode:'monitoring',running:true},active_channel:channel,recording:structuredClone(currentRecording)};
 else if(path==='/api/config'){fixtureConfig=JSON.parse(options.body);writes.push(fixtureConfig);value={ok:true}}
 else if(path==='/api/references'||path==='/api/playlist'||path==='/api/detection/recordings')value=[];
 else if(path.startsWith('/api/detection/recording')){if(options.method==='POST'){const body=JSON.parse(options.body);writes.push(body);if(rejectRecordingAction)return new Response('test recording action failed',{status:502});currentRecording={...recording,state:body.action==='start'?'recording':'ready',purpose:body.purpose}}value=path.includes('?id=')?recording:currentRecording}
 else if(path==='/api/detection/replay'){const metric={false_switches:0,false_switches_per_hour:0,missed_breaks:0,ad_delays_seconds:[4],return_delays_seconds:[4],unreturned_breaks:0};value={classic:metric,hybrid:metric}}
 return new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}});
};
window.alert=message=>{throw Error('Unexpected alert: '+message)};
`
	checks := `<script>
(async()=>{
const assert=(value,message)=>{if(!value)throw Error(message)};
for(let i=0;!formInitialized&&i<100;i++)await new Promise(r=>setTimeout(r,10));
assert(formInitialized,'bootstrap failed');
assert(!$('detection-record-start').hidden&&!$('detection-record-start').disabled&&$('detection-record-stop').hidden,'idle recording controls incorrect');
assert($('detection_mode').value==='compare'&&$('automatic_references').checked&&$('sender_learning').checked,'defaults missing');
$('detection_mode').value='hybrid';$('channel-auto').value='false';$('channel-learning').value='true';
await status();assert($('detection_mode').value==='hybrid'&&$('channel-auto').value==='false','polling discarded edits');
await saveConfig(false);assert(writes[0].detection_mode==='hybrid'&&writes[0].channel_detection[channel.url].automatic_references===false,'settings not saved');
await setLanguage('de',false);assert($('detection-settings').textContent.includes('Erweiterte Werbeerkennung'),'translation missing');
const beforeRecordingVersion=detectionRecordingVersion;
const starting=recordDetection('start');assert($('detection-record-start').disabled,'start request accepts duplicate clicks');await starting;
assert(writes.some(w=>w.action==='start'&&w.purpose==='validation'),'independent validation not default');
assert($('detection-record-start').hidden&&!$('detection-record-stop').hidden&&!$('detection-record-stop').disabled&&$('detection-purpose').disabled,'running recording controls incorrect');
renderDetectionStatus({config:fixtureConfig,status:{running:true},active_channel:channel,recording:null},beforeRecordingVersion);
assert($('detection-record-start').hidden,'stale status reverted successful start');
detectionRecordingState=null;detectionRecordingKnown=false;renderDetectionRecordingControls();await status();
assert($('detection-record-start').hidden&&!$('detection-record-stop').hidden,'poll did not restore running recording');
rejectRecordingAction=true;await recordDetection('stop');assert($('detection-record-start').hidden&&!$('detection-record-stop').hidden&&!$('detection-record-stop').disabled,'failed stop changed recording controls');rejectRecordingAction=false;
await recordDetection('stop');assert(!$('detection-record-start').hidden&&$('detection-record-stop').hidden&&!$('detection-purpose').disabled,'successful stop did not restore idle controls');
await recordDetection('start');currentRecording.state='ready';await status();
assert(!$('detection-record-start').hidden&&$('detection-record-stop').hidden&&$('detection-record-state').textContent===t('detection.recorded'),'automatic recording end was not reflected');
detectionRecord={...recording,local:true};renderDetectionRecording();addDetectionLabel({start:0,end:1,kind:'program'});
assert(collectDetectionLabels()[0].kind==='program','label controls failed');
await replayDetectionRecording();assert($('detection-report').querySelectorAll('tr').length===3,'metrics table missing');
assert($('detection-acceptance').textContent.includes('steht noch aus'),'acceptance limitation missing');
channel={name:'Second',url:'http://test/second'};await status();assert($('channel-auto').value==='','new channel did not inherit');
document.body.dataset.detectionTest='passed';
})().catch(error=>{document.body.dataset.detectionTest='failed';const output=document.createElement('pre');output.id='detection-test-error';output.textContent=error.stack;document.body.append(output)});
</script>`
	page := strings.Replace(indexHTML, "<script>", "<script>"+prelude+"</script><script>", 1)
	page = strings.Replace(page, "</html>", checks+"</html>", 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "detection.html")
	if err := os.WriteFile(path, []byte(page), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome, "--headless", "--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu", "--no-first-run", "--user-data-dir="+filepath.Join(dir, "profile"), "--virtual-time-budget=3000", "--dump-dom", "file://"+path)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Chrome: %v %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `data-detection-test="passed"`) {
		failure := regexp.MustCompile(`(?s)<pre id="detection-test-error">(.*?)</pre>`).FindString(stdout.String())
		if failure == "" {
			failure = stderr.String()
		}
		t.Fatal(failure)
	}
}
