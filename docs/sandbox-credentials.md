# Credentials for sandboxed agents

A plan, not built yet. An agent in a unibox sandbox clones from and pushes
to the forges, and reaches other services the host authenticates to (an
Artifactory, a registry). unibox already knows how to get a token there
without the sandbox ever holding it; what it needs is a source for the
token. This says how unagit becomes that source from its vault, and what
it has to build for it. The unibox side is in `../unibox/docs/design.md`,
"Host services the sandbox reaches".

## What unibox does already

A profile's `sandbox.yaml` declares the services:

```yaml
services:
  gitlab-2n:
    git: true                 # unibox wires the sandbox's git to the host
    host: gitlab.2n.cz
    scheme: basic             # Authorization: Basic base64(user:token)
    user: oauth2              # any token kind GitLab takes over https
    command: /Users/<you>/.local/bin/unagit credential get gitlab-2n gitlab.2n.cz
    refresh: 10m              # fetch again, shorter than the token's life
```

For each, unibox runs `sbx secret set-custom --command ...` when it creates
the sandbox. The sandbox sees a placeholder; the sbx proxy, on the host,
swaps the real header in on requests to the host. unibox never sees the
value. What matters for unagit, all checked against sbx itself:

- **sbx stores the command, not the token.** Its daemon runs the command
  at verification (when the sandbox is created) and again at every
  refresh: on demand by default, or every `refresh:` when the service sets
  one.
- **The command runs in the sbx daemon's world**, not under the unagit or
  unibox process that asked for the sandbox: from a fresh temporary
  directory, with no terminal, at any hour. It has to be named by an
  absolute path, and it cannot ask for anything. A passphrase in an
  environment variable or an unlocked parent does not reach it.
- **Its standard output is the secret.** One token, nothing else.
- A host behind the VPN works: the proxy connects from the host, and the
  host's VPN routes it (checked with gitlab.2n.cz).

Without unagit the same works with a long-lived narrow token in the login
keychain and `command: security find-generic-password -s <item> -w`. unagit
is the better source because it keeps the tokens in one vault and, where
the forge allows it, issues short-lived ones.

## The hard rule, and what this changes in it

AGENTS.md says the tokens are out of reach: never handed through a flag,
an environment variable, a file or a pipe, no cache daemon. This plan
hands a token to another process on purpose, so it widens that rule, and
only the user decides it. Kept narrow, it amounts to this:

- **Only through `unagit credential get`**, the unagit binary itself, and
  only while the user's unagit is running and unlocked. Nothing outlives
  that process: the socket is its own, closed with it - not a daemon that
  keeps the passphrase.
- **Only what the user granted.** A grant names a service, its hosts and
  the kind of token; anything else is refused. Off until the user turns
  it on, in Settings.
- **Minted, short-lived and narrow by preference.** The vault's own
  tokens - broad, long-lived, what unagit reviews with - are never handed
  out; a grant either mints a token for the purpose or hands out a narrow
  token the user stored for exactly this.
- **Seen.** Every handout is logged, and shown.

What it does not protect against: any process of the same user can
connect to the socket while unagit runs - including an agent running
unsandboxed on the host. The peer check stops other users, not other
programs. Grants, short lifetimes and the log are what bound it. The agent
inside the sandbox cannot reach the socket: nothing of the host's
configuration directory is mounted there, and sbx keeps its helpers out of
sandbox mounts.

Considered and rejected:

- **`unagit credential get` reading the passphrase from the keychain**,
  so it works without unagit running. The keychain item trusts the unagit
  binary alone; this subcommand is that binary, so every program of the
  user's could get any granted token silently, with unagit closed - the
  dialog that protects the item would never show. If wanted later, it
  needs its own decision.
- **Tokens passed to unibox with `prepare`.** unibox would hold values in
  argv, logs and state, the approval of the profile would not cover them,
  and an sbx secret is the sandbox's, not a task's.

## What unagit has to build

### The socket

- `~/.config/unagit/run/credential.sock`, its directory 0700 and the
  socket 0600, outside anything a sandbox mounts. A stale one from a
  crashed run is removed at start; a live one (another unagit) means this
  one does not serve, and says so.
- Opened after unlock, closed at lock and exit. While locked it does not
  exist, so the client's answer is simply "unagit is not running".
- The peer's uid is checked (`getpeereid` / `LOCAL_PEERCRED`); a different
  user is refused. The peer's pid and executable are recorded for the log
  where the platform gives them.
- One request and one answer per connection, a JSON line each, versioned:

  ```json
  {"v": 1, "service": "gitlab-2n", "host": "gitlab.2n.cz"}
  {"v": 1, "token": "...", "expires_at": "2026-10-14T15:10:00Z"}
  {"v": 1, "error": "no grant for gitlab-2n on gitlab.2n.cz"}
  ```

  Short timeouts on both sides; the server never blocks the UI.

### `unagit credential get <service> <host>`

- A thin client of the socket: no UI, no config beyond the socket path,
  no vault access of its own. It never prompts.
- Prints the token alone to stdout, with no newline, and exits 0.
  Anything else - no socket, refused, timeout - exits non-zero with one
  lower-case line on stderr saying what to do ("unagit is not running;
  start and unlock it"). sbx shows stderr only on request, since it may
  hold secrets; ours never does.
- Fast to start: it runs at every refresh.

### Grants

Settings › Integrations › unibox › Credentials, a list of grants:

| field | meaning |
| --- | --- |
| service | the name in the profile's `services` |
| hosts | where the token may be used; must match the request |
| source | the forge account of the list it comes from |
| kind | `mint` (a new token per request) or `stored` (a narrow token kept in the vault for this) |
| scope | what a minted token may do: read, or read and write the repositories |
| lifetime | for `mint`; the profile's `refresh` must be shorter |

Default deny: a request without a grant is refused and logged, and the UI
offers to create the grant (never silently).

### Minting short-lived tokens

Tokens only: no GitHub App, no OAuth application - the user's decision.
unagit holds the user's PATs and uses them to issue narrower ones where
the forge's API allows it. What each forge allows, to be confirmed when
built:

- **GitLab** - project and group access tokens can be created through the
  API (`POST /projects/:id/access_tokens`, a Maintainer's right; on
  gitlab.com a paid tier) with chosen scopes and an expiry, by the day,
  not the hour. A stored token can also rotate itself
  (`POST /personal_access_tokens/self/rotate`): a new value, the old one
  revoked. Either way unagit revokes what it issued once a newer one
  replaced it, so a day's expiry is not a day's exposure.
- **GitHub** - no API creates a personal access token, fine-grained or
  classic; they are made in the web UI only. unagit hands out `stored`
  tokens there - one fine-grained token per owner (it covers one owner),
  or one classic token for all - and can say when one nears its expiry.
  A profile tells owners on github.com apart by path
  (`host: github.com/some-org`), which unibox supports.

`stored` grants work on both forges from day one; minting is GitLab's.

### Seen

Each handout - time, service, host, kind, expiry, the peer when known -
goes to a log under unagit's state, and to Activity: a sandbox taking a
token is something the user should be able to see happen.

### Writing the profile's entries

unagit writes no YAML. From the Sandboxes tab, a grant runs `unibox
service add` for a profile - the host, the user the forge expects, a
`refresh` shorter than the lifetime, and a `command:` naming `unagit
credential get` by the absolute path of the running binary - and a
`stored` token reaches the platform's store through `unibox secret set`,
on its stdin. unibox owns the profile's format and the platforms'
stores (unibox's docs/design.md, "Configured through unibox"); the
profile changes host access, so unibox asks to approve it again.

## Open questions

- **sbx when a refresh fails** - unagit closed, VPN down: does it keep the
  last value until it expires, or send the placeholder and get a 401?
  Check on a throwaway sandbox before relying on either.
- **unibox run without unagit running** fails at sandbox creation, since
  sbx verifies the command then. Fine when unagit starts the run; the
  error from a bare `unibox run` should say to start unagit.
- **A `credential_helper` in unibox's configuration**, set by unagit, so a
  profile need not name unagit's path at all (`<helper> get <service>
  <host>` for a service without a command). Optional; decide with the
  first real use.

## Order of work

1. The socket and `unagit credential get` with `stored` grants, the log,
   and the Settings block - enough for the my2n and personal profiles.
2. Activity rows for handouts.
3. GitLab minting and rotation; expiry reminders for stored GitHub tokens.
4. Writing profile entries from the Sandboxes tab.
