// murmelmoney — minimal self-hosted personal finance tracker.
package main

import (
	"embed"
	"io/fs"
	"log"
	"mime"
	"os"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/cmd"
	"github.com/pocketbase/pocketbase/core"

	"github.com/floholz/murmelmoney/migrations"
)

//go:embed all:ui/dist
var uiEmbed embed.FS

const defaultAddr = "127.0.0.1:8070"

// version is set at build time: -ldflags "-X main.version=v1.0.0"
var version = "dev"

func registrationOpen(app core.App) (bool, error) {
	switch strings.ToLower(os.Getenv("MURMEL_REGISTRATION")) {
	case "true", "1", "on", "yes":
		return true, nil
	case "false", "0", "off", "no":
		return false, nil
	}
	n, err := app.CountRecords("users")
	return n == 0, err
}

func main() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	app := pocketbase.New()

	// Registration policy (MURMEL_REGISTRATION):
	//   "true"  → always open
	//   "false" → always closed (superusers can still create users in /_/)
	//   unset   → open until the first user exists, then closed
	app.OnRecordCreateRequest("users").BindFunc(func(e *core.RecordRequestEvent) error {
		if e.HasSuperuserAuth() {
			return e.Next()
		}
		open, err := registrationOpen(e.App)
		if err != nil {
			return err
		}
		if !open {
			return e.ForbiddenError("Registration is closed on this instance.", nil)
		}
		return e.Next()
	})

	// Every new user starts with the default tax rule; the very first user also
	// adopts any records left over from a pre-multi-user database.
	app.OnRecordAfterCreateSuccess("users").BindFunc(func(e *core.RecordEvent) error {
		if n, err := e.App.CountRecords("users"); err == nil && n == 1 {
			if err := migrations.AdoptOrphans(e.App, e.Record.Id); err != nil {
				e.App.Logger().Error("could not adopt orphaned records", "err", err)
			}
		}
		if n, err := e.App.CountRecords("rules", dbx.HashExp{"user": e.Record.Id}); err == nil && n > 0 {
			return e.Next() // already has rules (adopted)
		}
		rules, err := e.App.FindCollectionByNameOrId("rules")
		if err != nil {
			return err
		}
		r := core.NewRecord(rules)
		r.Set("user", e.Record.Id)
		r.Set("name", "AT rough estimate (freelancer + rental)")
		r.Set("script", migrations.DefaultRule)
		r.Set("active", true)
		if err := e.App.Save(r); err != nil {
			e.App.Logger().Error("could not create default rule", "err", err)
		}
		return e.Next()
	})

	// Materialize due recurring transactions right after a template is created
	// or edited, so backfill is visible without waiting for the daily cron.
	app.OnRecordAfterCreateSuccess("recurring").BindFunc(func(e *core.RecordEvent) error {
		generateTemplate(e.App, e.Record)
		return e.Next()
	})
	app.OnRecordAfterUpdateSuccess("recurring").BindFunc(func(e *core.RecordEvent) error {
		generateTemplate(e.App, e.Record)
		return e.Next()
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if err := bootstrapSuperuser(app); err != nil {
			return err
		}
		// Daily rollover (00:10 UTC) + catch-up for downtime at boot.
		app.Cron().MustAdd("recurring", "10 0 * * *", func() { generateRecurring(app) })
		go generateRecurring(app)
		ui, err := fs.Sub(uiEmbed, "ui/dist")
		if err != nil {
			return err
		}
		// tiny public status endpoint so the login page knows whether to offer sign-up
		e.Router.GET("/api/murmel/status", func(re *core.RequestEvent) error {
			open, err := registrationOpen(re.App)
			if err != nil {
				return err
			}
			return re.JSON(200, map[string]any{"registration": open, "version": version})
		})
		e.Router.GET("/{path...}", apis.Static(ui, true))
		return e.Next()
	})

	// Same as app.Start(), but with our own default listen address.
	app.RootCmd.AddCommand(cmd.NewSuperuserCommand(app))
	serve := cmd.NewServeCommand(app, true)
	if f := serve.PersistentFlags().Lookup("http"); f != nil {
		_ = f.Value.Set(defaultAddr)
		f.DefValue = defaultAddr
		f.Usage = "TCP address to listen for the HTTP server"
	}
	app.RootCmd.AddCommand(serve)
	if err := app.Execute(); err != nil {
		log.Fatal(err)
	}
}

// bootstrapSuperuser creates the PocketBase admin (for /_/) from
// MURMEL_ADMIN_EMAIL / MURMEL_ADMIN_PASSWORD when no superuser exists yet.
func bootstrapSuperuser(app core.App) error {
	email, pass := os.Getenv("MURMEL_ADMIN_EMAIL"), os.Getenv("MURMEL_ADMIN_PASSWORD")
	if email == "" || pass == "" {
		return nil
	}
	n, err := app.CountRecords(core.CollectionNameSuperusers)
	if err != nil || n > 0 {
		return err
	}
	col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		return err
	}
	u := core.NewRecord(col)
	u.SetEmail(email)
	u.SetPassword(pass)
	if err := app.Save(u); err != nil {
		return err
	}
	app.Logger().Info("created superuser from env", "email", email)
	return nil
}
