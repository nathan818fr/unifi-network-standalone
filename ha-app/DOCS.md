# UniFi Network (standalone)

**Unofficial and unsupported.** Full documentation:
[on GitHub](https://github.com/nathan818fr/unifi-network-standalone).

## First start

1. Start the app. The first start takes a few minutes: wait for **Network is up** in the **Log** tab.
2. Click **Open Web UI**, or open `https://<Home Assistant's address>:8443` from your home network. Your
   browser warns about the certificate: that's expected.
3. **Finish the setup right away:** until then, anyone on your network can create the admin. At the
   sign-in step, choose **Advanced Setup**, then **Skip**, to create a local admin.

Coming from another controller? Restore its backup instead:
[moving](https://github.com/nathan818fr/unifi-network-standalone/blob/main/docs/moving.md).

## Adding access points

Devices don't find the controller by themselves here. From a computer on the same network (on Windows,
in PowerShell), for each one:

```sh
ssh ubnt@<device address>
set-inform http://<host address>:8080/inform
```

- Your router lists the device's address.
- **`<host address>` is in the Log tab**, in the line `the host address is …`. Use that IP address:
  devices can't use `homeassistant.local`.
- The password is `ubnt` for a device in factory state. A device that was managed by another controller
  uses the SSH login set there (in its settings, search for **SSH**).

The device then shows up in Network, ready to adopt. Don't change ports 8080 and 3478 in
**Configuration**: devices are told these.

Stays offline?
[Troubleshooting](https://github.com/nathan818fr/unifi-network-standalone/blob/main/docs/troubleshooting.md#devices-stay-offline-or-stuck-at-adopting).

## Can't log in

In **Configuration**, set **Reset an admin** to the name you log in with, or your email, then restart
the app. The **Log** tab shows a temporary password: **change it once logged in.** The setting then
empties itself.

## Something else

[Troubleshooting](https://github.com/nathan818fr/unifi-network-standalone/blob/main/docs/troubleshooting.md):
the page doesn't open, a restore is refused, and more.
