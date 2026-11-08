# Troubleshooting

Look at the app's log first: a line starting with `error:` says what went wrong, and often what to do.
On Home Assistant, it's the app's **Log** tab; in a container, `podman logs unifi`.

## The web page doesn't open

- **Wait for `Network is up` in the log**: a first start, or the first after an update, takes a few
  minutes, more on a Raspberry Pi.
- Open `https://<host address>:8443`: **https**, and port 8443. The certificate warning is expected: it's
  self-signed.
- On Home Assistant, the page isn't in the sidebar, and doesn't open through Home Assistant's remote
  access: open it from your home network.
- Another controller using the same ports stops this one from starting: stop it, and turn off its
  **Start on boot**. This includes the beta and stable apps of this project.
- A firewall on the host must let in 8443/tcp, 8080/tcp and 3478/udp.

## Devices stay offline, or stuck at "Adopting"

Devices must reach the controller at the address it gives them.

- **Check that address in the log**: at each start, `the host address is …`. If it's wrong or missing:
  - On Home Assistant: in Network's settings, search for **Inform Host Override**, set it to the host's
    IP address (not a name such as `homeassistant.local`), then restart the app.
  - In a container: set `UNIFI_HOST_IP` to the host's IP address, and create the container again.
- **If Inform Host Override is on** in Network's settings, devices get its address instead. A restored
  backup can bring the old controller's: set it to this host's IP address, or turn it off, then restart
  the app.
- Devices already told a wrong address can't learn the new one: run `set-inform` on them again (see
  [adding devices](../README.md#adding-devices)).
- **Give the host a fixed address** in your router: if it changes, devices lose the controller.
- Keep ports 8080 and 3478 as they are on the host: devices are told these. A firewall on the host must
  let them in.
- A device that was managed by another controller ignores this one. Run `set-inform` on it (see
  [adding devices](../README.md#adding-devices)) with the SSH login set in that controller, not
  `ubnt`/`ubnt` (in its settings, search for **SSH**). Or factory reset it.

## Can't log in

- **A UI account's email and password can't work here:** only Network's own users can log in.
- After a restore from UniFi OS, the log says who can log in, and with which password: see
  [moving](moving.md#3-log-in).
- Logged in, but the page stays black: that user has no admin rights.
- Otherwise, see [managing users](users.md): it gives a user a new password, or admin rights back.

## A restore is refused

- **From a newer Network** than this image's: wait for an image with that Network, or try the beta. A
  backup can't be restored on an older Network.
- **An older version**: Network only restores from some versions. Update the old controller, then take a
  new backup.
- **A `.unifi` file**: it's a UniFi OS console backup, convert it first.

See [moving](moving.md#1-get-a-full-network-backup), which also has a way to check a file first.

## It stopped working after an update

- **On Home Assistant:** restore the backup taken before the update. Home Assistant offers to take one
  when updating.
- **In a container:** go back to the exact tag you had, such as `11.0.99-build1`, with your data folder
  from before the update: a new Network changes its data with no way back.

## The "Upgrade to UniFi OS Server" popup

It's expected, and harmless: close it with **Remind Me Later**.

## Still stuck

Look for an existing [issue](https://github.com/nathan818fr/unifi-network-standalone/issues), or open
one with the app's log.
