# Contributing

Bug reports, ideas and pull requests are welcome. For a large change, please
open an issue first, so that we can agree on the approach; the
[goals](GOALS.md) of the project say what fits in it.

## Development setup

The tools and tasks are managed by [mise](https://mise.jdx.dev), which installs the versions locked
in `.config/mise.lock`. The end-to-end tests also need `podman` and `curl`, and the host's ports 8443,
8080, 5434 and 27117 free.

```shell
mise run fmt          # Format everything
mise run lint         # Check formatting and lint
mise run test         # Run the unit tests
mise run e2e          # Build the image and run its end-to-end tests
mise run check        # All of the above
```

## Layout

- `docs`: everything beyond getting started.
- `ha-app-beta`: the Home Assistant app. Edit it here: promoting a beta copies it to `ha-app`, the
  stable app.
- `image`: everything that ends up in the container image:
  - `cmd/unifi-standalone`: one program with several names, as links in the image.
  - `internal/supervisor`: keeps everything running: processes, signals, root, the stand-in for
    `unifi-core`. It may use `store`, never the reverse.
  - `internal/store`: what the image keeps, and who can reach it: users, the settings' dump, MongoDB.
  - `internal/migration`: leaving another controller. It stands alone, as it also runs outside the
    image.
- `tests`: the end-to-end tests of the image.
- `.github`: pull requests, tests and releases: see [below](#versions-and-releases).

## Versions and releases

A version is the Network version, then `-build` and a number, such as `11.0.99-build2`. The number
starts at 1 and only the release workflow chooses it. Never use a plain `11.0.99` or `11.0.99-1` as the
app's version: Home Assistant wouldn't see the next one as an update.

| Image tag        | Moves? | What it follows                         |
| ---------------- | ------ | --------------------------------------- |
| `11.0.99-build2` | Never  | Home Assistant uses this one            |
| `beta`           | Yes    | The latest beta                         |
| `11.0.99`        | Yes    | The latest stable build of that Network |
| `11`             | Yes    | The latest stable Network 11            |
| `latest`         | Yes    | The latest stable release               |

Every version is pinned in `image/Containerfile`: images by tag and digest, PostgreSQL's packages by
version. Debian's other packages are frozen at the date of the base image, so its digest pins them too.
Dependabot updates the images, and `.github/scripts/update.sh` updates Network and PostgreSQL. All of
them open pull requests.

Merging one publishes a beta, if it changes `image` or `ha-app-beta`: the workflow tests the image on
both architectures, pushes what it tested, and only then writes `version` in `ha-app-beta/config.yaml`,
which makes Home Assistant offer it in the beta app. If the tests fail, nothing is offered, and at most
an unused `-amd64` or `-arm64` tag stays. If a run fails while publishing, re-run its failed jobs.

Run the beta app at home. Once a beta has run well there, run the Promote workflow: it offers that same
image to everyone, in `ha-app`, and moves the stable tags. Nothing is rebuilt.

The repository needs, before its first release: "Allow GitHub Actions to create and approve pull
requests", no branch rule that stops the workflows from pushing the version to `main`, and, after the
first push, the container package set to public: the release stops there until it is, so re-run it. For
the update pull requests to start their tests alone, create a GitHub App with write access to contents
and pull requests, and set the `UPDATE_APP_CLIENT_ID` variable and the `UPDATE_APP_PRIVATE_KEY` secret.

For a security fix that can't wait for Debian's next base image, set `DEBIAN_SNAPSHOT_MIN` in
`image/Containerfile` to a newer date, such as `20261112T000000Z`.

## Guidelines

- **Run as little as possible.** Add a process or a dependency only when
  Network can't work without it. Keep the stand-in for `unifi-core` small: a
  path it doesn't know answers 404 and is logged, so that it never approves
  anything by accident.
- **Tests that can fail.** Each test checks a behavior that could break. A
  safeguard, such as refusing to start rather than letting Network wipe its
  database, has a test that triggers it. A failure must have the expected cause,
  and what the real Network proves end to end needs no unit test as well.
- **Public test data only.** Fixtures are made from a clean controller. Never
  put a real backup, address or name in the repository: backups hold passwords.
- **Documentation with the code.** A change of behavior updates
  [the documentation](docs). The README stays the getting started path.
- **Comments explain why**: a Network quirk, a trap, a hack and what it works
  around. They are concise, and never restate the code.
- **Commits** have short, imperative subjects, such as "Add site export check".
