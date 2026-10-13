package unibox

import (
	"encoding/base64"
	"fmt"
	"path"
	"strings"

	"github.com/tobola/unagit/internal/unibox/api"
)

func writeGuest(file string, data []byte, mode string) string {
	return "mkdir -p " + Quote(path.Dir(file)) + "\nprintf %s " + Quote(base64.StdEncoding.EncodeToString(data)) + " | base64 -d > " + Quote(file) + "\nchmod " + mode + " " + Quote(file) + "\n"
}

func initializeScript(p Profile, instructions string) string {
	s := "set -eu\n"
	for _, id := range p.Agents {
		a, _ := FindAgent(id)
		s += "mkdir -p \"$HOME/" + path.Dir(a.Instructions) + "\"\nprintf %s " + Quote(base64.StdEncoding.EncodeToString([]byte(instructions))) + " | base64 -d > \"$HOME/" + a.Instructions + "\"\n"
	}
	if p.Start != "" {
		s += writeGuest("/var/lib/unibox/start.sh", p.Files[p.Start].Data, "700")
	}
	s += "boot=$(cat /proc/sys/kernel/random/boot_id)\nprevious=$(cat /var/lib/unibox/started 2>/dev/null || true)\nif [ \"$boot\" != \"$previous\" ]; then\n"
	if p.Start != "" {
		s += "  /bin/sh /var/lib/unibox/start.sh\n"
	}
	s += "  printf '%s\\n' \"$boot\" > /var/lib/unibox/started\nfi\n"
	return s
}

func prepareScript(t api.Task, p Profile, a Agent) string {
	var b strings.Builder
	b.WriteString("set -eu\nexport GIT_TERMINAL_PROMPT=0\nmkdir -p " + Quote(t.Directory) + "\n")
	for _, r := range t.Repositories {
		dir := path.Join(t.Directory, r.Name)
		name, email, _ := AuthorParts(r.Author)
		fmt.Fprintf(&b, "url=%s\nbranch=%s\ndir=%s\n", Quote(r.URL), Quote(r.Branch), Quote(dir))
		b.WriteString(`refs=$(git ls-remote --symref "$url" HEAD "refs/heads/$branch")
default=$(printf '%s\n' "$refs" | awk '$1 == "ref:" && $3 == "HEAD" {sub("refs/heads/", "", $2); print $2}')
tip=$(printf '%s\n' "$refs" | awk -v ref="refs/heads/$branch" '$2 == ref {print $1}')
[ -n "$default" ] || { echo 'cannot determine the default branch; check the forge HEAD' >&2; exit 4; }
[ "$branch" != "$default" ] || { echo 'default branch refused; make a work branch first' >&2; exit 4; }
[ -n "$tip" ] || { echo 'branch does not exist on the forge; push it first' >&2; exit 4; }
if [ ! -d "$dir" ]; then
  tmp="$dir.unibox-clone"
  [ ! -e "$tmp" ] || { echo 'incomplete clone remains; inspect it in unibox shell and remove it before retrying' >&2; exit 4; }
  git clone --filter=blob:none --single-branch --branch "$branch" -- "$url" "$tmp"
  mv "$tmp" "$dir"
else
  [ -d "$dir/.git" ] || { echo 'task directory is not a clone; inspect it in unibox shell' >&2; exit 4; }
  [ "$(git -C "$dir" remote get-url origin)" = "$url" ] || { echo 'task origin changed; inspect it in unibox shell' >&2; exit 4; }
  [ "$(git -C "$dir" symbolic-ref --short HEAD)" = "$branch" ] || { echo 'task branch changed; restore it in unibox shell' >&2; exit 4; }
  [ -z "$(git -C "$dir" status --porcelain --untracked-files=all)" ] || { echo 'task has local edits; finish them in unibox shell before preparing again' >&2; exit 4; }
  git -C "$dir" fetch origin "+refs/heads/$branch:refs/remotes/origin/$branch"
  git -C "$dir" merge-base --is-ancestor HEAD "refs/remotes/origin/$branch" || { echo 'task has unpushed or divergent commits; preserve them before continuing' >&2; exit 4; }
  git -C "$dir" merge --ff-only "refs/remotes/origin/$branch"
fi
`)
		fmt.Fprintf(&b, "git -C \"$dir\" config user.name %s\ngit -C \"$dir\" config user.email %s\n", Quote(name), Quote(email))
		b.WriteString("git -C \"$dir\" config push.default simple\n")
		if p.Git.CoAuthor == nil || *p.Git.CoAuthor {
			hook := "#!/bin/sh\nset -eu\ngit interpret-trailers --in-place --if-exists addIfDifferent --trailer " + Quote("Co-Authored-By: "+a.CoAuthor) + " \"$1\"\n"
			b.WriteString(writeGuest(path.Join(dir, ".git", "unibox-hooks", "commit-msg"), []byte(hook), "700"))
			fmt.Fprintf(&b, "git -C \"$dir\" config core.hooksPath %s\n", Quote(path.Join(dir, ".git", "unibox-hooks")))
		} else {
			b.WriteString("git -C \"$dir\" config --unset core.hooksPath || true\n")
		}
		if p.Bootstrap != "" {
			fmt.Fprintf(&b, "if [ -f %s ]; then (cd \"$dir\" && /bin/sh %s); fi\n", Quote(path.Join(dir, p.Bootstrap)), Quote(p.Bootstrap))
		}
	}
	return b.String()
}

// A disconnected host process is not proof that its guest process ended.
const guestIdleScript = `set -eu
for f in /var/lib/unibox/runs/*; do
  [ -f "$f" ] || continue
  read -r pid born < "$f"
  now=$(awk '{print $22}' "/proc/$pid/stat" 2>/dev/null || true)
  if [ -n "$now" ] && [ "$now" = "$born" ]; then
    echo 'a guest session is still running; inspect it with unibox shell' >&2
    exit 4
  fi
done
`

func safeToRemoveScript(t api.Task) string {
	var b strings.Builder
	b.WriteString("set -eu\nexport GIT_TERMINAL_PROMPT=0\n")
	for _, r := range t.Repositories {
		dir := path.Join(t.Directory, r.Name)
		fmt.Fprintf(&b, "dir=%s\n", Quote(dir))
		b.WriteString(`[ -d "$dir" ] || continue_missing=true
if [ -d "$dir" ]; then
  [ -d "$dir/.git" ] || { echo 'task contains an incomplete clone; inspect it before removing' >&2; exit 4; }
  [ -z "$(git -C "$dir" status --porcelain --untracked-files=all)" ] || { echo 'task has uncommitted files; commit and push before removing' >&2; exit 4; }
  git -C "$dir" fetch origin '+refs/heads/*:refs/remotes/origin/*'
  [ -z "$(git -C "$dir" rev-list --branches HEAD --not --remotes=origin)" ] || { echo 'task has unpushed commits; push them before removing' >&2; exit 4; }
  [ -z "$(git -C "$dir" stash list)" ] || { echo 'task has stashes; preserve them before removing' >&2; exit 4; }
fi
`)
	}
	// Files outside the registered clones have no forge copy either.
	b.WriteString("if [ -d " + Quote(t.Directory) + " ]; then\nfor entry in " + Quote(t.Directory) + "/* " + Quote(t.Directory) + "/.[!.]* " + Quote(t.Directory) + "/..?*; do\n[ -e \"$entry\" ] || continue\ncase \"$entry\" in\n")
	for _, r := range t.Repositories {
		b.WriteString(Quote(path.Join(t.Directory, r.Name)) + " ) ;;\n")
	}
	b.WriteString("*) echo 'task contains files outside its repositories; preserve them before removing' >&2; exit 4 ;;\nesac\ndone\nfi\n")
	return b.String()
}

func sessionScript(id string, args []string) string {
	s := "set -u\nmkdir -p /var/lib/unibox/runs\nborn=$(awk '{print $22}' /proc/$$/stat)\nprintf '%s %s\\n' \"$$\" \"$born\" > " + Quote("/var/lib/unibox/runs/"+id) + "\ntrap " + Quote("rm -f "+Quote("/var/lib/unibox/runs/"+id)) + " EXIT\n"
	for _, arg := range args {
		s += Quote(arg) + " "
	}
	return s + "\nexit $?\n"
}
