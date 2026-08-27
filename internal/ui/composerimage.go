package ui

// Image attachments on a draft: the state behind the `[Image #N]` chips a
// dropped image leaves in the box, and the two halves of sending one - the
// bytes that ride the wire and the chip that is stripped out of the text.
//
// The chip is the only tie between the draft and the bytes. An image sends only
// while its chip is in the draft, so deleting the chip drops the image and
// clearing the draft drops them all - the operator edits attachments the same
// way they edit words, with no second surface to manage.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
)

// imageAttachment is one dropped image and the id its chip carries.
type imageAttachment struct {
	id    int
	block core.ImageBlock
}

// imageChipFormat is how a chip is spelled, matching Claude Code's own
// `[Image #N]`. imageChipPattern reads the id back out, and stripImagePattern
// takes the chip and one trailing space with it.
const imageChipFormat = "[Image #%d]"

var (
	imageChipPattern  = regexp.MustCompile(`\[Image #(\d+)\]`)
	stripImagePattern = regexp.MustCompile(`\[Image #(\d+)\] ?`)
)

// Attach records a dropped image and inserts its chip at the cursor, returning
// the new composer. The id increments per draft so a chip's number never reuses
// one a deleted chip had.
func (c Composer) Attach(block core.ImageBlock) Composer {
	c.nextImageID++
	// A fresh slice keeps the value semantics the rest of Composer relies on:
	// a caller holding an older copy must not see this attachment appear.
	c.attachments = append(append([]imageAttachment(nil), c.attachments...),
		imageAttachment{id: c.nextImageID, block: block})
	c.ta.InsertString(fmt.Sprintf(imageChipFormat+" ", c.nextImageID))
	return c.fit().reposition()
}

// InsertText inserts text at the cursor - the path of a drop whose image could
// not be read is put back this way, so a failed drop loses nothing.
func (c Composer) InsertText(s string) Composer {
	c.ta.InsertString(s)
	return c.fit().reposition()
}

// Images are the attachments whose chip still appears in the draft, in the
// order they were attached. A chip the operator deleted drops its image.
func (c Composer) Images() []core.ImageBlock {
	backed := c.backedChipIDs()
	var out []core.ImageBlock
	for _, a := range c.attachments {
		if backed[a.id] {
			out = append(out, a.block)
		}
	}
	return out
}

// backedChipIDs is the set of chip ids that both appear in the draft and have a
// live attachment behind them. It is the one source of truth Images() and the
// strip share, so the bytes that ride the wire and the chips taken out of the
// text can never disagree - a chip with no attachment (a prompt recalled from
// history after its bytes were cleared, or one the operator typed by hand) is in
// neither set, so it sends no image and stays in the text as the literal words
// it is, rather than silently dropping an image or erasing text.
func (c Composer) backedChipIDs() map[int]bool {
	live := map[int]bool{}
	for _, a := range c.attachments {
		live[a.id] = true
	}
	backed := map[int]bool{}
	for id := range chipIDs(c.ta.Value()) {
		if live[id] {
			backed[id] = true
		}
	}
	return backed
}

// imageBytes is the base64 size of the images that would ride the wire now: the
// backed chips' blocks. It is what the aggregate size guard measures a new drop
// against, so a send can never build a frame past rpc's cap. See imagedrop.go.
func (c Composer) imageBytes() int {
	backed := c.backedChipIDs()
	n := 0
	for _, a := range c.attachments {
		if backed[a.id] {
			n += len(a.block.Data)
		}
	}
	return n
}

// chipIDs is the set of ids whose `[Image #id]` chip appears in a draft.
func chipIDs(s string) map[int]bool {
	ids := map[int]bool{}
	for _, m := range imageChipPattern.FindAllStringSubmatch(s, -1) {
		var id int
		if _, err := fmt.Sscanf(m[1], "%d", &id); err == nil {
			ids[id] = true
		}
	}
	return ids
}

// WireText is the draft as the agent should see it: the chips that stand for a
// real attachment removed (their images ride beside the text), and every other
// character - words, and any orphan chip with no attachment behind it - left
// exactly as typed. It takes the text to strip rather than reading the draft so
// the room can hand it the router's output, which has a leading @name removed.
func (c Composer) WireText(s string) string {
	backed := c.backedChipIDs()
	out := stripImagePattern.ReplaceAllStringFunc(s, func(m string) string {
		sub := stripImagePattern.FindStringSubmatch(m)
		var id int
		if _, err := fmt.Sscanf(sub[1], "%d", &id); err == nil && backed[id] {
			return ""
		}
		return m
	})
	return strings.TrimSpace(out)
}
