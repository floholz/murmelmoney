package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// v3: receipts/invoices must not be fetchable by URL alone — mark the
// attachments field as protected so a short-lived file token (issued to the
// owning user) is required.
func init() {
	m.Register(func(app core.App) error {
		tx, err := app.FindCollectionByNameOrId("transactions")
		if err != nil {
			return err
		}
		if f, ok := tx.Fields.GetByName("attachments").(*core.FileField); ok && !f.Protected {
			f.Protected = true
			return app.Save(tx)
		}
		return nil
	}, func(app core.App) error {
		tx, err := app.FindCollectionByNameOrId("transactions")
		if err != nil {
			return err
		}
		if f, ok := tx.Fields.GetByName("attachments").(*core.FileField); ok {
			f.Protected = false
			return app.Save(tx)
		}
		return nil
	})
}
