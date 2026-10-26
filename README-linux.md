# TV Logo Monitor — Linux installation

[Back to the main README](README.md) · [Windows installation](README-windows.md)

These instructions use a shell and build the monitor from source. The package commands below are for Debian/Ubuntu; use your distribution's package manager on other systems.

## 1. Install the prerequisites

Install FFmpeg and, if you want to clone the repository, Git:

```sh
sudo apt update
sudo apt install ffmpeg git
```

Install Go 1.24 or newer using the [official Go installation instructions](https://go.dev/doc/install). Choose the Linux download for your processor architecture and make sure the Go executable is in `PATH`. Your distribution's Go package can also be used if it meets the minimum version.

Open a new shell after changing `PATH`, then check:

```sh
go version
ffmpeg -version
```

Both commands should display their installed version. The monitor starts FFmpeg by name, so it must remain in `PATH` for the user running the application.

Have an M3U/M3U8 channel list URL and a running Kodi instance available. Kodi may run on this computer or another device reachable over the network.

## 2. Download and build the monitor

Choose a writable directory and run:

```sh
git clone https://github.com/tas2580/tvadblock.git
cd tvadblock
go build -o tv-logo-monitor .
```

Without Git, open the [repository on GitHub](https://github.com/tas2580/tvadblock), select **Code → Download ZIP**, extract the archive, and open a shell in the extracted directory containing `go.mod`. Then run the same `go build` command. Build the complete package with the final `.`; `go build main.go` omits required source files.

## 3. Start and configure

From the project directory, start the application:

```sh
./tv-logo-monitor -config config.json -data-dir data
```

On a fresh installation, the application creates `config.json` and the `data` directory automatically. Existing settings and data are reused. Keep the terminal open while the monitor runs; press **Ctrl+C** to stop it. Preserve `config.json` and `data` when updating the executable.

Open <http://127.0.0.1:8099> in your browser and follow [First configuration in the main README](README.md#first-configuration) to connect Kodi and the playlist, select fillers, and save logo references. The interface follows your browser language (German or English) and also offers a language selector.

## 4. Allow access from another device, if needed

If Kodi runs on another device and should display uploaded images, or you want to open the UI from another computer, it must be able to reach the monitor. Stop the monitor and restart it with:

```sh
./tv-logo-monitor -config config.json -data-dir data -http-addr 0.0.0.0:8099
```

Use `ip -4 addr` to find the monitor host's LAN IPv4 address. Open that address in your browser, for example `http://192.168.1.20:8099`. Under **Kodi & Automation**, set **Public base URL of this service** to the same address and save. The Kodi JSON-RPC address still points to the device running Kodi.

If a firewall is enabled, allow inbound TCP port `8099` from your trusted LAN. Kodi's HTTP port, commonly `8080`, must also be reachable from the monitor. The monitor UI has no authentication; limit access to your trusted LAN. When everything runs on the same computer, the default `127.0.0.1:8099` binding is sufficient.

## 5. Run as a systemd service (optional)

On a distribution using systemd, install the built executable and create a dedicated service user. Run these commands from the project directory; create the user only once:

```sh
sudo useradd --system --user-group --home-dir /var/lib/tv-logo-monitor --no-create-home --shell /usr/sbin/nologin tvmonitor
sudo install -d -m 0755 /opt/tv-logo-monitor
sudo install -d -o tvmonitor -g tvmonitor -m 0750 /etc/tv-logo-monitor /var/lib/tv-logo-monitor
sudo install -m 0755 tv-logo-monitor /opt/tv-logo-monitor/tv-logo-monitor
```

If you have already configured the foreground instance, stop it with **Ctrl+C** and copy its settings and data before starting the service:

```sh
sudo cp config.json /etc/tv-logo-monitor/config.json
sudo cp -a data/. /var/lib/tv-logo-monitor/
sudo chown -R tvmonitor:tvmonitor /etc/tv-logo-monitor /var/lib/tv-logo-monitor
```

For a fresh setup, skip the copy commands; the service creates its configuration on startup. The service user must have write access to the configuration file and data directory. Adjust any custom local file paths in your configuration for the service user.

Create `/etc/systemd/system/tv-logo-monitor.service` with:

```ini
[Unit]
Description=TV Logo Monitor
Wants=network-online.target
After=network-online.target

[Service]
User=tvmonitor
Group=tvmonitor
WorkingDirectory=/opt/tv-logo-monitor
ExecStart=/opt/tv-logo-monitor/tv-logo-monitor -config /etc/tv-logo-monitor/config.json -data-dir /var/lib/tv-logo-monitor
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

For LAN access, append `-http-addr 0.0.0.0:8099` to `ExecStart` and configure the public base URL and firewall as described above. Make sure `ffmpeg` is in the service's `PATH`; the Debian/Ubuntu package installs it in `/usr/bin`.

Enable the service at boot and start it:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now tv-logo-monitor.service
sudo systemctl status tv-logo-monitor.service
```

Read the logs with:

```sh
sudo journalctl -u tv-logo-monitor.service -f
```

After editing the unit file, run `sudo systemctl daemon-reload` and `sudo systemctl restart tv-logo-monitor.service`. For later executable updates, stop the service, install the new binary to `/opt/tv-logo-monitor/tv-logo-monitor`, and start the service again. Keep `/etc/tv-logo-monitor/config.json` and `/var/lib/tv-logo-monitor` when updating.

## Troubleshooting

- **`go` or `ffmpeg` is not found:** check the installation and `PATH`. For systemd, FFmpeg must also be available to the service user without relying on your interactive shell configuration.
- **The build reports an unsupported Go version:** install Go 1.24 or newer and check which installation `go version` uses.
- **Port `8099` is already in use:** stop the other monitor instance or choose another port with `-http-addr 127.0.0.1:8100`, then open `http://127.0.0.1:8100`. For LAN use, bind to `0.0.0.0:8100` and update the public base URL and firewall rule accordingly.
- **Kodi cannot be reached:** verify that Kodi is running, HTTP control is enabled, and its IP, port, credentials, and firewall settings match the monitor configuration.
- **Uploaded fillers do not appear on a separate Kodi device:** check the public base URL and firewall. `localhost` and `127.0.0.1` on that device refer to Kodi itself.
- **The systemd service cannot save settings or data:** check ownership and write permissions for `/etc/tv-logo-monitor/config.json` and `/var/lib/tv-logo-monitor`, then inspect the service logs.

For everyday use, detection settings, and diagnostics, continue with the [main README](README.md#operation-and-stream-limit).
