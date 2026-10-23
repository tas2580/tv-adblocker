# TV Logo Monitor

A local Go service that watches an HLS/M3U stream with `ffmpeg` and switches Kodi between a channel and a placeholder when a fixed-position channel logo disappears or reappears.

## Requirements and startup

`ffmpeg` must be available in `PATH`. Kodi needs an accessible JSON-RPC web server (typically `http://kodi-host:8080/jsonrpc`).

```sh
go build -o tv-logo-monitor .
./tv-logo-monitor -config config.json -data-dir data
```

Run `go test ./...` for the automated checks. The optional browser interaction test uses headless Chrome: `go test ./... -args -filler-browser`. It uses fixture data and does not contact Kodi.

Then open the local UI at `http://127.0.0.1:8099` and configure the M3U8 and Kodi JSON-RPC URLs once. The service loads the M3U8 automatically at startup, reads Kodi's active playback, and monitors the corresponding channel without manual selection. To create the first reference, switch Kodi to the desired channel and select “Load current image”.

The interface follows the browser language by default (German or English). Users can choose either language from the selector in the top-right corner; that choice is retained in the browser.

The status page shows the current programme title below the channel name when Kodi supplies EPG data. It refreshes with playback polling and continues to show the monitored sender's programme while a placeholder is playing. If programme information is unavailable, the line is hidden.

## Operation and stream limit

The service also loads Kodi's TV channel list at startup and every five minutes. It remembers the active PVR channel ID while the placeholder is playing and uses `Player.Open` with `channelid` for both automatic and manual returns. Kodi therefore resumes the TV channel with its channel identity available to web interfaces. Kodi and M3U channel names must match (ignoring case, spaces, and punctuation); the M3U stream URL is still used for logo detection. The optional `active_channel.kodi_channel_id` field in `/api/status` exposes the resolved ID.

If no unique Kodi channel can be resolved, or Kodi explicitly rejects the channel ID, the service falls back to opening the M3U stream URL and logs the reason. In that case Kodi may display a stream instead of a TV channel. Network failures do not trigger a second open request because Kodi may already have switched. Changing the Kodi connection clears cached channel IDs and reloads the channel list.

While the channel is playing, Kodi keeps one connection to the TV server and the monitor keeps another when logo references are available. Without references for the active channel, detection stays in setup mode. Opening the visible “Logo references” tab starts a preview decoder so “Load current image” can use an already decoded frame. Leaving or hiding the tab releases this decoder when no other browser is using the logo editor. Preview sessions expire within 20 seconds if a browser disconnects without notifying the service. Saving the first reference starts detection; deleting the last reference keeps decoding only while a logo editor is open. Normal detection continues independently of browser tabs. While the placeholder is playing, only the monitor remains connected to the TV server. Do not start additional Kodi clients for the same channel if the limit of two fetches must be maintained.

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

The detector compares weighted logo contours and locally normalized image structure. Background outside the mask is ignored. References keep their aspect ratio during filtered resizing. Detection first checks the last confirmed position and size, then searches nearby in the same corner at 90%, 95%, 100%, 105%, and 110% of the saved size. A different logo design still needs its own reference.

To make a stronger reference, load a current frame, select the logo, and choose **Capture image series · 30 seconds** while the logo remains visible. The existing decoder supplies the crops; no additional stream connection is opened. At least eight frames and three different background samples are required. With detection running, use an interval of three seconds or less. A channel change cancels the capture. Drafts expire after ten minutes. Explicit capture can temporarily use the monitor's decoder slot for preview while detection is stopped or paused; it does not enable automatic switching. Closing the editor releases this preview decoder through the existing lease mechanism.

Review the green mask overlay and transparent preview, then use the include/exclude brush to correct background contours. Brush size, undo, and restoration of the automatic proposal are available. Saving the series creates a new reference. Clicking an existing reference opens its mask for editing. A mask identifies relevant contour areas; the transparent preview does not reconstruct the original foreground colours of a semitransparent TV overlay.

Original `.rgb` references remain intact. Each new reference has a grayscale `.mask.png` sidecar and an `.auto-mask.png` proposal for resetting edits. Older references without sidecars receive an in-memory contour mask, and their existing contrast setting still applies until a mask is saved. Flat or unusable references cannot trigger automatic switching.

Under **Detection & timing**, the channel list lets you set an optional similarity threshold for each channel. Leave a field blank to use the current global default from the advanced settings; clearing an existing value restores that default. Save the settings to apply changes to running detection. Values must be greater than 0 and at most 1. Configuration and `/api/config` use the optional `channel_similarity_minimum` object, mapping stream URLs to numbers, for example `{"http://tv.test/stream": 0.75}`. Omit a channel entry to inherit `similarity_minimum`. Existing configurations need no changes. The mapping follows the stream URL, like logo references; a changed URL uses the default until configured again. Entries for channels temporarily absent from the playlist are retained.

The selected channel threshold, or the global default, has an uncertainty band of ±0.03. Scores at least 0.03 above the threshold confirm presence; scores below its lower edge indicate absence. Shifted or resized matches use at least 0.85 as their base threshold. Uncertain frames reset both confirmation counters and do not switch Kodi. Existing protection periods and consecutive-frame requirements still apply. Debug recordings store the selected channel threshold and each frame's effective threshold.

Under **Inspect logo detection** on the status page, load a snapshot with its crop, mask, location, scale, edge score, normalized structure score, and contour coverage. Export a ZIP labeled “logo present” or “logo missing” to collect real examples for calibration. Snapshot images and labels refer to the same frame; the last 32 snapshots are retained in memory. Improvements on real broadcasts still need verification against labeled programme and advertisement sequences.

Enable **Debug mode for automatic switches** under “Detection & timing” to save the last ten evaluated full frames before each successful automatic switch to the placeholder. Open **Inspect saved switches** on the status page to step through each frame with its logo scores, effective threshold, reference, and consecutive-frame counts, or download the whole case as a ZIP. Up to ten cases are retained in `data/debug/` (or the configured `-data-dir`); older cases are removed automatically. Manual switches do not create cases. Disabling debug mode stops new recordings and leaves saved cases available. The JSON metadata contains the channel name but no stream URL or Kodi credentials.

The additive API endpoints are `/api/logo-capture` (POST a rectangle to start; GET/DELETE with `?id=...`; POST with `?id=...` to save), `/api/logo-mask` (GET/PUT with `?id=...` or `?capture=...`; mask bytes are base64 JSON; `view=frame` or `view=transparent` returns an image), and `/api/logo-diagnostic` (GET a snapshot or image; POST `{ "present": true/false }` to export a labeled ZIP). Capture requires an active editor preview lease. `/api/status` adds `status.logo_detection`; uncertain observations omit `logo_present`.

Debug recordings are available through GET `/api/debug-captures`, `/api/debug-captures/{id}`, `/api/debug-captures/{id}/frames/{index}`, and `/api/debug-captures/{id}/download`. The setting is `debug_enabled` in `config.json` and defaults to `false`.

## systemd

```ini
[Unit]
Description=TV Logo Monitor
After=network-online.target

[Service]
WorkingDirectory=/opt/tv-logo-monitor
ExecStart=/opt/tv-logo-monitor/tv-logo-monitor -config /etc/tv-logo-monitor/config.json -data-dir /var/lib/tv-logo-monitor
Restart=always
User=tvmonitor

[Install]
WantedBy=multi-user.target
```

The service intentionally has no authentication for its UI. Keep it on `127.0.0.1` or protect it on the LAN with a firewall or reverse proxy.
