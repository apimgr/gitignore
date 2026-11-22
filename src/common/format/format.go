// Package format provides localized formatting for user-facing values.
package format

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/common/i18n"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Duration renders a duration using at most two largest applicable units.
func Duration(value time.Duration, lang string) string {
	if value < 0 {
		value = 0
	}
	seconds := int64(value / time.Second)
	if seconds < 60 {
		return durationUnit(lang, "seconds", seconds)
	}
	minutes := seconds / 60
	seconds %= 60
	if minutes < 60 {
		parts := []string{durationUnit(lang, "minutes", minutes)}
		if seconds > 0 {
			parts = append(parts, durationUnit(lang, "seconds", seconds))
		}
		return strings.Join(parts, " ")
	}
	hours := minutes / 60
	minutes %= 60
	if hours < 24 {
		parts := []string{durationUnit(lang, "hours", hours)}
		if minutes > 0 {
			parts = append(parts, durationUnit(lang, "minutes", minutes))
		}
		return strings.Join(parts, " ")
	}
	days := hours / 24
	hours %= 24
	parts := []string{durationUnit(lang, "days", days)}
	if hours > 0 {
		parts = append(parts, durationUnit(lang, "hours", hours))
	}
	return strings.Join(parts, " ")
}

// Size renders a byte count with binary units and at most one decimal place.
func Size(value int64, lang string) string {
	if value < 0 {
		value = 0
	}
	if value < 1024 {
		return localizedUnit(lang, "bytes", value)
	}
	units := []string{"kilobytes", "megabytes", "gigabytes", "terabytes"}
	amount := float64(value) / 1024
	unit := 0
	for amount >= 1024 && unit < len(units)-1 {
		amount /= 1024
		unit++
	}
	amount = math.Round(amount*10) / 10
	printer := message.NewPrinter(localeTag(lang))
	number := printer.Sprintf("%.1f", amount)
	if math.Mod(amount, 1) == 0 {
		return localizedUnit(lang, units[unit], int64(amount))
	}
	message := i18n.Translate(lang, "plurals.format_"+units[unit]+".other")
	return strings.ReplaceAll(message, "{count}", number)
}

// Count renders a count with locale-specific grouping separators.
func Count(value int64, lang string) string {
	return message.NewPrinter(localeTag(lang)).Sprintf("%d", value)
}

func durationUnit(lang, unit string, count int64) string {
	return localizedUnit(lang, unit, count)
}

func localizedUnit(lang, unit string, count int64) string {
	key := "plurals.format_" + unit
	message := i18n.TranslatePlural(lang, key, int(count))
	return strings.ReplaceAll(message, strconv.FormatInt(count, 10), Count(count, lang))
}

func localeTag(lang string) language.Tag {
	tag, err := language.Parse(lang)
	if err != nil {
		return language.English
	}
	return tag
}
