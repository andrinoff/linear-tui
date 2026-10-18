package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"

	"github.com/andrinoff/linear-tui/internal/auth"
	"github.com/andrinoff/linear-tui/internal/config"
	"github.com/andrinoff/linear-tui/internal/linear"
	"github.com/andrinoff/linear-tui/internal/tui"
)

var version = "dev"

type env struct {
	cfg      config.Config
	store    *auth.Store
	oauth    *auth.OAuth
	endpoint string
}

func load(cfgPath string) (*env, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	store := auth.NewStore(dir)
	store.Warn = func(s string) { fmt.Fprintln(os.Stderr, "warning:", s) }
	return &env{
		cfg:      cfg,
		store:    store,
		oauth:    &auth.OAuth{ClientID: cfg.OAuthClientID(), Port: cfg.OAuth.Port},
		endpoint: os.Getenv("LINEAR_API_URL"),
	}, nil
}

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var cfgPath, issue string
	var noBranch bool
	root := &cobra.Command{
		Use:          "linear-tui",
		Short:        "A terminal UI for Linear",
		SilenceUsage: true,
		Version:      version,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := load(cfgPath)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				issue = args[0]
			}
			if noBranch {
				e.cfg.Git.OpenFromBranch = false
			}
			app := tui.New(tui.Options{Cfg: e.cfg, Store: e.store, Endpoint: e.endpoint, OAuth: e.oauth, OpenIssue: issue})
			p := tea.NewProgram(app)
			app.SetSender(p.Send)
			_, err = p.Run()
			if seq := app.Cleanup(); seq != "" {
				fmt.Print(seq)
			}
			if serr := app.Save(); serr != nil && err == nil {
				fmt.Fprintln(os.Stderr, "warning: could not save state:", serr)
			}
			return err
		},
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file (default is the user config dir)")
	root.Flags().StringVar(&issue, "issue", "", "open an issue (e.g. ENG-123) on start")
	root.Flags().BoolVar(&noBranch, "no-branch", false, "don't open the issue matching the current git branch")
	root.AddCommand(authCmd(&cfgPath), &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run:   func(*cobra.Command, []string) { fmt.Println("linear-tui", version) },
	})
	return root
}

func authCmd(cfgPath *string) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage Linear credentials"}
	var useOAuth bool
	login := &cobra.Command{
		Use:   "login",
		Short: "Log in with a personal API key (or --oauth)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := load(*cfgPath)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if useOAuth {
				tok, err := e.oauth.Login(ctx, func(u string) {
					fmt.Println("Opening your browser to authorize linear-tui. If it doesn't open, visit:")
					fmt.Println(u)
					_ = browser.OpenURL(u)
				})
				if err != nil {
					return err
				}
				return finishLogin(ctx, e, &auth.Credentials{Kind: auth.KindOAuth, Token: tok})
			}
			fmt.Print("Paste your Linear API key: ")
			key, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil && key == "" {
				return err
			}
			key = strings.TrimSpace(key)
			if key == "" {
				return errors.New("no API key given")
			}
			return finishLogin(ctx, e, &auth.Credentials{Kind: auth.KindAPIKey, APIKey: key})
		},
	}
	login.Flags().BoolVar(&useOAuth, "oauth", false, "log in through the browser with OAuth")

	logout := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(*cobra.Command, []string) error {
			e, err := load(*cfgPath)
			if err != nil {
				return err
			}
			if err := e.store.Delete(); err != nil {
				return err
			}
			fmt.Println("Logged out.")
			return nil
		},
	}

	status := &cobra.Command{
		Use:   "status",
		Short: "Show who you are logged in as",
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := load(*cfgPath)
			if err != nil {
				return err
			}
			creds, err := e.store.Load()
			if err != nil {
				return err
			}
			v, err := viewer(cmd.Context(), e, creds)
			if err != nil {
				return err
			}
			src := string(creds.Kind)
			if creds.FromEnv {
				src += " from $" + auth.EnvAPIKey
			}
			fmt.Printf("Logged in to %s as %s <%s> (%s)\n", v.Organization.Name, v.Viewer.Name, v.Viewer.Email, src)
			return nil
		},
	}
	cmd.AddCommand(login, logout, status)
	return cmd
}

func viewer(ctx context.Context, e *env, creds *auth.Credentials) (*linear.Viewer, error) {
	rt, err := e.store.Transport(ctx, creds, e.oauth)
	if err != nil {
		return nil, err
	}
	v, err := linear.NewService(linear.NewClient(e.endpoint, rt)).Viewer(ctx)
	if err != nil {
		return nil, errors.New(linear.Message(err))
	}
	return v, nil
}

func finishLogin(ctx context.Context, e *env, creds *auth.Credentials) error {
	v, err := viewer(ctx, e, creds)
	if err != nil {
		return err
	}
	if err := e.store.Save(creds); err != nil {
		return err
	}
	fmt.Printf("Logged in to %s as %s.\n", v.Organization.Name, v.Viewer.Name)
	return nil
}
