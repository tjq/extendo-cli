package tui

import (
	"fmt"
	"time"
)

const (
	day  = 24 * time.Hour
	week = 7 * day
)

// Rel renders how long before now t was, in the coarsest unit that still says
// something: "now", "45s", "2m", "1h", "2d", "3w". Units are floored, and the
// result carries no "ago" — the column it sits in is the context.
//
// Weeks are the coarsest unit on purpose. A picker only ever shows a few
// hundred recent items, so "57w" is a signal that something is unusually old
// rather than a number anyone reads precisely.
//
// Clock skew between the macOS app and this process can date an item slightly
// in the future, which reads as "now" rather than as a negative age.
func Rel(t, now time.Time) string {
	elapsed := now.Sub(t)

	switch {
	case elapsed < time.Second:
		return "now"
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds", int(elapsed/time.Second))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed/time.Minute))
	case elapsed < day:
		return fmt.Sprintf("%dh", int(elapsed/time.Hour))
	case elapsed < week:
		return fmt.Sprintf("%dd", int(elapsed/day))
	default:
		return fmt.Sprintf("%dw", int(elapsed/week))
	}
}
