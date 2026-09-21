package secret

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadPassphrase reads a secret from the controlling terminal with echo off.
// It deliberately refuses to fall back to a pipe so secrets can never be fed
// in from a script, an environment variable or a command line argument.
func ReadPassphrase(prompt string) ([]byte, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, errors.New("a terminal is required to enter secrets")
	}
	defer tty.Close()

	fmt.Fprint(tty, prompt)
	b, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ReadPassphraseTwice asks for a secret and its confirmation.
func ReadPassphraseTwice(prompt, confirm string) ([]byte, error) {
	a, err := ReadPassphrase(prompt)
	if err != nil {
		return nil, err
	}
	b, err := ReadPassphrase(confirm)
	if err != nil {
		return nil, err
	}
	if string(a) != string(b) {
		return nil, errors.New("entries do not match")
	}
	if len(a) == 0 {
		return nil, errors.New("empty passphrase is not allowed")
	}
	return a, nil
}

// ReadLine reads a visible line from the terminal (for non-secret input).
func ReadLine(prompt, def string) (string, error) {
	if def != "" {
		fmt.Printf("%s [%s]: ", prompt, def)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	s, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	return s, nil
}
