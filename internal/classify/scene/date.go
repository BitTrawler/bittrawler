package scene

import (
	"regexp"
	"time"
)

// Date patterns. Only forms with an unambiguous or conventional reading are
// accepted. The year-first forms are the daily-show convention; day-first is
// tried before month-first because that is the convention in release names.
// Numeric parts must be two digits: a version string such as "2021.3.10" is
// not a date, while daily releases are zero-padded.
var (
	isoDateRegex = regexp.MustCompile(
		`(?:^|[^0-9])((?:19|20)\d{2})[._ -](0[1-9]|1[0-2])[._ -](0[1-9]|[12]\d|3[01])(?:[^0-9]|$)`)
	yearMonthDayNameRegex = regexp.MustCompile(
		`(?:^|[^0-9])((?:19|20)\d{2})[._ -](jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*[._ -](0?[1-9]|[12]\d|3[01])(?:[^0-9]|$)`)
	dayMonthNameRegex = regexp.MustCompile(
		`(?:^|[^0-9])(0?[1-9]|[12]\d|3[01])[._ -](jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*[._ -]((?:19|20)\d{2})(?:[^0-9]|$)`)
	// The optional comma allows the written form "May 5, 2016".
	monthNameDayRegex = regexp.MustCompile(
		`(?:^|[^0-9])(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*[._ -](0?[1-9]|[12]\d|3[01]),?[._ -]((?:19|20)\d{2})(?:[^0-9]|$)`)
	dayMonthYearRegex = regexp.MustCompile(
		`(?:^|[^0-9])(0[1-9]|[12]\d|3[01])[._ -](0[1-9]|1[0-2])[._ -]((?:19|20)\d{2})(?:[^0-9]|$)`)
	// yearMonthDayShortRegex is the two-digit-year form used by daily releases,
	// for example "23.05.15". All three parts must be two digits: single-digit
	// parts are version numbers such as "26.6.1", not dates. It is tried after
	// every four-digit-year form.
	yearMonthDayShortRegex = regexp.MustCompile(
		`(?:^|[^0-9])(\d{2})[._ -](0[1-9]|1[0-2])[._ -](0[1-9]|[12]\d|3[01])(?:[^0-9]|$)`)
)

var monthNames = map[string]time.Month{
	"jan": time.January,
	"feb": time.February,
	"mar": time.March,
	"apr": time.April,
	"may": time.May,
	"jun": time.June,
	"jul": time.July,
	"aug": time.August,
	"sep": time.September,
	"oct": time.October,
	"nov": time.November,
	"dec": time.December,
}

// parseDate reads the air date from a lowercased name and returns it as
// YYYY-MM-DD, or "" when the name carries no date.
func parseDate(lower string) string {
	if match := isoDateRegex.FindStringSubmatch(lower); match != nil {
		if date, ok := formatDate(atoi(match[1]), time.Month(atoi(match[2])), atoi(match[3])); ok {
			return date
		}
	}

	if match := yearMonthDayNameRegex.FindStringSubmatch(lower); match != nil {
		if date, ok := formatDate(atoi(match[1]), monthNames[match[2][:3]], atoi(match[3])); ok {
			return date
		}
	}

	if match := dayMonthNameRegex.FindStringSubmatch(lower); match != nil {
		if date, ok := formatDate(atoi(match[3]), monthNames[match[2][:3]], atoi(match[1])); ok {
			return date
		}
	}

	if match := monthNameDayRegex.FindStringSubmatch(lower); match != nil {
		if date, ok := formatDate(atoi(match[3]), monthNames[match[1][:3]], atoi(match[2])); ok {
			return date
		}
	}

	if match := dayMonthYearRegex.FindStringSubmatch(lower); match != nil {
		if date, ok := formatDate(atoi(match[3]), time.Month(atoi(match[2])), atoi(match[1])); ok {
			return date
		}
	}

	if match := yearMonthDayShortRegex.FindStringSubmatch(lower); match != nil {
		if date, ok := formatDate(2000+atoi(match[1]), time.Month(atoi(match[2])), atoi(match[3])); ok {
			return date
		}
	}

	return ""
}

// formatDate validates a calendar date and renders it as ISO. The round trip
// through time.Date rejects impossible dates such as 31 February.
func formatDate(year int, month time.Month, day int) (string, bool) {
	if year < 1900 || year > 2099 {
		return "", false
	}

	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if date.Year() != year || date.Month() != month || date.Day() != day {
		return "", false
	}

	return date.Format(time.DateOnly), true
}
