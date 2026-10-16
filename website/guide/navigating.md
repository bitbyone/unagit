# The Lists

The three main lists, Repositories (`1`), Merge requests (`2`) and Worktrees
(`3`), are made the same way: a table, a fuzzy filter on `/`, and a detail
column on `Enter` that follows the cursor as you move.

## What is on disk

The first column of Repositories says what is on disk, and the path column
says where. That matters once different groups live in different places.

| Marker | Means |
| --- | --- |
| `○` | nothing on disk |
| `●` | a branch worktree |
| `◐` | a review worktree |
| `◉` | both |
| `⊘` | hidden |
| `◎` | its pipelines are watched |
| `★` | a favourite |

A path not cloned yet is drawn dim: it is where the repository *will* go.

## The columns

| Column | Says |
| --- | --- |
| `RMT` | the clone's branch against origin: `✓` up to date, `↓3` behind, `↑2` not pushed, `↑2↓3` both |
| `CI` | the newest pipeline: green passed, red failed, turning while it runs, blue waiting, grey skipped |
| `✎` | files not committed: `12/3` is twelve versioned with changes and three unversioned |
| `NEW` | commits pushed to a merge request since your review last checked out its head |
| `APPR` | approvals: `✓` when you approved, `1/2` of those asked for |
| `COM` | threads still to resolve on GitLab, in amber; on GitHub, who has said something |
| `SIZE` | what a clone takes on disk with all its worktrees |
| `LABELS` | a merge request's labels, as pills in their own colours |

`RMT` and `✎` are read from the disk, so they cost nothing and are read again
whenever you switch to a list or come back to the terminal. An edit made in
your editor shows up without asking. A rebase or a merge git stopped in the
middle of shows in `RMT` instead, in red.

A column nothing has anything in stays out of the way. `v` lists the columns in
the order they stand, each with an eye: `space` hides one or shows it again.

## Refreshing

The lists are JSON caches on disk, so startup is instant. They are refreshed
only when you ask:

- `R` refreshes the whole list, behind the interface: a spinner in the header,
  the list usable meanwhile.
- `r` refreshes the row under the cursor.

A refresh asks the forge only about what the list shows. What you hide keeps
what was last read, so hiding what you never look at makes the refresh faster.
When it is done, the refresh sums up what it found: new merge requests, ones
with commits since your review, failed pipelines, worktrees tidied away.

When the server does not answer, out of the VPN say, the status line says so
and the pipeline column turns to `?` after a few tries. Never a box in front.

## Filtering and ordering

- `/` fuzzy-finds. `Esc` leaves the filter, then clears it.
- `L` hides everything you have not cloned.
- `x` hides a repository you never want to see. `X` lists the hidden ones.
- `H` on a merge request hides its author's merge requests: Renovate's, a CI bot's.
- `v` narrows Merge requests to yours, those you review, those assigned to you,
  can leave drafts out, and hides requests older than a number of days. In
  every list it can narrow to one server.
- `o` chooses the order: by activity or name everywhere, and per list by edits,
  size, remote, new commits, comments or frecency.
- `Ctrl-G` groups the list: repositories by their group, merge requests by repository.

The choices stick between starts.

## Marking several rows

`space` marks rows in Repositories and Merge requests. The keys then act on
every marked row at once: `x` and `H` hide, `r` refreshes, `p` pulls, `C`
clones, `Ctrl-T` tags, `Ctrl-F` stars, `y` copies one of each a line, `V` marks
merge requests reviewed, `Ctrl-W` makes one grouped worktree of repositories.
`Alt-Enter` lists what the marks can do.

## Finding any repository

`+`, from anywhere, is **Find Repository…**. It searches every repository
unagit knows, the hidden ones too, and once typing pauses it asks the servers
as well. `Enter` brings the repository in: a hidden one is shown again, one no
group of yours has is listed on its own.

## Tags and favourites

- `Ctrl-F` stars a repository or a merge request. Favourites come first in a
  flat list, above a line.
- `Ctrl-T` puts your own tags on a repository: `oss`, `work`, `personal` and
  whatever else you define in Settings › Tags. `f` shows only the repositories
  wearing the tags you pick, `F` all of them again.
