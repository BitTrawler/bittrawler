package config

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Type identifies a setting's value type. It is used to parse environment
// variables and database values, to render the settings page and to print the
// effective settings.
type Type string

// The value types a setting can have.
const (
	TypeBool     Type = "bool"
	TypeInt      Type = "int"
	TypeFloat    Type = "float"
	TypeString   Type = "string"
	TypeDuration Type = "duration"
	TypeStrings  Type = "strings"
)

// Setting describes one configurable value. Everything the settings page needs
// to render a control, and everything the loader needs to parse an override,
// comes from here.
type Setting struct {
	// Key is the dotted path of the setting, such as crawler.workers.metadata.
	Key string
	// Type is the setting's value type.
	Type Type
	// Group is the heading the setting belongs under, such as "Crawler".
	Group string
	// Usage is one sentence describing the setting.
	Usage string
	// Label is the field name shown in the settings page. Empty means derive
	// one from the key.
	Label string
	// Default is the value a new installation starts with. Its concrete type
	// matches Type.
	Default any
	// Advanced marks settings that belong behind an advanced toggle.
	Advanced bool
	// Restart reports that a change only applies after a restart.
	Restart bool
	// Secret marks a value that is not shown, printed or logged in full.
	Secret bool
	// Hidden marks a value the settings page does not render. It exists for
	// internal state such as the password hash and the session key.
	Hidden bool
	// Persist reports whether the value is stored in the settings database.
	// The data directory is the one setting that is not, because the database
	// is found through it.
	Persist bool
	// Enum lists the accepted values of a string setting.
	Enum []string
	// Min and Max bound a numeric setting.
	Min *float64
	Max *float64
}

// entry is a setting together with the path to its struct field.
type entry struct {
	Setting
	index []int
}

var (
	entriesOnce sync.Once
	allEntries  []entry
	byKey       map[string]int
)

// Settings returns every setting in declaration order.
func Settings() []Setting {
	entries := builtEntries()

	out := make([]Setting, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Setting)
	}

	return out
}

// Lookup returns the setting with a key.
func Lookup(key string) (Setting, bool) {
	e, ok := lookup(key)
	if !ok {
		return Setting{}, false
	}

	return e.Setting, true
}

// EnvName returns the environment variable that overrides a setting, such as
// BITTRAWLER_CRAWLER_WORKERS_METADATA for crawler.workers.metadata.
func EnvName(key string) string {
	return "BITTRAWLER_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

func builtEntries() []entry {
	entriesOnce.Do(func() {
		var out []entry

		walkConfig(reflect.TypeOf(Config{}), reflect.ValueOf(Default()), "", nil, &out)

		allEntries = out
		byKey = make(map[string]int, len(out))

		for i, e := range out {
			byKey[e.Key] = i
		}
	})

	return allEntries
}

func walkConfig(t reflect.Type, v reflect.Value, prefix string, index []int, out *[]entry) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		name := field.Tag.Get("key")
		if name == "" {
			continue
		}

		key := name
		if prefix != "" {
			key = prefix + "." + name
		}

		path := make([]int, len(index)+1)
		copy(path, index)
		path[len(index)] = i

		if isGroup(field.Type) {
			walkConfig(field.Type, v.Field(i), key, path, out)

			continue
		}

		*out = append(*out, newEntry(key, field, v.Field(i), path))
	}
}

// isGroup reports whether a field is a group of settings rather than a setting.
func isGroup(t reflect.Type) bool {
	return t.Kind() == reflect.Struct && t != durationType
}

var durationType = reflect.TypeOf(time.Duration(0))

func newEntry(key string, field reflect.StructField, value reflect.Value, index []int) entry {
	e := entry{
		Setting: Setting{
			Key:      key,
			Type:     typeOf(field.Type),
			Group:    groupOf(key),
			Usage:    field.Tag.Get("usage"),
			Label:    field.Tag.Get("label"),
			Default:  defaultValue(field.Type, value),
			Advanced: field.Tag.Get("advanced") == "true",
			Restart:  field.Tag.Get("restart") == "true",
			Secret:   field.Tag.Get("secret") == "true",
			Hidden:   field.Tag.Get("hidden") == "true",
			Persist:  field.Tag.Get("persist") != "false",
		},
		index: index,
	}

	if enum := field.Tag.Get("enum"); enum != "" {
		e.Enum = strings.Split(enum, ",")
	}

	if min := field.Tag.Get("min"); min != "" {
		e.Min = parseBound(key, "min", min)
	}

	if max := field.Tag.Get("max"); max != "" {
		e.Max = parseBound(key, "max", max)
	}

	return e
}

// typeOf maps a struct field type to a setting type. It panics on a type the
// registry does not know, because that is a mistake in the declaration and
// there is no sensible way to continue.
func typeOf(t reflect.Type) Type {
	switch {
	case t == durationType:
		return TypeDuration
	case t.Kind() == reflect.Bool:
		return TypeBool
	case t.Kind() == reflect.Int:
		return TypeInt
	case t.Kind() == reflect.Float64:
		return TypeFloat
	case t.Kind() == reflect.String:
		return TypeString
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.String:
		return TypeStrings
	}

	panic(fmt.Sprintf("config: unsupported setting type %s", t))
}

// groupOf returns the heading a setting belongs under, from the first part of
// its key.
func groupOf(key string) string {
	section, _, _ := strings.Cut(key, ".")

	switch section {
	case "data", "store":
		return "Storage"
	case "web":
		return "Web interface"
	case "log":
		return "Logging"
	case "crawler":
		return "Crawler"
	case "dht":
		return "DHT"
	case "torznab":
		return "Torznab"
	case "tmdb":
		return "TMDB"
	case "classify":
		return "Classification"
	default:
		return section
	}
}

// defaultValue returns a setting's default in the type the registry promises:
// time.Duration for durations and a copy of the slice for string lists.
func defaultValue(t reflect.Type, value reflect.Value) any {
	if t == durationType {
		return time.Duration(value.Int())
	}

	if t.Kind() == reflect.Slice {
		return append([]string{}, value.Interface().([]string)...)
	}

	return value.Interface()
}

func parseBound(key, tag, value string) *float64 {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		panic(fmt.Sprintf("config: setting %s has an invalid %s tag %q", key, tag, value))
	}

	return &parsed
}

// lookup returns the entry for a key.
func lookup(key string) (entry, bool) {
	entries := builtEntries()

	i, ok := byKey[key]
	if !ok {
		return entry{}, false
	}

	return entries[i], true
}

// value returns the struct field holding the setting.
func (e entry) value(cfg *Config) reflect.Value {
	return reflect.ValueOf(cfg).Elem().FieldByIndex(e.index)
}

// set parses raw as the setting's type and stores it.
func (e entry) set(cfg *Config, raw string) error {
	value, err := parseValue(e.Type, raw)
	if err != nil {
		return err
	}

	field := e.value(cfg)

	switch e.Type {
	case TypeBool:
		field.SetBool(value.(bool))
	case TypeInt:
		field.SetInt(value.(int64))
	case TypeFloat:
		field.SetFloat(value.(float64))
	case TypeString:
		field.SetString(value.(string))
	case TypeDuration:
		field.SetInt(int64(value.(time.Duration)))
	case TypeStrings:
		field.Set(reflect.ValueOf(value.([]string)))
	}

	return nil
}

// check validates the setting's current value against its declared type,
// enumeration and bounds. Rules that span several settings live in Validate.
func (e entry) check(cfg *Config) error {
	field := e.value(cfg)

	switch e.Type {
	case TypeString:
		if len(e.Enum) > 0 && !slices.Contains(e.Enum, field.String()) {
			return fmt.Errorf("%s: %q is not one of %s", e.Key, field.String(), strings.Join(e.Enum, ", "))
		}
	case TypeInt:
		return e.checkBounds(float64(field.Int()))
	case TypeFloat:
		return e.checkBounds(field.Float())
	}

	return nil
}

func (e entry) checkBounds(value float64) error {
	if e.Min != nil && value < *e.Min {
		return fmt.Errorf("%s: %v is below the minimum of %v", e.Key, value, *e.Min)
	}

	if e.Max != nil && value > *e.Max {
		return fmt.Errorf("%s: %v is above the maximum of %v", e.Key, value, *e.Max)
	}

	return nil
}

// parseValue converts a textual value, from an environment variable or the
// settings database, into the setting's value.
func parseValue(t Type, raw string) (any, error) {
	switch t {
	case TypeBool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid boolean %q", raw)
		}

		return value, nil
	case TypeInt:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q", raw)
		}

		return value, nil
	case TypeFloat:
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", raw)
		}

		return value, nil
	case TypeString:
		return raw, nil
	case TypeDuration:
		value, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid duration %q", raw)
		}

		return value, nil
	case TypeStrings:
		return splitList(raw), nil
	default:
		return nil, fmt.Errorf("unsupported type %s", t)
	}
}

// Format renders a setting's value as text, the form the settings database and
// the environment variables use.
func Format(value any) string {
	switch v := value.(type) {
	case time.Duration:
		return formatDuration(v)
	case []string:
		return strings.Join(v, ", ")
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

// formatDuration prints a duration the way a person would write it: "15m"
// rather than "15m0s", and "45s" for something that is not a whole minute.
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}

	if d%time.Minute == 0 {
		if d >= time.Hour {
			return fmt.Sprintf("%dh%dm", d/time.Hour, (d%time.Hour)/time.Minute)
		}

		return fmt.Sprintf("%dm", d/time.Minute)
	}

	text := d.String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}

	return text
}

// splitList splits a comma separated list and drops empty entries.
func splitList(raw string) []string {
	out := make([]string, 0)

	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

// Get returns a setting's typed value. Durations are time.Duration and string
// lists are copies, so the caller cannot modify the configuration through them.
func Get(cfg Config, key string) (any, bool) {
	e, ok := lookup(key)
	if !ok {
		return nil, false
	}

	field := e.value(&cfg)

	switch e.Type {
	case TypeBool:
		return field.Bool(), true
	case TypeInt:
		return int(field.Int()), true
	case TypeFloat:
		return field.Float(), true
	case TypeString:
		return field.String(), true
	case TypeDuration:
		return time.Duration(field.Int()), true
	case TypeStrings:
		return append([]string{}, field.Interface().([]string)...), true
	default:
		return nil, false
	}
}

// Set parses a textual value, as a settings form submits it, and stores it in
// cfg. Validation is the caller's job, because a group of settings only makes
// sense together.
func Set(cfg *Config, key, raw string) error {
	e, ok := lookup(key)
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}

	if !e.Persist {
		return fmt.Errorf("setting %q is not stored in the settings database", key)
	}

	return e.set(cfg, raw)
}
