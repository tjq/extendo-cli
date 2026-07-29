// Package store reads and writes the clipboard history persisted by the
// extendo macOS app.
package store

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"
)

// refDate is the Foundation reference date. The app persists createdAt as a
// JSON number of seconds since this instant — never a Unix timestamp.
var refDate = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// Kind classifies an item by the most specific pasteboard representation it
// carries.
type Kind int

const (
	KindText Kind = iota
	KindRichText
	KindImage
	KindFile
	KindOther
)

// String implements fmt.Stringer so test failures and CLI output read clearly.
func (k Kind) String() string {
	switch k {
	case KindText:
		return "text"
	case KindRichText:
		return "richText"
	case KindImage:
		return "image"
	case KindFile:
		return "file"
	case KindOther:
		return "other"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// Pasteboard type tables, in the priority order the app itself uses: an item
// carrying both an image and a text rep is an image.
var (
	imageTypes    = []string{"public.tiff", "public.png", "public.jpeg"}
	fileTypes     = []string{"public.file-url"}
	richTextTypes = []string{"public.rtf", "public.html", "Apple HTML pasteboard type"}
	textTypes     = []string{"public.utf8-plain-text", "public.plain-text", "public.text", "NSStringPboardType"}
)

// Payload is a representation's bytes, held either inline or as a path to a
// blob file. Exactly one field is set.
type Payload struct {
	Inline []byte // set when the payload kind is "inline"
	Path   string // set when the payload kind is "external"; relative to <dir>/blobs
}

type payloadJSON struct {
	Kind string `json:"kind"`
	Data string `json:"data,omitempty"`
	Path string `json:"path,omitempty"`
}

func (p *Payload) UnmarshalJSON(data []byte) error {
	var raw payloadJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decoding payload: %w", err)
	}

	switch raw.Kind {
	case "inline":
		decoded, err := base64.StdEncoding.DecodeString(raw.Data)
		if err != nil {
			return fmt.Errorf("decoding inline payload: %w", err)
		}

		*p = Payload{Inline: decoded}
	case "external":
		*p = Payload{Path: raw.Path}
	default:
		return fmt.Errorf("unknown payload kind %q", raw.Kind)
	}

	return nil
}

func (p Payload) MarshalJSON() ([]byte, error) {
	if p.Path != "" {
		return json.Marshal(payloadJSON{Kind: "external", Path: p.Path})
	}

	return json.Marshal(payloadJSON{
		Kind: "inline",
		Data: base64.StdEncoding.EncodeToString(p.Inline),
	})
}

// Representation is one pasteboard type/payload pair.
type Representation struct {
	Type    string  `json:"type"`
	Payload Payload `json:"payload"`
}

// Item is a single clipboard history entry.
//
// Decoding keeps every field of the source object in raw, so re-encoding an
// item preserves keys this CLI does not know about. IsPinned is the only field
// we ever write back.
type Item struct {
	ID           string
	CreatedAt    time.Time
	IsPinned     bool
	SourceBundle string // "" when absent
	Reps         []Representation

	raw map[string]json.RawMessage
}

func (it *Item) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decoding item: %w", err)
	}

	item := Item{raw: raw, Reps: []Representation{}}

	if err := decodeField(raw, "id", &item.ID); err != nil {
		return err
	}

	var seconds float64
	if err := decodeField(raw, "createdAt", &seconds); err != nil {
		return err
	}

	item.CreatedAt = refDate.Add(secondsToDuration(seconds))

	if err := decodeField(raw, "isPinned", &item.IsPinned); err != nil {
		return err
	}

	if err := decodeField(raw, "sourceBundleIdentifier", &item.SourceBundle); err != nil {
		return err
	}

	if err := decodeField(raw, "representations", &item.Reps); err != nil {
		return err
	}

	*it = item

	return nil
}

// secondsToDuration converts a fractional second count without losing
// sub-second precision. Scaling the whole value by 1e9 in one multiplication
// would overflow float64's 53-bit mantissa — timestamps this far from the
// reference date land near 7.7e17ns — so the integer and fractional parts are
// scaled separately.
func secondsToDuration(seconds float64) time.Duration {
	whole, frac := math.Modf(seconds)

	return time.Duration(whole)*time.Second + time.Duration(frac*float64(time.Second))
}

// decodeField decodes raw[key] into dest, leaving dest untouched when the key
// is absent or JSON null.
func decodeField(raw map[string]json.RawMessage, key string, dest any) error {
	value, ok := raw[key]
	if !ok || string(value) == "null" {
		return nil
	}

	if err := json.Unmarshal(value, dest); err != nil {
		return fmt.Errorf("decoding item field %q: %w", key, err)
	}

	return nil
}

func (it Item) MarshalJSON() ([]byte, error) {
	if it.raw == nil {
		return json.Marshal(it.knownFields())
	}

	out := make(map[string]json.RawMessage, len(it.raw))
	for key, value := range it.raw {
		out[key] = value
	}

	pinned, err := json.Marshal(it.IsPinned)
	if err != nil {
		return nil, fmt.Errorf("encoding isPinned: %w", err)
	}

	out["isPinned"] = pinned

	return json.Marshal(out)
}

// knownFields renders an item that never came from JSON — one built in code or
// in a test — using only the fields this package understands.
func (it Item) knownFields() map[string]any {
	fields := map[string]any{
		"id":              it.ID,
		"createdAt":       it.CreatedAt.Sub(refDate).Seconds(),
		"isPinned":        it.IsPinned,
		"representations": it.Reps,
	}

	if it.SourceBundle != "" {
		fields["sourceBundleIdentifier"] = it.SourceBundle
	}

	return fields
}

// Kind reports the item's classification, most specific representation wins.
func (it Item) Kind() Kind {
	table := []struct {
		types []string
		kind  Kind
	}{
		{imageTypes, KindImage},
		{fileTypes, KindFile},
		{richTextTypes, KindRichText},
		{textTypes, KindText},
	}

	for _, entry := range table {
		if it.hasRepType(entry.types) {
			return entry.kind
		}
	}

	return KindOther
}

func (it Item) hasRepType(types []string) bool {
	for _, rep := range it.Reps {
		if slices.Contains(types, rep.Type) {
			return true
		}
	}

	return false
}
