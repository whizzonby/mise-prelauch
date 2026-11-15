// Command misectl is the operator tool: database migrations and staff accounts.
//
//	misectl migrate up
//	misectl migrate status
//	misectl migrate down                       (local only)
//	misectl catalog seed-demo                  (needs CATALOG_DEMO_ENABLED)
//	misectl commerce seed-demo                 (demo plans and menus; needs CATALOG_DEMO_ENABLED)
//	misectl commerce tick [-at 2026-11-20T04:00:00Z]   (one scheduler run; -at only when local)
//	misectl production seed-demo               (a released demo production plan; local only, needs the worker)
//	misectl analytics catalog                  (prints the metric catalogue)
//	misectl analytics backfill -from 2026-11-01 -to 2026-11-30   (recompute daily snapshots; idempotent)
//	misectl delivery seed-demo [-ready]        (demo zone, days, partner and courier; -ready also completes the newest demo plan; local only)
//	misectl events replay -consumer procurement|fulfillment -from 0   (re-deliver domain events after an id; consumers are idempotent)
//	misectl admin create -email a@b.c -name "Ada" -role admin
//	misectl admin grant  -email a@b.c -role marketing
//	misectl admin revoke -email a@b.c -role marketing
//	misectl admin reset-mfa -email a@b.c       (lost authenticator)
//	misectl admin disable -email a@b.c         (also signs out everywhere)
//	misectl admin enable  -email a@b.c
//	misectl admin signout -email a@b.c         (ends every session)
//	misectl doctor                             (checks permissions in code exist in the database)
//
// "admin create" reads the password from MISE_ADMIN_PASSWORD so it never
// appears in shell history or process listings. Run against an existing
// account, it sets a new password and signs that person out everywhere; this
// is also how a staff password is reset.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"mise.tt/api/internal/analytics"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/consumer"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/server"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "misectl:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "doctor" {
		args = append(args, "")
	}
	if len(args) < 2 {
		return errors.New("usage: misectl migrate up|status|down | misectl catalog seed-demo | misectl commerce seed-demo|tick | misectl production seed-demo | misectl delivery seed-demo [-ready] | misectl analytics catalog|backfill | misectl events replay | misectl admin create|grant|revoke|reset-mfa|disable|enable|signout -email … | misectl doctor")
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

	case "catalog seed-demo":
		// Loads clearly marked demo meals, categories and a demo kitchen
		// profile, and publishes them. Refused unless CATALOG_DEMO_ENABLED is
		// on, so it cannot put placeholder food in front of real customers.
		n, err := server.NewCatalogService(cfg, pool).SeedDemo(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("demo catalogue ready (%d new items)\n", n)
		return nil

	case "commerce seed-demo":
		// The business-plan draft plans, as demo content, and published demo
		// menus for the next two open weeks from the demo catalogue.
		if !cfg.CatalogDemoEnabled {
			return errors.New("demo content is off (CATALOG_DEMO_ENABLED=false)")
		}
		ids, err := server.NewIdentityService(cfg, pool)
		if err != nil {
			return err
		}
		com, err := server.NewCommerce(cfg, pool, ids)
		if err != nil {
			return err
		}
		n, err := com.Plans.SeedDemo(ctx)
		if err != nil {
			return err
		}
		weeks, err := server.SeedDemoMenus(ctx, cfg, pool, com)
		if err != nil {
			return err
		}
		fmt.Printf("demo plans ready (%d new); demo menus published for %v\n", n, weeks)
		return nil

	case "commerce tick":
		ids, err := server.NewIdentityService(cfg, pool)
		if err != nil {
			return err
		}
		com, err := server.NewCommerce(cfg, pool, ids)
		if err != nil {
			return err
		}
		fs := flag.NewFlagSet("tick", flag.ContinueOnError)
		at := fs.String("at", "", "run as if it were this time (RFC 3339; local development only)")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		now := time.Now()
		if *at != "" {
			if cfg.Env != "local" {
				return errors.New("-at is only allowed when MISE_ENV=local")
			}
			if now, err = time.Parse(time.RFC3339, *at); err != nil {
				return fmt.Errorf("-at: %w", err)
			}
			com.SetClock(func() time.Time { return now })
		}
		rep, err := com.Service.Tick(ctx, now)
		if err != nil {
			return err
		}
		fmt.Printf("%+v\n", *rep)
		return nil

	case "production seed-demo":
		// Demo customers, recipes and a released plan for the next week
		// without one, made through the real API with a moved clock. See
		// server.SeedDemoProduction for why the clock moves.
		out, err := server.SeedDemoProduction(ctx, cfg, pool)
		if err != nil {
			return err
		}
		fmt.Printf("demo production ready: plan %s for the week of %s, %d orders, meals %v, batches %v\n", out.Plan, out.Week, out.Orders, out.Meals, out.Batches)
		return nil

	case "delivery seed-demo":
		fs := flag.NewFlagSet("delivery seed-demo", flag.ContinueOnError)
		ready := fs.Bool("ready", false, "also complete the newest released demo production plan, so fulfillments exist")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		out, err := server.SeedDemoDelivery(ctx, cfg, pool, *ready)
		if err != nil {
			return err
		}
		fmt.Printf("demo delivery ready: zone %s, %d service days, partner %s, courier %s, batches completed %v, fulfillments created %d\n",
			out.Zone, len(out.ServiceDays), out.Partner, out.Courier, out.Completed, out.Obligations)
		return nil

	case "analytics catalog":
		// The metric catalogue (docs/analytics/METRIC_CATALOG.md) from the registry.
		fmt.Print(analytics.Catalog())
		return nil

	case "analytics backfill":
		fs := flag.NewFlagSet("analytics backfill", flag.ContinueOnError)
		from := fs.String("from", "", "first local day (YYYY-MM-DD)")
		to := fs.String("to", "", "last local day (YYYY-MM-DD)")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		com, err := server.NewCommerce(cfg, pool, nil)
		if err != nil {
			return err
		}
		an := server.NewAnalytics(pool, com)
		r, err := an.ParseRange(*from, *to)
		if err != nil {
			return err
		}
		run, err := an.Refresh(ctx, "backfill", "", r)
		if err != nil {
			return err
		}
		fmt.Printf("analytics backfill %s to %s: %s, %d values written\n", run.From, run.To, run.Status, run.ValuesWritten)
		return nil

	case "events replay":
		fs := flag.NewFlagSet("events replay", flag.ContinueOnError)
		consumerName := fs.String("consumer", "", "the consumer to move back (procurement or fulfillment)")
		from := fs.Int64("from", -1, "deliver again every event after this id")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if *consumerName != "procurement" && *consumerName != "fulfillment" {
			return errors.New("-consumer must be procurement or fulfillment")
		}
		if err := consumer.Replay(ctx, pool, *consumerName, *from); err != nil {
			return err
		}
		fmt.Printf("%s will receive events after %d again on the worker's next dispatch\n", *consumerName, *from)
		return nil

	case "doctor ":
		ids, err := server.NewIdentityService(cfg, pool)
		if err != nil {
			return err
		}
		missing, err := ids.MissingPermissions(ctx)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return fmt.Errorf("permissions checked in code but missing from the database: %v (run misectl migrate up)", missing)
		}
		fmt.Println("ok: every permission the code checks exists in the database")
		return nil
	}

	if args[0] != "admin" {
		return fmt.Errorf("unknown command %q", args[0]+" "+args[1])
	}
	ids, err := server.NewIdentityService(cfg, pool)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("admin "+args[1], flag.ContinueOnError)
	email := fs.String("email", "", "account email address")
	name := fs.String("name", "", "display name (create)")
	role := fs.String("role", "", "role key: super_admin, admin, operations, finance, marketing, support, nutritionist, chef, kitchen_staff, farmer")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("-email is required")
	}

	switch args[1] {
	case "create":
		password := os.Getenv("MISE_ADMIN_PASSWORD")
		if password == "" {
			return errors.New("set MISE_ADMIN_PASSWORD to the new admin's password")
		}
		if *role == "" {
			*role = "admin"
		}
		if err := ids.UpsertStaff(ctx, *email, *name, *role, password); err != nil {
			return err
		}
		fmt.Printf("%s is ready with role %s\n", *email, *role)
		if cfg.AdminMFARequired {
			fmt.Println("They will set up an authenticator app the first time they sign in.")
		}
	case "grant":
		if err := ids.GrantRole(ctx, *email, *role); err != nil {
			return err
		}
		fmt.Printf("%s now holds %s\n", *email, *role)
	case "revoke":
		if err := ids.RevokeRole(ctx, *email, *role); err != nil {
			return err
		}
		fmt.Printf("%s no longer holds %s\n", *email, *role)
	case "reset-mfa":
		if err := ids.ResetMFA(ctx, *email); err != nil {
			return err
		}
		fmt.Printf("MFA removed for %s; they will set it up again at their next sign-in\n", *email)
	case "disable", "enable":
		if err := ids.SetDisabled(ctx, *email, args[1] == "disable"); err != nil {
			return err
		}
		fmt.Printf("%s is %sd\n", *email, args[1])
	case "signout":
		n, err := ids.RevokeAllSessions(ctx, *email)
		if err != nil {
			return err
		}
		fmt.Printf("ended %d session(s) for %s\n", n, *email)
	default:
		return fmt.Errorf("unknown command %q", "admin "+args[1])
	}
	return nil
}
