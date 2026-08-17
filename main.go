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
	"github.com/pocketbase/pocketbase/core"

	"github.com/floholz/murmelmoney/migrations"
)

//go:embed all:ui/dist
var uiEmbed embed.FS

func main() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	app := pocketbase.New()

	// Optionally close registration: MURMEL_REGISTRATION=false
	if v := strings.ToLower(os.Getenv("MURMEL_REGISTRATION")); v == "false" || v == "0" || v == "off" {
		app.OnRecordCreateRequest("users").BindFunc(func(e *core.RecordRequestEvent) error {
			if !e.HasSuperuserAuth() {
				return e.ForbiddenError("Registration is disabled on this instance.", nil)
			}
			return e.Next()
		})
	}

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

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if err := bootstrapSuperuser(app); err != nil {
			return err
		}
		ui, err := fs.Sub(uiEmbed, "ui/dist")
		if err != nil {
			return err
		}
		e.Router.GET("/{path...}", apis.Static(ui, true))
		return e.Next()
	})

	if err := app.Start(); err != nil {
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
