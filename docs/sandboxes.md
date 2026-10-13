# Sandboxed agents: agentbox

## Where it stands

Designed (2026-10-12), nothing built. This is the brief for whoever
implements it; the reasoning is here so that the decisions are not undone
by accident. It replaces "Agents in a container" in [herdr.md](herdr.md),
and wins wherever the two disagree: that plan mounted the worktree and the
main clone's `.git` into a Docker container, and its launcher for herdr is
not used either.

Checked so far: `sbx` 0.47.0 on macOS (Apple Silicon) - its commands and
flags below come from its `--help`; `sbx ls --json` answers
`{"sandboxes": []}`, `sbx template ls --json` `{"images": []}`. Everything
marked **spike** has not been tried. `agentbox` is a working name.

## What it is for

A worktree - one repository's or a group's - gets a coding agent that runs
in a VM instead of on the machine. The agent has full autonomy inside it;
the machine, its home, its keys and unagit's tokens are out of its reach.
The user picks the agent, a sandbox profile (a Dockerfile and the
resources, kept in a git repository of their own), and whether the agent
shares a VM with others or gets one of its own.

## Two programs

The sandboxing is **agentbox**, a program of its own, usable without
unagit; **unagit is a UI over it** and one of its users.

- agentbox lives in this repository for now, but is built as its own
  binary (`cmd/agentbox`, `make build` makes both) with its own home, its
  own configuration and its own runtimes. Moving it to a repository of its
  own later - as incomm is - is moving directories.
- `make install` builds and installs both binaries in one invocation, under
  the same `PREFIX`. It compares each built binary with its installed copy
  and installs only one whose contents differ (or which is missing). Go's
  build cache avoids recompiling unchanged packages; modification times
  alone do not decide whether to replace an installed binary.
- unagit drives it as it drives herdr or Zellij: through its command line,
  `--json` for what it reads. It never imports agentbox's code, beyond the
  package of the JSON types (see [the boundary](#the-boundary)).
- unagit also **configures** agentbox: what the user chooses in unagit's
  Settings (the environments repository picked from the list of
  repositories, the runtimes) is handed to agentbox as its configuration.

| agentbox | unagit |
| --- | --- |
| its home: configuration, state, cache | Integrations: agentbox on or off |
| the environments repository: profiles, build, the agents' check, approvals | which worktree, which repositories, which branch |
| the runtimes: sbx now, Podman later | pushing the branch, with the vault's token |
| VMs and tasks: create, start, stop, remove | clone URLs from the forge's API |
| a task's preparation: clones, author, co-author hook, instructions, `start`, bootstrap | the Sandboxes tab, the pickers, Activity |
| running the agent, stopping an idle VM | the questions: approve, build, commit first |
| its herdr plugin | the commit dialog's warning, the pull |
| refusing a default branch | picking the environments repository from the list, handing agentbox its path |

## The picture

The parts:

```
+------------------------------- unagit (host) --------------------------------+
|  internal/ui                                                                 |
|  +------------------+ +------------------+ +------------+ +----------------+ |
|  | Sandboxes tab [6]| | Open in Sandboxed| | Activity   | | Settings [5]   | |
|  | Profiles         | | Agent...         | |            | | Integrations:  | |
|  | Sandboxes (VMs)  | | agent > profile  | |            | | agentbox, env  | |
|  | Tasks            | | > sandbox > place| |            | | repo, runtimes | |
|  +--------+---------+ +--------+---------+ +-----+------+ +-------+--------+ |
|           |                    |                 |                |          |
|           +--------------------+--------+--------+----------------+          |
|                                         | agentbox ... --json                |
|  workspace, gitx: push the branch, pull | (and agentbox config set ...)      |
+---------+-------------------------------+------------------------------------+
          |                               |
          |                               v
          |      +------------------- agentbox (host) ---------------------+
          |      | ~/.config/agentbox/config.yaml, state.json              |
          |      | profiles: parse, tree hash, build, agents' check, trust |
          |      | tasks: manifest, instructions, author, hook             |
          |      | Runtime: Available List Images Build Create Exec Stop   |
          |      |          Remove                                         |
          |      |   +----------------+      +----------------+            |
          |      |   | sbx adapter    |      | podman adapter |            |
          |      |   |                |      | (later)        |            |
          |      |   +-------+--------+      +----------------+            |
          |      | herdr plugin: agentbox herdr observe                    |
          |      +-----------+---------------------------------------------+
          |                  | sbx CLI
          | push, pull       v
          v           +--------------------------------------+
   +-------------+    | sbx microVM  ab-<profile>[-<task>]   |
   | forge       |<-->|  template = the profile's image      |
   | GitLab,     |    |  /work/<task>/<repo>   git clones    |
   | GitHub      |    |  the agents (claude, codex, ...)     |
   | protected   |    |  Docker Engine (compose, tests)      |
   | branches    |    |  GRADLE_USER_HOME (the VM's own)     |
   +-------------+    |  ~/.gradle/caches :ro <- host        |
     clone, push:     +--------------------------------------+
     the VM's own
     narrow token
```

Where each kind of setting lives:

```
 environments repository         ~/.config/agentbox/         unagit
 (git, the user's or a team's)                               config.yaml
 +--------------------------+   +-----------------------+   +----------------------+
 | kotlin/                  |   | config.yaml           |   | integrations:        |
 |   Dockerfile             |<--+   environments: <path>|<--+   agentbox:          |
 |   profile.yaml           |   |   runtimes:           |   |     on: true         |
 |   start.sh               |   |     sbx: {on, stop_   |   |     environments:    |
 |   instructions.md        |   |           idle}       |   |       gl/team/envs   |
 |   sbx/build.args         |   | state.json            |   +----------------------+
 | instructions/*.md        |   |   approvals, VMs,     |    the user's choices,
 +------------+-------------+   |   tasks, agents run   |    handed to agentbox
              |                 +-----------------------+
  what the    |                  agentbox's own
  environment v
  is      +--------------------------------------+
          | generated just before a start        |
          | ~/.cache/agentbox/sbx/               |
          |   agentbox/kotlin:<tree>  template   |
          |   flags: --cpus -m -e --deny-network |
          +--------------------------------------+
            a cache: never edited, regenerated
```

A start from unagit:

```
 user              unagit                     agentbox            sbx, VM        forge
  | Open in          |                          |                    |             |
  | Sandboxed Agent  |                          |                    |             |
  +----------------->| profiles, VMs --json --->|                    |             |
  | agent, profile,  |                          |                    |             |
  | sandbox, place   |                          |                    |             |
  +----------------->| check --json ------------>| approved? built?   |             |
  |<- approve? build?|<-- needs: build ---------+                    |             |
  +-- yes ---------->| build (log) ------------>| docker build, load>|             |
  |                  | push the branch ---------+--------------------+------------>|
  |                  | prepare (log) ---------->| create/start, ---->| clone ----->|
  |                  |                          | start.sh, clones   |             |
  |                  | the place runs:          |                    |             |
  |                  |   agentbox run --------->| sbx exec -it ----->| the agent   |
  |                  |                          |                    | push ------>|
  | Pull ----------->| fetch, fast-forward <----+--------------------+-------------+
```

What crosses the line between the machine and the VM:

```
   +------------- trusted --------------+       +----- untrusted -----+
   | host: home, ~/.ssh, unagit's vault |       | VM: the agent, sudo,|
   | main clones, the user's worktrees  | <-X-- | its clones, its     |
   | the configurations, the            |       | Docker              |
   | environments repository (approved) |       |                     |
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
host                                   sandbox VM
grouped worktree  <── git pull ───┐    /work/<task>/<repo>  clones of the
  (yours)                         │      branch, the agent commits
                                  │      and pushes here
                       forge ◄────┴──── git push
```

- **The agent clones from the forge, commits and pushes; the user pulls.**
  The worktree's branch is pushed before the agent starts, the VM clones
  every repository of the worktree at that branch, the agent pushes back
  to it, and the user pulls into the worktree as they would a colleague's
  commits.
- **Nothing of the VM is mounted on the host, and nothing git-related of the
  host is mounted in the VM.** That is the point of the design. A `.git`
  the agent can write holds commands the host would run: a hook,
  `core.fsmonitor`, `core.sshCommand`, a filter, `credential.helper` - and
  unagit, gitsigns, lazygit and IntelliJ run git in a worktree all the
  time. Through `credential.helper` the agent would even receive the forge
  token unagit hands git. A fetch from the forge carries objects and refs,
  never config or hooks, so the host stays safe however the agent behaves.
- **The cost**: what the agent has not pushed is not seen on the host. Its
  instructions tell it to commit and push as it goes.

Considered and rejected:

- **The worktree and the main clone's `.git` mounted read-write** (herdr.md's
  plan, with `config`, `hooks` and `config.worktree` mounted read-only over
  them). It works - herdr.md verified the locks - but every other branch's
  ref stays writable, the agent's files are on the host to be run by
  mistake, and the list of what must be locked is only as complete as our
  knowledge of git.
- **The same with the main clone read-only, the worktree on a detached HEAD
  and an object directory of its own** (`GIT_OBJECT_DIRECTORY`, the main
  objects as an alternate). Safe, but the agent sees a detached HEAD and the
  branch has to be moved after it. The user found it unpleasant.
- **`sbx run --clone`**: sbx's own clone inside the VM, of the host
  repository mounted read-only. One repository per sandbox, and how its
  commits come back is undocumented (the docs' git-workflow page is a 404).
- **Podman**: not now. agentbox's runtimes are neutral so that it can be
  added without touching unagit.

## Sandboxes and VMs

One sbx sandbox is one microVM: its own kernel, its own Docker Engine, its
own disk, persistent until removed. The agent runs in the VM itself, beside
that engine, as a non-root user with sudo (sbx's security docs); `sbx exec`
runs another process in the same environment, like `docker exec`.

Both arrangements are offered, and the user picks at the start:

- **A shared VM** per profile (`ab-<profile>`): tasks are directories
  `/work/<task>`, agents are processes side by side. Memory, the Gradle
  cache and Docker images are paid once. Between the agents there is no
  isolation at all - one user with sudo, one filesystem, one Docker, one
  set of ports (two `docker compose` Postgreses on 5432 collide). The
  picker says so.
- **A VM of its own** (`ab-<profile>-<task>`): nothing it does reaches
  another task.

Both persist when the agent ends, for their caches; removing is the user's
action. A VM in which the last agent agentbox started has ended is
**stopped** (`sbx stop`), so idle VMs hold no memory; its disk, caches and
tasks stay, and the next start boots it again (and runs the profile's
`start`). agentbox's configuration can turn the stopping off
(`runtimes.sbx.stop_idle`). It stops only VMs it made (`ab-` names), and
only when no agent it started is left in them.

`<task>` is the worktree's folder name and a short hash of its host path,
since two roots can hold folders of the same name.

## A start

### From unagit

On a worktree, a group or a merge request's branch worktree (never a review
worktree: its HEAD is the merge base on purpose, nothing is committed
there):

1. **Open in Sandboxed Agent…** asks, each picker skipped when it has one
   item and ordered by use: the agent, the profile, the sandbox, the place
   (the existing `agentPlaces`). The pickers are filled from `agentbox
   profiles --json` and `agentbox ls --json`. The sandbox picker is also
   where the runtime is chosen: the profile's VMs of every runtime, each
   carrying its own, then New Sandbox in <runtime>… for each runtime
   agentbox has on - plain New Sandbox… while there is one.
2. `agentbox check --json` with the profile, runtime, branch and
   directories. It answers what stands in the way, and unagit asks about
   each in its own dialogs: a default branch (refused: "make a worktree with
   a branch first"), uncommitted changes (they will not reach the VM, which
   clones from the forge - Commit… or go on without them), a profile not
   approved (Approve…, see [trust](#trust)), an image outdated or not built
   (Build…, with the profile's diff).
3. unagit **pushes the branch** of every repository of the worktree that
   has no upstream or is ahead of it - from the host, with its own
   credentials, through the path `P` uses. A branch origin has moved past
   stops the start with the reason; nothing is forced.
4. `agentbox prepare` with the repositories' clone URLs **from the forge's
   API** (the project's http URL), never from a local remote, which can
   carry a token (a chezmoi origin, AGENTS.md); https always - the VM has
   no SSH key. A `runTask` log of its output; it prints the VM and the task
   as JSON at the end.
5. The place chosen runs `agentbox run …` for the task - This Terminal,
   Zellij, Ghostty or herdr, as for any agent.
6. unagit's session record (`<config>/sessions/`) gets the VM and the task
   beside the pane, so Activity lists the agent with them.

### What agentbox does

`agentbox prepare` (and `run`, when not prepared):

1. Creates the VM if new (`sbx create`, see [runtime](#the-runtime)) or
   starts it, and runs the profile's `start` if the VM booted since it last
   ran.
2. For each repository, clones at the branch into `/work/<task>/<dir>`
   (`--filter=blob:none`), or fetches and fast-forwards when it is there
   already.
3. Sets the author and the co-author hook ([commits](#commits)), writes
   the agent's [instructions](#instructions-for-the-agent) into the VM's
   home, and runs the profile's bootstrap script from each repository that
   has one.

`agentbox run` then runs the agent: `sbx exec -it -w /work/<task> <vm>
<agent command>` as its child - not `exec`'d, so that when the agent ends
it can stop an idle VM and update its records. It passes the terminal,
signals and the window size through, and exits with the agent's status.

Without unagit, agentbox takes what unagit would give it from where it is
run: the repositories and the branch from the current directory or the
directories named, their clone URLs from `origin` (credentials stripped,
an ssh URL turned into https where the host is plain). It never pushes on
its own: a branch the forge does not have is refused ("push it first"),
unless `--push` asks it to run the user's own `git push -u`.

## Credentials

- **unagit's tokens never go into a VM**, nor into agentbox, in any form
  (AGENTS.md, the hard rule). The push before a start is unagit's, from
  the host; everything after it is the VM's own.
- **The VM pushes with a token of its own**, issued by the user for this,
  stored by sbx (`sbx secret set`, its keychain and proxy). Neither
  program sees it. Narrow: a GitLab project or group access token with
  Developer on the projects concerned; a GitHub fine-grained token on the
  repositories, contents write.
- **The forge is now the boundary.** The agent can push to any branch the
  token may, force-push, delete. Protect `main` and release branches on the
  forge, and if possible restrict the token to the work branches (GitLab
  push rules and protected branches, GitHub rulesets). The README says so
  where it explains the feature.
- The agents' own logins (Claude, Codex) are sbx's service secrets
  ([below](#one-image-several-agents)).

**Spike**: how sbx hands git its credential - its `github` service, a
service of one's own for a self-hosted GitLab, or a secret the VM's git
reads through a helper - and whether that works behind a VPN.

## Profiles

### Where they live

Profiles are files in a git repository of the user's, the **environments
repository**, edited and versioned as any repository is - in the editor,
committed with unagit's commit dialog, pushed, shared with a team:

```
sandbox-envs/
├── kotlin/
│   ├── Dockerfile
│   ├── profile.yaml
│   ├── start.sh
│   ├── instructions.md
│   ├── sbx/
│   │   └── build.args
│   └── podman/
│       └── build.args
├── node/
│   ├── Dockerfile
│   └── profile.yaml
└── instructions/
    ├── review-style.md
    └── no-migrations.md
```

A directory with a `profile.yaml` is a profile, named after the directory.

agentbox knows the repository by its path (`environments` in its
configuration) and reads the working tree - what the user sees in their
editor is what runs. In unagit, Settings picks it **from the list of
repositories** (instance and project path), which keeps unagit's
config.yaml portable; unagit finds its main clone on this machine and
hands agentbox the path (`agentbox config set environments <path>`) at
every start and whenever the choice changes, if agentbox's differs. With
nothing chosen in unagit, agentbox's own setting is left as it is.

Neither program edits a profile. The Sandboxes tab shows them read-only,
parsed, and opens a profile's directory or file in the editor.

### The format

`profile.yaml` is agentbox's own format, kept small, and **knows no
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
agents: [claude, codex]       # what the image has
start: start.sh               # in the VM, once per boot of the VM
bootstrap: .agent/bootstrap.sh # per worked-on repository, if present, in the VM
instructions:                 # for the agent, in this order, after agentbox's own
  - ../instructions/review-style.md
  - instructions.md
agent_config:                 # JSON seeds merged into an agent's own
  claude: claude.json         # configuration in the VM home; seed keys win,
                              # the agent's state - projects, paths - stays
git:
  author: user                # the user's identity, from the host's git config
  co_author: true             # a Co-Authored-By trailer naming the agent
```

- An unknown field is an error naming it, never skipped: a misspelt
  `memmory` must not quietly change the environment. A profile that does
  not parse is listed with the error and cannot be started.
- **The image is the environment.** Everything static - JDK, Node, the
  agents' CLIs, certificates - goes into the Dockerfile. What depends on
  the checkout belongs to the worked-on repository's bootstrap.
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
- Considered: devcontainer.json (a standard, but agentbox would honour a
  fraction of it and the rest would silently not work; the read-only cache
  and the network rules do not map onto it) and sbx's `sbxenv.yaml`
  (sbx's alone, and experimental).

### Three scripts

Each at its own moment, all running inside the VM:

| what | lives in | runs | for |
| --- | --- | --- | --- |
| `Dockerfile` | the profile | when the image is built | everything static: toolchains, tools, certificates |
| `start` | the profile | once per boot of the VM | services and daemons (a shared `docker compose`), configuration from the environment |
| `bootstrap` | the worked-on repository | after a task's clone or fetch | the project's dependencies, generated code |

- **`start` runs once per boot, whoever booted the VM.** A VM can be
  started by something else (`sbx exec` from a terminal, sbx's TUI), so
  before every agent agentbox compares the kernel's `boot_id` with a mark
  it left in the VM (`/var/lib/agentbox/started`) and runs the script only
  when they differ, by `sbx exec`, its output in the preparation's output.
  A failure stops the start.
- Nothing runs once when the VM is made: what should happen once is static
  and belongs in the Dockerfile. If that proves wrong, `setup:` comes the
  same way.
- The scripts run only inside the VM, so they need no approval.

### Instructions for the agent

The agent has to know how it is meant to work - its branch, to commit and
push as it goes, never to force - and the user has more to say, in general
and per profile. None of it may go into the worked-on repository, where
it would be committed. So:

- **agentbox's own**, always first, fixed in the binary: the branch, the
  repositories and where they are, commit and push as you go, never force
  push, never push another branch.
- **Then the profile's `instructions`**, markdown files of the
  environments repository, in the order listed: shared ones from anywhere
  in it (`instructions/` by convention, attached to as many profiles as
  want them) and the profile's own. A path that does not exist is an
  error. They go only into the VM, so they need no approval.
- At a task's preparation agentbox writes the whole into the VM's home as
  the agent's user-level instructions (`~/.claude/CLAUDE.md`,
  `~/.codex/AGENTS.md` - each agent's file is in agentbox's table of
  agents), never into `/work`.
- `agentbox instructions <profile>` prints them as the agent gets them;
  the Sandboxes tab shows that in a pane under Profiles, rendered by
  `internal/md`, and Edit Instructions… opens the files in the editor.

### Commits

- `author: user` takes `user.name` and `user.email` from the host's git
  configuration of the worktree (unagit passes them as `--author`), and
  sets them in each clone in the VM. The commits are the user's, so a
  forge's push rule that the author must be one of its users passes. It is
  the one value; a profile cannot name someone else.
- `co_author: true` adds `Co-Authored-By: <agent>` to every commit, the
  agent's name and address from agentbox's table (Claude Code's
  `Claude <noreply@anthropic.com>`, and so on). It is not left to the
  agent: agentbox puts a `commit-msg` hook into each clone in the VM that
  adds the trailer when it is missing. A hook in the VM's own clone is the
  VM's business; nothing of it reaches the host.
- Absent, `git` means both as shown.

### What differs between runtimes

`profile.yaml` says nothing about a runtime, but an image may have to be
built a little differently for one - another `FROM`, a step only one of
them needs. That is said with Docker's own means, build arguments, from a
subdirectory named after the runtime:

```dockerfile
ARG BASE=ubuntu:24.04
FROM ${BASE}

ARG AGENTBOX_RUNTIME
USER root
RUN apt-get update && apt-get install -y openjdk-21-jdk git curl
RUN if [ "$AGENTBOX_RUNTIME" = sbx ]; then echo "sbx alone"; fi
```

```
# kotlin/sbx/build.args
BASE=docker/sandbox-templates:shell
```

- Building for runtime X, agentbox always passes `--build-arg
  AGENTBOX_RUNTIME=X`, so a condition needs no file, and every `KEY=VALUE`
  line of `X/build.args` as a `--build-arg`. No `X/` means the `ARG`
  defaults, which also keep the Dockerfile buildable by hand.
- Larger differences can be a stage per runtime (`FROM common AS sbx`,
  built with `--target`); not until one is needed.
- `build.args` is the one file a runtime directory has for now; an unknown
  file there is an error, as an unknown field is.
- The tree hash covers the whole profile, so a change under `podman/`
  marks sbx's image outdated too. A needless rebuild, kept for the simple
  rule.

### Trust

The environments repository is the user's choice, so it is trusted - but a
pull can bring a teammate's change, and some fields reach the host: the
caches (host paths mounted), `env` (host values passed in) and the network
rules. A change to those must be approved before the profile runs, as
`direnv allow` does:

- agentbox's state keeps, per profile, a hash of its host-facing fields as
  last approved. A new profile starts unapproved.
- `agentbox check` reports a profile whose fields differ, with the
  difference of those fields alone; `agentbox approve <profile>` records
  them. unagit shows the difference and asks, then approves; agentbox run
  from a terminal asks itself.
- The worked-on repositories still choose nothing: they give the bootstrap
  script, which runs inside the VM.

### sbx's templates and kits

sbx has two words for shaping a sandbox; the design uses one and keeps away
from the other.

- **A template** is the image a sandbox is made from: an OCI image in sbx's
  own image store (apart from Docker's), its root filesystem being what is
  installed in the VM - the OS, the toolchains, the agents' CLIs. `sbx
  create -t <template>` picks it. It is how the environment gets into the
  VM, so it is what a profile's Dockerfile becomes: built, saved and
  loaded (`sbx template load`), tagged by the profile's tree.
- **A kit** is a declarative package laid over a sandbox: files, an
  install hook, a startup hook, network rules, credential bindings,
  instructions for the agent. The built-in agents are kits; a *mixin* kit
  adds to one (`--kit`). Kits v3 are Early Access. **agentbox neither
  writes nor takes kits**: everything a profile says is expressible as
  flags and `sbx exec`, and a format still moving is not one to build on.
  If the spike finds something only a kit can do - the credential for
  git's push is the candidate - that is decided then.

### One image, several agents

A profile is one image holding every agent it offers, built on Docker's
agent-less base, and its VMs are made as shell sandboxes:

```dockerfile
# kotlin/sbx/build.args:  BASE=docker/sandbox-templates:shell
ARG BASE=ubuntu:24.04
FROM ${BASE}
USER root
RUN apt-get update && apt-get install -y openjdk-21-jdk nodejs npm
RUN npm install -g @anthropic-ai/claude-code @openai/codex
USER agent
```

`sbx create shell …` makes the VM, and each agent runs in it by `sbx exec
… claude`, `sbx exec … codex`.

- **A shared VM can hold several agents.** An image per agent
  (`sandbox-templates:claude`) would allow one.
- **One build per profile**, not per profile and agent: fewer builds, less
  disk, one image state on the tab.
- **The agents' versions are the user's**, in the Dockerfile, versioned. A
  new Claude Code is a rebuild; updating them in `start` instead trades
  that for a slower start.

What makes it likely to work, checked 2026-10-12: `docker/sandbox-templates`
has a `shell` tag beside the agents' (claude, codex, copilot, cursor,
devin, docker, droid, gemini, kiro, opencode). And the agents' logins are
not the agent kit's: sbx keeps them as service secrets (`anthropic`,
`openai`, `github`), global or per sandbox, and its proxy signs the
requests to those services with them, the key never entering the VM - so
they should hold for any process in it, an agent started by `exec`
included.

**Spike**: that `sbx exec … claude` in a shell VM does get its login
through the proxy, and whether sbx needs anything of a template beyond
`:shell`'s base. Should the login fail, the fallback is a VM per profile
and agent made by `sbx create <agent>`, the image still one.

### Which agents a profile offers

The profile declares them (`agents:`), and the build checks the
declaration against the image:

- **Declared**, so what to offer is known before anything is built, for an
  `image:` too without pulling it.
- **Checked**: after a build, before the image is loaded into the runtime,
  agentbox runs `docker run --rm <image> sh -c 'command -v …'` for the
  declared agents' commands. One missing fails the build, naming it
  ("profile kotlin declares codex, but the image has no codex - install it
  in the Dockerfile"), so the declaration cannot quietly stop being true.
  One present but not declared is mentioned and not offered. An `image:`
  is checked the same way once pulled.
- **Offered** by unagit: the agents declared by the profile and not
  turned off in unagit's Integrations. Whether an agent is installed on
  the host does not matter here; the VM runs the image's. The agent picker
  lists the agents of every profile, the profile picker then the profiles
  that have the one chosen, skipped when there is one.

agentbox has its own table of agents - id, command, its instructions file,
its co-author line, sbx's name for it - and never reads unagit's
`agents.All`; the ids are the same, so unagit maps them.

### Building

- `<tree>` is the git tree hash of the profile's directory at HEAD, so
  whether the image is current is a comparison with `sbx template ls
  --json`, per runtime: **built**, **outdated**, **not built**. A profile
  with uncommitted changes is marked so, and builds as it is, tagged
  `<tree>-dirty`.
- For sbx: `docker build` with the build arguments, the agents' check,
  `docker save`, `sbx template load`, tagged `agentbox/<profile>:<tree>`.
  A local build is for this machine's architecture, which is all a local
  sandbox needs.
- **A build shows what it is about to build.** `agentbox diff <profile>`
  prints what changed in the profile's directory since its image last
  built (the tree is in the tag) as a unified diff. unagit's Build…
  question draws it in unagit's own differ - the one the Changes dialog
  draws (`drawDiff`, `diffdraw.go`) - before it asks. A teammate's
  Dockerfile runs in the host's Docker when built; isolated there, but
  `RUN --mount` and build secrets can reach out, so the user sees it
  first. No approval of its own beyond that question.

## agentbox

### Its home

```
~/.config/agentbox/config.yaml   the user's choices
~/.config/agentbox/state.json    what use taught: approvals, VMs, tasks,
                                 the agents it runs, the boot marks
~/.cache/agentbox/               generated: tars, never edited
```

```yaml
environments: ~/workspace/bitbyone/sandbox-envs
runtimes:
  sbx:
    on: true
    stop_idle: true
```

A runtime not named is on when installed. unagit writes this file only
through `agentbox config set`, never by hand, and only what the user chose
in unagit's Settings.

### The command line

The contract unagit is written against. Everything a list shows has
`--json`; a long operation prints its progress as lines, for a log.
Called by unagit (`--json`, or `--yes` where a question would come) it
never asks: what stands in the way is an answer, not a prompt.

| command | does |
| --- | --- |
| `agentbox profiles [--json]` | profiles: name, agents, image state per runtime, approval, error, last commit |
| `agentbox instructions <profile>` | the instructions as the agent gets them |
| `agentbox diff <profile>` | what changed since the last build, a unified diff |
| `agentbox approve <profile>` | records the host-facing fields as approved |
| `agentbox build <profile> [--runtime R]` | builds, checks the agents, loads |
| `agentbox check …  --json` | what stands in the way of a start: default branch, uncommitted, approval, image |
| `agentbox prepare …` | the VM and the task, ready; JSON of both at the end |
| `agentbox run …` | the agent, in this terminal; prepares first if needed |
| `agentbox ls [--json]` | VMs: runtime, profile, state, memory, tasks |
| `agentbox tasks <vm> [--json]` | tasks: directory on the host, repositories, branch, agents |
| `agentbox shell <vm>` | a shell in the VM |
| `agentbox stop <vm>`, `rm <vm>`, `rm-task <vm> <task>` | as named |
| `agentbox config get/set <key> [<value>]` | its configuration |
| `agentbox herdr install-plugin`, `uninstall-plugin`, `status [--json]` | [herdr](#herdr) |

`check`, `prepare` and `run` take the same task: `--profile`,
`--agent`, `--runtime`, `--vm <name>` or `--new-vm`, `--branch`,
`--repo <dir>=<clone URL>` (repeated; or directories, as without unagit),
`--author "Name <email>"`.

Exit codes say what kind of failure: 2 not approved, 3 image outdated or
not built, 4 refused (a default branch, a branch the forge lacks), 1 any
other error. Errors are lower case and say what to do: `sbx is not signed
in - run sbx login`.

### The runtime

agentbox's neutral vocabulary; nothing above it says sbx.

```go
type Runtime interface {
	Name() string
	Available() error                     // installed, signed in, daemon up
	List() ([]Box, error)                 // name, profile, state, memory
	Images() ([]Image, error)             // built templates, by tag
	Build(p Profile, log io.Writer) error // the profile's image, for this runtime
	Create(spec Spec) error               // detached, idempotent by name
	Exec(name string, cmd Command) *exec.Cmd // -it when Interactive
	Stop(name string) error
	Remove(name string) error
}
```

sbx's side, from its `--help`:

| Runtime | sbx |
| --- | --- |
| `List` | `sbx ls --json` |
| `Images` | `sbx template ls --json` |
| `Build` | `docker build`, `docker save`, `sbx template load` |
| `Create` | `sbx create --name N -t T --cpus C -m M --deny-network H -e K shell <mounts>` |
| `Exec` | `sbx exec [-it] [-w DIR] [-e K=V] N cmd…` (starts a stopped VM) |
| `Stop` | `sbx stop N` |
| `Remove` | `sbx rm N` |

How a profile becomes flags:

| profile | sbx |
| --- | --- |
| `dockerfile` | the built template, `-t agentbox/<profile>:<tree>` |
| `image` | `-t <image>` |
| `cpus`, `memory` | `--cpus`, `--memory` |
| `deny_network` | `--deny-network` |
| `env` | `-e NAME` |
| `caches` | `<host>:ro` as an extra workspace, `-e <env>=<host>` |
| `start` | run by `sbx exec` when the VM's `boot_id` is new |
| `bootstrap` | run by `sbx exec` after the clones |

Spike questions about it:

- `sbx create` takes an agent and mounts the current directory when given
  no path. A VM must be created with no repository mounted: give it a
  directory of agentbox's own (`~/.cache/agentbox/vms/<vm>`, empty,
  read-only) as its workspace, unless sbx can do without one.
- Does `sbx exec` take `-w`?
- Do read-only mounts of host directories outside the workspace work
  (`<path>:ro` as an extra workspace)?
- Does `sbx template load` take a tar from `docker save`, or only one from
  `sbx template save`? If only the latter: build in a sandbox and `sbx
  template save` it, or push to a registry.

### herdr

herdr recognises an agent by its pane's foreground process, and an agent
behind `sbx exec` is not one it knows: no agent, no state. agentbox fills
that in with **a herdr plugin of its own**. It is the one way; there is no
launcher and no third-party plugin to choose instead.

What it does, as long as herdr runs - unagit open or not:

- `agentbox run` started in a herdr pane records the pane
  (`HERDR_PANE_ID`), the agent and the VM in agentbox's state. For every
  pane so recorded whose `agentbox run` is alive, the plugin reads the
  screen (`pane.read`), has herdr classify it by herdr's own rules for that
  agent (`herdr agent explain --file … --agent <kind> --json`), and reports
  the state (`pane.report_agent`, `pane.release_agent` when the agent is
  gone), with the VM's name as the pane's metadata
  (`pane.report_metadata`). It reports only on a change.
- It never parses argv: agentbox started the agent and wrote it down.
- unagit's Activity matches herdr's agents by pane, so the state arrives
  there with no change to it.

The idea, and the herdr calls, come from
[sbx-herdr](https://github.com/dvdksn/sbx-herdr) (read 2026-10-12 at
0.1.0). It has no licence, so none of its code is taken.

**The plugin is a manifest and nothing to build.** `herdr-plugin.toml`, its
commands calling the agentbox binary: a startup command and the
`pane.created` / `pane.focused` events run `agentbox herdr ensure`, which
starts one `agentbox herdr observe` per herdr socket (a lock in
`HERDR_PLUGIN_STATE_DIR`, so a second ensure does nothing), detached, its
log in that directory. Both are hidden commands that refuse to run outside
herdr (no `HERDR_SOCKET_PATH`).

**Installed only with the user's consent**, never quietly - it changes
herdr's configuration and leaves a process running with herdr:

- `agentbox herdr install-plugin` asks unless `--yes`; it writes the
  manifest, embedded in the binary, to `~/.config/agentbox/herdr-plugin/`
  with the absolute path of the running agentbox, then `herdr plugin link
  <dir>` and `enable`. `uninstall-plugin` undoes it.
- unagit's Settings › Integrations, in herdr's block, shows `agentbox herdr
  status --json` and offers Install herdr Plugin… (and Uninstall) behind
  its own question, then calls the command with `--yes`.
- The path changes when agentbox is upgraded or moved. Every agentbox
  command that finds the plugin installed compares the linked manifest
  with what it would write and rewrites it if it differs - the user
  consented to the plugin, not to a path.
- A sandboxed agent started in herdr without the plugin runs, with no
  state; unagit says once (`note`) that Install herdr Plugin… in Settings
  would show it.

The agents' own hooks (Claude's session id, opencode's states) do not cross
into the VM; the state is the screen's.

**Spike**: the herdr here is 0.9.1 and sbx-herdr asks for 0.9.3 - find the
version whose plugins have `pane.read`, `agent explain` and the events, and
have `status` say "herdr 0.9.3 or newer" when older. Check that `herdr
plugin link` takes a manifest whose commands are absolute paths outside its
directory, and that a plugin's commands get `HERDR_SOCKET_PATH` and
`HERDR_PLUGIN_STATE_DIR`.

### The boundary

- agentbox's code is `cmd/agentbox` and `internal/agentbox/...`. It
  imports nothing of unagit's packages; a test (`go list -deps
  ./cmd/agentbox`) fails if it does, as `TestSharedFieldsAreTheOnlyWayToMakeThem`
  fails on a stray widget.
- unagit imports nothing of agentbox but `internal/agentbox/api`, the JSON
  types of its command line; a test fails on anything else. unagit runs
  the binary, found through `a.executable("agentbox")` like any program.
- agentbox follows the repository's rules - gofmt, vet, race, the
  testenv isolation, the hard rule on tokens - as any package here.

## In unagit

**An integration**, in Settings › Integrations: agentbox on or off
(unset, on when installed), and its block:

- the environments repository, picked from the list of repositories and
  handed to agentbox as a path (see [where they live](#where-they-live));
- the runtimes agentbox has, each on or off, and stopping idle VMs -
  written to agentbox's configuration with `agentbox config set`;
- in herdr's block, agentbox's herdr plugin.

unagit's config.yaml keeps only what it must to be portable: whether
agentbox is on, and the environments repository as a repository of the
list. The rest is agentbox's.

**A Sandboxes tab**, `6`, after Settings (`5` stays, as the messages that
point at `Settings [5]` do). Three parts, each a list made the way the
other screens' are (`pane.go`, its actions as `uiAction`s), filled from
agentbox's `--json`:

| part | rows | actions |
| --- | --- | --- |
| **Profiles** | name, image state per runtime, approval, agents, last commit to its directory; a pane with its instructions as the agent gets them | Open in Editor…, Edit Instructions…, Build…, Approve…, Open in Sandboxed Agent… |
| **Sandboxes** | VM: runtime, profile, state, memory, tasks | Shell… (`agentbox shell` in a place), Stop, Remove (a question naming its tasks) |
| **Tasks** | in the VM under the cursor: worktree, agent, its state | Open Worktree, Remove Task |

Without agentbox installed or an environments repository chosen, the tab
says what is missing and where to set it. Activity still lists every
agent, sandboxed ones with their VM: Activity is what runs, the Sandboxes
tab is what it runs in.

Elsewhere:

- **Open in Sandboxed Agent…** on repositories with a worktree, worktrees,
  groups, merge requests with a branch worktree. Alt picks again what use
  would skip (the profile and the sandbox, even when one).
- **Pull** of a worktree or a group is what brings the agent's work; it
  exists already for branches with an upstream, check it covers a group in
  one go.
- **A worktree whose branch an agent works on in a VM** (agentbox's tasks
  name the host directory) wears a mark in the lists, and the commit
  dialog on it warns before a commit: "an agent works on this branch in
  <vm> - pull first". Nothing is forbidden; two commits apart on one
  branch only end in a conflict at the pull.
- Deleting a worktree or a group with a task in a VM offers to remove the
  task (`agentbox rm-task`) and, for a VM of its own, the VM. The default
  keeps the VM.
- What unagit learns from use - the order of the profile and sandbox
  pickers - goes to its `state.json`, as the place picker's does.
- Help rows for the tab and the new keys; the README explains the
  feature, agentbox, the environments repository and the forge-side
  protection.

Later, not in the first version: following the branch on the forge
(`internal/watch`) to say when the agent pushed, and pulling on request
from that news.

## Tests

`make test-unibox` runs only `cmd/unibox/...` and `internal/unibox/...`, with
the race detector; `make test-unibox-fast` runs the same packages without it.
These are the checks while implementing unibox, including before commits
that change only unibox. The unagit suite is not run for those changes.
`make test` and `make test-fast` retain the whole repository's suite for
changes across the boundary or to shared build machinery. The binary and
package name agreed after the original design is `unibox`.

agentbox:

- A stand-in `sbx` and a stand-in `docker` on their own PATH
  (`testenv.WriteExecutable`), answering `ls --json` and `template ls
  --json` from files and logging every argv: the commands for a shared and
  an own VM, the task script, `start` run on a new `boot_id` and not on the
  same one, the VM stopped when its last agent ends and not one it did not
  make.
- Profiles: parsing (an unknown field, a missing instructions file are
  errors), the tree hash and the image state, the build arguments
  (`AGENTBOX_RUNTIME` always, `build.args` of the runtime alone), the
  generated flags, the agents' check failing a build that lacks one, the
  approval refusing a changed cache or `env` and passing a changed `cpus`,
  the instructions put together in order, the author and the trailer added
  by the hook once.
- The command line: `--json` shapes against `internal/agentbox/api`, the
  exit codes, no question asked under `--json` or `--yes`; without unagit,
  URLs from `origin` with credentials stripped, a branch the forge lacks
  refused.
- No token in any argv or environment agentbox builds.
- The herdr plugin: the manifest written with the binary's path and
  rewritten when it moves; `observe` against a stand-in `herdr` reporting a
  state on a change only and releasing a pane whose `agentbox run` ended;
  the install asking first. A real herdr only in `make integration`.
- A real sbx only opt-in by a variable on macOS (`AGENTBOX_SBX_TEST=1`), as
  Ghostty and the keychain are - sbx needs a hypervisor, not the Docker
  image of `make integration`.

unagit:

- A stand-in `agentbox` on its own PATH answering the `--json` commands
  from files and logging its argv: the order check → (approve, build) →
  push → prepare → run; the clone URLs from the forge, never from a
  remote; the environments repository handed over as a path; no token in
  any argv or environment unagit gives it.
- The questions on what `check` reports: a default branch refused,
  uncommitted changes named, Approve… and Build… drawing their diffs.
- The commit dialog's warning on a branch an agent works on.
- UI: the Sandboxes tab and the pickers at several terminal sizes,
  `assertLegible`, agentbox's block in Integrations, help rows,
  `TestEveryActionIsNamedAndExplained`, `TestNoTwoActionsShareAKey`.
- The boundary tests of [the boundary](#the-boundary).

## Order of work

1. **Spike by hand**, everything marked so above, written back into this
   file: a VM with no repository mounted, a clone and a push from it to
   the user's GitLab with an sbx secret, an agent by `sbx exec` in a shell
   VM with its login, `-w`, the read-only Gradle cache, `sbx template load`
   of a `docker save`, what a template needs beyond `:shell`, the herdr
   plugin API.
2. agentbox's core: the runtime with sbx, profiles, the build and the
   agents' check, approvals, its stand-ins and tests.
3. agentbox's tasks: `check`, `prepare`, `run`, the instructions, the
   author and hook, `start`, stopping idle VMs; usable from a terminal
   without unagit.
4. unagit's integration: Settings and the configuration handed over, Open
   in Sandboxed Agent… for one repository's worktree in This Terminal; then
   groups; then every place.
5. The Sandboxes tab, the removal with a worktree, the commit warning,
   Activity.
6. agentbox's herdr plugin and its install from Integrations.
7. AGENTS.md's map gains `cmd/agentbox` and `internal/agentbox`; the
   README its section, and one of agentbox's own.
