package guid

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Base is an embeddable bun model that gives a table a Sonyflake primary key and
// created/updated timestamps. Embed it in a business model and BeforeAppendModel
// fills the id from the process-wide generator on insert and maintains the
// timestamps, so callers set neither by hand.
type Base struct {
	ID        ID        `bun:"id,pk" json:"id"`
	CreatedAt time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,notnull,default:current_timestamp" json:"updated_at"`
}

var _ bun.BeforeAppendModelHook = (*Base)(nil)

// BeforeAppendModel assigns the id on insert (when unset) and bumps updated_at on
// insert and update. A missing generator surfaces as a query error rather than a
// panic.
func (b *Base) BeforeAppendModel(_ context.Context, query bun.Query) error {
	now := time.Now()
	switch query.(type) {
	case *bun.InsertQuery:
		if b.ID.Zero() {
			id, err := Next()
			if err != nil {
				return err
			}
			b.ID = ID(id)
		}
		if b.CreatedAt.IsZero() {
			b.CreatedAt = now
		}
		b.UpdatedAt = now
	case *bun.UpdateQuery:
		b.UpdatedAt = now
	}
	return nil
}
