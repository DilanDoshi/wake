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
