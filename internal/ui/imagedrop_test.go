package ui

// Dropping an image into the composer: a bracketed paste of a file path becomes
// an `[Image #N]` chip and, on send, an image block on the wire.

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// imageBlockFor is a decoded image block for the composer-level tests that do
// not go through a file read.
func imageBlockFor(t *testing.T) core.ImageBlock {
	t.Helper()
	return core.ImageBlock{MediaType: "image/png", Data: onePixelPNG}
}

// sendFrameOf is the one FrameSend among what a command wrote. Opening a DM
// queues a history frame on the same connection, so a send test cannot assume
// its message is the only frame there.
func sendFrameOf(t *testing.T, a App, cmd tea.Cmd) rpc.Frame {
	t.Helper()
	var found []rpc.Frame
	for _, f := range sentFrames(t, a, cmd) {
		if f.Kind == rpc.FrameSend {
			found = append(found, f)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d send frames reached the daemon, want exactly 1", len(found))
	}
	return found[0]
}

// onePixelPNG is a 1x1 PNG - a real one, so http.DetectContentType sniffs
// image/png from its magic bytes exactly as it would a screenshot.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// writePNG drops a real PNG into a temp dir and returns its absolute path.
func writePNG(t *testing.T, name string) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(onePixelPNG)
	if err != nil {
		t.Fatalf("decode test png: %v", err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write test png: %v", err)
	}
	return path
}

// writeBigJPEG writes a file of the given raw size whose first bytes are the
// JPEG magic (FF D8 FF), so http.DetectContentType sniffs image/jpeg while the
// size drives the base64 total the aggregate guard measures.
func writeBigJPEG(t *testing.T, name string, size int) string {
	t.Helper()
	data := make([]byte, size)
	data[0], data[1], data[2] = 0xFF, 0xD8, 0xFF
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write big jpeg: %v", err)
	}
	return path
}

// pastePath delivers a path the way a terminal delivers a drag-and-drop: one
// bracketed paste carrying the whole path as runes.
func pastePath(a App, path string) (App, tea.Cmd) {
	return pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune(path)})
}

// dropImage pastes a path and folds the read the way Bubble Tea would: the
// paste's command can be a batch (the read plus a freshly-opened pane's history
// asks), which the runtime unwraps and runs one by one. drainBatch does the
// same so the imageDropMsg reaches Update.
func dropImage(t *testing.T, a App, path string) App {
	t.Helper()
	a, cmd := pastePath(a, path)
	if cmd == nil {
		t.Fatal("a dropped image path produced no read command")
	}
	for _, msg := range drainBatch(cmd) {
		m, _ := a.Update(msg)
		a = m.(App)
	}
	return a
}

// drainBatch runs a command and flattens any tea.Batch it produces into the
// messages the runtime would deliver.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, drainBatch(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

func TestImageDropPathsRecognizesImagePaste(t *testing.T) {
	cases := []struct {
		name  string
		paste string
		want  []string
	}{
		{"one absolute png", "/a/b/c.png", []string{"/a/b/c.png"}},
		{"escaped spaces", `/a/Screenshot\ 2026.png`, []string{"/a/Screenshot 2026.png"}},
		{"two images", "/a/x.png /a/y.jpg", []string{"/a/x.png", "/a/y.jpg"}},
		{"uppercase extension", "/a/B.PNG", []string{"/a/B.PNG"}},
		{"plain text", "hello world", nil},
		{"relative path", "b/c.png", nil},
		{"absolute non-image", "/a/notes.txt", nil},
		{"one image one not", "/a/x.png /a/notes.txt", nil},
		{"empty", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := imageDropPaths(c.paste)
			if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
				t.Errorf("imageDropPaths(%q) = %v, want %v", c.paste, got, c.want)
			}
		})
	}
}

// WireText strips only chips that have a live attachment behind them; an orphan
// chip - one the operator typed, or one recalled from history after its bytes
// were cleared - is left as the literal text it is, never silently erased.
func TestWireTextStripsOnlyBackedChips(t *testing.T) {
	// A composer with one real attachment (#1) and its chip in the draft.
	c := NewComposer().Attach(imageBlockFor(t))

	cases := []struct{ in, want string }{
		{"[Image #1] what is this?", "what is this?"}, // backed → stripped
		{"look [Image #1] here", "look here"},
		{"[Image #1]", ""},
		{"no chips here", "no chips here"},
		{"[Image #2] typed by hand", "[Image #2] typed by hand"}, // orphan → kept
		{"[Image #1] and [Image #7]", "and [Image #7]"},          // one each
	}
	for _, tc := range cases {
		if got := c.WireText(tc.in); got != tc.want {
			t.Errorf("WireText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A prompt recalled from history carries the chip text but no attachment (Reset
// cleared it on send). Resending must not silently drop an image or strip the
// chip: the chip stays as literal text and no image rides along.
func TestRecalledChipSendsAsLiteralTextNoImage(t *testing.T) {
	// Simulate a recalled prompt: a composer with the chip text but no
	// attachment behind it, which is exactly what WithDraft produces.
	c := NewComposer().WithDraft("[Image #1] what is this?")
	if got := len(c.Images()); got != 0 {
		t.Fatalf("Images() = %d for a chip with no attachment, want 0", got)
	}
	if got := c.WireText(c.Value()); got != "[Image #1] what is this?" {
		t.Errorf("WireText = %q, want the chip left as literal text - never silently stripped", got)
	}
}

// A dropped image path becomes a chip in the draft, not the raw path typed as
// text - which is the whole point of the feature.
func TestPastedImagePathBecomesAChip(t *testing.T) {
	path := writePNG(t, "shot.png")
	a := dropImage(t, newRoomApp(t).withSize(120, 30), path)

	draft := a.composer().Value()
	if !strings.Contains(draft, "[Image #1]") {
		t.Errorf("draft = %q, want an [Image #1] chip", draft)
	}
	if strings.Contains(draft, path) {
		t.Errorf("draft = %q, still holds the raw path", draft)
	}
}

// An ordinary text paste is left alone - it is inserted as text, never treated
// as a drop.
func TestPlainPasteStaysText(t *testing.T) {
	a := newRoomApp(t).withSize(120, 30)
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("just some text")})
	if cmd != nil {
		if _, ok := cmd().(imageDropMsg); ok {
			t.Fatal("a plain text paste was taken as an image drop")
		}
	}
	if got := a.composer().Value(); got != "just some text" {
		t.Errorf("draft = %q, want the pasted text", got)
	}
}

// The dropped image reaches the daemon as an image block, and the chip is
// stripped out of the text that rides beside it.
func TestDroppedImageSendsAsBlock(t *testing.T) {
	path := writePNG(t, "shot.png")
	a := newRoomApp(t).withSize(120, 30).
		withRoster(rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateIdle}).
		openDMWith("s1", "sydney")
	a = dropImage(t, a, path)

	// Type a question after the chip, then send. Opening the DM also queued a
	// history frame, so pick the send out of what reached the daemon.
	a = a.withDraft("what is this?")
	sent, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	frame := sendFrameOf(t, sent, cmd)

	if len(frame.Images) != 1 {
		t.Fatalf("frame carried %d images, want 1", len(frame.Images))
	}
	if frame.Images[0].MediaType != "image/png" {
		t.Errorf("media type = %q, want image/png", frame.Images[0].MediaType)
	}
	if frame.Images[0].Data != onePixelPNG {
		t.Errorf("image data does not match the file's bytes")
	}
	if strings.Contains(frame.Text, "[Image") {
		t.Errorf("wire text = %q, still carries the chip", frame.Text)
	}
	if frame.Text != "what is this?" {
		t.Errorf("wire text = %q, want the question alone", frame.Text)
	}
}

// A drop whose file cannot be read reports the failure and puts the path back
// into the draft, so nothing the operator did is lost.
func TestFailedDropPutsPathBack(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.png")
	a := dropImage(t, newRoomApp(t).withSize(120, 30), missing)

	draft := a.composer().Value()
	if strings.Contains(draft, "[Image") {
		t.Errorf("draft = %q, a failed read must not leave a chip", draft)
	}
	if !strings.Contains(draft, missing) {
		t.Errorf("draft = %q, want the path put back after a failed read", draft)
	}
}

// A drop whose images together would exceed the frame budget is refused for the
// image that crosses it, so the send path never builds a frame the daemon would
// hang up on. The first image fits; the second is over budget and refused.
func TestOversizeTotalDropIsRefused(t *testing.T) {
	// Two files each ~7MB of base64 - each under the per-file cap, together over
	// the 12MB total. writeBigJPEG makes a real JPEG so the sniff passes.
	big1 := writeBigJPEG(t, "a.jpg", 7<<20)
	big2 := writeBigJPEG(t, "b.jpg", 7<<20)
	a := newRoomApp(t).withSize(120, 30)

	a = dropImage(t, a, big1)
	a = dropImage(t, a, big2)

	if got := len(a.composer().Images()); got != 1 {
		t.Fatalf("Images() = %d, want 1 - the second image is over the total budget and must be refused", got)
	}
	// The first chip is in; the second never got one.
	if strings.Count(a.composer().Value(), "[Image #") != 1 {
		t.Errorf("draft = %q, want exactly one chip", a.composer().Value())
	}
}

// A leading image chip must not hide the @mention behind it: dropping an image
// into the room then typing @john routes to john, not the manager/nobody. The
// draft is "[Image #1] @john look"; routing reads the chip-stripped text.
func TestDroppedImageThenMentionRoutesToTheMentionedAgent(t *testing.T) {
	path := writePNG(t, "shot.png")
	a := roomWithHeldDMs(t, "s2") // john = s2, room focused, no manager seated
	a = dropImage(t, a, path)
	a = a.withDraft("@john look") // appended after the chip

	sent, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	frames := sentFrames(t, sent, cmd)
	var send *rpc.Frame
	for i := range frames {
		if frames[i].Kind == rpc.FrameSend {
			if send != nil {
				t.Fatalf("more than one send frame reached the daemon")
			}
			send = &frames[i]
		}
	}
	if send == nil {
		t.Fatal("nothing was sent - the leading chip defeated the @mention route")
	}
	if send.SessionID != "s2" {
		t.Errorf("routed to %q, want s2 (john) - the chip must not hide the mention", send.SessionID)
	}
	if send.Text != "look" {
		t.Errorf("wire text = %q, want %q - chip and mention both stripped", send.Text, "look")
	}
	if len(send.Images) != 1 {
		t.Errorf("the image did not ride along: %d images", len(send.Images))
	}
}

// Deleting a chip drops its image: what sends is what the draft still shows.
func TestDeletingChipDropsImage(t *testing.T) {
	c := NewComposer().Attach(imageBlockFor(t))
	if got := len(c.Images()); got != 1 {
		t.Fatalf("Images() = %d after Attach, want 1", got)
	}
	// Clear the draft; the chip is gone, so the image is too.
	c = c.WithDraft("no image now")
	if got := len(c.Images()); got != 0 {
		t.Errorf("Images() = %d after the chip was removed, want 0", got)
	}
}
