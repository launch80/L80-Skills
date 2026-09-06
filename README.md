# launch80/skills

A local CLI (`L80`) plus a skill bundle that lets any LLM agent publish
structured results to launch80 and get back a shareable URL.

## The shape of it

**Thin skill, thick CLI.** The skill is ~50 lines of markdown telling the model
which command to run. Everything network-facing, credential-handling, and
failure-prone is deterministic Go. A 7B local model behaves the same as Opus,
and the token never enters a model's context window.

**Publish data, not markup.** The CLI uploads JSON. The server validates it
against a schema it holds and renders the page from a first-party template. No
user-supplied HTML or JavaScript is ever served, which removes — rather than
mitigates — the XSS and abuse-hosting problems that come with hosting markup.

**A template registry.** Adding support for a new kind of report means writing a
schema and a template on the server. The CLI, the auth path, and the publish
pipeline are untouched.

## Install

```sh
make build     # -> ./bin/L80
make install   # -> ~/.local/bin/L80
```

Then install the skill for your agent:

```sh
L80 skills link --target claude-code           # copy from the embedded bundle
L80 skills link --target claude-code --dev \
  --from ./skills                              # symlink, so edits are live
```

## Publishing a BetterBench run

`L80 betterbench` runs [BetterBench](https://github.com/GGZ14/BetterBench)
against an OpenAI-compatible endpoint and publishes the `results.json` it
writes, as written, to the `bench.betterbench.v1` template. The server derives
every table and figure from the raw samples with a port of BetterBench's own
report code, so the page shows what the local HTML report shows.

```sh
L80 betterbench --endpoint http://host:8080/v1 --model Qwen3-8B --note engine="vLLM 0.9.3"
L80 betterbench --endpoint http://host:8080/v1 --model Qwen3-8B --quick        # 5-pass smoke run
L80 betterbench --results results.json                                        # publish an existing file
L80 betterbench --results results.json --dry-run --payload-out payload.json   # inspect first
L80 betterbench --results results.json --template bench.report.v1             # summary mapping instead
```

BetterBench is found on PATH, or bootstrapped with `uvx` from GitHub when
`uv` is installed; `L80_BETTERBENCH=/path/to/betterbench` overrides both.
`results.json` is kept in the working directory (`--out` to choose). Two
fields never leave the machine: `env.endpoint` and `env.host`; the server
refuses a file that still carries them. Flags after `--` are passed to
`betterbench run` untouched.

## Update

```sh
L80 update            # download the latest release, verify its checksum, replace the binary in place
L80 update --check    # only report whether a newer release exists
```

Prebuilt releases exist for macOS and Linux on amd64 and arm64. Elsewhere,
pull and `make install`. Re-running the install script
(`curl -fsSL https://launch80.com/install.sh | sh`) is equivalent to `L80 update`.

## Configuration

| Variable | Meaning |
|---|---|
| `L80_API_BASE` | API base URL. Default `https://launch80.com`; use `http://localhost:3007` against a local server. |
| `L80_TOKEN` | Publish token, shaped `L80_<keyid>_<secret>`. |

Falls back to `~/.config/L80/credentials.json` (mode 0600):

```json
{ "key_id": "example-fake-for-test-only", "token": "L80_example-fake-for-test-only_..." }
```

The CLI never prints a token in full — `L80 auth status` shows a masked
fragment. Do not paste a token into a chat.

## Commands

```
L80 publish <file.json> [--template <id>]      Publish a payload, print the URL
            [--json] [--dry-run]
L80 betterbench --endpoint <url> --model <m>   Run BetterBench, publish results as written
L80 betterbench --results <results.json>       Publish an existing results.json
L80 templates list [--json]                    List available templates
L80 skills print [name]                        Print a bundled SKILL.md
L80 skills link --target claude-code [--dev]   Install a skill for an agent
L80 auth status                                Show the resolved endpoint and key
L80 doctor                                     Check connectivity and config
L80 update [--check] [--json]                  Install the latest release in place
L80 version
```

## Exit codes

Errors are read by a model, so each carries a code, a message, and a `remedy`
line naming the next action.

| Exit | Meaning |
|---|---|
| 0 | success |
| 1 | internal error |
| 2 | usage error |
| 3 | input file missing or unreadable |
| 4 | validation failed (`E_SCHEMA_INVALID`, `E_PAYLOAD_UNSAFE`, `E_JSON_INVALID`) |
| 5 | auth (`E_TOKEN_MISSING`, `E_UNAUTHORIZED`) |
| 6 | rate limited or over quota |
| 7 | network unreachable |
| 8 | server error |
| 9 | payload too large |
| 10 | unknown template |

`--template <id>` names the layout on the command line instead of (or as well
as) the file's `$template` field. Given both, they must agree — a disagreement
is `E_TEMPLATE_CONFLICT` (exit 2), never a silent choice.

An error code the CLI does not recognise still prints its message and remedy
verbatim and exits non-zero, so the server can add codes without a CLI release.

## Development

```sh
make test   # go test ./...
make vet
make fmt
```

The binary depends on nothing outside the Go standard library. That is
deliberate: this process holds a credential, so every dependency it does not
have is a supply-chain surface it cannot inherit.
