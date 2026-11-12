// Package classify decides what a torrent is from its name and file list, and
// whether it must not be indexed at all.
//
// Classification runs once at intake, while the metadata is in memory: the
// result is stored on the torrent row and is never recomputed. It is a rewrite
// of the bitmagnet classifier, whose ordered rules are kept as knowledge but
// whose CEL workflow is replaced by plain Go.
//
// Two things differ from the original on purpose. The file list is the complete
// list from the metadata, not the first hundred rows the old schema stored, and
// every rule also works when the file list is missing, because a third of the
// torrents in the reference database were indexed without one.
package classify
