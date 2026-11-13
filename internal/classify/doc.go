// Package classify decides what a torrent is from its name and file list, and
// whether it may be indexed at all.
//
// Classification runs once at intake, while the metadata is in memory. The
// result is stored on the row and never recomputed, so a change to the rules
// only affects torrents crawled afterwards.
//
// The rules are plain Go over embedded keyword lists and the scene grammar,
// replacing the CEL workflow bitmagnet uses. Classification sees the complete
// file list rather than the first hundred files, and every rule also works when
// the metadata carries no files at all.
package classify
