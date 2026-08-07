package guid

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// ID is a snowflake int64 primary/foreign key that crosses the JSON boundary as
// a STRING. 19-digit int64 snowflakes exceed JavaScript's 2^53 safe range, so a
// JSON number would silently lose precision in the browser; marshaling as a
// string keeps it exact. UnmarshalJSON still accepts a bare number for
// non-browser callers. The Schema hook makes Huma document/validate it as a
// numeric string (Huma reflection would otherwise emit `integer`).
type ID int64

// MarshalJSON emits the id as a quoted decimal string.
func (id ID) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatInt(int64(id), 10) + `"`), nil
}

// UnmarshalJSON accepts a JSON string ("123"), a bare number (123), or
// null/empty (→ 0).
func (id *ID) UnmarshalJSON(b []byte) error {
	v, err := Parse(string(b))
	*id = v
	return err
}

// MarshalText lets ID render in path/query params and other text contexts.
func (id ID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

// UnmarshalText parses ID from path/query params.
func (id *ID) UnmarshalText(b []byte) error {
	v, err := Parse(string(b))
	*id = v
	return err
}

// Schema reports the OpenAPI schema as a numeric string.
func (ID) Schema(_ huma.Registry) *huma.Schema {
	return &huma.Schema{Type: huma.TypeString, Pattern: `^-?[0-9]+$`}
}

// Value stores the id as a BIGINT.
func (id ID) Value() (driver.Value, error) { return int64(id), nil }

// Scan reads the id from a BIGINT (or its text form).
func (id *ID) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*id = 0
	case int64:
		*id = ID(v)
	case []byte:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			return err
		}
		*id = ID(n)
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return err
		}
		*id = ID(n)
	default:
		return fmt.Errorf("guid.ID: cannot scan %T", src)
	}
	return nil
}

// Int64 returns the underlying value, for passing into services that take int64.
func (id ID) Int64() int64 { return int64(id) }

// String returns the decimal representation.
func (id ID) String() string { return strconv.FormatInt(int64(id), 10) }

// Zero reports whether the id is unset (0).
func (id ID) Zero() bool { return id == 0 }

// Parse parses an ID from its decimal string form (path/query params). Empty,
// null, or `""` yield the zero id.
func Parse(s string) (ID, error) {
	s = strings.TrimSpace(s)
	if s == "null" || s == "" || s == `""` {
		return 0, nil
	}
	s = strings.TrimSpace(strings.Trim(s, `"`))
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("guid.ID: invalid id %q: %w", s, err)
	}
	return ID(n), nil
}
