// Command xauth runs the XAuth OpenID Provider.
//
//	xauth [serve]                       start the server (configured by XAUTH_* variables)
//	xauth create-admin -email a@b.c     create an admin, or promote an existing user
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"xauth/internal/config"
	"xauth/internal/oidcprovider"
	"xauth/internal/password"
	"xauth/internal/server"
	"xauth/internal/store"
	"xauth/internal/webui"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cmd, args := "serve", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(log)
	case "create-admin":
		err = createAdmin(args)
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "xauth:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage:
  xauth [serve]                          start the server
  xauth create-admin -email EMAIL [-name NAME]
                                         create an admin, or promote an existing user

Environment:
  XAUTH_ISSUER       public URL, e.g. https://auth.example.com (default http://localhost:8080)
  XAUTH_LISTEN       listen address (default 127.0.0.1:8080)
  XAUTH_DB           SQLite file (default data/xauth.db)
  XAUTH_DEV          allow http:// issuer and SPA redirect URIs (default false)
  XAUTH_SIGNUP       allow self-service sign-up (default true)
  XAUTH_TRUST_PROXY  use X-Real-IP / X-Forwarded-For for rate limits (default false)
  XMAIL_BASE_URL     xmail deployment, e.g. https://mail.example.com (optional)
  XMAIL_API_KEY      xmail API key (sk_live_...); required with XMAIL_BASE_URL
  XAUTH_ADMIN_PASSWORD  password for create-admin instead of a prompt
`)
}

func serve(log *slog.Logger) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	provider, err := oidcprovider.New(ctx, cfg, st)
	if err != nil {
		return err
	}
	ui := webui.FS()
	if ui == nil {
		log.Warn("web UI is not built; run `npm run build` in web/ and rebuild")
	}
	srv := server.New(cfg, st, provider, ui, log)

	if n, err := st.CountAdmins(ctx); err == nil && n == 0 {
		log.Warn("no admin account yet; create one with: xauth create-admin -email you@example.com")
	}
	go purgeLoop(ctx, st, log)

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("xauth listening", "addr", cfg.Listen, "issuer", cfg.Issuer, "dev", cfg.Dev, "xmail", cfg.XMailBaseURL)
		errc <- httpSrv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

func purgeLoop(ctx context.Context, st *store.Store, log *slog.Logger) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		if err := st.Purge(ctx); err != nil && ctx.Err() == nil {
			log.Error("purge expired rows", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func createAdmin(args []string) error {
	fs := flag.NewFlagSet("create-admin", flag.ContinueOnError)
	email := fs.String("email", "", "admin email address")
	name := fs.String("name", "", "display name")
	dbPath := fs.String("db", "", "SQLite file (default $XAUTH_DB or data/xauth.db)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	addr := store.NormalizeEmail(*email)
	if parsed, err := mail.ParseAddress(addr); err != nil || parsed.Address != addr {
		return errors.New("-email must be a valid email address")
	}
	path := *dbPath
	if path == "" {
		path = os.Getenv("XAUTH_DB")
	}
	if path == "" {
		path = config.Defaults().DBPath
	}
	ctx := context.Background()
	st, err := store.Open(ctx, path)
	if err != nil {
		return err
	}
	defer st.Close()

	u, err := st.UserByEmail(ctx, addr)
	switch {
	case err == nil:
		if u.IsAdmin && !u.Disabled {
			fmt.Printf("%s is already an admin.\n", addr)
			return nil
		}
		u.IsAdmin, u.Disabled = true, false
		if err := st.UpdateUser(ctx, u); err != nil {
			return err
		}
		fmt.Printf("%s is now an admin. Their password is unchanged.\n", addr)
		return nil
	case !errors.Is(err, store.ErrNotFound):
		return err
	}

	pw, err := readPassword()
	if err != nil {
		return err
	}
	if err := password.Validate(pw); err != nil {
		return fmt.Errorf("password must be %d characters to 72 bytes", password.MinLength)
	}
	hash, err := password.Hash(pw)
	if err != nil {
		return err
	}
	u = &store.User{Email: addr, Name: strings.TrimSpace(*name), PasswordHash: hash, IsAdmin: true, EmailVerified: true}
	if err := st.CreateUser(ctx, u); err != nil {
		return err
	}
	fmt.Printf("Created admin %s.\n", addr)
	return nil
}

// readPassword takes XAUTH_ADMIN_PASSWORD, prompts on a terminal, or reads
// one line from piped stdin (Git Bash's mintty is not a terminal to Go;
// use `winpty xauth ...` there to get a hidden prompt).
func readPassword() (string, error) {
	if pw := os.Getenv("XAUTH_ADMIN_PASSWORD"); pw != "" {
		return pw, nil
	}
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "Password: ")
		a, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		fmt.Fprint(os.Stderr, "Repeat password: ")
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if string(a) != string(b) {
			return "", errors.New("passwords do not match")
		}
		return string(a), nil
	}
	fmt.Fprint(os.Stderr, "Password (input is visible): ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no password given; set XAUTH_ADMIN_PASSWORD")
	}
	return strings.TrimRight(line, "\r\n"), nil
}
