# Project goals

UniFi Network Standalone runs the latest UniFi Network application in one
container, without UniFi OS. It exists for people who only need to manage a few
access points, and is first of all a Home Assistant app.

## Goals

In order of priority:

1. **A few access points, simply.** Adopt, configure and update a small number
   of access points with the latest Network, and nothing else from UniFi OS:
   no extra services, no privileged container, little to set up by hand.
2. **Safe by default.** Once the first admin exists, a LAN client can't get past
   the login, and settings are never silently wiped.
3. **No impact on Ubiquiti.** An unofficial setup should never affect their
   work: it sends them no analytics or crash reports, and its problems never
   reach their support.
4. **Home Assistant first.** Add the repository, install, start. It needs no
   extra privileges, and its backups are small by default and fit how Home
   Assistant does backups.
5. **Modest hardware.** It runs on small single-board computers, from a
   Raspberry Pi 4 up. Small memory and SD cards are normal setups: memory and
   disk writes are limited, measured, and the real minimum is documented.
6. **Long-term use.** Built to last for years, with no need to go back to UniFi
   OS. Few moving parts, from common sources where possible, and a stand-in for
   `unifi-core` kept as small as Network allows. Ubiquiti can break this at any
   update, so a break must be seen early, and an older image must keep working.
7. **Little upkeep.** One person can keep this going. Automatic pull requests
   bump every component and run the tests, whatever a new version breaks is
   reported so it can be fixed before the next release, and a release that
   breaks nothing only needs merging.
8. **Easy migration.** Moving from UniFi OS, or from another way of running
   Network, is not hard: the tools check what they can, and say what to do
   about the rest.

## Non-goals

- **Replacing UniFi OS.** Protect, Talk, Access and other UniFi applications
  are out of scope, and so are gateways and large sites: they may work, but
  nothing is designed or tested for them.
- **What needs the real `unifi-core`.** Remote access, logging in with a UI
  account, API keys and the official Integration API. Tools log in with a local
  admin.
- **Official support.** The project is unofficial, is not affiliated with
  Ubiquiti, and is maintained for its author's own use.
