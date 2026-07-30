package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule computes the next activation time after a given moment. The zero
// return value means "never again" (no future match within the search bound).
type Schedule interface {
	Next(after time.Time) time.Time
	String() string
}

// ParseSchedule parses the schedule expression formats defined in AI.md PART 18
// "Schedule Format": standard 5-field cron, the @hourly/@daily/@weekly/@monthly/
// @yearly macros, and @every <duration> intervals.
func ParseSchedule(expr string) (Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty schedule expression")
	}

	if strings.HasPrefix(expr, "@every ") {
		d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(expr, "@every ")))
		if err != nil {
			return nil, fmt.Errorf("invalid @every duration %q: %w", expr, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("@every duration must be positive: %q", expr)
		}
		return everySchedule{interval: d, raw: expr}, nil
	}

	if macro, ok := cronMacros[expr]; ok {
		expr = macro
	}

	return parseCron(expr)
}

var cronMacros = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

// everySchedule fires at a fixed interval relative to the previous fire time.
type everySchedule struct {
	interval time.Duration
	raw      string
}

func (e everySchedule) Next(after time.Time) time.Time { return after.Add(e.interval) }
func (e everySchedule) String() string                 { return e.raw }

// Interval exposes the underlying duration for @every schedules.
func (e everySchedule) Interval() time.Duration { return e.interval }

// cronSchedule matches standard 5-field cron expressions
// (minute hour day-of-month month day-of-week).
type cronSchedule struct {
	minute  uint64
	hour    uint64
	dom     uint64
	month   uint64
	dow     uint64
	domStar bool
	dowStar bool
	raw     string
}

func (c cronSchedule) String() string { return c.raw }

const cronSearchYears = 5

// Next returns the first minute strictly after `after` that matches all fields.
// Day-of-month and day-of-week combine with OR when both are restricted, per
// standard cron semantics.
func (c cronSchedule) Next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(cronSearchYears, 0, 0)
	for t.Before(limit) {
		if c.month&(1<<uint(t.Month())) == 0 {
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, 0)
			continue
		}
		if !c.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, 1)
			continue
		}
		if c.hour&(1<<uint(t.Hour())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location()).Add(time.Hour)
			continue
		}
		if c.minute&(1<<uint(t.Minute())) == 0 {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}

func (c cronSchedule) dayMatches(t time.Time) bool {
	domMatch := c.dom&(1<<uint(t.Day())) != 0
	dowMatch := c.dow&(1<<uint(t.Weekday())) != 0
	switch {
	case c.domStar && c.dowStar:
		return true
	case c.domStar:
		return dowMatch
	case c.dowStar:
		return domMatch
	default:
		return domMatch || dowMatch
	}
}

func parseCron(expr string) (cronSchedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return cronSchedule{}, fmt.Errorf("cron expression must have 5 fields, got %d: %q", len(fields), expr)
	}

	minute, _, err := parseField(fields[0], 0, 59)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("minute: %w", err)
	}
	hour, _, err := parseField(fields[1], 0, 23)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("hour: %w", err)
	}
	dom, domStar, err := parseField(fields[2], 1, 31)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("day-of-month: %w", err)
	}
	month, _, err := parseField(fields[3], 1, 12)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("month: %w", err)
	}
	// Cron accepts 0-7 for day-of-week, where both 0 and 7 mean Sunday.
	dow, dowStar, err := parseField(fields[4], 0, 7)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("day-of-week: %w", err)
	}
	// Fold 7 onto bit 0 (Sunday) so it matches time.Weekday values.
	if dow&(1<<7) != 0 {
		dow |= 1 << 0
		dow &^= 1 << 7
	}

	return cronSchedule{
		minute:  minute,
		hour:    hour,
		dom:     dom,
		month:   month,
		dow:     dow,
		domStar: domStar,
		dowStar: dowStar,
		raw:     expr,
	}, nil
}

// parseField parses one cron field into a bitmask over [min,max]. It reports
// whether the field was an unrestricted "*" (needed for dom/dow OR semantics).
func parseField(field string, min, max int) (uint64, bool, error) {
	if field == "*" {
		return rangeMask(min, max), true, nil
	}

	var mask uint64
	for _, part := range strings.Split(field, ",") {
		step := 1
		rng := part
		if idx := strings.Index(part, "/"); idx >= 0 {
			s, err := strconv.Atoi(part[idx+1:])
			if err != nil || s < 1 {
				return 0, false, fmt.Errorf("invalid step in %q", part)
			}
			step = s
			rng = part[:idx]
		}

		lo, hi := min, max
		star := rng == "*"
		if !star {
			if idx := strings.Index(rng, "-"); idx >= 0 {
				var err error
				lo, err = strconv.Atoi(rng[:idx])
				if err != nil {
					return 0, false, fmt.Errorf("invalid range start in %q", part)
				}
				hi, err = strconv.Atoi(rng[idx+1:])
				if err != nil {
					return 0, false, fmt.Errorf("invalid range end in %q", part)
				}
			} else {
				v, err := strconv.Atoi(rng)
				if err != nil {
					return 0, false, fmt.Errorf("invalid value %q", part)
				}
				lo, hi = v, v
			}
		}

		if lo < min || hi > max || lo > hi {
			return 0, false, fmt.Errorf("value out of range [%d,%d] in %q", min, max, part)
		}
		for v := lo; v <= hi; v += step {
			mask |= 1 << uint(v)
		}
	}
	return mask, false, nil
}

func rangeMask(min, max int) uint64 {
	var mask uint64
	for v := min; v <= max; v++ {
		mask |= 1 << uint(v)
	}
	return mask
}
