package core

// RewindResult, in Wake's vocabulary. Not an airlock file - it decodes
// nothing itself, holding only the struct protocol.go's controlResponseEvent
// fills in from wireControlBody. See event.go for KindRewindReceipt and
// Event.Rewind, which stay beside their EventKind and Event siblings.

// RewindResult is the payload of a KindRewindReceipt, nil on every other kind.
// Its tags reuse the receipt's own spelling, ControlResult's still_queued
// precedent for the same reason: a direct restatement earns no second name -
// see airlock_test.go's allowlist entry for this file.
//
// Rewound is not omitempty - omitempty on a bool drops false same as zero,
// and false is the refusal this field exists to report.
type RewindResult struct {
	Rewound                bool   `json:"rewound"`
	TargetMessageUUID      string `json:"targetMessageUuid,omitempty"`
	PrefillText            string `json:"prefillText,omitempty"`
	PrecedingAssistantUUID string `json:"precedingAssistantUuid,omitempty"`
	Error                  string `json:"error,omitempty"`
}

// FilesRewind is the payload of a KindFilesRewindReceipt: claude's answer to a
// rewind_files request, nil on every other kind. Target and Preview are what
// was asked, filled in by the session that asked (Session.RewindFiles) - the
// receipt itself names neither. Files, Insertions and Deletions answer a
// preview; Skipped, a restore: the paths left alone because a link stood there.
//
// Restorable is not omitempty, Rewound's reason: false is the refusal it reports.
type FilesRewind struct {
	Target     string   `json:"target,omitempty"`
	Preview    bool     `json:"dry,omitempty"`
	Restorable bool     `json:"restorable"`
	Files      []string `json:"files,omitempty"`
	Insertions int      `json:"insertions,omitempty"`
	Deletions  int      `json:"deletions,omitempty"`
	Skipped    int      `json:"skipped,omitempty"`
	Error      string   `json:"error,omitempty"`
}
