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
- `ha-app`: the Home Assistant app.
- `image`: everything that ends up in the container image:
  - `cmd/unifi-standalone`: one program with several names, as links in the image.
  - `internal/supervisor`: keeps everything running: processes, signals, root, the stand-in for
    `unifi-core`. It may use `store`, never the reverse.
  - `internal/store`: what the image keeps, and who can reach it: users, the settings' dump, MongoDB.
  - `internal/migration`: leaving another controller. It stands alone, as it also runs outside the
    image.
- `tests`: the end-to-end tests of the image.

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
