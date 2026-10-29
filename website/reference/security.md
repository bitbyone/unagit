# Security

unagit holds tokens that can read and write your repositories. It keeps them
out of reach of everything but a running unagit, which hands out only the
tokens you keep for unibox's sandboxes.

## The vault

Tokens live in `~/.config/unagit/tokens.enc`, encrypted with **AES-256-GCM**
under a key derived from your passphrase with **Argon2id**. They exist in plain
text only in memory, for as long as unagit runs.

The passphrase is asked for in a dialog at every start. It is **never** read
from a flag, an environment variable, a file or a pipe. That is deliberate: no
script, and no coding agent working in a repository of yours, can feed unagit
its passphrase or read the tokens.

## Sandbox tokens

A token you keep in unagit for unibox's sandboxes - never a forge token -
is printed by `unagit credential get <name>` for whoever of your user asks,
over a socket that exists only while unagit runs unlocked. sbx asks this
way when a sandbox needs the token; any program of yours could, so keep such
tokens narrow. Every handout is written to Activity.

## The macOS keychain

On macOS, Settings › Security › `k` can keep the passphrase in the login
keychain instead. It is off unless you turn it on.

The item is created by the unagit binary, so its access list trusts that binary
alone. unagit then opens without asking, and any other program, `security`
included, gets a macOS dialog. After a rebuild macOS asks once whether the new
binary may.

It needs a build made with a C compiler, the Xcode Command Line Tools. There is
no equivalent on Linux: the Secret Service lets every program of yours read
what it holds, so unagit does not use it.

## Credentials never touch disk

git gets the token through a one-shot credential helper passed on its command
line, which reads it from the child process's environment. So the token never
reaches `.git/config`, a remote URL, or any other file. The tests check this by
reading every byte under the clone root after a clone and a worktree.

## SSH

You can clone over SSH and keep the token for the API alone. git runs with
`BatchMode=yes`, so a passphrase-protected key without an agent fails at once
instead of hanging, and unagit says what to do about it.

## Watching

Watching runs only while unagit runs. There is no background service, because
the token is only ever in a running unagit's memory.

## Force pushes

Nothing forces without asking. Every force push is chosen in a question, and
leases what origin had as last fetched, so a commit someone pushed meanwhile
makes git refuse instead of being lost.
