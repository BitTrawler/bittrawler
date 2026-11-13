// Package settings stores the values the application owns: the settings the web
// interface changes.
//
// The store is a small SQLite database in the data directory, separate from the
// torrent shards and the blocklist. Keeping the application's own state apart
// from the configuration file means a deployment can keep its configuration
// under version control and read only, while the application still has
// somewhere to write the API key it generates and the password hash it sets
// during setup.
package settings
