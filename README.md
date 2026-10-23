# TV Logo Monitor

A local Go service that watches an HLS/M3U stream with `ffmpeg` and switches Kodi between a channel and a placeholder when a fixed-position channel logo disappears or reappears.

## Requirements and startup

Building from source requires Go 1.24 or newer. `ffmpeg` must be available in `PATH`. Kodi needs an accessible JSON-RPC web server (typically `http://kodi-host:8080/jsonrpc`). For Windows, follow the [installation instructions below](#windows-installation-powershell).

```sh
go build -o tv-logo-monitor .
./tv-logo-monitor -config config.json -data-dir data
```

Run `go test ./...` for the automated checks. The optional browser interaction test uses headless Chrome: `go test ./... -args -filler-browser`. It uses fixture data and does not contact Kodi.

Then open the local UI at `http://127.0.0.1:8099` and configure the M3U8 and Kodi JSON-RPC URLs once. The service loads the M3U8 automatically at startup, reads Kodi's active playback, and monitors the corresponding channel without manual selection. To create the first reference, switch Kodi to the desired channel and select “Load current image”.

The interface follows the browser language by default (German or English). Users can choose either language from the selector in the top-right corner; that choice is retained in the browser.

The status page shows the current programme title below the channel name when Kodi supplies EPG data. It refreshes with playback polling and continues to show the monitored sender's programme while a placeholder is playing. If programme information is unavailable, the line is hidden.

## Windows installation (PowerShell)

### 1. Install the prerequisites

- Install a current stable [Go release for Windows](https://go.dev/doc/install) using the MSI installer (at least Go 1.24). Choose the download for your processor architecture.
- Download a Windows build through the **Windows EXE Files** links on the [FFmpeg download page](https://ffmpeg.org/download.html). Extract it so that `ffmpeg.exe` is located at, for example, `C:\Tools\ffmpeg\bin\ffmpeg.exe`.
- Add `C:\Tools\ffmpeg\bin` to your user's `Path`: search the Windows Start menu for **Edit environment variables for your account**, select **Path**, then **Edit → New** and enter the directory containing `ffmpeg.exe`.
- Install [Git for Windows](https://git-scm.com/install/windows) if you want to clone the repository. Alternatively, download and extract the repository ZIP in the next step.

Close and reopen PowerShell after installation and changes to `Path`, then check:

```powershell
go version
ffmpeg -version
```

Both commands should display their installed version. The monitor starts FFmpeg by name, so its directory must remain in `Path` when running the application.

### 2. Download and build the monitor

Choose a writable directory, for example your user profile, and run:

```powershell
Set-Location $env:USERPROFILE
git clone https://github.com/tas2580/tvadblock.git
Set-Location .\tvadblock
go build -o tv-logo-monitor.exe .
```

Without Git, open the repository on GitHub, select **Code → Download ZIP**, extract the archive, and open PowerShell in the extracted directory containing `go.mod`. Then run the same `go build` command. Build the complete package with the final `.`; `go build main.go` omits required source files.

### 3. Start and configure

From the project directory, start the application:

```powershell
.\tv-logo-monitor.exe -config .\config.json -data-dir .\data
```

On a fresh installation, the application creates `config.json` and the `data` directory automatically. Existing settings and data are reused. Keep the PowerShell window open while the monitor runs; press **Ctrl+C** to stop it. Preserve `config.json` and `data` when updating the executable.

Open <http://127.0.0.1:8099> in your browser. The interface follows your browser language and also offers a language selector.

1. In Kodi, open **Settings → Services → Control**, enable **Allow remote control via HTTP**, and note the port, username, and password. See [Kodi's web server settings](https://kodi.wiki/view/Settings/Services/Control).
2. In the monitor's **Kodi & Automation** tab, enter the M3U/M3U8 channel list URL and Kodi's JSON-RPC address, for example `http://127.0.0.1:8080/jsonrpc` when Kodi runs on this PC, or `http://192.168.1.50:8080/jsonrpc` for another device. Enter the Kodi credentials and save the settings. Use the port configured in Kodi.
3. In **Fillers**, upload an image or add a file/URL accessible to Kodi, enable it, and select a standard filler as fallback. Save the selection. Local file paths refer to the device running Kodi.
4. Start a TV channel in Kodi. In **Logo references**, select **Load current image**, mark the channel logo, and select **Save logo**. Repeat for each channel you want to monitor. Saving the first reference starts detection for that channel.

### 4. Allow access from another device, if needed

If Kodi runs on another device and should display uploaded images, it must be able to reach the monitor. Stop the monitor and restart it with:

```powershell
.\tv-logo-monitor.exe -config .\config.json -data-dir .\data -http-addr 0.0.0.0:8099
```

Use `ipconfig` to find the Windows PC's LAN IPv4 address. Under **Kodi & Automation**, set **Public base URL of this service** to that address, for example `http://192.168.1.20:8099`, and save. Use the Windows PC's address here; the Kodi JSON-RPC address still points to the device running Kodi.

Allow inbound TCP port `8099` for the monitor on your private network in Windows Defender Firewall. Kodi's HTTP port, commonly `8080`, must also be reachable from the monitor. The monitor UI has no authentication; limit access to your trusted LAN. When everything runs on the same PC, the default `127.0.0.1:8099` binding is sufficient.

### Troubleshooting

- **`go` or `ffmpeg` is not recognized:** check the installation and `Path`, then reopen PowerShell. For FFmpeg, add the directory containing `ffmpeg.exe`, not the executable itself or the archive's top-level directory.
- **The executable is not found:** run it from the build directory using the PowerShell prefix `.\`, as shown above.
- **Port `8099` is already in use:** stop the other monitor instance or choose another port with `-http-addr 127.0.0.1:8100`, then open `http://127.0.0.1:8100`. For LAN use, bind to `0.0.0.0:8100` and update the public base URL and firewall rule accordingly.
- **Kodi cannot be reached:** verify that Kodi is running, HTTP control is enabled, and its IP, port, credentials, and firewall settings match the monitor configuration.
- **Uploaded fillers do not appear on a separate Kodi device:** check the public base URL and firewall. `localhost` and `127.0.0.1` on that device refer to Kodi itself.

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
