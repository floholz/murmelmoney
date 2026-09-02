package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// v6: recurring.interval becomes free text with a small syntax (presets such
// as "half-yearly" plus "<n> weeks|months|years"), validated by a record hook
// in the main package (see recurring.go). PocketBase refuses to change a
// field's type in place, so: add a text column, copy the values, drop the
// select, rename. The existing preset names stay valid, so no value changes.
func init() {
	m.Register(func(app core.App) error {
		rec, err := app.FindCollectionByNameOrId("recurring")
		if err != nil {
			return err
		}
		if _, isSelect := rec.Fields.GetByName("interval").(*core.SelectField); !isSelect {
			return nil // already migrated (or a fresh database)
		}
		rec.Fields.Add(&core.TextField{Name: "interval_text", Required: true, Max: 32})
		if err := app.Save(rec); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery("UPDATE {{recurring}} SET interval_text = interval").Execute(); err != nil {
			return err
		}
		rec.Fields.RemoveByName("interval")
		rec.Fields.GetByName("interval_text").SetName("interval")
		return app.Save(rec)
	}, func(app core.App) error {
		rec, err := app.FindCollectionByNameOrId("recurring")
		if err != nil {
			return err
		}
		if _, isText := rec.Fields.GetByName("interval").(*core.TextField); !isText {
			return nil
		}
		rec.Fields.Add(&core.SelectField{Name: "interval_select", Required: true, MaxSelect: 1,
			Values: []string{"weekly", "monthly", "quarterly", "yearly"}})
		if err := app.Save(rec); err != nil {
			return err
		}
		// values outside the old enum have no equivalent; fall back to monthly
		_, err = app.DB().NewQuery(`UPDATE {{recurring}} SET interval_select = CASE
			WHEN interval IN ('weekly', 'monthly', 'quarterly', 'yearly') THEN interval ELSE 'monthly' END`).Execute()
		if err != nil {
			return err
		}
		rec.Fields.RemoveByName("interval")
		rec.Fields.GetByName("interval_select").SetName("interval")
		return app.Save(rec)
	})
}
