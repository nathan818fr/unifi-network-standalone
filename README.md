# UniFi Network Standalone

The latest UniFi Network application without UniFi OS, for managing a few access points. It installs
as a Home Assistant app, or runs as a single container.

Ubiquiti now only ships Network for UniFi OS, about 18 services built for much more than this. This
project runs Ubiquiti's package, unmodified, with only what it needs: about 650 MB of memory when idle,
measured with no access point connected. Remote access, UI account logins and API keys aren't
available: see [how it works](docs/how-it-works.md).

> **Unofficial and unsupported.** Any Network update may break this, and one day it may not be fixable.
> Access points should keep serving Wi-Fi with the controller down. Please don't contact Ubiquiti's
> support about it.

## Install

### Home Assistant

[![Add repository to my Home Assistant](https://my.home-assistant.io/badges/supervisor_add_addon_repository.svg)](https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2Fnathan818fr%2Funifi-network-standalone)

Or add `https://github.com/nathan818fr/unifi-network-standalone` under **Repositories** in the app store.

Install **UniFi Network (standalone)**, start it, then open the web interface. It needs a 64-bit Home
Assistant OS, and no extra privileges.

### Container

```sh
podman run -d --name unifi --restart=unless-stopped --stop-timeout 120 \
  -p 8443:8443 -p 8080:8080 -p 3478:3478/udp \
  -e UNIFI_HOST_IP=<host address> \
  -v unifi-data:/data \
  ghcr.io/nathan818fr/unifi-network-standalone:latest
```

Then open `https://<host>:8443`. Ports 8880 and 8843 serve the guest portal, if used.

To choose when to update, replace `latest`. `11.0.99` stays on that Network, with only fixes to the rest
of the image. `11` follows Network 11, whose upgrades can migrate your data with no way back.
`11.0.99-build1` never changes.

Don't use `--network=host`: Postgres (no password) and Network's internal port would be open to
anything running on the host, and more unauthenticated ports open to the network.

## First start

**Finish the setup right away:** until an admin exists, anyone who can reach the port is treated as
one. Your browser will warn about the self-signed certificate: that's expected. At the wizard's sign-in
step, choose **Advanced Setup**, then **Skip**, to create a local admin: signing in with a UI account
can't work here.

## Adding devices

Devices don't appear by themselves: Network doesn't listen for their discovery broadcasts here. Point
them at the controller in one of these ways, and they then show up, ready to adopt:

- **DNS:** make the name `unifi` resolve to the host. A device in factory state tries it, and it
  covers future devices.
- **DHCP:** set option 43 on your router to `01:04`, then the host's address in hexadecimal. For
  `192.0.2.10`, that's `01:04:c0:00:02:0a`.
- **SSH:** connect to the device (factory login `ubnt` / `ubnt`) and run
  `set-inform http://<host address>:8080/inform`.

If the host's address isn't known or changes, set **Inform Host Override** in Network's settings.

## Going further

- [Moving from another controller](docs/moving.md): UniFi OS, or another way of running Network.
- [Backups](docs/backups.md): what they hold, and how to restore them.
- [Managing users](docs/users.md): for when you can't log in.
- [How it works](docs/how-it-works.md): what runs, and what isn't available.
- [Goals](GOALS.md): what this project is for, and what it isn't.

## License

The code in this repository is under the [MIT license](LICENSE). UniFi Network is Ubiquiti's software,
under Ubiquiti's own terms. This project is not affiliated with Ubiquiti, and is maintained for the
author's own use.
