package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// v4: recurring transaction templates + loans.
//
// Templates materialize real transactions server-side (see recurring.go in the
// main package); `last_generated` is the generator's watermark. Loan payments
// are ordinary expense transactions linked via the new `loan` relation, with
// `loan_interest` holding the interest portion (which doesn't reduce principal).
func init() {
	m.Register(func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		cats, err := app.FindCollectionByNameOrId("categories")
		if err != nil {
			return err
		}
		tags, err := app.FindCollectionByNameOrId("tags")
		if err != nil {
			return err
		}

		// loans -------------------------------------------------------------------
		if _, err := app.FindCollectionByNameOrId("loans"); err != nil {
			c := core.NewBaseCollection("loans")
			c.Fields.Add(
				userField(users.Id),
				&core.TextField{Name: "name", Required: true, Max: 100},
				&core.NumberField{Name: "principal", Required: true, Min: ptr(0.0)},
				&core.NumberField{Name: "interest_rate", Min: ptr(0.0)}, // annual %, informational
				&core.DateField{Name: "start"},
				&core.TextField{Name: "note", Max: 10000},
				&core.FileField{Name: "attachments", MaxSelect: 10, MaxSize: 25 * 1024 * 1024, Protected: true},
				&core.BoolField{Name: "closed"},
				&core.AutodateField{Name: "created", OnCreate: true},
				&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
			)
			c.AddIndex("idx_loans_user", false, "user", "")
			ownerRules(c)
			if err := app.Save(c); err != nil {
				return err
			}
		}
		loans, err := app.FindCollectionByNameOrId("loans")
		if err != nil {
			return err
		}

		// recurring ---------------------------------------------------------------
		if _, err := app.FindCollectionByNameOrId("recurring"); err != nil {
			c := core.NewBaseCollection("recurring")
			c.Fields.Add(
				userField(users.Id),
				&core.SelectField{Name: "type", Required: true, MaxSelect: 1, Values: []string{"income", "expense"}},
				&core.NumberField{Name: "amount", Required: true, Min: ptr(0.0)},
				&core.SelectField{Name: "area", Required: true, MaxSelect: 1, Values: []string{"business", "rental", "private"}},
				&core.RelationField{Name: "category", MaxSelect: 1, CollectionId: cats.Id},
				&core.RelationField{Name: "tags", MaxSelect: 50, CollectionId: tags.Id},
				&core.TextField{Name: "note", Max: 10000},
				&core.SelectField{Name: "interval", Required: true, MaxSelect: 1, Values: []string{"weekly", "monthly", "quarterly", "yearly"}},
				&core.DateField{Name: "start", Required: true},
				&core.DateField{Name: "end"},
				&core.BoolField{Name: "active"},
				// Watermark: date of the newest generated occurrence. Owners can
				// technically write it via the API; that only affects their own
				// generation, so it isn't worth a separate rule.
				&core.DateField{Name: "last_generated"},
				&core.AutodateField{Name: "created", OnCreate: true},
				&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
			)
			c.AddIndex("idx_recurring_user_active", false, "user, active", "")
			ownerRules(c)
			if err := app.Save(c); err != nil {
				return err
			}
		}
		rec, err := app.FindCollectionByNameOrId("recurring")
		if err != nil {
			return err
		}

		// transactions: provenance + loan link ------------------------------------
		tx, err := app.FindCollectionByNameOrId("transactions")
		if err != nil {
			return err
		}
		// CascadeDelete stays false: deleting a template or loan keeps the
		// transactions (PocketBase clears the dangling relation ids).
		if tx.Fields.GetByName("recurring") == nil {
			tx.Fields.Add(&core.RelationField{Name: "recurring", MaxSelect: 1, CollectionId: rec.Id})
		}
		if tx.Fields.GetByName("loan") == nil {
			tx.Fields.Add(&core.RelationField{Name: "loan", MaxSelect: 1, CollectionId: loans.Id})
		}
		if tx.Fields.GetByName("loan_interest") == nil {
			tx.Fields.Add(&core.NumberField{Name: "loan_interest", Min: ptr(0.0)})
		}
		tx.AddIndex("idx_transactions_loan", false, "loan", "loan != ''")
		return app.Save(tx)
	}, func(app core.App) error {
		tx, err := app.FindCollectionByNameOrId("transactions")
		if err == nil {
			tx.RemoveIndex("idx_transactions_loan")
			for _, name := range []string{"recurring", "loan", "loan_interest"} {
				if f := tx.Fields.GetByName(name); f != nil {
					tx.Fields.RemoveByName(name)
				}
			}
			if err := app.Save(tx); err != nil {
				return err
			}
		}
		for _, name := range []string{"recurring", "loans"} {
			if c, err := app.FindCollectionByNameOrId(name); err == nil {
				if err := app.Delete(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
