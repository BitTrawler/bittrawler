package app

import (
	"context"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/BitTrawler/bittrawler/internal/protocol"
	"github.com/BitTrawler/bittrawler/internal/store"
)

// torrentActionTimeout bounds one removal. Deleting a row is a small write on
// one shard; anything slower is a problem worth reporting rather than waiting
// on.
const torrentActionTimeout = 10 * time.Second

// handleTorrentRemove deletes one torrent from the index. It does not remember
// the hash, so a later crawl that meets it again may index it again.
func (a *App) handleTorrentRemove(w http.ResponseWriter, r *http.Request) {
	a.removeTorrent(w, r, false)
}

// handleTorrentBlock deletes one torrent from the index and blocks its hash, so
// the crawl will not index it again. This is the action for a torrent that
// should not have been indexed at all.
func (a *App) handleTorrentBlock(w http.ResponseWriter, r *http.Request) {
	a.removeTorrent(w, r, true)
}

// removeTorrent is the shared body of the two maintenance actions.
func (a *App) removeTorrent(w http.ResponseWriter, r *http.Request, block bool) {
	if !a.authorised(w, r) {
		return
	}

	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)

		return
	}

	hash, ok := infoHashFromPath(r)
	if !ok {
		a.renderNotFound(w, r, r.PathValue("hash"))

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), torrentActionTimeout)
	defer cancel()

	torrent, found, err := a.torrentStore.Get(ctx, hash)
	if err != nil {
		a.logger.Error("read a torrent to remove it", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	if !found {
		a.renderNotFound(w, r, hex.EncodeToString(hash[:]))

		return
	}

	if block {
		if _, err := a.blocklist.Block(ctx, []protocol.ID{hash}); err != nil {
			a.logger.Error("block a torrent", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)

			return
		}
	}

	deleted, err := a.torrentStore.Delete(ctx, []store.Removal{{
		InfoHash:  hash,
		CreatedAt: torrent.CreatedAt,
		Category:  torrent.Category,
	}})
	if err != nil {
		a.logger.Error("remove a torrent", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	// A retention pass may have dropped the whole month between the read and the
	// delete. The row is gone either way.
	if deleted == 0 {
		a.renderNotFound(w, r, hex.EncodeToString(hash[:]))

		return
	}

	target := "/?removed=removed"
	if block {
		target = "/?removed=blocked"
	}

	http.Redirect(w, r, target, http.StatusSeeOther)
}

// infoHashFromPath decodes the hex infohash the route carries.
func infoHashFromPath(r *http.Request) ([20]byte, bool) {
	var hash [20]byte

	decoded, err := hex.DecodeString(r.PathValue("hash"))
	if err != nil || len(decoded) != 20 {
		return hash, false
	}

	copy(hash[:], decoded)

	return hash, true
}
