# Why unagit

Work spread over a few dozen repositories and two forges turns into a mess of
its own. Half of them are cloned and half are not, nobody remembers where, and
the ones you cloned last year sit in whatever directory you happened to be in
at the time. Add a review and it is a browser tab for the diff, a terminal for
the code, and a ritual of `fetch`, `checkout`, `stash` every time you switch.

unagit is the navigator over that:

- **one list of everything**, across every GitLab server and GitHub at once;
- **a layout on disk that you decide** instead of inherit;
- **one keystroke to be inside any of it**, in the editor you use.

## A navigator first

The name is the wider job. Reviewing merge requests is the sharpest part of
unagit, but the tool is a navigator over every repository you work with: what
exists, where on disk it goes, and getting inside it.

## Reviews that read like your own edits

Most review tools show you a diff. unagit gives you a checkout where the merge
request *is* the diff: HEAD and git's index sit on the merge base, the files on
the merge request's head. Your editor's gutter, `git diff`, `]c` and
`:DiffviewOpen` all show the change as a whole, because to them it is simply
uncommitted work. See [The Review Worktree](/guide/review).

## Worktrees, not clones

Each repository has one main clone. A merge request gets a worktree of it, so
three half-finished reviews can sit on disk at once without committing
anything. They cost a checkout, not a clone, because they share the
repository's object store.

## Nothing behind your back

- **Instant startup.** The lists are cached on disk and refreshed only when you
  ask; only what you watch, and pipelines still running, is asked about by
  itself.
- **Your choices stay yours.** `config.yaml` changes only when you change a
  setting, so it can live in a dotfiles repository and travel between machines.
- **Tokens are sealed.** They exist in plain text only in the memory of a
  running unagit. See [Security](/reference/security).
- **Nothing forces without asking.** Every force push is chosen in a question,
  and leases what origin had, so nobody's work is lost.
- **Every rewrite can be undone.** See [Rewriting History](/guide/rewrites).

## One vocabulary

A GitHub organisation is a group, and a pull request is a merge request. Where
one forge lacks something, such as subgroups or a group-wide merge request
listing on GitHub, unagit makes it up from what the API does have, so the
screen reads the same whichever server you are on.
