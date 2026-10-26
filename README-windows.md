# TV Logo Monitor — Windows installation

[Back to the main README](README.md) · [Linux installation](README-linux.md)

These instructions use PowerShell and build the monitor from source.

## 1. Install the prerequisites

- Install a current stable [Go release for Windows](https://go.dev/doc/install) using the MSI installer (at least Go 1.24). Choose the download for your processor architecture.
- Download a Windows build through the **Windows EXE Files** links on the [FFmpeg download page](https://ffmpeg.org/download.html). Extract it so that `ffmpeg.exe` is located at, for example, `C:\Tools\ffmpeg\bin\ffmpeg.exe`.
- Add `C:\Tools\ffmpeg\bin` to your user's `Path`: search the Windows Start menu for **Edit environment variables for your account**, select **Path**, then **Edit → New** and enter the directory containing `ffmpeg.exe`.
- Install [Git for Windows](https://git-scm.com/install/windows) if you want to clone the repository. Alternatively, download and extract the repository ZIP in the next step.
- Have an M3U/M3U8 channel list URL and a running Kodi instance available. Kodi may run on this PC or another device reachable over the network.

Close and reopen PowerShell after installation and changes to `Path`, then check:

```powershell
go version
ffmpeg -version
```

Both commands should display their installed version. The monitor starts FFmpeg by name, so its directory must remain in `Path` when running the application.

## 2. Download and build the monitor

Choose a writable directory, for example your user profile, and run:

```powershell
Set-Location $env:USERPROFILE
git clone https://github.com/tas2580/tvadblock.git
Set-Location .\tvadblock
go build -o tv-logo-monitor.exe .
```

Without Git, open the [repository on GitHub](https://github.com/tas2580/tvadblock), select **Code → Download ZIP**, extract the archive, and open PowerShell in the extracted directory containing `go.mod`. Then run the same `go build` command. Build the complete package with the final `.`; `go build main.go` omits required source files.

## 3. Start and configure

From the project directory, start the application:

```powershell
.\tv-logo-monitor.exe -config .\config.json -data-dir .\data
```

On a fresh installation, the application creates `config.json` and the `data` directory automatically. Existing settings and data are reused. Keep the PowerShell window open while the monitor runs; press **Ctrl+C** to stop it. Preserve `config.json` and `data` when updating the executable.

Open <http://127.0.0.1:8099> in your browser and follow [First configuration in the main README](README.md#first-configuration) to connect Kodi and the playlist, select fillers, and save logo references. The interface follows your browser language (German or English) and also offers a language selector.

## 4. Allow access from another device, if needed

If Kodi runs on another device and should display uploaded images, it must be able to reach the monitor. Stop the monitor and restart it with:

```powershell
.\tv-logo-monitor.exe -config .\config.json -data-dir .\data -http-addr 0.0.0.0:8099
```

Use `ipconfig` to find the Windows PC's LAN IPv4 address. Under **Kodi & Automation**, set **Public base URL of this service** to that address, for example `http://192.168.1.20:8099`, and save. Use the Windows PC's address here; the Kodi JSON-RPC address still points to the device running Kodi.

Allow inbound TCP port `8099` for the monitor on your private network in Windows Defender Firewall. Kodi's HTTP port, commonly `8080`, must also be reachable from the monitor. The monitor UI has no authentication; limit access to your trusted LAN. When everything runs on the same PC, the default `127.0.0.1:8099` binding is sufficient.

## Troubleshooting

- **`go` or `ffmpeg` is not recognized:** check the installation and `Path`, then reopen PowerShell. For FFmpeg, add the directory containing `ffmpeg.exe`, not the executable itself or the archive's top-level directory.
- **The executable is not found:** run it from the build directory using the PowerShell prefix `.\`, as shown above.
- **Port `8099` is already in use:** stop the other monitor instance or choose another port with `-http-addr 127.0.0.1:8100`, then open `http://127.0.0.1:8100`. For LAN use, bind to `0.0.0.0:8100` and update the public base URL and firewall rule accordingly.
- **Kodi cannot be reached:** verify that Kodi is running, HTTP control is enabled, and its IP, port, credentials, and firewall settings match the monitor configuration.
- **Uploaded fillers do not appear on a separate Kodi device:** check the public base URL and firewall. `localhost` and `127.0.0.1` on that device refer to Kodi itself.

For everyday use, detection settings, and diagnostics, continue with the [main README](README.md#operation-and-stream-limit).
