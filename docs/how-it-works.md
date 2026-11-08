# How it works

## Network and UniFi OS

**UniFi Network** is the application that manages access points, switches and gateways: a Java program
with its own web interface. Ubiquiti stopped shipping it on its own at version 10.6.

**UniFi OS** is what it runs on now, on Ubiquiti's consoles and in UniFi OS Server. It's a platform for
several applications (Network, Protect, Talk...) and handles what they share, with about 18 services.
Most of them exist for what a few access points don't need:

| Part of UniFi OS            | What it does there                                               | Here                                          |
| --------------------------- | ---------------------------------------------------------------- | --------------------------------------------- |
| `unifi-core`                | The heart: logins, API keys, remote access, the console's portal | A small stand-in answers Network's questions  |
| nginx                       | The front door: asks core who is calling, then forwards          | Not needed: browsers talk to Network directly |
| `ulp-go`, `unifi-directory` | Accounts shared between applications                             | Not needed: Network has its own users         |
| RabbitMQ                    | Messages between services                                        | Not needed: turned off in Network             |
| `uos`, `uos-agent`          | The platform itself                                              | `unifi-standalone` starts what's needed       |
| `uos-discovery-client`      | Finds new devices on the network                                 | Not included: see [discovery](#discovery)     |
| Postgres                    | Network's database                                               | Kept                                          |
| MongoDB                     | Network's former database                                        | Kept, only for old backups                    |

## What runs here

| Process            | Role                                            | Comes from                                  |
| ------------------ | ----------------------------------------------- | ------------------------------------------- |
| Network            | The application, unmodified                     | Ubiquiti's package, checked by its SHA-256  |
| Java               | Runs Network                                    | Temurin, the major version Network asks for |
| Postgres           | Network's database                              | PostgreSQL's packages, a current major      |
| `unifi-standalone` | Starts and watches the rest, stands in for core | This project                                |
| MongoDB            | Only during the migration of an old backup      | MongoDB's 3.6, as on UniFi OS               |

`UNIFI_JAVA_HEAP` changes Java's heap (default `384M`). Most of Java's memory is outside it, so a
lower value saves little.

## Logging in

On UniFi OS, Network doesn't check logins. nginx asks core, then forwards the request on a private port,
with a header saying who it comes from, and Network trusts that header.

Network has a setting for whether core is there, `unifi.core.enabled`. With it off, as here, Network
does what it did as a standalone application: it checks logins itself, against its own users, and answers
on its own web port. It ignores those headers, and the private port still opens, on loopback, but answers 400.

```text
UniFi OS:   browser -> nginx -> Network (port 8081, trusts the header)
                         |
                         +-> unifi-core: who is this?

Here:       browser -> Network (port 8443, checks the login itself)
```

Devices were never part of this: they talk to Network directly, on UniFi OS too. Adoption, inform, STUN
and firmware don't involve core at all, which is why this setup can work.

## The stand-in for core

Network 11 only comes in its UniFi OS form, so the image starts it as UniFi OS Server. In that form it
still asks core a few questions, and a wrong answer has real effects: an empty list of network interfaces,
for example, makes it tell devices to reach it at `127.0.0.1`.

So `unifi-standalone` answers, on `127.0.0.1`, at the ports where Network expects these services:

| Network asks                                    | The answer                                                                                           |
| ----------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| What is this machine? (`/api/system`)           | A UniFi OS Server, with the core version Network's package asks for, and an id made once per install |
| What's its address? (`/api/network/interfaces`) | The host's address, so that devices get one they can reach                                           |
| Which applications and services are there?      | None                                                                                                 |
| Publishing to core's messages (`/mb/*`)         | Accepted, and dropped                                                                                |
| Device tags, console status (gRPC, 11051)       | No tags, and no news                                                                                 |
| Which applications for users? (`ulp-go`, 9080)  | None                                                                                                 |
| Anything else (11081)                           | Refused, and logged as `core stub: unexpected`                                                       |

Refusing what it doesn't know is deliberate: the stand-in never approves anything by accident, and its log
shows what a new version of Network newly expects from core.

## Discovery

A factory-reset device announces itself on the local network, and a controller on that network lists it.
With UniFi OS Server's settings, Network doesn't listen for that: a separate program does, which isn't
included, and it couldn't hear those announcements from behind a container's bridged network anyway. Devices
find the controller another way instead: see [adding devices](../README.md#adding-devices).

## The databases

**Postgres** holds everything since Network 11: settings, statistics, logs. It listens on `127.0.0.1`, in
`/data/postgres`.

**MongoDB** was the database up to 10.x, so backups from those versions are in its format. Network loads
such a backup into MongoDB, then migrates it into Postgres, as any upgrade to 11 does. Backups from 11 hold
Postgres data and don't need it.

Network starts MongoDB itself, by running `sudo systemctl start unifi-mongodb`. The image's `sudo` is a
small command that only does this: it starts MongoDB, or stops it and deletes its files. So it runs during
a migration, and uses nothing the rest of the time.

## What happens at start

1. As root, `/data` is given to the image's user, and Ubiquiti's telemetry hosts are
   blocked. Root is then dropped.
2. Postgres starts. With no database and a saved dump, as after a Home Assistant restore, it loads the dump.
3. The stand-in starts.
4. Before each start of Network, what it can't lose is checked:
   - Analytics are off.
   - `is_default=false` is in its settings file. Without it, Network takes itself for a new install and lets
     anyone in as the first admin.
   - `db_migrator/version` exists. Without it, Network takes the database for a new one and wipes it, so it's
     restored from the dump, or the start is refused.
   - A safety copy is saved if a restore is about to run.
5. Network starts. If it exits on its own, it's started again, which is how it applies a restore.
6. After a restore that left nobody able to log in, one real admin gets the rights back: see
   [moving](moving.md#3-log-in). This is written to the logs.

On stop, the settings are saved to a dump, then Postgres stops cleanly.

## Where the data is

| In `/data`                     | What it holds                           | In Home Assistant's backups |
| ------------------------------ | --------------------------------------- | --------------------------- |
| `postgres`                     | The database                            | No: rebuilt from the dump   |
| `backup`                       | The settings' dump, and the safety copy | The dump                    |
| `unifi/data`                   | Network's certificate and settings file | Yes                         |
| `unifi/data/backup/autobackup` | Network's auto-backups                  | Yes                         |
| `unifi/data/backup`            | Network's manual backups                | No                          |
| `unifi/logs`                   | Logs                                    | No                          |

See [backups](backups.md) for how to use them.

## Ports

| Port       | What it's for                       |
| ---------- | ----------------------------------- |
| 8443       | The web interface and API           |
| 8080       | Devices talking to Network (inform) |
| 3478/udp   | STUN, for devices                   |
| 8880, 8843 | The guest portal, if used           |

## What isn't available

These need the real `unifi-core`:

- **API keys and Ubiquiti's Integration API:** core checks the keys, and Network only accepts those calls
  from core. A tool can log in with `POST /api/login` and `remember: true`, then use the `unifises` cookie.
- **Remote access** and **logging in with a UI account:** both go through core and Ubiquiti's cloud.

The "Upgrade to UniFi OS Server" popup also stays: it comes from the web interface's own code, and can be
closed with **Remind Me Later**.
