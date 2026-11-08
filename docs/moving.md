# Moving from another controller

## 1. Get a full Network backup

**From UniFi OS:** download a console backup from **Control Plane > Backups > Download**. Network can't
restore it as is: convert it, offline, on any computer with Docker or Podman (on Windows or a Mac: Docker
Desktop). In the folder that holds the file, run, with the tag of the image you'll restore on (`beta`
for the beta app):

```sh
docker run --rm -v "${PWD}:/out:Z" -w /out --entrypoint unifi-export \
  ghcr.io/nathan818fr/unifi-network-standalone:latest --from-console unifi_os_backup_<...>.unifi
```

On Windows, in PowerShell, put it on one line, without the `\`. It writes
`network-backup-<version>.unf` next to it, already checked: that's the file to restore.

**From a standalone controller:** use the backup from its settings.

<details>
<summary>Other way: download it from the running controller</summary>

This needs a local admin of the old controller, not a UI account. It asks for the password, and a 2FA
code if needed:

```sh
docker run --rm -it -v "${PWD}:/out:Z" -w /out --entrypoint unifi-export \
  ghcr.io/nathan818fr/unifi-network-standalone:latest https://<controller address> -u <local admin> \
  --cacert <certificate file>
```

For a standalone controller, add its port: `https://<controller address>:8443`. `--cacert` takes the
controller's certificate, saved in this folder. Without it, `--insecure` works too, but the password
could then be intercepted. The file is written and checked as above.

</details>

| Backup                                 | Result                                    |
| -------------------------------------- | ----------------------------------------- |
| Full `.unf` from a version it restores | Accepted                                  |
| Full `.unf` from a version it doesn't  | Refused: upgrade the old controller first |
| Newer than this version                | Refused: update this image first          |
| Console backup (`.unifi`)              | Refused: convert it first                 |
| Cloud backups                          | Not available                             |
| Site export from Network 10.x or older | Refused                                   |
| Site export from Network 11            | Not a restore: imports as a new site      |

Network only restores from some versions, which change at each release. A converted file is already
checked. To check another one, with the same tag:

```sh
docker run --rm -v "${PWD}:/out:Z" -w /out --entrypoint unifi-export \
  ghcr.io/nathan818fr/unifi-network-standalone:latest --check <file>
```

This also catches damaged files: Network accepts a truncated one, and restores it as a nearly empty
controller with no error.

## 2. Restore it

**Stop the old controller first**, and turn off its **Start on boot**, if it runs on the same host: both
use the same ports.

**On Home Assistant, take a backup first:** restoring it is the way back if this goes wrong.

Restore the file from Network's web interface: on its setup page, or later in its settings. It replaces
your admins and settings.

| Kept                                                          | Lost                          |
| ------------------------------------------------------------- | ----------------------------- |
| Admins and passwords, Wi-Fi, networks, devices and their keys | Statistics, events and alarms |
| Known clients, vouchers, certificates in the database         | Logs and audit log            |

After restoring a backup from Network 10.x or older, look at your networks: the migration skips records it
can't convert with only a line in `/data/unifi/logs/migration.log`. Look closely at legacy firewall rules,
sites with a USG, 2FA, and admins linked to a UI account.

## 3. Log in

**A UI account's email and password can't work here:** log in with a local user.

Look at the app's log (on Home Assistant, its **Log** tab). After a restore from UniFi OS, often
nobody can log in as an admin, so the app gives a real admin a way in: the log says which user to log in
as, with a temporary password if needed. **Change that password once logged in.**

If the log says it `changed nothing`, see [managing users](users.md).

<details>
<summary>How it chooses</summary>

UniFi OS can leave a person's password on one user and their rights on another. A user with a password
and the same name or email as an admin gets admin rights. Otherwise the owner, or the only admin, gets a
temporary password. Otherwise nothing changes. `/data/unifi/logs/access-repair.log` keeps what was done,
without the password.

</details>

## 4. Move your devices

**Same address as the old controller** (the same host): devices come back by themselves.

After the restore, look at **Inform Host Override** in Network's settings: if it's on, it must give this
host's IP address, or be off.

**A new address:** while the old controller still runs, set its **Inform Host Override** to the new
address (in its settings, search for **Inform Host**), and wait for the devices to move. Or run
`set-inform` on each device (see [adding devices](../README.md#adding-devices)), with the SSH login set
in the old controller, not `ubnt`/`ubnt` (in its settings, search for **SSH**).

A console that is also your router (UDM, UCG, UDR...) stays as it is: it can't be managed from here.

## If a restore goes wrong

**On Home Assistant:** restore the backup you took before.

**In a container:** Network has no way back from restoring an old backup, so the image saves the settings
first. To return to them, with the container stopped, in your data folder (for the README's volume:
`podman volume inspect unifi-data --format '{{.Mountpoint}}'`, then `podman unshare` to get a shell
that can change its files):

```sh
cd <your data folder>
cp backup/settings.pre-restore.dump backup/settings.dump
cp backup/settings.pre-restore.dump.version backup/settings.dump.version
rm -rf postgres unifi/data/db unifi/data/restore unifi/data/db_migrator
```
