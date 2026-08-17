package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// v1 schema (single user). Never edit an applied migration — later changes
// live in their own numbered files (see 0002_multiuser.go).
func init() {
	m.Register(func(app core.App) error {
		tx := core.NewBaseCollection("transactions")
		tx.Fields.Add(
			&core.SelectField{Name: "type", Required: true, MaxSelect: 1, Values: []string{"income", "expense"}},
			&core.DateField{Name: "date", Required: true},
			&core.NumberField{Name: "amount", Required: true, Min: ptr(0.0)},
			&core.SelectField{Name: "area", Required: true, MaxSelect: 1, Values: []string{"business", "rental", "private"}},
			&core.TextField{Name: "category", Max: 100},
			&core.TextField{Name: "note", Max: 10000},
			&core.FileField{Name: "attachments", MaxSelect: 10, MaxSize: 25 * 1024 * 1024},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		tx.AddIndex("idx_transactions_date", false, "date", "")
		tx.AddIndex("idx_transactions_type_area", false, "type, area", "")
		if err := app.Save(tx); err != nil {
			return err
		}

		rules := core.NewBaseCollection("rules")
		rules.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 100},
			&core.TextField{Name: "script", Max: 100000},
			&core.BoolField{Name: "active"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(rules); err != nil {
			return err
		}

		r := core.NewRecord(rules)
		r.Set("name", "AT rough estimate (freelancer + rental)")
		r.Set("script", DefaultRule)
		r.Set("active", true)
		return app.Save(r)
	}, func(app core.App) error {
		for _, name := range []string{"rules", "transactions"} {
			if c, err := app.FindCollectionByNameOrId(name); err == nil {
				if err := app.Delete(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func ptr[T any](v T) *T { return &v }
