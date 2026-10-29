# Files on Disk

## What unagit keeps

```
~/.config/unagit/config.yaml      written by the Settings tab, your choices alone
~/.config/unagit/tokens.enc       sealed with your passphrase
~/.config/unagit/index-*.json     the cached lists
~/.config/unagit/state.json       what use taught it: usual places, usual people
~/.config/unagit/sessions/        what is open in an editor right now
~/.config/unagit/watch/           what is watched, what was last seen of it
~/.config/unagit/themes/*.json    themes of your own
```

`$XDG_CONFIG_HOME/unagit` is used instead when that is set; `unagit where`
prints the paths, and `UNAGIT_CONFIG_DIR` moves them.

## Portable and local

**`config.yaml` is the user's choices, nothing else.** It changes only when you
change a setting, so it can be kept in a dotfiles repository, with
[chezmoi](/guide/chezmoi) say, and carried to another machine.

Everything else is this machine's, to sync another way or not at all:

| File | Holds |
| --- | --- |
| `index-*.json` | the lists as last refreshed, so startup is instant |
| `index-users.json` | names for GitHub logins, kept for a month |
| `index-starred.json` | repositories cloned from your GitHub stars |
| `state.json` | where things were opened, the last Open…, whom merge requests went to |
| `sessions/` | a file per open directory, named by pid; private (0700), since its sockets reach the editor |
| `watch/` | the watches, each thing's history, `notify.log` |
| `publications/` | the progress of a New Repository from Folder… |
| `readmes/` | READMEs of starred repositories, as last read |
| `run/credential.sock` | where sbx asks for sandbox tokens; there only while unagit runs unlocked |

## In your repositories

```
<root>/<group>/<repo>                               the main clone
<root>/<group>/.unagit/<repo>/<iid>-<branch>        a merge request's branch worktree
<root>/<group>/.unagit/<repo>/review-<iid>-<branch> its review worktree
<root>/.unagit/groups/<folder>/<repo>               a grouped worktree's member
<root>/.unagit/groups/<folder>/.unagit-group.json   which repository each member is
```

In each repository's git configuration and git directory:

| Where | Holds |
| --- | --- |
| `unagit.mr.*` (per worktree) | what the worktree is: base, head, iid, project, source, target, url, mode |
| `branch.<name>.unagitBase` | the branch a branch unagit made was made from |
| `branch.<name>.unagitRebasedFrom` | the upstream a rebase moved away from, the lease for the force push |
| `refs/unagit/rewrites` | both states of every rewrite, so it can be undone; never pushed |
