# Sandboxed agents

The sandboxing - envs, sandboxes, tasks, the agents' tokens and network
rules - is **unibox**, a project of its own beside this one (`../unibox`).
Its design, the reasons behind it and what was verified about Docker
Sandboxes (`sbx`) are in unibox's `docs/design.md`. unibox imports nothing
of unagit's and unagit nothing of unibox's: unagit is one UI over it,
driving its CLI and reading its versioned `--json`. What follows is that
UI's part, kept here because it is unagit's to build.

Since unibox 2 (2026-10-25) a team describes its sandboxes as sbx's env
files (`*.sbxenv.yaml`) and kits; an **env** names those files, the
**sandbox** is what sbx creates from it, a **task** is your checkouts
mounted into it, or clones from the forge, with an agent. unibox 1's
profiles, runtimes, templates and image builds are gone, and with them
everything unagit had for them.

## What unagit reads

The contract is unibox's command line (its docs/design.md, "The command
line" and "The boundary"); what unagit relies on in it:

- Every `--json` object carries `api_version`; unagit knows 2, decodes into
  types of its own (`internal/unibox/api.go`) and refuses another, saying
  to update unagit or unibox.
- The overview is `version`, `env ls` and `env show <env>` for each env (its
  sandbox's name, its agent, what it lacks), `sandbox ls`, `context`,
  `config get` and `secret ls --all`; a failure of the last two leaves the
  screen standing (`Overview.Unread`), and an env whose files do not load
  keeps its row, saying why.
- A row's detail is read the first time it is looked at: an env's `var ls`,
  `secret ls` and `repo ls`; a sandbox's `port ls`, `network ls` and
  `sandbox resources`.
- `task check` answers `needs`, each with a `code`; `branch_not_pushed` and
  `branch_missing` are offered **Push and Start**, what `run` asks about at
  a terminal (`arg_missing`, `uncommitted`, `local_commits`,
  `branch_switch`) **Start Anyway**, anything else refuses the start.
- A failure under `--json` is `{"api_version", "code", "message"}`; exit
  status 4 is a refusal, shown as a warning.
- What sbx or unibox asks at a terminal - approving an env and its host
  commands (`sandbox create`, `recreate`, `init`), a token (`secret set`) -
  runs in this terminal with unagit suspended; no value passes through
  unagit unless the user keeps the token in its vault.

## Built

- `internal/unibox`: the client, its stand-in for tests in
  `internal/unibox/uniboxtest` (two envs: web, made and the context, with
  two tasks; api, not made, lacking a value).
- The Sandboxes tab, `5`, laid out as before: the sandboxes as cards and
  the environment beside them, the tasks and the detail, the **envs** (where
  the profiles were) and the log of every command given to unibox.
- The context on the tab bar, after **Sandboxes [5]**, in `tabs.context`,
  and in the tab's own headline (`sandboxContextBadge`).
- Tasks: resume (any agent of the sandbox, afresh with `--new`), shell,
  instructions, add a repository, open the mounted checkout, rename,
  remove. Sandboxes: new task, shell, ports (on, off, on at start), network
  (allow, deny, take back, allow what was refused), resources, stop,
  recreate, remove and force remove, the context. Envs: new task, create the
  sandbox, values (this env, its repository's, every env), tokens (in the
  terminal, or kept in unagit), the repository index, the context, default
  env, update the files, forget.
- unibox's settings (the default env and the switches), doctor with its
  fixes run in the terminal, prune, herdr's plugin, setup step by step
  (`unibox init` from a clone or an address, then each env's values,
  tokens, sandbox).
- Across unagit, on repositories, worktrees, grouped worktrees and merge
  requests: **Sandbox with &lt;agent&gt; in &lt;context&gt;…** (only where is
  asked), **Sandbox with &lt;agent&gt;…** (which sandbox only when more than
  one runs it; an env's sandbox not made yet among them), **Open in
  Sandbox…** (everything), and each agent in each place by typing. A clone
  is mounted; a worktree cannot be, so it - each member of a group - is
  cloned at its branch; a merge request is cloned from the forge's HTTPS
  address on its source branch, a repository not cloned on its default.
- **Add to unibox Index…** on a repository: one env's index or every
  env's, and when a task takes it.
- Credentials from unagit's vault: `secret set <service> -e <env>
  --command "unagit credential get <service>"`
  ([sandbox-credentials.md](sandbox-credentials.md)).
- A session record names its sandbox and task, and the mounted checkout as
  its directory, so Activity and the lists see the agent where it works.

## Not yet

- A mark on a clone an agent works on in a sandbox, and the commit dialog's
  warning before committing beside it.
- Removing a task with the worktree or clone it holds.
- `task new`'s picker of the env's index and recent repositories, beside
  unagit's own sources.
- `env add` from unagit (an env is added by `unibox init` for now) and
  `network init`, which `init` walks.
