package purfecterm

// Storage for the kitty graphics protocol: transmitted images live independently
// of where they are shown, so one image can carry many placements and a
// placement can outlive the escape code that made it.

import "sort"

// kittyImage is a transmitted image, held until deleted. Its pixels are shared
// by every placement of it.
type kittyImage struct {
	id     uint32
	number uint32
	bitmap *Bitmap

	// Animation. frames[0] is the root frame — the image as first transmitted
	// — so a frame number from the wire indexes this directly. current is the
	// 1-based frame a placement of this image shows.
	frames  []*kittyFrame
	current int
	gapMS   int
	loops   int
	running bool
}

// KittyImageStore holds transmitted images and hands out IDs. It is owned by a
// Buffer and guarded by the buffer's lock.
type kittyImageStore struct {
	byID     map[uint32]*kittyImage
	byNumber map[uint32]uint32 // image number -> most recent image ID
	nextAuto uint32            // IDs the terminal picks when the client sends none
}

func newKittyImageStore() *kittyImageStore {
	return &kittyImageStore{
		byID:     make(map[uint32]*kittyImage),
		byNumber: make(map[uint32]uint32),
	}
}

// MaxKittyImages caps how many transmitted images are retained. A client can
// transmit without ever deleting, so something has to bound it; the oldest
// image with no placements is evicted first, and only then the oldest overall.
const MaxKittyImages = 256

// allocID picks an unused ID for a client that transmitted without one. The
// protocol reserves 0 to mean "none", so allocation starts above it.
func (s *kittyImageStore) allocID() uint32 {
	for {
		s.nextAuto++
		if s.nextAuto == 0 {
			s.nextAuto = 1
		}
		if _, taken := s.byID[s.nextAuto]; !taken {
			return s.nextAuto
		}
	}
}

// put stores an image, replacing any previous one with the same ID.
func (s *kittyImageStore) put(img *kittyImage) {
	s.byID[img.id] = img
	if img.number != 0 {
		s.byNumber[img.number] = img.id
	}
}

// get returns a stored image by ID.
func (s *kittyImageStore) get(id uint32) *kittyImage {
	return s.byID[id]
}

// byImageNumber resolves an image number to the most recently transmitted image
// carrying it, which is what the protocol's I= key selects.
func (s *kittyImageStore) byImageNumber(number uint32) *kittyImage {
	if id, ok := s.byNumber[number]; ok {
		return s.byID[id]
	}
	return nil
}

// remove drops an image's stored pixels.
func (s *kittyImageStore) remove(id uint32) {
	if img, ok := s.byID[id]; ok {
		if img.number != 0 && s.byNumber[img.number] == id {
			delete(s.byNumber, img.number)
		}
		delete(s.byID, id)
	}
}

// evictTo trims the store to at most max images, preferring to drop images that
// have no placement on screen. referenced reports whether an ID is placed.
func (s *kittyImageStore) evictTo(max int, referenced func(uint32) bool) {
	if len(s.byID) <= max {
		return
	}
	ids := make([]uint32, 0, len(s.byID))
	for id := range s.byID {
		ids = append(ids, id)
	}
	// Lowest ID first approximates oldest first, since IDs are handed out
	// ascending and a client reusing an ID replaces in place.
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, pass := range []bool{true, false} { // unreferenced first, then any
		for _, id := range ids {
			if len(s.byID) <= max {
				return
			}
			if _, still := s.byID[id]; !still {
				continue
			}
			if pass && referenced(id) {
				continue
			}
			s.remove(id)
		}
	}
}
