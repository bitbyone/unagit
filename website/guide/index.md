# Getting Started

unagit is the **Universal Navigator Around GIT**: one place to see every
repository you work with, decide where each one lands on disk, and get into it.
Clone it, switch its branch, open it in your editor. When it is a merge
request you are after, land in a checkout where the whole change is already
sitting there as pending edits.

It speaks to [GitLab](https://gitlab.com) and [GitHub](https://github.com) at
the same time, in one list. Pull requests are merge requests here too: one
word for one thing.

If you have a review waiting right now, the
[Your First Review](/tutorials/first-review) tutorial walks you through it
from installing to approving.

## Install unagit

### Homebrew

The tap lives in unagit's own repository. Homebrew builds the release on your
machine from its tagged commit; nothing compiled is downloaded.

```sh
brew tap bitbyone/unagit https://github.com/bitbyone/unagit
brew install unagit
```

### From a checkout

```sh
git clone https://github.com/bitbyone/unagit
cd unagit
make install     # -> ~/.local/bin/unagit
```

`unagit --version` (or `unagit version`) prints the release it was built from.
A build that is not a release calls itself `dev`, with its commit.

### Requirements

- **git 2.38** or newer
- **Go 1.26** or newer, to build it
- an editor: `nvim` by default, or IntelliJ IDEA, VS Code, Zed or a command of your own

On macOS the passphrase can be kept in the keychain only in a build made with a
C compiler: the Xcode Command Line Tools, which `git` there needs anyway.

## Start it

```sh
unagit
```

The first start asks for a passphrase. It seals the tokens you are about to
give unagit, and it is asked for at every start after. Then you are walked into
**Settings**, where the whole configuration happens: there is no file to edit
by hand. [First Setup](/guide/setup) goes through it.

## What you see

```
 Repositories [1] │ Merge requests [2] │ Worktrees [3] │ Activity [4] │ Settings [5]
 /
╭ Merge requests ─────────────────────────────╮╭ acme/api-gateway !42 ─────────╮
│   REPO              MR  TITLE           COM ││ !42  Fix login rate limiting  │
│ ● acme/api-gateway  !42 Fix login rat…    7 ││ acme/api-gateway              │
│ ○ acme/billing      !17 Invoice roundi…     ││ feat/rate → main              │
│                                             ││                               │
│                                             ││ MERGE REQUEST                 │
│                                             ││ Author       jane (Jane Doe)  │
│                                             ││ Created      3d ago           │
│                                             ││ Reviewers    john             │
│                                             ││ Approvals    1 of 2 · john    │
│                                             ││ Pipeline     ● running        │
╰─────────────────────────────────────────────╯╰───────────────────────────────╯
 DETAIL  84/84 merge requests · indexed 4m ago · scope all projects
 ? help · q quit
```

Five tabs, reached by their number:

| Tab | What it holds |
| --- | --- |
| **Repositories** `1` | every repository of every server and group you picked, and what is on disk |
| **Merge requests** `2` | the merge requests open in them, with pipeline, approvals and threads |
| **Worktrees** `3` | every worktree unagit made: branches, reviews, grouped worktrees |
| **Activity** `4` | what runs in the background: watches, agents, open editors, the log |
| **Settings** `5` | servers, tokens, groups, destinations, integrations, themes |

## The three keys to remember

You do not have to learn the keys up front. Three are enough:

- `Alt-Enter` (or `Ctrl-A`) lists everything that can be done with the row under
  the cursor, each with its key beside it.
- `:` lists everything the screen itself can do.
- `?` shows the keys of the screen you are on.

The pickers teach the keys as you use them. More in
[Actions and Keys](/guide/actions).

## Next steps

- [First Setup](/guide/setup): servers, tokens, groups and where they go
- [The Lists](/guide/navigating): what the columns and markers say
- [The Review Worktree](/guide/review): the feature the whole tool exists for
- [Tutorials](/tutorials/): step by step through the common jobs
