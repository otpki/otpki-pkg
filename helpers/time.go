package helpers

import (
	"fmt"
	"strings"
	"time"
)

var (
	Tn         = time.Now
	MockOffset = time.Duration(0)
)

func Now() time.Time {
	return Tn().UTC().Add(MockOffset)
}

func UnixTime() int64 {
	return Now().Unix()
}

func UnixMilli() int64 {
	return Now().UnixMilli()
}

// PrettyPrintDuration converts a time.Duration into a human-readable string,
// showing only the most significant units.
// For example:
//   - 1h30m5s -> "1 hour 30 minutes 5 seconds"
//   - 2h -> "2 hours"
//   - 3d -> "3 days"
//   - 7w -> "7 weeks"
//   - 1y -> "1 year"
//
// It handles negative durations by making them positive for display and prepending "Negative ".
func PrettyPrintDuration(d time.Duration) string {
	if d == 0 {
		return "0 seconds"
	}

	isNegative := false
	if d < 0 {
		isNegative = true
		d = -d // Work with positive duration for calculation
	}

	// Define common duration units in order of decreasing size
	// Note: Go's time.Duration does not have direct "year" or "week" units.
	// We'll approximate them for pretty printing.
	const (
		year        = 365 * 24 * time.Hour
		week        = 7 * 24 * time.Hour
		day         = 24 * time.Hour
		hour        = time.Hour
		minute      = time.Minute
		second      = time.Second
		millisecond = time.Millisecond
		microsecond = time.Microsecond
		nanosecond  = time.Nanosecond
	)

	parts := []string{}

	// Years
	if d >= year {
		years := d / year
		d %= year
		parts = append(parts, fmt.Sprintf("%d year%s", years, plural(int64(years))))
	}
	// Weeks
	if d >= week {
		weeks := d / week
		d %= week
		parts = append(parts, fmt.Sprintf("%d week%s", weeks, plural(int64(weeks))))
	}
	// Days
	if d >= day {
		days := d / day
		d %= day
		parts = append(parts, fmt.Sprintf("%d day%s", days, plural(int64(days))))
	}
	// Hours
	if d >= hour {
		hours := d / hour
		d %= hour
		parts = append(parts, fmt.Sprintf("%d hour%s", hours, plural(int64(hours))))
	}
	// Minutes
	if d >= minute {
		minutes := d / minute
		d %= minute
		parts = append(parts, fmt.Sprintf("%d minute%s", minutes, plural(int64(minutes))))
	}
	// Seconds
	if d >= second {
		seconds := d / second
		d %= second
		parts = append(parts, fmt.Sprintf("%d second%s", seconds, plural(int64(seconds))))
	}
	// Milliseconds
	if d >= millisecond {
		milliseconds := d / millisecond
		d %= millisecond
		parts = append(parts, fmt.Sprintf("%d millisecond%s", milliseconds, plural(int64(milliseconds))))
	}
	// Microseconds
	if d >= microsecond {
		microseconds := d / microsecond
		d %= microsecond
		parts = append(parts, fmt.Sprintf("%d microsecond%s", microseconds, plural(int64(microseconds))))
	}
	// Nanoseconds (if anything left)
	if d > 0 {
		nanos := d / nanosecond
		parts = append(parts, fmt.Sprintf("%d nanosecond%s", nanos, plural(int64(nanos))))
	}

	if len(parts) == 0 {
		return "0 seconds" // Fallback for very small non-zero durations if no larger units are captured
	}

	var result strings.Builder
	for i, part := range parts {
		result.WriteString(part)
		if i < len(parts)-2 {
			result.WriteString(", ")
		} else if i == len(parts)-2 {
			result.WriteString(" and ")
		}
	}

	if isNegative {
		return "Negative " + result.String()
	}
	return result.String()
}

// plural is a helper function to add 's' for pluralization.
func plural(count int64) string {
	if count != 1 {
		return "s"
	}
	return ""
}
