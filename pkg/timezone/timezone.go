package timezone

import (
	"os"
	"time"
)

// SetTimezone sets the process-wide timezone and TZ environment variable.
// An empty value defaults to UTC.
func SetTimezone(timezone string) error {
	if timezone == "" {
		timezone = "UTC"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return err
	}
	time.Local = loc
	return os.Setenv("TZ", loc.String())
}

// Now returns the current time in UTC.
func Now() time.Time {
	return time.Now().UTC()
}

// ToUTC ensures a time value is in UTC.
func ToUTC(t time.Time) time.Time {
	return t.UTC()
}

// FormatUTC formats a time in UTC using RFC3339.
func FormatUTC(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// ParseUTC parses an RFC3339 string and returns UTC time.
func ParseUTC(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
