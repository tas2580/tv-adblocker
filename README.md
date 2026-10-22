# TV Logo Monitor

A local Go service that watches an HLS/M3U stream with `ffmpeg` and switches Kodi between a channel and a placeholder when a fixed-position channel logo disappears or reappears.

## Requirements and startup

`ffmpeg` must be available in `PATH`. Kodi needs an accessible JSON-RPC web server (typically `http://kodi-host:8080/jsonrpc`).

```sh
go build -o tv-logo-monitor .
./tv-logo-monitor -config config.json -data-dir data
```

Then open the local UI at `http://127.0.0.1:8099` and configure the M3U8 and Kodi JSON-RPC URLs once. The service loads the M3U8 automatically at startup, reads Kodi's active playback, and monitors the corresponding channel without manual selection. To create the first reference, switch Kodi to the desired channel and select “Load current image”.

The interface follows the browser language by default (German or English). Users can choose either language from the selector in the top-right corner; that choice is retained in the browser.

The status page shows the current programme title below the channel name when Kodi supplies EPG data. It refreshes with playback polling and continues to show the monitored sender's programme while a placeholder is playing. If programme information is unavailable, the line is hidden.

## Operation and stream limit

The service also loads Kodi's TV channel list at startup and every five minutes. It remembers the active PVR channel ID while the placeholder is playing and uses `Player.Open` with `channelid` for both automatic and manual returns. Kodi therefore resumes the TV channel with its channel identity available to web interfaces. Kodi and M3U channel names must match (ignoring case, spaces, and punctuation); the M3U stream URL is still used for logo detection. The optional `active_channel.kodi_channel_id` field in `/api/status` exposes the resolved ID.

If no unique Kodi channel can be resolved, or Kodi explicitly rejects the channel ID, the service falls back to opening the M3U stream URL and logs the reason. In that case Kodi may display a stream instead of a TV channel. Network failures do not trigger a second open request because Kodi may already have switched. Changing the Kodi connection clears cached channel IDs and reloads the channel list.

While the channel is playing, Kodi keeps one connection to the TV server and the monitor keeps another when logo references are available. Without references for the active channel, detection stays in setup mode. Opening the visible “Logo references” tab starts a preview decoder so “Load current image” can use an already decoded frame. Leaving or hiding the tab releases this decoder when no other browser is using the logo editor. Preview sessions expire within 20 seconds if a browser disconnects without notifying the service. Saving the first reference starts detection; deleting the last reference keeps decoding only while a logo editor is open. Normal detection continues independently of browser tabs. While the placeholder is playing, only the monitor remains connected to the TV server. Do not start additional Kodi clients for the same channel if the limit of two fetches must be maintained.

The defaults check every two seconds and require three consecutive frames before each switch. Only after an automatically detected advertising break does a ten-minute program lock prevent another switch; it does not apply at service startup, when Kodi changes channel, or when the channel is changed manually. By default, the detector is stopped for this lock period to release its stream connection and save resources; it starts automatically when the lock ends. This can be disabled in “Detection & timing”. The placeholder remains visible for at least 30 seconds after switching. Both timings and the detection thresholds can be changed in the “Detection & timing” tab; `0` disables the respective lock. The status also shows the program or advertising duration since its respective detection.

While the program protection period is active, the status page offers “End protection now”. This ends only the current period and immediately resumes detection if it was paused by that protection. Manually stopped detection stays stopped; the configured duration still applies after the next automatically detected advertising break.

## Image placeholder

Under “Image placeholder”, you can upload a JPG, PNG, GIF, WebP, or BMP. The service serves it from a URL with the appropriate file extension and writes that URL as the placeholder URL. If Kodi runs on another device, that device must be able to reach the URL: for example, start the service with `-http-addr 0.0.0.0:8099` and, if necessary, replace `localhost` in the automatically set address with the monitor host's LAN IP address.

Alternatively, select a file or URL that Kodi itself can access under “Placeholder in use”, such as `/storage/videos/placeholder.mp4`, `file:///storage/pictures/placeholder.jpg`, or a Kodi-compatible network URL. Enter several Kodi files or URLs—one per line—and select the one to use from the list. These paths are interpreted exclusively from Kodi's perspective.

## Reference images

References belong to the channel currently detected in Kodi. After saving one channel image, choose the logo again from a different image section; the monitor uses the best match among all references for that channel. The UI shows only references for the active channel and can delete them individually. An existing single image from earlier versions is adopted automatically on the first start.

The detector compares weighted logo contours and locally normalized image structure. Background outside the mask is ignored. References keep their aspect ratio during filtered resizing. Detection first checks the last confirmed position and size, then searches nearby in the same corner at 90%, 95%, 100%, 105%, and 110% of the saved size. A different logo design still needs its own reference.

To make a stronger reference, load a current frame, select the logo, and choose **Capture image series · 30 seconds** while the logo remains visible. The existing decoder supplies the crops; no additional stream connection is opened. At least eight frames and three different background samples are required. With detection running, use an interval of three seconds or less. A channel change cancels the capture. Drafts expire after ten minutes. Explicit capture can temporarily use the monitor's decoder slot for preview while detection is stopped or paused; it does not enable automatic switching. Closing the editor releases this preview decoder through the existing lease mechanism.

Review the green mask overlay and transparent preview, then use the include/exclude brush to correct background contours. Brush size, undo, and restoration of the automatic proposal are available. Saving the series creates a new reference. Clicking an existing reference opens its mask for editing. A mask identifies relevant contour areas; the transparent preview does not reconstruct the original foreground colours of a semitransparent TV overlay.

Original `.rgb` references remain intact. Each new reference has a grayscale `.mask.png` sidecar and an `.auto-mask.png` proposal for resetting edits. Older references without sidecars receive an in-memory contour mask, and their existing contrast setting still applies until a mask is saved. Flat or unusable references cannot trigger automatic switching.

The configured similarity threshold has an uncertainty band of ±0.03. Scores at least 0.03 above the threshold confirm presence; scores below its lower edge indicate absence. Shifted or resized matches use at least 0.85 as their base threshold. Uncertain frames reset both confirmation counters and do not switch Kodi. Existing protection periods and consecutive-frame requirements still apply.

Under **Inspect logo detection** on the status page, load a snapshot with its crop, mask, location, scale, edge score, normalized structure score, and contour coverage. Export a ZIP labeled “logo present” or “logo missing” to collect real examples for calibration. Snapshot images and labels refer to the same frame; the last 32 snapshots are retained in memory. Improvements on real broadcasts still need verification against labeled programme and advertisement sequences.

The additive API endpoints are `/api/logo-capture` (POST a rectangle to start; GET/DELETE with `?id=...`; POST with `?id=...` to save), `/api/logo-mask` (GET/PUT with `?id=...` or `?capture=...`; mask bytes are base64 JSON; `view=frame` or `view=transparent` returns an image), and `/api/logo-diagnostic` (GET a snapshot or image; POST `{ "present": true/false }` to export a labeled ZIP). Capture requires an active editor preview lease. `/api/status` adds `status.logo_detection`; uncertain observations omit `logo_present`.

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
