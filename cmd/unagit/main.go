// Command unagit is a GitLab TUI for cloning projects and reviewing merge
// requests.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tobola/unagit/internal/config"
	"github.com/tobola/unagit/internal/gitlab"
	"github.com/tobola/unagit/internal/secret"
	"github.com/tobola/unagit/internal/ui"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "unagit",
		Short: "GitLab TUI for cloning projects and reviewing merge requests",
		Long: "unagit lists the projects and open merge requests of the GitLab groups you\n" +
			"selected, clones them under a root directory and opens your editor there.\n\n" +
			"The GitLab token is stored encrypted (Argon2id + AES-256-GCM). The passphrase\n" +
			"is asked for in a dialog on every start and the token only ever exists in\n" +
			"memory.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
	root.AddCommand(initCmd(), tokenCmd(), passphraseCmd(), whereCmd())
	return root
}

// runTUI unlocks the token and starts the interface.
func runTUI() error {
	cfg, err := config.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no configuration at %s - run 'unagit init' first", config.Path())
		}
		return err
	}
	if _, err := os.Stat(config.TokenPath()); err != nil {
		return fmt.Errorf("no encrypted token at %s - run 'unagit init' first", config.TokenPath())
	}
	// The passphrase is asked for inside the TUI, so unagit looks the same
	// whether it is started from a shell or from inside nvim.
	blob, err := secret.Load(config.TokenPath())
	if err != nil {
		return err
	}
	return ui.NewLocked(cfg, blob).Run()
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the configuration and store the GitLab token encrypted",
		Long: "init asks for the GitLab URL, the root directory, the editor, and finally for\n" +
			"the GitLab personal access token (api scope) plus a passphrase to encrypt it.\n" +
			"Both secrets are read from the terminal with echo off - they are never taken\n" +
			"from arguments, environment variables or pipes.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Default()
			if existing, err := config.Load(); err == nil {
				cfg = existing
				fmt.Printf("Updating the existing configuration at %s\n\n", config.Path())
			}
			var err error
			if cfg.GitLabURL, err = secret.ReadLine("GitLab URL", cfg.GitLabURL); err != nil {
				return err
			}
			cfg.GitLabURL = strings.TrimRight(cfg.GitLabURL, "/")
			if cfg.RootDir, err = secret.ReadLine("Root directory for clones", cfg.RootDir); err != nil {
				return err
			}
			if cfg.Editor, err = secret.ReadLine("Editor command", cfg.Editor); err != nil {
				return err
			}
			if err := os.MkdirAll(config.Expand(cfg.RootDir), 0o755); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Printf("\nWrote %s\n\n", config.Path())
			if err := storeToken(cfg); err != nil {
				return err
			}
			fmt.Println("\nAll set. Run 'unagit', press 's' to pick your groups, then 'p' and 'm' to build the indexes.")
			return nil
		},
	}
}

func tokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "token",
		Short:         "Replace the stored GitLab token",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("run 'unagit init' first: %w", err)
			}
			return storeToken(cfg)
		},
	}
}

func passphraseCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "passphrase",
		Short:         "Change the passphrase that protects the stored token",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			blob, err := secret.Load(config.TokenPath())
			if err != nil {
				return err
			}
			old, err := secret.ReadPassphrase("Current passphrase: ")
			if err != nil {
				return err
			}
			token, err := secret.Decrypt(blob, old)
			zero(old)
			if err != nil {
				return err
			}
			next, err := secret.ReadPassphraseTwice("New passphrase: ", "Repeat new passphrase: ")
			if err != nil {
				return err
			}
			newBlob, err := secret.Encrypt(token, next)
			zero(token)
			zero(next)
			if err != nil {
				return err
			}
			if err := secret.Save(config.TokenPath(), newBlob); err != nil {
				return err
			}
			fmt.Println("Passphrase changed.")
			return nil
		},
	}
}

func whereCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "where",
		Short: "Print the configuration paths",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("config:  ", config.Path())
			fmt.Println("token:   ", config.TokenPath())
			fmt.Println("projects:", config.IndexPath("projects"))
			fmt.Println("mrs:     ", config.IndexPath("mrs"))
			fmt.Println("groups:  ", config.IndexPath("groups"))
		},
	}
}

// storeToken reads a token and a passphrase from the terminal, verifies the
// token against the API and writes the encrypted blob.
func storeToken(cfg *config.Config) error {
	fmt.Println("Paste your GitLab personal access token (scope: api). It will not be shown.")
	token, err := secret.ReadPassphrase("Token: ")
	if err != nil {
		return err
	}
	token = []byte(strings.TrimSpace(string(token)))
	if len(token) == 0 {
		return fmt.Errorf("empty token")
	}

	fmt.Print("Verifying against ", cfg.GitLabURL, " ... ")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	user, err := gitlab.New(cfg.GitLabURL, string(token)).CurrentUser(ctx)
	if err != nil {
		fmt.Println("failed")
		return err
	}
	fmt.Printf("ok, hello %s\n\n", user.Username)

	pass, err := secret.ReadPassphraseTwice(
		"Passphrase to encrypt the token: ",
		"Repeat the passphrase: ")
	if err != nil {
		return err
	}
	blob, err := secret.Encrypt(token, pass)
	zero(token)
	zero(pass)
	if err != nil {
		return err
	}
	if err := secret.Save(config.TokenPath(), blob); err != nil {
		return err
	}
	fmt.Printf("Encrypted token written to %s\n", config.TokenPath())
	return nil
}
