# UniFi Network Standalone

The latest UniFi Network application without UniFi OS, for managing a few access points. It installs
as a Home Assistant app, or runs as a single container.

Ubiquiti now only ships Network for UniFi OS, about 18 services built for much more than this. This
project runs Ubiquiti's package, with only what it needs: about 650 MB of memory when idle,
measured with no access point connected. Remote access, UI account logins and API keys aren't
available: see [how it works](docs/how-it-works.md).

> **Unofficial and unsupported.** Any Network update may break this, and one day it may not be fixable.
> Access points should keep serving Wi-Fi with the controller down. Please don't contact Ubiquiti's
> support about it.

## Install

### Home Assistant

[![Add repository to my Home Assistant](https://my.home-assistant.io/badges/supervisor_add_addon_repository.svg)](https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2Fnathan818fr%2Funifi-network-standalone)

Or add `https://github.com/nathan818fr/unifi-network-standalone` under **Repositories** in the app store.

Install **UniFi Network (standalone)**, then follow its **Documentation** tab. It needs a 64-bit Home
Assistant (amd64 or aarch64), and no extra privileges.

**UniFi Network (standalone, beta)** gets each build first. It is marked experimental.
It keeps its own data: stop one app before starting the other, as they use the same ports.

### Container

```sh
podman run -d --name unifi --restart=unless-stopped --stop-timeout 120 \
  -p 8443:8443 -p 8080:8080 -p 3478:3478/udp \
  -e UNIFI_HOST_IP=<host address> \
  -v unifi-data:/data \
  ghcr.io/nathan818fr/unifi-network-standalone:latest
```

Then open `https://<host address>:8443`. Ports 8880 and 8843 serve the guest portal, if used.

`UNIFI_HOST_IP` is the host's IPv4 address on your network: devices are told to reach the controller
there. Keep `--stop-timeout 120`: the settings are saved at stop (with Compose: `stop_grace_period: 2m`).

To choose when to update, replace `latest`. `11.0.99` stays on that Network, with only fixes to the rest
of the image. `11` follows Network 11, whose upgrades can migrate your data with no way back.
`11.0.99-build1` never changes. `beta` gets each build before the others.

Don't use `--network=host`: Postgres (no password) and Network's internal port would be open to
anything running on the host, and more unauthenticated ports open to the network.

## First start

The first start takes a few minutes, more on a Raspberry Pi: wait for `Network is up` in the log
(`podman logs -f unifi`, or the app's **Log** tab).

**Finish the setup right away:** until an admin exists, anyone who can reach the port is treated as
one. Your browser will warn about the self-signed certificate: that's expected. At the wizard's sign-in
step, choose **Advanced Setup**, then **Skip**, to create a local admin: signing in with a UI account
can't work here.

## Adding devices

Devices don't find the controller by themselves here: tell each one where it is. **With SSH**, from a
computer on the same network (your router lists the device's address):

```sh
ssh ubnt@<device address>
set-inform http://<host address>:8080/inform
```

The password is `ubnt` for a device in factory state. A device that was managed by another controller
uses the SSH login set there, or needs a factory reset. The device then shows up in Network, ready to
adopt.

For many devices, or future ones:

- **DNS:** make the name `unifi` resolve to the host. A device in factory state tries it.
- **DHCP:** set option 43 on your router to `01:04`, then the host's address in hexadecimal. For
  `192.0.2.10`, that's `01:04:c0:00:02:0a`.

Keep ports 8080 and 3478 as they are on the host: devices are told these. Devices stay offline?
See [troubleshooting](docs/troubleshooting.md#devices-stay-offline-or-stuck-at-adopting).

## Going further

- [Moving from another controller](docs/moving.md): UniFi OS, or another way of running Network.
- [Backups](docs/backups.md): what they hold, and how to restore them.
- [Managing users](docs/users.md): for when you can't log in.
- [Troubleshooting](docs/troubleshooting.md): the page doesn't open, devices stay offline, and more.
- [How it works](docs/how-it-works.md): what runs, and what isn't available.
- [Goals](GOALS.md): what this project is for, and what it isn't.

## License

The code in this repository is under the [MIT license](LICENSE). UniFi Network is Ubiquiti's software,
under Ubiquiti's own terms. This project is not affiliated with Ubiquiti, and is maintained for the
author's own use.
