# Moving from another controller

## 1. Get a full Network backup

**From UniFi OS:** its web interface has no button for this backup. Download a console backup from
**Control Plane > Backups > Download**, then convert it, offline, on any computer with Podman or
Docker. Only Network's data is taken from it:

```sh
podman run --rm -v "$PWD:/out:Z" -w /out --entrypoint unifi-export \
  ghcr.io/nathan818fr/unifi-network-standalone:latest --from-console unifi_os_backup_<...>.unifi
```

**From a standalone controller:** use the backup from its settings.

**From any running controller**, the same command can ask it directly, with `-it` to answer its
questions: `unifi-export https://<controller> -u <local admin>`. It verifies the certificate: add
`--cacert <file>`, or `--insecure` if you accept that the password could be intercepted.

| Backup                                     | Result                                    |
| ------------------------------------------ | ----------------------------------------- |
| Full `.unf` from a version Network lists   | Accepted                                  |
| Full `.unf` from a version it doesn't list | Refused: upgrade the old controller first |
| Newer than this version                    | Refused: update this image first          |
| Console backup (`.unifi`)                  | Refused: convert it first                 |
| Cloud backups                              | Not available                             |
| Site export from Network 10.x or older     | Refused                                   |
| Site export from Network 11                | Not a restore: imports as a new site      |

Network lists the versions it can restore from 7.0 on, with gaps between them that change at each
release: Network 11.0.99 refuses 10.6.107, but accepts 10.6.106 and 10.6.108. **Check your file**, for an
answer from this exact Network, using the same command as above:

```sh
unifi-export --check <file>
```

This also catches damaged files: Network accepts a truncated one, and restores it as a nearly empty
controller with no error.

## 2. Restore it

**On Home Assistant, take a backup first:** restoring it is the way back if this goes wrong.

Restore the file from Network's web interface. It replaces your admins and settings.

| Kept                                                          | Lost                          |
| ------------------------------------------------------------- | ----------------------------- |
| Admins and passwords, Wi-Fi, networks, devices and their keys | Statistics, events and alarms |
| Known clients, vouchers, certificates in the database         | Logs and audit                |

Look at your networks afterwards: a migration skips records it can't convert with only a line in
`/data/unifi/logs/migration.log`. Look closely at legacy firewall rules, sites with a USG, 2FA, and
admins linked to a UI account.

## 3. Log in

A backup from UniFi OS can leave a person's password on one user and their rights on another. So when
nobody could log in as an admin, the image gives a way in, and only to a real admin: a user with the same
name or email as an admin gets admin rights, otherwise the owner gets a temporary password.

**The app's log tells you what happened**, and any temporary password: on Home Assistant, in the app's
**Log** tab. `/data/unifi/logs/access-repair.log` keeps what was done, without the password.

If it can't tell who the real admin is, it changes nothing: see [managing users](users.md).

## 4. Move your devices

Point them at the new address with `set-inform`, or with Inform Host Override on the old controller
first.

## If a restore goes wrong

**On Home Assistant:** restore the backup you took before.

**In a container:** Network has no way back from restoring an old backup, so the image saves the settings
first. To return to them, with the container stopped:

```sh
cd <your data folder>
cp backup/settings.pre-restore.dump backup/settings.dump
cp backup/settings.pre-restore.dump.version backup/settings.dump.version
rm -rf postgres unifi/data/db unifi/data/restore unifi/data/db_migrator
```
