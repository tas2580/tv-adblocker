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

## Operation and stream limit

The service also loads Kodi's TV channel list at startup and every five minutes. It remembers the active PVR channel ID while the placeholder is playing and uses `Player.Open` with `channelid` for both automatic and manual returns. Kodi therefore resumes the TV channel with its channel identity available to web interfaces. Kodi and M3U channel names must match (ignoring case, spaces, and punctuation); the M3U stream URL is still used for logo detection. The optional `active_channel.kodi_channel_id` field in `/api/status` exposes the resolved ID.

If no unique Kodi channel can be resolved, or Kodi explicitly rejects the channel ID, the service falls back to opening the M3U stream URL and logs the reason. In that case Kodi may display a stream instead of a TV channel. Network failures do not trigger a second open request because Kodi may already have switched. Changing the Kodi connection clears cached channel IDs and reloads the channel list.

While the channel is playing, Kodi keeps one connection to the TV server and the monitor keeps another. While the placeholder is playing, only the monitor remains connected to the TV server. Do not start additional Kodi clients for the same channel if the limit of two fetches must be maintained.

The defaults check every two seconds and require three consecutive frames before each switch. Only after an automatically detected advertising break does a ten-minute program lock prevent another switch; it does not apply at service startup, when Kodi changes channel, or when the channel is changed manually. By default, the detector is stopped for this lock period to release its stream connection and save resources; it starts automatically when the lock ends. This can be disabled in “Detection & timing”. The placeholder remains visible for at least 30 seconds after switching. Both timings and the detection thresholds can be changed in the “Detection & timing” tab; `0` disables the respective lock. The status also shows the program or advertising duration since its respective detection.

## Image placeholder

Under “Image placeholder”, you can upload a JPG, PNG, GIF, WebP, or BMP. The service serves it from a URL with the appropriate file extension and writes that URL as the placeholder URL. If Kodi runs on another device, that device must be able to reach the URL: for example, start the service with `-http-addr 0.0.0.0:8099` and, if necessary, replace `localhost` in the automatically set address with the monitor host's LAN IP address.

Alternatively, select a file or URL that Kodi itself can access under “Placeholder in use”, such as `/storage/videos/placeholder.mp4`, `file:///storage/pictures/placeholder.jpg`, or a Kodi-compatible network URL. Enter several Kodi files or URLs—one per line—and select the one to use from the list. These paths are interpreted exclusively from Kodi's perspective.

## Reference images

References belong to the channel currently detected in Kodi. After saving one channel image, choose the logo again from a different image section; the monitor uses the best match among all references for that channel. The UI shows only references for the active channel and can delete them individually. An existing single image from earlier versions is adopted automatically on the first start.

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
