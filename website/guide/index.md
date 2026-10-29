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

![unagit's merge requests, the detail of one beside the list](/shots/merge-requests.svg)

Five tabs, reached by their number, and Settings at the right end:

| Tab | What it holds |
| --- | --- |
| **Repositories** `1` | every repository of every server and group you picked, and what is on disk |
| **Merge requests** `2` | the merge requests open in them, with pipeline, approvals and threads |
| **Worktrees** `3` | every worktree unagit made: branches, reviews, grouped worktrees |
| **Activity** `4` | what runs in the background: watches, agents, open editors, the log |
| **Sandboxes** `5` | unibox's sandboxes, their tasks and the envs they are made from; grey without unibox |
| **Settings** `S` | servers, tokens, groups, destinations, integrations, themes |

`unagit -s` is the other way in: one repository, its changes, log, branches
and shelf, with no server and no setup. See
[One Repository Alone](/guide/standalone).

## Try it first

`make demo`, in a checkout of unagit, runs the real unagit over a made-up
world - two servers answered on your own machine, a dozen repositories,
merge requests, clones with changes, a few sandboxes - and touches nothing
of yours. The passphrase is `demo`.

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
- [One Repository Alone](/guide/standalone): `unagit -s`, a git interface for any checkout
- [Tutorials](/tutorials/): step by step through the common jobs
