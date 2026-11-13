// Package config declares BitTrawler's settings, loads their effective values
// and validates them.
//
// The declaration is the single source of truth. Settings are struct fields
// with tags, and the defaults, environment variable names, validation, the
// settings page and the reset command are all derived from that declaration.
//
// Values come from four places, each overriding the one before it: the
// defaults, the settings database, the environment and the bootstrap command
// line flags. The data directory cannot come from the settings database,
// because the database lives inside it; it is the one value that is only ever
// a default, an environment variable or a flag.
package config
