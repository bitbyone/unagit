# Sandboxed agents

## Where it stands

Designed (2026-10-12), nothing built. This is the brief for whoever
implements it; the reasoning is here so that the decisions are not undone
by accident. It replaces "Agents in a container" in [herdr.md](herdr.md),
and wins wherever the two disagree:
that plan mounted the worktree and the main clone's `.git` into a Docker
container. Its launcher for herdr is not used either: unagit tells herdr
about its agents with a plugin of its own (see [herdr](#herdr)).

Checked so far: `sbx` 0.47.0 on macOS (Apple Silicon) - its commands and
flags below come from its `--help`; `sbx ls --json` answers
`{"sandboxes": []}`. Everything marked **spike** has not been tried.

## What it is for

A worktree - one repository's or a group's - gets a coding agent that runs
in a VM instead of on the machine. The agent has full autonomy inside it;
the machine, its home, its keys and unagit's tokens are out of its reach.
The user picks the agent, a sandbox profile (a Dockerfile and the
resources, kept in a git repository of their own), and whether the agent
shares a VM with others or gets one of its own.

## The picture

The parts, and what stays outside unagit:

```
+------------------------------- unagit (host) --------------------------------+
|                                                                              |
|  internal/ui                                                                 |
|  +------------------+ +------------------+ +------------+ +----------------+ |
|  | Sandboxes tab [6]| | Open in Sandboxed| | Activity   | | Settings [5]   | |
|  | Profiles         | | Agent...         | | (agents,   | | env repository | |
|  | Sandboxes (VMs)  | | agent > profile  | |  states)   | | Integrations:  | |
|  | Tasks            | | > sandbox > place| |            | | sbx, herdr     | |
|  +--------+---------+ +--------+---------+ +-----+------+ +-------+--------+ |
|           |                    |                 |                |          |
|           v                    v                 |                v          |
|  internal/sandbox (the neutral vocabulary, as internal/forge is)             |
|  +-----------------------------------------------------------------------+   |
|  | Profile   parse profile.yaml, tree hash, an unknown field is an error |   |
|  | Trust     host-facing fields hashed, Approve...                       |   |
|  | Task      the manifest: branch and clone URLs (from the forge's API)  |   |
|  |                                                                       |   |
|  | Runtime   Available List Images Build Create Exec Stop Remove         |   |
|  +-----------------+---------------------------------+-------------------+   |
|                    |                                 |                       |
|        +-----------v-----------+         +-----------v-----------+           |
|        | sbx adapter           |         | podman adapter        |           |
|        | profile -> flags      |         | (later)               |           |
|        | Dockerfile -> template|         |                       |           |
|        | + integrations.sbx    |         | + integrations.podman |           |
|        +-----------+-----------+         +-----------------------+           |
|                    |                                                         |
|  +--------------+  |  +--------------+ +--------------+ +-----------------+  |
|  | workspace,   |  |  | cache        | | session      | | mux             |  |
|  | gitx: push   |  |  | <cache>/     | | records:     | | This Terminal,  |  |
|  | the branch,  |  |  | sandboxes/   | | pane, agent, | | Zellij, Ghostty,|  |
|  | pull         |  |  | (generated)  | | VM, task     | | herdr           |  |
|  +------+-------+  |  +--------------+ +------+-------+ +--------+--------+  |
+---------+----------+------------------------+------------------+-----------+
          |          |                        | read             | the pane runs
          |          |                        v                  v
          |          |       +-------------------------+   sbx exec -it -w
          |          |       | herdr + unagit's plugin |   /work/<task> <vm>
          |          |       | unagit herdr observe:   |   claude
          |          |       | pane.read -> agent      |        |
          |          |       | explain -> report_agent |        |
          |          |       +-------------------------+        |
          |          | sbx CLI                                  |
          | push,    +-----------------------------------+      |
          | pull     |                                   v      v
          v          |                     +--------------------------------------+
   +-------------+   |    clone, push      | sbx microVM  unagit-<profile>[-task] |
   | forge       |<--+-------------------->|  template = the profile's image      |
   | GitLab,     |     (the VM's own       |  /work/<task>/<repo>   git clones    |
   | GitHub      |      narrow token,      |  the agent (claude, codex, ...)      |
   | protected   |      an sbx secret)     |  Docker Engine (compose, tests)      |
   | branches    |                         |  GRADLE_USER_HOME (the VM's own)     |
   +-------------+                         |  ~/.gradle/caches :ro <- host        |
                                           +--------------------------------------+
```

Where each kind of setting lives:

```
 environments repository            config.yaml (chezmoi)        state.json
 +---------------------------+   +---------------------------+  +------------------+
 | kotlin/                   |   | sandboxes:                |  | approvals:       |
 |   Dockerfile              |<--+   envs: gl/team/sandbox-  |  |  kotlin: <hash>  |
 |   profile.yaml            |   |         envs              |  | sandboxes:       |
 | node/                     |   | integrations:             |  |  <dir> -> vm,task|
 |   ...                     |   |   sbx:                    |  | uses: profile,   |
 +-------------+-------------+   |     kits: [...]           |  |  sandbox, place  |
               |                 |     profiles:             |  +------------------+
  what the     |                 |       kotlin: {kits: ...} |   what use taught
  environment  |                 +---------------------------+
  is, no       |                  the user's choices for
  runtime      v                  one runtime
       +--------------------------------------+
       | generated just before a start        |
       | <cache>/sandboxes/sbx/               |
       |   unagit-env/kotlin:<tree>  template |
       |   flags: --cpus -m -e --deny-network |
       +--------------------------------------+
         a cache: never edited, regenerated
```

A start:

```
 user                  unagit                        sbx, the VM            forge
  | Open in Sandboxed    |                              |                     |
  | Agent... on a group  |                              |                     |
  +--------------------->|                              |                     |
  | agent, profile,      | parse the profile, check     |                     |
  | sandbox, place       | its approval                 |                     |
  +--------------------->|                              |                     |
  |                      | push every member's branch --+-------------------->|
  |                      | template current? ---------->| template ls --json  |
  |<-- build? (if not) --+                              |                     |
  +-- yes -------------->| docker build, save, load --->|                     |
  |                      | create or start the VM ----->|                     |
  |                      | exec: clone or fetch, ------>| git clone --------->|
  |                      |       bootstrap              |                     |
  |                      | session record, the pane --->| exec -it ... claude |
  |                      |                              |  the agent works    |
  |                      |                              |  commit, push ----->|
  | Pull on the group -->| fetch, fast-forward <--------+---------------------+
```

What crosses the line between the machine and the VM:

```
   +------------- trusted --------------+       +----- untrusted -----+
   | host: home, ~/.ssh, unagit's vault |       | VM: the agent, sudo,|
   | main clones, the user's worktrees  | <-X-- | its clones, its     |
   | config.yaml, the environments      |       | Docker              |
   | repository (as approved)           |       |                     |
   +-----------------+------------------+       +----------+----------+
                     | only:                               ^ | only:
                     |  ~/.gradle/caches  :ro  ------------+ |
                     |  the env values a profile names ----+ |
                     |                                       |
                     +---- git fetch <---- forge <-----------+ git push
                          (objects and refs only,     (a narrow token,
                           no config, no hooks)        protected branches)
```

## The model, and why

```
host                                   sandbox VM (sbx)
grouped worktree  <── git pull ───┐    /work/<task>/<repo>  clones of the
  (yours)                         │      branch, the agent commits
                                  │      and pushes here
                       forge ◄────┴──── git push
```

- **The agent clones from the forge, commits and pushes; the user pulls.**
  unagit pushes the worktree's branch before the agent starts, the VM
  clones every repository of the worktree at that branch, the agent pushes
  back to it, and the user pulls into the grouped worktree as they would a
  colleague's commits.
- **Nothing of the VM is mounted on the host, and nothing git-related of the
  host is mounted in the VM.** That is the point of the design. A `.git`
  the agent can write holds commands the host would run: a hook,
  `core.fsmonitor`, `core.sshCommand`, a filter, `credential.helper` - and
  unagit, gitsigns, lazygit and IntelliJ run git in a worktree all the
  time. Through `credential.helper` the agent would even receive the forge
  token unagit hands git. A fetch from the forge carries objects and refs,
  never config or hooks, so the host stays safe however the agent behaves.
- **The cost**: what the agent has not pushed is not seen on the host.
  Tell the agent (in its instructions) to commit and push as it goes.

Considered and rejected:

- **The worktree and the main clone's `.git` mounted read-write** (herdr.md's
  plan, with `config`, `hooks` and `config.worktree` mounted read-only over
  them). It works - herdr.md verified the locks - but every other branch's
  ref stays writable, the agent's files are on the host to be run by
  mistake, and the list of what must be locked is only as complete as our
  knowledge of git.
- **The same with the main clone read-only, the worktree on a detached HEAD
  and an object directory of its own** (`GIT_OBJECT_DIRECTORY`, the main
  objects as an alternate). Safe, but the agent sees a detached HEAD and
  unagit has to move the branch after it. The user found it unpleasant.
- **`sbx run --clone`**: sbx's own clone inside the VM, of the host
  repository mounted read-only. One repository per sandbox, and how its
  commits come back is undocumented (the docs' git-workflow page is a 404).
- **Podman**: not now. `internal/sandbox` is neutral so that it can be added
  without touching the UI.

## Sandboxes and VMs

One sbx sandbox is one microVM: its own kernel, its own Docker Engine, its
own disk, persistent until removed. The agent runs in the VM itself, beside
that engine, as a non-root user with sudo (sbx's security docs); `sbx exec`
runs another process in the same environment, like `docker exec`.

Both arrangements are offered, and the user picks at the start:

- **A shared VM** per profile (`unagit-<profile>`): tasks are directories
  `/work/<task>`, agents are processes side by side. Memory, the Gradle
  cache and Docker images are paid once. Between the agents there is no
  isolation at all - one user with sudo, one filesystem, one Docker, one
  set of ports (two `docker compose` Postgreses on 5432 collide). The
  picker says so.
- **A VM of its own** (`unagit-<profile>-<task>`): nothing it does reaches
  another task.

Both persist when the agent ends, for their caches; removing is the user's
action. The default offered is the shared VM; the picker orders by use, as
the place picker does (`orderPlaces`).

`<task>` is the worktree's folder name and a short hash of its host path,
since two roots can hold folders of the same name.

## A start, step by step

On a worktree, a group or a merge request's branch worktree (never a review
worktree: its HEAD is the merge base on purpose, nothing is committed
there):

1. **Open in Sandboxed Agent…** asks, each picker skipped when it has one
   item and ordered by use: the agent, the profile, the sandbox, the place
   (the existing `agentPlaces`). The sandbox picker is also where the
   runtime is chosen: the profile's VMs of every runtime (from `sbx ls
   --json`), each carrying its own, then New Sandbox in <runtime>… for
   each runtime turned on in Integrations and installed - plain New
   Sandbox… while there is one. A profile whose host-facing fields changed
   since they were approved does not start (see [trust](#trust)).
2. unagit **pushes the branch** of every repository of the worktree that
   has no upstream or is ahead of it - from the host, with its own
   credentials, through the path `P` uses. A branch origin has moved past
   stops the start with the reason; nothing is forced.
3. **The image is checked** for the runtime: built from the profile's
   tree at HEAD, or not. When it is not, the start asks to build it
   (minutes, a `runTask` log) and never builds quietly.
   The VM is then created if new (`sbx create`, see
   [runtime](#the-runtime)) or started.
4. **The task is prepared** by `sbx exec <vm> sh -c <script>` with the
   manifest in the environment: for each repository, clone at the branch
   into `/work/<task>/<dir>` (`--filter=blob:none`), or fetch and
   fast-forward when it is there already; set the author; run the profile's
   bootstrap script from the repository if it has one. This is a `runTask`
   log - a clone is worth reading when it fails.
5. **The agent starts** in the place chosen: `sbx exec -it -w
   /work/<task> <vm> <agent command>`, a terminal command like any other,
   so This Terminal, Zellij, Ghostty and herdr all work as they do for an
   agent on the host.
6. The record goes to `<config>/sessions/` as an agent's does, with the VM
   and the task added, so Activity lists it.

The manifest is clone URLs and directory names:

```
UNAGIT_BRANCH=feature/x
UNAGIT_REPOS=api=https://gitlab.example.com/team/api.git web=https://…/web.git
```

**Clone URLs come from the forge's API** (the project's http URL), never
from a local remote: a chezmoi origin can carry a token in its URL
(AGENTS.md, chezmoi). https always - the VM has no SSH key.

What a worktree's sandbox is, for the next start and for removing:
`config.State` (state.json) keeps `Sandboxes`, host directory → VM and
task. It is use, not a choice, so not config.yaml; entries whose
directory is gone are swept.

## Credentials

- **unagit's tokens never go into a VM**, in any form (AGENTS.md, the hard
  rule). The push from step 2 is the host's; everything after it is the
  VM's own.
- **The VM pushes with a token of its own**, issued by the user for this,
  stored by sbx (`sbx secret set`, its keychain and proxy). unagit never
  sees it. Narrow: a GitLab project or group access token with Developer on
  the projects concerned; a GitHub fine-grained token on the repositories,
  contents write.
- **The forge is now the boundary.** The agent can push to any branch the
  token may, force-push, delete. Protect `main` and release branches on the
  forge, and if possible restrict the token to the work branches (GitLab
  push rules and protected branches, GitHub rulesets). The README says so
  where it explains the feature.
- The agents' own logins (Claude, Codex) are sbx's business, as with
  `sbx run claude`.

**Spike**: how sbx hands git its credential - a credential binding through
the proxy, or a secret the VM's git reads through a helper - and whether
that works for a self-hosted GitLab, behind a VPN included.

## Profiles

### Where they live

Profiles are files in a git repository of the user's, the **environments
repository**, edited and versioned as any repository is - in the editor,
committed with unagit's commit dialog, pushed, shared with a team:

```
sandbox-envs/
├── kotlin/
│   ├── Dockerfile
│   └── profile.yaml
└── node/
    ├── Dockerfile
    └── profile.yaml
```

A directory with a `profile.yaml` is a profile, named after the directory.

Settings names the environments repository as **a repository of the
list** (instance and project path), so config.yaml stays portable: unagit
finds its main clone wherever it cloned it on this machine, as it does for
any repository. A plain path is the fallback for a repository on no forge.
Profiles are read from the main clone's working tree - what the user sees
in their editor is what runs.

unagit never edits a profile. The Sandboxes tab shows them read-only,
parsed, and opens a profile's directory or file in the editor.

### The format

`profile.yaml` is unagit's own format, kept small, and **knows no
runtime**. It describes the environment; where it runs is chosen at the
start.

```yaml
dockerfile: Dockerfile        # relative to the profile; or image: <ref>
cpus: 8
memory: 12g
env: [SPRING_PROFILES_ACTIVE] # names; values from the environment
caches:
  - host: ~/.gradle/caches
    env: GRADLE_RO_DEP_CACHE
deny_network: []
bootstrap: .agent/bootstrap.sh # per worked-on repository, if present, in the VM
```

- An unknown field is an error naming it, never skipped: a misspelt
  `memmory` must not quietly change the environment. The Sandboxes tab
  shows a profile that does not parse with the error, and it cannot be
  started.
- **The image is the environment.** Everything static - JDK, Node, the
  agents' CLIs, certificates - goes into the Dockerfile. What depends on
  the checkout belongs to the worked-on repository's bootstrap, which runs
  inside the VM.
- `image:` instead of `dockerfile:` names an image built elsewhere (a
  team's CI); then nothing is built.
- **Caches are read-only from the host.** `~/.gradle/caches` mounted
  read-only as Gradle's read-only dependency cache: what the host already
  has is not downloaded again, and what is missing goes to the VM's own
  `GRADLE_USER_HOME`, kept with the VM. Read-write would let the agent put
  a jar or an `init.d` script that the host's next build runs, and Gradle's
  file locks do not hold between VMs over virtiofs. Gradle does not promise
  consistency while the host writes the cache - at worst a build in the VM
  fails and runs again. A mount is at the host's absolute path, as sbx
  mounts everything.
- Considered: devcontainer.json (a standard, but unagit would honour a
  fraction of it and the rest would silently not work; the read-only cache
  and the network rules do not map onto it) and sbx's `sbxenv.yaml`
  (sbx's alone, and experimental).

### Trust

The environments repository is the user's choice, so it is trusted - but a
pull can bring a teammate's change, and some fields reach the host: the
caches (host paths mounted), `env` (host values passed in) and the network
rules. A change to those must be approved before the profile runs, as
`direnv allow` does:

- `config.State` (state.json) keeps, per profile, a hash of its
  host-facing fields as last approved.
- A profile whose fields differ shows "changed since approved" on the
  Sandboxes tab; Approve… shows the difference of those fields alone and
  asks. A new profile starts unapproved.
- The worked-on repositories still choose nothing: they give the bootstrap
  script, which runs inside the VM.

### sbx's templates and kits

sbx has two words for shaping a sandbox, and the design leans on one and
keeps away from the other.

- **A template** is the image a sandbox is made from: an OCI image in sbx's
  own image store (apart from Docker's), its root filesystem being what is
  installed in the VM - the OS, the toolchains, the agent's CLI.
  `sbx create -t <template>` picks it; without one, sbx takes the agent's
  own (Docker publishes one per agent). It is how the environment gets into
  the VM, so it is what a profile's Dockerfile becomes: built, saved and
  loaded (`sbx template load`), tagged by the profile's tree. `sbx template
  save` can also snapshot a running sandbox as a template.
- **A kit** is a declarative package laid over a sandbox: files, an
  install hook run once when the sandbox is made, a startup hook run at
  every start, network rules, credential bindings, instructions for the
  agent. A *workload* kit says what runs - the built-in agents are kits,
  which is how `sbx create claude` knows to install and log in Claude - and
  a *mixin* kit adds to one (`--kit`). Kits v3 are Early Access.

Why they matter here: the template is the profile, so building and
tagging it is the runtime adapter's main job, and whether it is current is
what the Profiles part of the tab shows. The kit is where sbx keeps what
makes an agent work inside - its login, its proxy - so unagit uses the
built-in agent kits as they are, passes the user's own mixins through from
`integrations.sbx`, and writes none of its own: everything a profile says
is expressible as flags, and a format still moving is not one to generate.

**Spike**: does sbx need anything of a template beyond an image - its
`agent` user, its workdir, the agent's kit installing into it? If so, a
profile's Dockerfile starts `FROM` Docker's template for sandboxes, and
the README says which.

### What a runtime makes of it

Everything runtime-specific is generated just before a start, from the
neutral profile, and kept as a cache (`<cache>/sandboxes/<runtime>/`) that
is never edited and always regenerated. For sbx:

| profile | sbx |
| --- | --- |
| `dockerfile` | `docker build`, `docker save`, `sbx template load`, tagged `unagit-env/<profile>:<tree>` |
| `image` | `-t <image>` |
| `cpus`, `memory` | `--cpus`, `--memory` |
| `deny_network` | `--deny-network` |
| `env` | `-e NAME` |
| `caches` | `<host>:ro` as an extra workspace, `-e <env>=<host>` |
| `bootstrap` | run by `sbx exec` after the clones |

- `<tree>` is the git tree hash of the profile's directory at HEAD, so
  whether the image is current is a comparison with `sbx template ls
  --json`, per runtime: **built**, **outdated**, **not built**. A profile
  with uncommitted changes is marked so; Build… builds them as they are,
  tagged `<tree>-dirty`.
- A local build is for this machine's architecture, which is all a local
  sandbox needs.
- **No kit is generated** while flags can say it: kits v3 are Early Access,
  and a generator tied to their format would break with sbx's releases.
  One is generated only if the spike finds something flags cannot do -
  the credential for git's push is the candidate.

### What the user adds for one runtime

What the user wants for sbx alone - a kit of the company's certificates,
a mirror - is a choice on this machine, not part of the environment. It
lives in config.yaml under the runtime's integration and is edited in
Settings › Integrations, in sbx's block:

```yaml
integrations:
  sbx:
    kits: [docker.io/bitbyone/company-ca:1]    # every profile
    profiles:
      kotlin:
        kits: [docker.io/bitbyone/gradle-mirror:1]
```

It is the user's own file, so no approval. It is keyed by the profile's
name: one whose profile is gone is a warning in Settings, never dropped. A
runtime added later gets a block of its own the same way. Kits are passed
through and never interpreted.

sbx appears in Integrations like the other programs (`a.executable("sbx")`);
only runtimes turned on there are offered.

## The runtime

`internal/sandbox`, the neutral vocabulary, as `internal/forge` is for
forges. Nothing above it says sbx.

```go
type Runtime interface {
	Name() string
	Available() error                    // installed, signed in, daemon up
	List() ([]Box, error)                // name, profile, state, memory
	Images() ([]Image, error)            // built templates, by tag
	Build(p Profile, log io.Writer) error // the profile's image, for this runtime
	Create(spec Spec) error              // detached, idempotent by name
	Exec(name string, cmd Command) *exec.Cmd // -it when Interactive
	Stop(name string) error
	Remove(name string) error
}
```

sbx's side, from its `--help`:

| Runtime | sbx |
| --- | --- |
| `List` | `sbx ls --json` |
| `Build` | `docker build`, `docker save`, `sbx template load` (see [profiles](#what-a-runtime-makes-of-it)) |
| `Images` | `sbx template ls --json` |
| `Create` | `sbx create --name N -t T --kit K --cpus C -m M --deny-network H -e K <agent> <mounts>` |
| `Exec` | `sbx exec [-it] [-w DIR] [-e K=V] N cmd…` (starts a stopped VM) |
| `Stop` | `sbx stop N` |
| `Remove` | `sbx rm N` |

Spike questions about it:

- `sbx create` takes an agent and mounts the current directory when given
  no path. A VM must be created with no repository mounted: give it a
  directory of unagit's own (`<cache>/sandboxes/<vm>`, empty, read-only)
  as its workspace, unless sbx can do without one.
- A shared VM is created for one agent. Does a second agent started with
  `sbx exec` have its login and sbx's proxy? If not, a shared VM is per
  profile and agent, `unagit-<profile>-<agent>`, and the picker says so.
- Does `sbx exec` take `-w`?
- Do read-only mounts of host directories outside the workspace work
  (`<path>:ro` as an extra workspace)?
- Does `sbx template load` take a tar from `docker save`, or only one from
  `sbx template save`? If only the latter: build in a sandbox and `sbx
  template save` it, or push to a registry.

Errors are lower case and say what to do: `sbx is not signed in - run
sbx login`.

## The agents

`agents.Agent` gains what a VM needs: the command inside the VM (usually
`Command`), and sbx's agent name for `sbx create` (`claude`, `codex`,
`copilot`, `opencode`; Antigravity has none, so it is not offered in a
sandbox). An agent not installed in the profile's template fails in the VM
with the template named in the error.

## herdr

herdr recognises an agent by its pane's foreground process, and `sbx exec
… claude` in a pane is the `sbx` process: herdr sees no agent and has no
state for it. unagit fills that in with **a herdr plugin of its own**. It
is the one way; there is no launcher and no third-party plugin to choose
instead.

What it does, as long as herdr runs - unagit open or not:

- For every pane in which unagit started a sandboxed agent (the session
  records in `<config>/sessions/`: the pane, the agent, the VM), it reads
  the screen (`pane.read`), has herdr classify it by herdr's own rules for
  that agent (`herdr agent explain --file … --agent <kind> --json`), and
  reports the state (`pane.report_agent`, `pane.release_agent` when the
  agent is gone), with the VM's name as the pane's metadata
  (`pane.report_metadata`). It reports only on a change.
- It need not parse argv to know what runs where - unagit started it and
  wrote it down - but it checks with `pane.process_info` that the pane
  still runs `sbx exec`, so a pane reused for something else is released.
- The Agents part of Activity matches herdr's agents by pane, so the state
  arrives there with no change.

The idea, and the herdr calls, come from
[sbx-herdr](https://github.com/dvdksn/sbx-herdr) (read 2026-10-12 at
0.1.0). It has no licence, so none of its code is taken; ours is smaller
anyway, since it knows its panes instead of guessing them from argv.

**The plugin is a manifest and nothing to build.** `herdr-plugin.toml`, its
commands calling the unagit binary: a startup command and the
`pane.created` / `pane.focused` events run `unagit herdr ensure`, which
starts one `unagit herdr observe` per herdr socket (a lock in
`HERDR_PLUGIN_STATE_DIR`, so a second ensure does nothing), detached, its
log in that directory. Both are hidden cobra commands that refuse to run
outside herdr (no `HERDR_SOCKET_PATH`). No Go on the user's machine, and
the plugin's version is unagit's.

**Installed only with the user's consent**, never quietly - it changes
herdr's configuration and leaves a process running with herdr:

- Settings › Integrations, in herdr's block: whether the plugin is
  installed and enabled (`herdr plugin list`), and an action Install herdr
  Plugin… (and Uninstall) behind a question that says what it does.
- The same from the command line: `unagit herdr install-plugin` and
  `uninstall-plugin`, asking unless `--yes`.
- Installing writes the manifest, embedded in the binary, to
  `<config>/herdr-plugin/` with the absolute path of the running unagit,
  then `herdr plugin link <dir>` and `enable`.
- The path changes when unagit is upgraded or moved. At every start,
  unagit compares the linked manifest with what it would write and, if it
  differs, rewrites it - the user consented to the plugin, not to a path.
- When a sandboxed agent is started in herdr without the plugin, it runs,
  with no state, and unagit says once (`note`) that Install herdr Plugin…
  in Settings would show it.

What the command must look like, so that the plugin's check holds: `sbx
exec` itself is the pane's process - directly or through `unagit launch`,
which `exec`s it, never inside `sh -c` - and the agent is the command
(`sbx exec -it -w /work/<task> <vm> claude`).

The agents' own hooks (Claude's session id, opencode's states) do not cross
into the VM; the state is the screen's.

**Spike**: the herdr here is 0.9.1 and sbx-herdr asks for 0.9.3 - find the
version whose plugins have `pane.read`, `agent explain` and the events, and
make Integrations say "herdr 0.9.3 or newer" when older. Check that `herdr
plugin link` takes a manifest whose commands are absolute paths outside its
directory, and that a plugin's commands get `HERDR_SOCKET_PATH` and
`HERDR_PLUGIN_STATE_DIR`.

## What the user sees

**A Sandboxes tab**, `6`, after Settings (`5` stays, as the messages that
point at `Settings [5]` do). Three parts, each a list made the way the
other screens' are (`pane.go`, its actions as `uiAction`s):

| part | rows | actions |
| --- | --- | --- |
| **Profiles** | name, image state per runtime, approval, last commit to its directory | Open in Editor…, Build…, Approve…, Open in Sandboxed Agent… |
| **Sandboxes** | VM: runtime, profile, state, memory, tasks | Shell… (`sbx exec -it <vm> bash` in a place), Stop, Remove (a question naming its tasks) |
| **Tasks** | in the VM under the cursor: worktree, agent, its state | Open Worktree, Remove Task (`rm -rf /work/<task>` in the VM) |

Without an environments repository set, the tab says where to set it.
Activity still lists every agent, sandboxed ones with their VM: Activity is
what runs, the Sandboxes tab is what it runs in.

Elsewhere:

- **Open in Sandboxed Agent…** on repositories with a worktree, worktrees,
  groups, merge requests with a branch worktree. Alt picks again what use
  would skip (the profile and the sandbox, even when one).
- Settings: the environments repository, and sbx's block in
  Integrations (on or off, its kits).
- **Pull** of a worktree or a group is what brings the agent's work; it
  exists already for branches with an upstream, check it covers a group in
  one go.
- Deleting a worktree or a group with a task in a VM offers to remove the
  task and, for a VM of its own, the VM. The default keeps the VM.
- Help rows for the tab and the new keys; the README explains the feature,
  the environments repository and the forge-side protection.

Later, not in the first version: following the branch on the forge
(`internal/watch`) to say when the agent pushed, and pulling on request
from that news.

## Tests

- A stand-in `sbx` on its own PATH (`testenv.WriteExecutable`), answering
  `ls --json` from a file and logging every argv: the commands built for a
  shared and an own VM, the task script, the order push → create → exec.
- The manifest: URLs from the forge, never from a remote; no token in any
  argv or environment unagit builds (grep them, as the token test greps
  the disk).
- Profiles: parsing (an unknown field is an error), the tree hash and the
  image state against a stand-in `sbx template ls`, the generated flags,
  the approval refusing a changed cache or `env` and passing a changed
  `cpus`.
- UI: the Sandboxes tab and the pickers at several terminal sizes,
  `assertLegible`, sbx's block in Integrations, help rows,
  `TestEveryActionIsNamedAndExplained`, `TestNoTwoActionsShareAKey`.
- The plugin: the manifest written with the binary's path and rewritten
  when it moves; `observe` against a stand-in `herdr` reporting a state on
  a change only, releasing a pane that no longer runs `sbx exec`; the
  install asking first. A real herdr only in `make integration`, beside
  the existing herdr integration test.
- A real sbx only opt-in by a variable on macOS (`UNAGIT_SBX_TEST=1`), as
  Ghostty and the keychain are - sbx needs a hypervisor, not the Docker
  image of `make integration`.

## Order of work

1. **Spike by hand**, everything marked so above, written back into this
   file: a VM with no repository mounted, a clone and a push from it to the
   user's GitLab with an sbx secret, a second agent by `sbx exec`, `-w`,
   the read-only Gradle cache, `sbx template load` of a `docker save`,
   what a template must have beyond an image.
2. `internal/sandbox` with sbx, the stand-in, the tests.
3. Profiles: the environments repository in Settings, `profile.yaml`, the
   build and the image state, the approval, sbx's block in Integrations.
4. Open in Sandboxed Agent… for one repository's worktree, This Terminal
   only; then groups; then every place.
5. The Sandboxes tab, the removal with a worktree, Activity.
6. The herdr plugin and its install in Integrations.
7. AGENTS.md's map gains `internal/sandbox`; the README its section.
