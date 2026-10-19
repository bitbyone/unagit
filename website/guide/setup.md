# First Setup

There is nothing to edit by hand. Everything below happens in **Settings**
(`S`), which unagit opens on its first start.

## 1. Choose a passphrase

The first start asks for a passphrase. It seals your tokens in
`~/.config/unagit/tokens.enc` and is asked for in a dialog at every start. It
is never read from a flag, an environment variable or a pipe.

::: tip On macOS
Settings › Security › `k` can keep the passphrase in the login keychain, where
only the unagit binary may read it. unagit then opens without asking. After a
rebuild macOS asks once whether the new binary may.
:::

## 2. Add a server

In Settings, `a` adds a server: GitLab.com, a self-hosted GitLab, or GitHub.
Paste a token when asked.

| Forge | Token scopes |
| --- | --- |
| GitLab | `api` |
| GitHub | `repo` **and `read:org`** |

::: warning GitHub needs read:org
Without `read:org` GitHub answers the organisation listing with an empty array
rather than an error, so your organisations simply would not show up.
:::

`v` checks a token against the server before you trust it.

## 3. Pick your groups

Under the server, `space` picks the groups you work with. A group can be taken
on its own, with its own repositories, or with its whole tree of subgroups. A
GitHub organisation is a group here.

A repository that belongs to none of your groups can still be listed on its
own: `+` from anywhere finds it on the server, and `Enter` brings it in.

## 4. Say where things go

`d` sets a directory. It can be set at every level, and a repository is cloned
under the first of these that applies:

1. its own exact destination (`e` in Repositories)
2. its group's directory
3. its server's directory
4. the global default, `~/unagit`

So `acme/platform` can live in `~/work/platform` while everything else stays
under `~/unagit`, and a subgroup can override its parent.
[Where Things Land](/guide/layout) shows the whole layout.

## 5. Refresh

Go to Repositories (`1`) and press `R`. The refresh runs behind the list: the
header turns a spinner and the lists stay usable. When it is done, every
repository of your groups is listed with what is on disk.

## Cloning over SSH

You can clone over SSH and keep the token for the API alone. git runs with
`BatchMode=yes`, so a passphrase-protected key without an agent fails at once
instead of hanging. Load the key into the agent first:

```sh
ssh-add --apple-use-keychain ~/.ssh/id_rsa
```

## The configuration file

What you choose in Settings is written to `~/.config/unagit/config.yaml`, and
nothing else is. It changes only when you change a setting, so it can be kept
in a dotfiles repository and carried to another machine. `unagit where` prints
where everything is kept.
