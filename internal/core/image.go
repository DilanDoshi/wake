package core

// ImageBlock is one image attached to a user message: its media type and the
// raw base64 of its bytes. Wake reads it off a file dropped into the composer
// and hands it to EncodeUserMessage, which renders Claude's wire shape behind
// the airlock.
//
// The tags are Wake's own spelling on purpose. This value crosses the rpc
// socket (rpc.Frame.Images), not Claude's stdin, so naming Claude's snake_case
// media_type here would be a wire word outside the airlock - encode.go owns
// that translation. The bytes are handed over raw: Claude budgets and
// downscales an oversized image itself, and only for a base64 source, so Wake
// does not pre-resize (docs/superpowers/notes/2026-08-15-image-input-findings.md).
type ImageBlock struct {
	MediaType string `json:"mediaType"`
	Data      string `json:"data"`
}
