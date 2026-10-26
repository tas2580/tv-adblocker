# TV Logo Monitor

A local Go service that watches TV streams from an M3U/M3U8 playlist with `ffmpeg` and switches Kodi between a channel and a filler when a channel logo disappears or reappears. Logos can move within their saved corner as the broadcast format changes.

The browser interface manages logo references, seasonal fillers, detection settings, and diagnostics. Classic logo detection, multi-signal comparison, automatic reference discovery, and offline replay are available.

## Contents

- [Installation](#installation)
- [First configuration](#first-configuration)
- [Operation and stream limit](#operation-and-stream-limit)
- [Fillers and seasonal images](#fillers-and-seasonal-images)
- [Reference images](#reference-images)
- [Multi-signal detection and automatic references](#multi-signal-detection-and-automatic-references)
- [Recording, labels, and offline comparison](#recording-labels-and-offline-comparison)
- [Development and checks](#development-and-checks)

## Installation

Choose the guide for your operating system:

- [Linux installation](README-linux.md): prerequisites, build, startup, LAN access, and an optional systemd service.
- [Windows installation (PowerShell)](README-windows.md): prerequisites, build, startup, LAN access, and troubleshooting.

Both guides build from source and require Go 1.24 or newer, `ffmpeg` in `PATH`, a reachable M3U/M3U8 channel list, and Kodi with HTTP/JSON-RPC control enabled. Git is optional when downloading the repository as a ZIP.

On the first start, the service creates `config.json` and `data` automatically. Preserve both when updating: they contain settings, logo references, uploaded images, sender profiles, and recordings. Custom locations can be set with `-config` and `-data-dir`.

The service has no authentication for its UI. The default address is `127.0.0.1:8099`; when enabling LAN access, restrict it with a firewall or reverse proxy as described in the installation guides.

## First configuration

After starting the monitor, open <http://127.0.0.1:8099> on the same computer.

1. In Kodi, open **Settings → Services → Control**, enable **Allow remote control via HTTP**, and note the port, username, and password. See [Kodi's web server settings](https://kodi.wiki/view/Settings/Services/Control).
2. In the monitor's **Kodi & Automation** tab, enter the M3U/M3U8 channel list URL and Kodi's JSON-RPC address, for example `http://127.0.0.1:8080/jsonrpc` when Kodi runs on the same computer, or `http://192.168.1.50:8080/jsonrpc` for another device. Enter the Kodi credentials and save the settings. Use the port configured in Kodi.
3. In **Fillers**, upload an image or add a file/URL accessible to Kodi, enable it, and select a standard filler as fallback. Save the selection. Local file paths refer to the device running Kodi. For uploaded images on another Kodi device, configure LAN access using the [Linux](README-linux.md#4-allow-access-from-another-device-if-needed) or [Windows](README-windows.md#4-allow-access-from-another-device-if-needed) guide.
4. Start a TV channel in Kodi. In **Logo references**, select **Load current image**, mark the channel logo, and select **Save logo**. Repeat for each channel you want to monitor. Saving the first reference starts detection for that channel. Compare and Hybrid modes can also [discover references automatically](#multi-signal-detection-and-automatic-references).

The service loads the playlist automatically at startup, reads Kodi's active playback, and monitors the corresponding channel without manual selection. Kodi and playlist channel names must match as described under [Operation and stream limit](#operation-and-stream-limit).

The interface follows the browser language by default (German or English). Users can choose either language from the selector in the top-right corner; that choice is retained in the browser.

The status page shows the current programme title below the channel name when Kodi supplies EPG data. It refreshes with playback polling and continues to show the monitored sender's programme while a filler is playing. If programme information is unavailable, the line is hidden.

## Operation and stream limit

The service also loads Kodi's TV channel list at startup and every five minutes. It remembers the active PVR channel ID while the placeholder is playing and uses `Player.Open` with `channelid` for both automatic and manual returns. Kodi therefore resumes the TV channel with its channel identity available to web interfaces. Kodi and M3U channel names must match (ignoring case, spaces, and punctuation); the M3U stream URL is still used for logo detection. The optional `active_channel.kodi_channel_id` field in `/api/status` exposes the resolved ID.

If no unique Kodi channel can be resolved, or Kodi explicitly rejects the channel ID, the service falls back to opening the M3U stream URL and logs the reason. In that case Kodi may display a stream instead of a TV channel. Network failures do not trigger a second open request because Kodi may already have switched. Changing the Kodi connection clears cached channel IDs and reloads the channel list.

While the channel is playing, Kodi keeps one connection to the TV server and the monitor keeps another when logo references are available. In Classic mode, or with automatic reference discovery disabled, a channel without references stays in setup mode. Compare and Hybrid can instead keep the decoder running to discover and validate references automatically. Opening the visible “Logo references” tab starts a preview decoder so “Load current image” can use an already decoded frame. Leaving or hiding the tab releases this decoder when no other browser is using the logo editor. Preview sessions expire within 20 seconds if a browser disconnects without notifying the service. Saving the first reference starts detection; deleting the last reference keeps decoding only while a logo editor is open. Normal detection continues independently of browser tabs. While the placeholder is playing, only the monitor remains connected to the TV server. Do not start additional Kodi clients for the same channel if the limit of two fetches must be maintained.

The defaults check every two seconds and require three consecutive frames before each switch. Only after an automatically detected advertising break does a ten-minute program lock prevent another switch; it does not apply at service startup, when Kodi changes channel, or when the channel is changed manually. By default, the detector is stopped for this lock period to release its stream connection and save resources; it starts automatically when the lock ends. This can be disabled in “Detection & timing”. The placeholder remains visible for at least 30 seconds after switching. Both timings and the detection thresholds can be changed in the “Detection & timing” tab; `0` disables the respective lock. The status also shows the program or advertising duration since its respective detection.

While the program protection period is active, the status page offers “End protection now”. This ends only the current period and immediately resumes detection if it was paused by that protection. Manually stopped detection stays stopped; the configured duration still applies after the next automatically detected advertising break.

## Fillers and seasonal images

The “Fillers” tab manages one shared list of uploaded images and Kodi files or URLs. Upload one or several JPG, PNG, GIF, WebP, or BMP images (up to 20 MB each); uploads add new entries without replacing previous images. Kodi paths and URLs can be added one per line, for example `/storage/videos/placeholder.mp4` or `file:///storage/pictures/placeholder.jpg`. These paths are interpreted from Kodi's perspective. Enable entries with their checkboxes and save the selection.

Each automatic or manual switch randomly chooses one eligible filler for the entire break. Enabled seasonal fillers take priority over year-round entries. When at least two fillers are eligible, the last successfully played entry is excluded. The last selection is remembered until the service restarts.

Each filler can be year-round, repeat annually using month/day boundaries, or have a one-time start and end date including the year. Both boundary days are included, using the server's local time zone shown in the UI. Annual ranges can cross New Year; February 29 only matches in leap years. A date change does not interrupt a filler already playing.

Select a standard filler as a fallback. It is used when no enabled filler matches the current date, regardless of its own enabled state or date range. If neither an eligible entry nor a standard exists, the channel keeps playing and the switch reports an error. Removing the standard clears the fallback selection. Removing an entry leaves its stored image on disk and does not interrupt ongoing playback.

Uploaded images use the public base URL under “Kodi & automation”. Kodi must be able to reach this address: for example, start the service with `-http-addr 0.0.0.0:8099` and use the monitor host's LAN IP instead of `localhost` when Kodi runs on another device. Uploading saves current settings first; successfully added images are kept if a later upload fails.

Configuration uses a `fillers` array (ID, name, source, image or target, enabled, schedule) and `fallback_filler_id`. Schedule modes are `always`, `annual` (`MM-DD` boundaries), and `once` (`YYYY-MM-DD` boundaries). Existing single-image and Kodi-file configurations are migrated automatically, keeping the selected filler enabled and as the standard, with other entries initially disabled. An explicit empty array stays empty; legacy fields do not restore removed entries.

## Reference images

References belong to the channel currently detected in Kodi. After saving one channel image, choose the logo again from a different image section; the monitor uses the best match among all references for that channel. The UI shows only references for the active channel and can delete them individually. An existing single image from earlier versions is adopted automatically on the first start.

Each saved-logo card lists the available image, capture type, saved or generated mask, mask for resetting, usable shape, and current colour-learning progress. Missing or damaged essential data includes advice to review the mask or capture a new image series. Missing colour data alone does not require recapturing: it is learned automatically during monitoring and resets with the decoder session. The cards update while the logo tab is visible. GET `/api/references` keeps its existing array and reference fields and adds a `data` object with these availability states and the per-reference colour snapshot; these details are not stored in `references.json`.

The detector compares weighted logo contours and locally normalized image structure. Background outside the mask is ignored. References keep their aspect ratio during filtered resizing. Detection first checks the last confirmed position and size, then the original position, then searches nearby in the same corner at 90%, 95%, 100%, 105%, and 110% of the saved size. If no confirmed match is found, the search expands to the rest of that corner (the quadrant containing the saved logo's centre). This handles larger position changes when the broadcast format changes, without requiring new references or settings. Black bars are not used to determine the search area. A different logo design still needs its own reference.

A match found by the expanded search must appear in three consecutive evaluated frames at the same size step and within four pixels horizontally and vertically of its first position. Until then, the result is `uncertain` with `uncertainty_reason: "logo_position_pending"`: it resets the switching counters, does not switch Kodi, and does not contribute to colour learning. A missing, uncertain, or different-position match breaks the confirmation series. Once confirmed, the position is tracked and checked directly on subsequent frames; a confirmed match at the original position or in the nearby search area can still be accepted immediately. Existing switching confirmation counts and protection periods continue to apply. The pending-position explanation appears in the diagnostic and saved-switch views and their JSON/ZIP exports.

To make a stronger reference, load a current frame, select the logo, and choose **Capture image series · 30 seconds** while the logo remains visible. The existing decoder supplies the crops; no additional stream connection is opened. At least eight frames and three different background samples are required. With detection running, use an interval of three seconds or less. A channel change cancels the capture. Drafts expire after ten minutes. Explicit capture can temporarily use the monitor's decoder slot for preview while detection is stopped or paused; it does not enable automatic switching. Closing the editor releases this preview decoder through the existing lease mechanism.

The series learns a contour mask across the captured frames, then selects the original image that best matches the other frames. Candidates are ranked by confirmed matches, lower-quartile score, and mean score; ties keep the earlier image. This preserves details that a pixel-wise median can erase, such as SAT.1's individual logo segments. The editor previews and saves that selected image with the series mask. Existing references are kept as saved; capture a new series to benefit from the improved selection. Detection thresholds are unchanged.

Review the green mask overlay and transparent preview, then use the include/exclude brush to correct background contours. Brush size, undo, and restoration of the automatic proposal are available. Saving the series creates a new reference. Clicking an existing reference opens its mask for editing. A mask identifies relevant contour areas; the transparent preview does not reconstruct the original foreground colours of a semitransparent TV overlay.

Original `.rgb` references remain intact. Each new reference has a grayscale `.mask.png` sidecar and an `.auto-mask.png` proposal for resetting edits. Older references without sidecars receive an in-memory contour mask, and their existing contrast setting still applies until a mask is saved. Flat or unusable references cannot trigger automatic switching.

Under **Detection & timing**, the channel list lets you set an optional similarity threshold for each channel. Leave a field blank to use the current global default from the advanced settings; clearing an existing value restores that default. Save the settings to apply changes to running detection. Values must be greater than 0 and at most 1. Configuration and `/api/config` use the optional `channel_similarity_minimum` object, mapping stream URLs to numbers, for example `{"http://tv.test/stream": 0.75}`. Omit a channel entry to inherit `similarity_minimum`. Existing configurations need no changes. The mapping follows the stream URL, like logo references; a changed URL uses the default until configured again. Entries for channels temporarily absent from the playlist are retained.

The selected channel threshold, or the global default, has an uncertainty band of ±0.03. Scores at least 0.03 above the threshold confirm presence; scores below its lower edge indicate absence. Shifted or resized matches use at least 0.85 as their base threshold. Uncertain frames reset both confirmation counters and do not switch Kodi. Existing protection periods and consecutive-frame requirements still apply. Debug recordings store the selected channel threshold and each frame's effective threshold.

The detector also learns the foreground logo colour automatically from at least six confirmed matches with changing backgrounds, keeping up to eight distinct crops per reference in memory. It fits the colour mixture across logo edges to account for transparency; simply averaging reference pixels would retain the programme background. Once calibrated, an otherwise missing logo is checked at its last confirmed and original positions in small neighbourhoods along the masked contours. Local colour medians, median absolute deviations and central colour ranges tolerate isolated background edges and sky gradients while rejecting texture. Protection requires at least 50% of the weighted contour to be low-contrast across at least three spatial cells, with low-contrast and remaining matching contours together covering at least 85%. This also protects a logo that is only partly hidden against a white sky; missing contours in clearly observable areas still count against protection. Colour alone never confirms presence, moves the tracked position, trains the colour model or returns from a filler. The protection lasts while the area remains ambiguous, so advertising with a matching background may be detected later. Until calibration succeeds, the existing shape detection applies. Calibration restarts when the decoder restarts, the channel changes, or references are reloaded; existing references and configuration need no changes.

Under **Inspect logo detection** on the status page, load a snapshot with its crop, mask, location, scale, edge score, normalized structure score, and contour coverage. Export a ZIP labeled “logo present” or “logo missing” to collect real examples for calibration. Snapshot images and labels refer to the same frame; the last 32 snapshots are retained in memory. Improvements on real broadcasts still need verification against labeled programme and advertisement sequences.

Enable **Debug mode for automatic switches** under “Detection & timing” to save the last ten evaluated full frames before each successful automatic switch to the placeholder or back to the channel. Open **Inspect saved switches** on the status page and select **Channel → Advertisement** or **Advertisement → Channel** to inspect the cases for that direction. Step through each frame with its logo scores, effective threshold, reference, and consecutive-frame counts, or download the whole case as a ZIP. Up to ten cases across both directions are retained in `data/debug/` (or the configured `-data-dir`); older cases are removed automatically. Manual switches do not create cases. Disabling debug mode stops new recordings and leaves saved cases available. The JSON metadata contains the channel name and `direction` (`to-placeholder` or `to-sender`), but no stream URL or Kodi credentials. Existing cases without a direction are treated as switches to the placeholder.

The additive API endpoints are `/api/logo-capture` (POST a rectangle to start; GET/DELETE with `?id=...`; POST with `?id=...` to save), `/api/logo-mask` (GET/PUT with `?id=...` or `?capture=...`; mask bytes are base64 JSON; `view=frame` or `view=transparent` returns an image), and `/api/logo-diagnostic` (GET a snapshot or image; POST `{ "present": true/false }` to export a labeled ZIP). Capture requires an active editor preview lease. `/api/status` adds `status.logo_detection`; uncertain observations omit `logo_present`.

Logo results optionally include `logo_color` with `rgb` (three numbers from 0 to 255), `confidence` (agreement of the colour estimates from 0 to 1), and `samples` (calibration frame count). `uncertainty_reason: "logo_color_low_contrast"` identifies colour-based protection. These values appear in the diagnostic and saved-switch views and their JSON/ZIP exports; older recordings remain readable.

Logo results also include optional `rejection_reason` and `position_checks` diagnostics. Each position check preserves a direct shape `result` at the last tracked or original position, even when a failed search selects a stronger background candidate elsewhere. Its optional `contrast` object reports `hidden_coverage`, `visible_coverage`, `supported_coverage` (weighted fractions from 0 to 1), `hidden_cells`, `total_cells`, and `protected`. Contrast checks require colour calibration and are omitted for confirmed shape matches. Rejection reasons distinguish invalid regions or references, insufficient edge energy, insufficient spatial support or contour coverage, and similarity below or within the threshold's uncertainty band. The same evidence is displayed in live diagnostics and saved switches and retained in JSON/ZIP exports. Existing fields keep their meanings; old recordings need no migration.

Debug recordings are available through GET `/api/debug-captures`, `/api/debug-captures/{id}`, `/api/debug-captures/{id}/frames/{index}`, and `/api/debug-captures/{id}/download`. The setting is `debug_enabled` in `config.json` and defaults to `false`.

## Multi-signal detection and automatic references

Under **Detection & timing**, select one of three modes:

- **Classic** keeps the existing consecutive-logo decisions.
- **Compare** runs both decision engines and lets only Classic control Kodi.
- **Multiple signals** (`hybrid`) uses the new engine to control Kodi.

Existing configurations stay in Classic. A new installation starts in Compare;
changing mode requires saving the settings. In Compare and Hybrid, automatic
reference discovery and sender learning default to enabled. Both have global
switches and optional overrides for the current sender.

The new engine retains the configured logo confirmation counts and playback
locks. An unequivocal logo absence can switch at the usual confirmation time.
Recent uncertainty starts an additional decision window of at most ten seconds
from the usual switch point. Fresh strong absence evidence or corroborating
transition signals can resolve it sooner. Unresolved cases keep the channel
playing; another attempt requires fresh confirming evidence. Black pictures,
silence, rapid cuts, format changes, and learned durations never switch playback
on their own. Return still requires confirmed logo presence and the configured
minimum placeholder duration.

The same FFmpeg input supplies RGB images and native-rate measurements of black
pictures, silence, scene changes, and visible-format changes where the installed
filters support them. This opens no additional stream connection. Signal
availability appears under **Signals, comparison and recordings**; streams without
audio continue with video signals. Restarted or discontinuous decoder timelines
discard old evidence. Paused detection does not collect observations or learn.

Automatic reference discovery examines compact, recurring contours in the four
outer image quarters. It requires at least 30 seconds, eight samples, and three
backgrounds to create a candidate. The actual mask must span at least 20 by 20
pixels in the 1280-by-720 decoder image and contain at least 200 pixels with mask
weight above 100. Padding does not count. A covariance eigenvalue ratio of at
least 0.05 rejects nearly straight contours, including diagonal lines.
A separate five-minute validation window follows immediately, with at least
30 checks and at least 90% confirmed matches. More than ten seconds without
confirmation rejects a candidate; the final check must also confirm it.
Decoder changes, backwards timestamps, and gaps longer than the greater of ten
seconds or three configured check intervals restart discovery. There is no
unchecked waiting period; the earliest activation is after about 5.5 minutes.
Only then is the reference saved and used. There are at most four automatic
references per sender. Reference cards show origin and validation and allow
disabling, editing, or deleting a reference.
Deleting an automatic reference suppresses rediscovery in that region until the
sender profile is reset.

On service startup, active automatic references that fail the shape requirements
are removed from the index together with their images and masks, freeing learning
slots without suppressing those regions. Manual and disabled references remain
untouched. Unreadable images or masks are retained and reported as errors.
Suitable existing automatic references do not need to repeat the longer validation.

Automatic references are cleaned up when they stop appearing. A changed Kodi
programme title must appear in two consecutive successful EPG polls. Existing
active automatic references that do not match during five minutes of observed
programme time after that change are removed. A match during this review keeps
the reference. The first known title and missing EPG data do not start a review.
Independently of EPG, an active automatic reference expires after 30 minutes of
observed programme time without a match; each match resets that timer. Manual
and disabled references are retained. Time counts only while the sender is
playing, detection is running, and another logo confirms programme content.
Advertisements, protection periods, independent validation recordings, decoder
interruptions, and uncertain observations do not age references. An uncertain
result pauses that reference's timer even when another logo is visible.
Cleanup removes the saved images and masks and frees discovery slots without
restarting the decoder or suppressing the region, so recurring designs can be
learned again. Timers are kept in memory and reset on service restart, channel
change, or a change to the Kodi connection.

Sender profiles activate after five complete, unambiguous logo-presence/absence/
presence cycles. They retain typical break durations and observed transition
frequency. Only corroborating signals within the additional decision window are
affected; similarity thresholds, confirmation counts, and lock durations are not
changed automatically. Profiles and suppressed regions are stored atomically in
`data/sender-profiles.json`, keyed by stream URL like references. Resetting a
sender profile clears both its learned values and suppressed regions.

Configuration adds `detection_mode` (`classic`, `compare`, `hybrid`), optional
`automatic_references` and `sender_learning` booleans, and optional
`channel_detection` overrides keyed by stream URL, for example:

```json
{
  "detection_mode": "compare",
  "automatic_references": true,
  "sender_learning": true,
  "channel_detection": {
    "http://tv.test/channel": {"automatic_references": false}
  }
}
```

`GET /api/status` adds `status.detection` containing both decisions, their reasons
and confirmation counts, remaining decision time, current signals, and learning
state. Existing status fields retain their meanings.
The top-level `recording` field reports the current recording's ID, state, and
purpose, or `null` before the first recording. This keeps recording controls in
sync after reloads and automatic stops without transferring stored observations.
Saved-switch JSON/ZIP files include the same evidence when available. Reference metadata adds `automatic`,
`validated_at`, `validation_rate`, and `disabled`; older references remain usable.
`GET /api/detection/profile` reads the active sender's profile; `DELETE` resets it.
`PATCH /api/references/{id}` with `{"disabled":true}` disables an automatic reference.

## Recording, labels, and offline comparison

Under **Signals, comparison and recordings**, explicitly start a recording in
Compare or Hybrid mode, then stop and save it. Recordings contain observations
and JPEG preview images, up to ten minutes or 600 observations and at most 32 MB
of images. They are saved under `data/observations/`. Normal monitoring does not
record video or audio.

Choose **Independent validation** for held-out material: sender learning and
automatic reference discovery pause during that recording. **Training recording**
allows those adaptations to continue. The recording stores the initial sender
profile; replays keep it fixed and do not learn from evaluation labels. Keep
training and validation recordings separate.

Load a saved recording, inspect its images using the sample slider, and label
non-overlapping intervals as Program or Commercial. Times are seconds from the
first observation; unlabeled intervals are excluded from quality measurements.
Save labels and evaluate both engines. Reports include false switches per observed
program hour, missed breaks, detection and return delays, and breaks without an
observed return. Download a ZIP including images or a standalone observations JSON.
Configuration changes, stopping detection, and channel changes finish a recording.

The standalone replay never starts Kodi automation or contacts a TV stream:

```sh
./tv-logo-monitor -replay observations.json
./tv-logo-monitor -replay detection-recording.zip
./tv-logo-monitor -replay recording.mkv -replay-reference REFERENCE_ID -replay-labels labels.json -data-dir data
```

For a local video file, use an existing reference's ID and the matching channel
geometry. Video replay uses the saved channel threshold and current timing
configuration; JSON/ZIP replay uses its recorded timings. Optional labels use
`[{"start":0,"end":60,"kind":"program"},{"start":60,"end":180,"kind":"ad"}]`.
The report is written as JSON to stdout. Local-video replay allows only local
file/pipe protocols and performs no profile or reference writes.

Recording API: `POST /api/detection/recording` with
`{"action":"start","purpose":"validation"}` or `{"action":"stop"}`;
`GET /api/detection/recordings` lists saved recordings;
`GET /api/detection/recording?id=ID` reads one (add `download=1` for its ZIP);
`PUT` at that URL saves `{"labels":[...]}`.
`POST /api/detection/replay` accepts a recording and returns the comparison report.

The known DMAX, ProSieben, and SAT.1 fixtures cover logo regressions, not real ad
recall. Empirical acceptance remains pending until separate, labeled real
transitions demonstrate no additional false switches or missed breaks compared
with Classic. The report explicitly retains that limitation. Synthetic and local
video integration tests do not establish improved real-world detection.

## Development and checks

Build the complete package from the directory containing `go.mod`, as described in the [Linux](README-linux.md) and [Windows](README-windows.md) installation guides. Building only `main.go` omits required source files.

Run the automated checks with:

```sh
go test ./...
```

With Chrome installed, run the optional browser interaction checks for fillers and detection controls:

```sh
go test ./... -args -filler-browser -detection-browser
```

These browser checks use fixture data and do not contact Kodi.
