# Backups

## On Home Assistant

Use Home Assistant's backups: the app takes part in them by itself, and restoring one brings your
settings back. To keep statistics, logs and the audit log too, turn on **Keep history in backups** in the
app's **Configuration**: backups can then grow much larger.

If the app can't save its settings, it is left out of that backup. Home Assistant currently shows no
error for this, only a line in the Supervisor log: check that recent backups list the app.

## In a container

`/data/backup/settings.dump` holds the settings, in a few hundred KB. It's refreshed at each clean stop,
and by:

```sh
podman exec unifi /opt/unifi-standalone/bin/unifi-backup
```

Back up `/data/backup`, enough to get the settings back, and `/data/unifi/data`, which holds the
certificate. To restore, put back `/data` without its `postgres` folder: a start with no database loads
the dump. Set `UNIFI_BACKUP_HISTORY=true` to keep statistics, logs and the audit log too.

## Network's own backups

Home Assistant's backups also keep Network's auto-backups, a way out in a format any Network install can
restore. Their schedule is in Network's settings: monthly by default, so a new install has none for the
first weeks, keeping 7. A restored install keeps the schedule of its backup.

**Treat every backup as a secret.** Anyone with a `.unf` file can read it, as every install uses the same
key, including its admins and device keys. Only restore files you trust.
