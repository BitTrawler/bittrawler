package ktable

import (
	"net/netip"
)

type reverseMap struct {
	addrs map[string]*infoForAddr
}

type infoForAddr struct {
	peerID ID
	hashes map[ID]struct{}
}

func newInfoForAddr(peerID ID, hashes ...ID) *infoForAddr {
	info := infoForAddr{
		peerID: peerID,
		hashes: make(map[ID]struct{}, len(hashes)),
	}
	info.addHashes(hashes...)

	return &info
}

func (i infoForAddr) addHashes(hashes ...ID) {
	for _, h := range hashes {
		i.hashes[h] = struct{}{}
	}
}

func (m reverseMap) putAddrPeerID(addr netip.Addr, id ID) {
	str := addr.String()
	if _, ok := m.addrs[str]; ok {
		m.addrs[str].peerID = id
	} else {
		m.addrs[str] = newInfoForAddr(id)
	}
}

func (m reverseMap) putAddrHashes(addr netip.Addr, hashes ...ID) {
	str := addr.String()
	if _, ok := m.addrs[str]; ok {
		m.addrs[str].addHashes(hashes...)
	} else {
		m.addrs[str] = newInfoForAddr(ID{}, hashes...)
	}
}

func (m reverseMap) getPeerIDForAddr(addr netip.Addr) (ID, bool) {
	info, ok := m.addrs[addr.String()]
	if ok && !info.peerID.IsZero() {
		return info.peerID, ok
	}

	return ID{}, false
}

// dropPeerIDForAddr forgets that an address is a node. The entry stays while
// it still has hashes that list it as a peer.
func (m reverseMap) dropPeerIDForAddr(addr netip.Addr) bool {
	str := addr.String()

	info, ok := m.addrs[str]
	if !ok || info.peerID.IsZero() {
		return false
	}

	info.peerID = ID{}

	if len(info.hashes) == 0 {
		delete(m.addrs, str)
	}

	return true
}

// dropHashForAddr removes one hash from an address, deleting the entry once it
// refers to neither a node nor any hash.
func (m reverseMap) dropHashForAddr(addr netip.Addr, hash ID) {
	str := addr.String()

	info, ok := m.addrs[str]
	if !ok {
		return
	}

	delete(info.hashes, hash)

	if len(info.hashes) == 0 && info.peerID.IsZero() {
		delete(m.addrs, str)
	}
}
