package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// v2: multi-user, creatable categories, tags.
//
// Upgrades a v1 database in place. Rows that existed before (they have no
// owner) are adopted by the first user who registers — see AdoptOrphans.

// ownerRules locks a collection to the authenticated owner (`user` relation).
func ownerRules(c *core.Collection) {
	owned := "@request.auth.id != '' && user = @request.auth.id"
	create := "@request.auth.id != '' && @request.body.user = @request.auth.id"
	c.ListRule, c.ViewRule, c.UpdateRule, c.DeleteRule = &owned, &owned, &owned, &owned
	c.CreateRule = &create
}

func userField(usersId string) *core.RelationField {
	// Not marked Required so pre-v2 rows stay valid until adopted; ownership is
	// enforced by the API rules instead.
	return &core.RelationField{Name: "user", MaxSelect: 1, CollectionId: usersId, CascadeDelete: true}
}

func labelCollection(name, usersId string) *core.Collection {
	c := core.NewBaseCollection(name)
	c.Fields.Add(
		userField(usersId),
		&core.TextField{Name: "name", Required: true, Max: 100},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	c.AddIndex("idx_"+name+"_user_name", true, "user, name", "")
	ownerRules(c)
	return c
}

func init() {
	m.Register(func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}

		// categories & tags ------------------------------------------------------
		for _, name := range []string{"categories", "tags"} {
			if _, err := app.FindCollectionByNameOrId(name); err == nil {
				continue // already there
			}
			if err := app.Save(labelCollection(name, users.Id)); err != nil {
				return err
			}
		}
		cats, _ := app.FindCollectionByNameOrId("categories")
		tags, _ := app.FindCollectionByNameOrId("tags")

		// transactions -----------------------------------------------------------
		tx, err := app.FindCollectionByNameOrId("transactions")
		if err != nil {
			return err
		}
		if tx.Fields.GetByName("user") == nil {
			tx.Fields.AddAt(1, userField(users.Id))
		}
		// v1 had a free-text `category`; keep its content as `category_text`
		// (used by AdoptOrphans) and add the relation under the old name.
		if f, ok := tx.Fields.GetByName("category").(*core.TextField); ok {
			f.Name = "category_text"
		}
		if tx.Fields.GetByName("category") == nil {
			tx.Fields.Add(&core.RelationField{Name: "category", MaxSelect: 1, CollectionId: cats.Id})
		}
		if tx.Fields.GetByName("tags") == nil {
			tx.Fields.Add(&core.RelationField{Name: "tags", MaxSelect: 50, CollectionId: tags.Id})
		}
		tx.RemoveIndex("idx_transactions_date")
		tx.RemoveIndex("idx_transactions_type_area")
		tx.AddIndex("idx_transactions_user_date", false, "user, date", "")
		ownerRules(tx)
		if err := app.Save(tx); err != nil {
			return err
		}

		// rules ------------------------------------------------------------------
		rules, err := app.FindCollectionByNameOrId("rules")
		if err != nil {
			return err
		}
		if rules.Fields.GetByName("user") == nil {
			rules.Fields.AddAt(1, userField(users.Id))
		}
		ownerRules(rules)
		return app.Save(rules)
	}, func(app core.App) error {
		return nil // one-way; a v1 rollback would lose ownership data anyway
	})
}

// AdoptOrphans hands every ownerless (pre-v2) transaction and rule to the given
// user, converting the old free-text categories into category records. Called
// from the users create hook for the first registered user.
func AdoptOrphans(app core.App, userId string) error {
	txs, err := app.FindRecordsByFilter("transactions", "user = ''", "", 0, 0)
	if err != nil {
		return err
	}
	if len(txs) > 0 {
		cats, err := app.FindCollectionByNameOrId("categories")
		if err != nil {
			return err
		}
		catIds := map[string]string{}
		for _, t := range txs {
			t.Set("user", userId)
			if name := t.GetString("category_text"); name != "" {
				id, ok := catIds[name]
				if !ok {
					c := core.NewRecord(cats)
					c.Set("user", userId)
					c.Set("name", name)
					if err := app.Save(c); err != nil {
						return err
					}
					id, catIds[name] = c.Id, c.Id
				}
				t.Set("category", id)
				t.Set("category_text", "")
			}
			if err := app.Save(t); err != nil {
				return err
			}
		}
	}
	rules, err := app.FindRecordsByFilter("rules", "user = ''", "", 0, 0)
	if err != nil {
		return err
	}
	for _, r := range rules {
		r.Set("user", userId)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	if n := len(txs) + len(rules); n > 0 {
		app.Logger().Info("adopted pre-v2 records", "user", userId, "records", n)
	}
	return nil
}
