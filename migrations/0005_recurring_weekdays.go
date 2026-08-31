package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// v5: recurring templates can shift weekend occurrences to the following
// Monday ("first weekday of the month" rent-style payments: start on the 1st
// and enable this).
func init() {
	m.Register(func(app core.App) error {
		rec, err := app.FindCollectionByNameOrId("recurring")
		if err != nil {
			return err
		}
		if rec.Fields.GetByName("weekdays_only") == nil {
			rec.Fields.Add(&core.BoolField{Name: "weekdays_only"})
			return app.Save(rec)
		}
		return nil
	}, func(app core.App) error {
		rec, err := app.FindCollectionByNameOrId("recurring")
		if err != nil {
			return err
		}
		if rec.Fields.GetByName("weekdays_only") != nil {
			rec.Fields.RemoveByName("weekdays_only")
			return app.Save(rec)
		}
		return nil
	})
}
