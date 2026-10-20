package credential

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// parentOf names the program that ran the client - sbx's daemon when a
// sandbox asks - for the record of the handout. It is what ps says, and
// nothing when ps says nothing: the record is for the user to read, not
// a check.
func parentOf(pid int) string {
	if pid <= 0 {
		return ""
	}
	ps := func(field string, of int) string {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ps", "-o", field+"=", "-p", strconv.Itoa(of)).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	ppid, err := strconv.Atoi(ps("ppid", pid))
	if err != nil || ppid <= 1 {
		return ""
	}
	return filepath.Base(ps("comm", ppid))
}
