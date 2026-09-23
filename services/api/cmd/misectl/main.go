// Command misectl is the operator tool: database migrations and admin users.
//
//	misectl migrate up
//	misectl migrate status
//	misectl migrate down            (local only)
//	misectl admin create -email a@b.c -name "Ada" -role admin
//
// The admin password is read from the MISE_ADMIN_PASSWORD environment variable
// so it never appears in shell history or process listings.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"mise.tt/api/internal/admin"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/db"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "misectl:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: misectl migrate up|status|down | misectl admin create -email … -name … -role …")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch args[0] + " " + args[1] {
	case "migrate up":
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
		version, err := db.MigrationVersion(ctx, pool)
		if err != nil {
			return err
		}
		fmt.Println("schema is at version", version)
		return nil

	case "migrate status":
		version, err := db.MigrationVersion(ctx, pool)
		if err != nil {
			return err
		}
		fmt.Println("schema is at version", version)
		return nil

	case "migrate down":
		if cfg.Env != "local" {
			return errors.New("migrate down is only allowed when MISE_ENV=local")
		}
		return db.MigrateDown(ctx, pool)

	case "admin create":
		fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
		email := fs.String("email", "", "admin email address")
		name := fs.String("name", "", "display name")
		role := fs.String("role", "admin", "super_admin, admin, marketing, operations or support")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		password := os.Getenv("MISE_ADMIN_PASSWORD")
		if password == "" {
			return errors.New("set MISE_ADMIN_PASSWORD to the new admin's password")
		}
		if err := admin.NewAuth(pool, cfg.AdminSessionTTL).CreateUser(ctx, *email, *name, *role, password); err != nil {
			return err
		}
		fmt.Printf("admin %s is ready with role %s\n", *email, *role)
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0]+" "+args[1])
}
