// The airlock, enforced by the compiler rather than by review.
//
// CLAUDE.md's third non-negotiable is that the internal/core airlock files
// are the only non-test files that know Claude Code's stream-json format.
// Nothing checked it, and it had not been true for some time: 23 wire
// literals had accumulated in internal/ui/dm_blocks.go and
// internal/render/tool.go, none of them announced by anything. A rule nothing
// enforces is a rule that decays at exactly the rate people stop remembering
// it.
//
// The rule named one file, protocol.go, until that file passed the 800-line
// hard max and was split; airlockFiles below is the set, and it is what makes
// the restatement checkable rather than a sentence in a header.
//
// This walks every non-test .go file in the tree, collects every string
// literal and every json struct tag, and fails on any that names a word from
// Claude's wire vocabulary - unless the exact (file, literal) pair is
// allowlisted below. The allowlist is the amended rule in a form the compiler
// reads: every entry is a place Wake deliberately uses the same word for its
// own purposes, and adding one is a decision somebody has to write down.
//
// golangci-lint cannot do this. forbidigo matches identifiers and call
// expressions, not arbitrary string literals, and a custom plugin costs more
// than these hundred lines.
//
// SCOPE, stated because the review found leaks this does not cover. This
// enforces "knows Claude's JSON". It does not enforce "knows Claude" - the
// fifteen claude-specific CLI flags in the argv internal/core/argv.go's
// buildArgs produces - thirteen on any argv, plus --resume and --fork-session
// on a fork's, which identityArgs is the only place allowed to spell - are
// outside JSON and outside this test. They are a real second leak with a
// different shape (argv, not a decoder) and they want their own ruling, not a
// silent extension of this one.
//
// The --permission-mode *values* used to be named in that same breath, as a
// duplication across internal/ui/composer.go and internal/daemon/spawn.go. They
// are not a leak any more and not because this test grew: ⇧⇥ made the mode real,
// so the words travel as JSON as well as as argv, and they moved behind the
// airlock into vocabulary.go. Both files ask core for the spelling.
//
// **The identity third of that leak now has one**, and it lives beside this
// file rather than inside it: argv_test.go's
// TestTheIdentityFlagsAreSpelledOnlyInArgv walks the same tree and holds
// --session-id, --resume, --fork-session and --continue to argv.go. It is a
// separate check because it polices a different thing in a different way - a
// substring of a literal that reaches a command line, rather than a whole
// literal that came off a wire - and folding it in here would have made one
// vocabulary answer to two rules. The --permission-mode values are still
// unruled.
//
// That count was written as "twelve" and had been wrong since the day it was
// written: ec2748e's own buildArgs already spelled thirteen. It is now counted
// from the source rather than incremented, which is the only way a number in a
// comment nothing asserts stays true. Re-counted at the split, from argv.go:
// still fifteen and thirteen.

package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is where the walk starts, relative to this package's directory.
const repoRoot = "../.."

// airlockFiles are the non-test files permitted to name any of this.
//
// It was one file until protocol.go passed the project's 800-line hard max.
// docs/notes/decisions.md ruled ahead of time that the rule would then be restated
// to name the airlock *files* - and this list is what makes that restatement
// checkable rather than a sentence in a header: a fifth member cannot be
// added without editing it, and the set is what the leak check exempts.
var airlockFiles = wordSet([]string{
	"internal/core/protocol.go",
	"internal/core/wire.go",
	"internal/core/vocabulary.go",
	"internal/core/encode.go",
})

// claudeWireVocabulary is what a file must not name outside the airlock.
//
// Every entry is a word that appears in testdata/stream/*.jsonl as a frame
// type, a block type, a subtype, a field name, a field value or a tool name -
// checked against the corpus, not against documentation. A word here is a
// word that only code reading Claude's JSON has any business writing.
//
// Deliberately absent: generic words the corpus carries but nothing could act
// on ("role", "type", "content"), because they would fire on half the tree
// and the allowlist would become the file. The test is worth having for the
// words that are unmistakably Claude's.
// It is built from a slice rather than written as a map literal on purpose:
// a map of string to bool can be disarmed one word at a time by changing a
// value to false, and nothing reading it would notice. A slice has no such
// spelling.
var claudeWireVocabulary = wordSet([]string{
	// Frame types.
	"assistant", "control_request", "control_response",
	"control_cancel_request",
	"command_lifecycle", "rate_limit_event", "conversation_reset",

	// The token stream, under --include-partial-messages: the frame, the one
	// of seven streaming event types Wake reads, and the one of five delta
	// types inside it. All three are unmistakably Claude's - an underscored
	// wire type no Go program writes by accident - where "event" and "delta"
	// beside them are in deliberatelyGeneric, and cannot be reached without
	// first naming one of these.
	"stream_event", "content_block_delta", "text_delta", "message_delta",
	"message_start",

	// Block types.
	"tool_use", "tool_result", "thinking",

	// The image block and its base64 source. Recorded only in
	// testdata/input/image-block.stdin.jsonl (see notInTheCorpus): Wake writes
	// it and reads it back off a transcript, but no recorded stdout carries one.
	// "source" and "data" beside them are in deliberatelyGeneric - words any
	// program uses, and unreachable without first naming one of these three.
	"image", "base64", "media_type",

	// The init frame's MCP roster, and the one status worth a line on screen.
	// Both are unmistakably Claude's spellings - an underscored wire key and a
	// hyphenated value no Go program writes by accident - so both are policed
	// while "connected" and "pending" beside them are not; see
	// deliberatelyGeneric for why those two cannot be.
	"mcp_servers", "needs-auth",

	// The init frame's own command set, which is what a completion menu
	// offers. Policed rather than excused as generic for "mcp_servers"'
	// reason: it is an underscored wire key no Go program writes by accident,
	// and a file outside the airlock naming it would be a view reading
	// claude's announcement of itself. internal/ui reaches the words through
	// core.SessionFacts.SlashCommands, whose own tag is Wake's spelling.
	"slash_commands",

	// On the on-disk transcript only, which is a different format from the
	// stream and is read by DecodeTranscriptLine. It marks a subagent's line;
	// the conversation Wake restores is the one the operator was having.
	//
	// "timestamp" is the second such key and the only clock in either format.
	// It is policed rather than excused as generic for "header" and "preview"'s
	// recorded reason, and the same check was run rather than assumed: no file
	// in this tree names the literal outside this package, so the word costs
	// Wake nothing and an exemption would be paying for a collision that does
	// not exist. It reaches a reader as core.Event.At, which is Wake's word for
	// it - and a file outside the airlock naming it would be one deciding when
	// a turn happened by reading Claude's on-disk format.
	"isSidechain", "timestamp",

	// The rest of the on-disk tree, read by DecodeTranscriptNode rather than
	// DecodeTranscriptLine: a line's own identity, its parent's, and the
	// last-prompt marker a rewind leaves behind, naming the leaf it rewound
	// to. Policed for "isSidechain" and "timestamp"'s own reason, and the same
	// check was run rather than assumed: none of the four is named outside
	// this package. They reach a reader as core.TranscriptNode's fields,
	// which are Wake's words for them.
	"uuid", "parentUuid", "leafUuid", "last-prompt",

	// system subtypes.
	"compact_boundary", "permission_denied", "hook_started",
	"hook_response", "thinking_tokens", "task_started",
	"task_progress", "task_updated", "task_notification",
	"background_tasks_changed", "status", "system",

	// The compaction status frame's payload: the value the start flag carries
	// and the key the end frame carries its outcome under. Both are read only in
	// systemNoticeFor, which tells the two subtype-"status" frames apart. Wake's
	// own words for what they mean are NoticeCompacting and NoticeCompacted.
	"compacting", "compact_result",

	// The task lifecycle's own fields and values, policed as the rest of the
	// subtype's payload is. None of the ten appears anywhere in this tree
	// outside internal/core, so policing them costs Wake nothing - the
	// argument "header" and "preview" are policed on.
	//
	// "stopped" and "killed" are the reason core.TaskStopped is spelled
	// "halted": a Wake constant carrying Claude's word for the same thing is
	// a passthrough wearing Wake's type, and task.go is not an airlock file.
	//
	// "tasks" and "patch" are the two that look generic and are not. Both are
	// wire keys with one meaning each here, and Wake's own words for what
	// they carry are TaskSet.Live and TaskUpdate.Status.
	"task_type", "local_agent", "local_bash",
	"tasks", "patch", "stopped", "killed",

	// The live checklist tools and their input keys. TodoWrite is retired in
	// 2.1.240 and its replacement builds a list across TaskCreate/TaskUpdate
	// calls (task-checklist.jsonl). "subject" and "taskId" are policed for
	// "patch" and "content"'s reason - wire keys with one meaning here, and
	// Wake's own words are ChecklistOp.Text and .ID. "deleted" is the fourth
	// status the 2.1.240 bundle lists beside the three drawn ones.
	"TaskCreate", "TaskUpdate", "subject", "taskId", "deleted",
	"total_tokens", "tool_uses", "duration_ms",

	// control_request / control_response.
	"can_use_tool", "cancel_queued", "still_queued",
	"updatedInput", "permission_suggestions", "display_name",
	"requires_user_interaction",

	// The second control_request Wake sends, and init's report of what it did.
	// Both are policed where "mode" itself is not: these two spellings are
	// unmistakably Claude's, and they are the only route to the mode a session
	// is really in. A file outside the airlock naming either is a file deciding
	// what a frame means by reading Claude's English.
	"set_permission_mode", "permissionMode",

	// The rewind_conversation control_request, and its fields. Rewinding a
	// session's conversation to an earlier point in its transcript.
	"rewind_conversation", "target_message_uuid", "last_seen_user_message_uuid",
	"interrupt_if_running",

	// The rewind_conversation control_response - the receipt for the request
	// above. Claude spells its own fields differently across the two frames:
	// the request's target_message_uuid comes back camelCase as
	// targetMessageUuid, and prefillText/precedingAssistantUuid have no
	// snake_case counterpart at all, since nothing Wake sends carries them.
	// rewound is the discriminator itself - see wireControlBody.Rewound.
	"rewound", "targetMessageUuid", "prefillText", "precedingAssistantUuid",

	// Two of the five permission modes, and the two that are *not* in
	// deliberatelyGeneric with "auto" and "default". The argument there was that
	// policing the plainest English in the corpus would fire across the tree;
	// these two are camelCase mode words no Go program writes by accident, so
	// they cost Wake nothing and internal/ui reaches both through core's
	// constants. Run rather than assumed: no file outside this package names
	// either literal.
	"acceptEdits", "dontAsk",

	// An interactive ask, its payload, and the answer that rides back inside
	// its allow.
	//
	// "label" is deliberately not here and sits in deliberatelyGeneric below:
	// Wake has its own Label on every session and every roster row, so
	// policing the word would fire on Wake's own vocabulary and buy an
	// exemption where the leak is not. It is also not a route in on its own -
	// a file cannot reach an option's label without first naming "questions"
	// and "options", both of which are policed here.
	//
	// "header" and "preview" are policed rather than excused as generic, and
	// the test for that was run rather than assumed: no file outside this
	// package names either literal, so the word costs Wake nothing and buying
	// an exemption would be paying for a collision that does not exist. Both
	// are drawn - the chip above a question, the sample beside an option - and
	// both reach a renderer as Question.Header and Option.Preview, which are
	// Wake's words for them.
	"questions", "question", "options", "answers", "plan", "header", "preview",

	// Field names that only a decoder needs.
	"parent_tool_use_id", "subagent_type", "task_description",
	"tool_use_id", "agent_id", "agentId", "agentType",
	"tool_use_result", "command_uuid", "new_conversation_id",
	"rate_limit_info", "isReplay", "isSynthetic",
	"run_in_background", "last_tool_name", "task_id",
	"non_execution_kind", "permission_denials", "terminal_reason",
	"modelUsage", "total_cost_usd", "num_turns",

	// How full the window is. Every one of these is unmistakably Claude's
	// accounting: nothing above the airlock computes a context percentage from
	// raw token fields, it reads the two numbers Wake resolved.
	//
	// "model" is deliberately NOT here and sits in deliberatelyGeneric: which
	// model a session runs is a fact Wake carries end to end, the way it
	// carries a directory and a name, so policing the word would fire on
	// Wake's own vocabulary rather than on a leak.
	//
	// "iterations" is the per-round-trip breakdown inside a turn's usage; its
	// last element is the context level resultFacts reads, since the three
	// fields above sum input across the whole turn. Policed with them - nothing
	// outside the airlock names it.
	"usage", "contextWindow",
	"input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens",
	"output_tokens", "iterations",

	// The task list a TodoWrite carries, and the two statuses the airlock
	// resolves. "content", "status" and "completed" are already policed or
	// generic above; these are the rest of that vocabulary, and nothing above
	// the airlock names any of them - core.TodoStatus is what a renderer
	// compares against.
	"todos", "activeForm", "in_progress", "TodoWrite",

	// The system subtype that opens a session. Policed for the same reason
	// "status" beside it is: it is a subtype, and matching on one outside the
	// airlock is a view deciding what a frame is.
	"init",

	// Tool input keys. These are the ones primaryArg reads, and they are why
	// the coverage check below scans every literal rather than struct tags
	// alone: a file above the airlock that does input["command"] names
	// Claude's JSON exactly as surely as one that decodes a field, and the
	// tag-only version of this test could not see them.
	"old_string", "new_string", "file_path", "command", "pattern", "url",
	"query", "prompt",

	// Field values the airlock compares against.
	"async_launched", "completed", "error_during_execution", "user-rejected",
	"permission-rule", "task-notification", "allowed", "success", "interrupt",
	"allow", "deny", "user",

	// Tool names. Task is here even though no recorded tool_use block is
	// named Task - init.tools advertises it, so a file guessing at the
	// dispatch tool's name would guess this one, and that guess is exactly
	// what put a bare "⏺ Agent" in front of a reader.
	"Bash", "Read", "Edit", "Write", "Glob",
	"Grep", "WebFetch", "WebSearch", "Agent", "Task",

	// The two interactive tools. Policed precisely because neither is ever
	// named: askKind classifies an ask from requires_user_interaction and its
	// payload, and a tool-name allowlist is the wrong shape - any tool whose
	// permission check asks for interaction reasons can carry the flag, and
	// these two are only the two that have been recorded.
	"AskUserQuestion", "ExitPlanMode",

	// Transcript plumbing.
	"<local-command-stdout>", "</local-command-stdout>",

	// The invocation and caveat envelopes a slash command leaves on disk,
	// dropped whole by isLocalCommandPlumbing. Policed like the stdout one: the
	// tag is the only thing identifying the frame, so a view matching on it
	// would be reading Claude's wire format.
	"<command-name>", "</command-args>",
	"<local-command-caveat>", "</local-command-caveat>",

	// The cross-session envelope a peer's message arrives wrapped in. Policed
	// like the abort markers: the tag is the only thing identifying the frame,
	// and a view matching on it would be reading Claude's wire format. The open
	// carries attributes, so it is the tag start rather than a whole tag.
	"<cross-session-message", "</cross-session-message>",

	// Claude's abort markers. Policed as hard as any field name, and for a
	// sharper reason than most: they arrive on a frame with no subtype and no
	// isSynthetic, so the *string* is the only thing that identifies them, and
	// a renderer that matched on it would be the airlock's most direct leak -
	// a view deciding what a frame is by reading Claude's English.
	"[Request interrupted by user]", "[Request interrupted by user for tool use]",

	// The bare /model reply's leading phrase. Policed for the abort markers'
	// reason: it is Claude's rendered English, and the daemon reads the effort
	// level out of it, so a view matching on it would be reading Claude's prose.
	// It is a value *prefix* rather than a whole value, so it is in
	// embeddedMarkers below.
	"Current model:",
})

// deliberatelyGeneric is the other half of the vocabulary's honesty: wire
// field names left out because they are words any program uses, so listing
// them would fire on half the tree and the allowlist would become the file.
//
// It is not a comfort list. Every name here is checked against the airlock's
// own struct tags below, so leaving a name out of both sets is a build
// failure rather than a silent narrowing.
var deliberatelyGeneric = wordSet([]string{
	"type", "subtype", "role", "content", "message", "result", "id", "name",
	"input", "text", "description", "state", "request", "response",
	"session_id", "request_id", "is_error", "tool_name", "behavior",
	"cancelled", "label", "model",

	// The character that ends the cross-session envelope's opening tag, used to
	// find where the body begins. Punctuation, not a wire word.
	">",

	// An image block's source object and its bytes. Both are Claude's wire keys
	// and both are words any program uses about a payload, so policing them
	// would fire across the tree - and neither is a route in on its own, since
	// a file cannot reach an image source without first naming "image", which is
	// policed. core.ImageBlock spells "data" too, as Wake's own rpc field.
	"source", "data",

	// Both belong to set_permission_mode's receipt and both are words any
	// program uses - "mode" beside "state" and "behavior", "error" beside
	// "result". The two spellings that are unmistakably Claude's,
	// "set_permission_mode" and "permissionMode", are policed above, and a file
	// cannot reach either of these without first naming one of those.
	"mode", "error",

	// The stream_event envelope's two keys. "event" is the name of the central
	// type in this whole tree - core.Event - and "delta" is a word this
	// project's own performance notes use about a measurement, so policing
	// either would fire on Wake's own vocabulary rather than on a leak. Neither
	// is a route in on its own: a file cannot reach the text inside a partial
	// without first naming "stream_event", "content_block_delta" and
	// "text_delta", all three of which are policed above.
	"event", "delta",

	// Two of the three MCP statuses, for the reason the two permission modes
	// below are here: they are Claude's words and they are also words any
	// program uses about a socket or a queue. "needs-auth" is the one that
	// could only be Claude's, and it is policed above - so a file cannot read
	// this roster without naming the word that is actually distinctive.
	"connected", "pending",

	// Two of the three permission modes ⇧⇥ cycles. They are Claude's words and
	// they are also the plainest English in the corpus: policing "auto" and
	// "default" would fire across the tree on code that has nothing to do with
	// a permission mode, which is the decay this list exists to prevent.
	//
	// The one word two airlocks legitimately own. `cwd` is a key on the stream's
	// init frame, which wire.go reads, and a key on the *on-disk transcript*,
	// which is a different format with an airlock of its own -
	// internal/daemon/discover.go, where it is spelled keyCwd and guarded by
	// TestTheTranscriptKeysAreSpelledOnlyInDiscover.
	//
	// Policing it here would fire on that file, which is not a leak but the
	// other airlock doing its job - "label"'s reason exactly, one layer over:
	// an exemption bought where the leak is not.
	//
	// **The other guard does not cover the shape wire.go uses, and that is luck
	// rather than a ruling.** TestTheTranscriptKeysAreSpelledOnlyInDiscover
	// unquotes string literals, and a struct tag unquotes to `json:"cwd"`
	// rather than to `cwd` - so it would not have caught a production file
	// reading the transcript through a tag either. Recorded in deferred.md
	// rather than widened here: which files may name a transcript key from a
	// tag is a ruling, and buying it inside a feature is how an exemption list
	// starts.
	"cwd",

	// "plan" is the third and it stays policed above - not for being a mode,
	// but because it is ExitPlanMode's input key, which the airlock reads to
	// resolve an AskDetail. core.PermissionModePlan is how internal/ui reaches
	// it without naming it, and that is why composer.go no longer needs an
	// exemption for the word.
	"auto", "default",
})

// notNamedByTheAirlock is every policed word that appears in none of the four
// airlock files, with the reason it is policed anyway.
//
// It closes the fourth route to the exemption this file keeps having to
// defend against - "an excuse in any list is an exemption for the whole
// tree". The other three are overlaps between the classification lists and
// TestTheThreeListsDoNotOverlap covers them. This one is deletion: a word the
// airlock never names can be dropped from the vocabulary with nothing
// failing, and the tree is then free to name it. 26 of the 81 policed words
// were in that state, including "prompt" - one of the five tool-input keys
// the coverage check exists for - and "user-rejected" / "permission-rule",
// the deny-vs-interrupt discriminators CLAUDE.md's traps section rests on.
//
// Most of these are policed *ahead of* the code that will read them, which is
// the point: the sidebar work lands 64 task_* frames, and it must not be able
// to start by naming them in internal/ui.
var notNamedByTheAirlock = map[string]string{
	// The subagent lifecycle was here, excused as "not decoded yet" across
	// seven words. Five of the seven are decoded now - protocol.go's
	// taskUpdate, resolved through vocabulary.go's taskPhases, taskKinds and
	// taskStatuses - so those excuses are deleted, which is exactly the
	// deletion this list exists to force. It worked as designed: the first
	// file to read those frames had to be an airlock file, and it was.
	//
	// These two are the herald frame, and they stay policed *and* unnamed on
	// purpose. background_tasks_changed is the whole live set, which is the
	// frame a row list looks like it wants - and it is redundant: every
	// membership change it reports is reported again on the next line by a
	// frame carrying a dispatch, a status and a usage, none of which it has.
	// TestTheHeraldFrameIsRedundant holds that to the corpus and is what
	// would license decoding it. Until then, policing it is what stops
	// internal/ui building a row list out of the easiest frame to read.
	"background_tasks_changed": "the herald frame: redundant, deliberately not decoded",
	"tasks":                    "the herald frame's payload; see background_tasks_changed",

	// System subtypes the decoder passes through rather than names: Event.Text
	// carries the raw subtype, so these reach a consumer as data. A file that
	// *matched* on one would be decoding, which is what this polices.
	"hook_started":    "passed through as Event.Text, never named",
	"hook_response":   "passed through as Event.Text, never named",
	"thinking_tokens": "passed through as Event.Text, never named",

	// Advertised by init.tools and never a tool_use name. Policed precisely
	// because a file guessing the dispatch tool's name would guess this one.
	"Task": "advertised, never on the wire; the guess this test exists to catch",

	// An Agent input key and a task_started field. primaryArg maps Agent to
	// description, not prompt, so the airlock names one and not the other -
	// and prompt is the obvious second key a renderer would reach for.
	"prompt": "Agent input key the airlock does not read",

	// control_request fields the decoder deliberately does not read.
	"display_name":           "control_request field, deliberately not decoded",
	"permission_suggestions": "control_request field, deliberately not decoded",

	// Named in wire.go's prose as a trap and deliberately given no field: it
	// is not the successor id. Policed so nobody re-adds it above the airlock.
	"new_conversation_id": "deliberately not decoded; wire.go says why",

	// Agent input key whose behaviour §11 of the subagent findings lists as
	// unverified - the async trigger nobody has established.
	"run_in_background": "unverified per findings §11; nothing may key on it",

	// result-frame fields nothing decodes.
	"num_turns":          "result field, not decoded",
	"total_cost_usd":     "result field, not decoded",
	"terminal_reason":    "result field, not decoded",
	"permission_denials": "result field, not decoded",
	"non_execution_kind": "result field, not decoded",

	// Policed and never named, for the reason the two interactive tools below
	// are: toolTodos keys on the payload's shape rather than on the tool's
	// name, the way toolDiff does. A name test would be one more place
	// Claude's vocabulary decides what a renderer draws - and the dispatch
	// tool already proved that trap, arriving as Agent while init.tools
	// advertises Task. The first file to reach for the name has to delete this
	// line to do it.
	"TodoWrite": "the task list is recognised by its shape, never by the tool's name",

	// The interactive tools, neither of which the airlock names. That is the
	// whole design: an ask is classified from the wire fields it carries, so a
	// tool name has no business anywhere - and the first file to reach for one
	// has to delete an entry here to do it.
	"AskUserQuestion": "askKind reads the wire, never the tool's name",
	"ExitPlanMode":    "askKind reads the wire, never the tool's name",

	// Field *values*, not keys. The first three are the deny-vs-interrupt
	// discriminators CLAUDE.md's traps section is built on; the last marks the
	// unprompted turn an async subagent causes. Nothing reads them yet, and
	// whatever does must be behind the airlock.
	"user-rejected":          "deny/interrupt discriminator, not decoded yet",
	"permission-rule":        "deny/interrupt discriminator, not decoded yet",
	"error_during_execution": "deny/interrupt discriminator, not decoded yet",
	"task-notification":      "unprompted-turn marker, not decoded yet",
}

// policedWordCount is a tripwire, not a fact worth knowing. Any change to the
// vocabulary - a word added, a word removed - has to update it, which is what
// makes a deletion a three-place edit (the word, its excuse, this number)
// rather than something that can happen by accident in a rebase.
// 150 → 155: the cross-session envelope's two tags, <cross-session-message and
// </cross-session-message>, policed like the abort markers; "iterations", the
// per-round-trip usage breakdown resultFacts reads the context level from; and
// the compaction status frame's "compacting"/"compact_result".
// 155 → 159: the slash-command invocation and caveat envelopes' four tags,
// <command-name>/</command-args> and <local-command-caveat>/</local-command-caveat>,
// dropped whole by isLocalCommandPlumbing.
const policedWordCount = 159

// notWireVocabulary is every remaining string the airlock names: Wake's own
// error text and the formatting constants. Import paths are skipped
// structurally rather than listed.
//
// It exists so the coverage check below can be exhaustive. Every string
// literal in the four airlock files is either Claude's (policed), too generic
// to police, or here - and a new one is a build failure until somebody says
// which. That is what makes the vocabulary self-maintaining for *values* and
// not only for field names.
var notWireVocabulary = wordSet([]string{
	"", " \t\r\n", "\n\n",
	"%s: %w", "%w: %s: %w",
	"decode stream-json line: %w",
	"decode transcript line: %w",
	"encode user message",
	"%w: encode user message: nothing to send",
	// The text a decoded image block carries up in place of its bytes.
	ImagePlaceholder,
	"encode control response",
	"%w: encode control response: empty request id",
	"encode interrupt",
	"%w: encode interrupt: empty request id",
	"encode set mode",
	"%w: encode set mode: empty request id",
	"%w: encode set mode: empty mode",
	"encode rewind",
	"%w: encode rewind: empty request id",
	"%w: encode rewind: empty target or last-seen uuid",
	defaultDenyReason,

	// EncodeAnswer's refusals: Wake's own English, telling whoever gave an
	// answer why it is not going to arrive.
	ErrNotWritten.Error(),
	"%w: this ask carries no questions, so an allow is already its whole answer",
	"%w: a question in this ask is not an object",
	"%w: a question in this ask has no text, so nothing can be keyed to it",
	"%w: nothing was chosen for %q",
	"%w: the choice for %q is blank",
	"%w: %d choices for %d questions - one of them names a question this ask did not put",

	// A folded result's receipt: Wake's own English about a Read, in a format
	// a renderer fills with the body's line count. See ToolCall.Receipt for
	// why Read is the only tool that has one.
	"Read %d lines",

	// Wake's own regexp for pulling the level out of a /model reply. The
	// phrase it matches ("Current model:") is Claude's and is policed above;
	// this pattern is Wake's construction, not a wire word.
	`\(effort:\s*([a-zA-Z]+)\)`,

	// Wake's own regexp for the cross-session envelope's from-name. The tags it
	// parses are policed above; the pattern is Wake's construction.
	`from-name="([^"]*)"`,
})

func wordSet(words []string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}

// allowed is the enumerated exception list, keyed on (file, literal) rather
// than on file. Keying on file alone would mean one legitimate reuse of a
// word bought a whole file a permanent exemption - which is how the 23 leaks
// grew in two files in the first place.
//
// Every entry is Wake's own vocabulary colliding with Claude's, not a leak
// being tolerated. There are no leaks tolerated: the count is zero, and it
// should stay zero.
var allowed = map[string]map[string]bool{
	// EventKind is Wake's vocabulary and three of its values are spelled the
	// same as Claude's block types. Renaming them to avoid the collision
	// would make every kind switch in the tree read worse for no gain: these
	// are constant *values*, compared against Event.Kind, and nothing here
	// parses a frame.
	//
	// still_queued is ControlResult's own json tag - Wake's rpc format,
	// which deliberately reuses the name the receipt uses so a client reading
	// both sees one word for one thing. The type is Wake's; only the spelling
	// is Claude's.
	"internal/core/event.go": {
		"tool_use": true, "tool_result": true, "thinking": true,
		"system": true, "still_queued": true,
	},
	// RewindResult's own json tags, still_queued's case one file over: a
	// direct, uninterpreted restatement of the rewind_conversation receipt's
	// four fields, reusing the receipt's own spelling rather than inventing a
	// second one for the same concept.
	"internal/core/rewind.go": {
		"rewound": true, "targetMessageUuid": true, "prefillText": true,
		"precedingAssistantUuid": true,
	},
	// The daemon's fleet report and its frame kind - Wake's own protocol,
	// unrelated to Claude's system/status frame and never decoded from one.
	//
	// FrameAllow / FrameDeny are the same case: Wake's own client-to-daemon
	// verbs, spelled to match core.Session.AllowTool / DenyTool either side
	// of them. Nothing here reaches Claude - core.EncodeAllow builds that
	// frame from its own constants.
	//
	// Frame.Answers is that case one more time, and it is the one worth
	// stating because it is a json tag rather than a constant. The tag names
	// Wake's socket, which a second client reads; the word is Claude's too,
	// and the two maps never touch - a Frame.Answers is map[string]string,
	// core.EncodeAnswer is the only thing that folds it into an ask's input,
	// and it spells the far key from its own const. The alternative was
	// renaming Wake's field to avoid a collision with a word Claude does not
	// own, which buys nothing and costs a reader the through-line from
	// FrameAnswer to AnswerQuestion to EncodeAnswer.
	"internal/rpc/wire.go": {"status": true, "allow": true, "deny": true, "answers": true},
	// FrameInterrupt is the same case as FrameAllow / FrameDeny one line up:
	// Wake's own client-to-daemon verb, spelled to match core.Session.Interrupt
	// beneath it so one word means one thing across the three layers. Nothing
	// here reaches Claude - core.EncodeInterrupt builds that frame from its own
	// constant, and this string never leaves Wake's socket.
	"internal/rpc/lifecycle.go": {"status": true, "interrupt": true},
	// The `wake status` subcommand.
	"cmd/wake/main.go": {"status": true},
	// The legend's own English: the word a human reads beside ⎋. It is not a
	// wire subtype - core.EncodeInterrupt builds that frame from its own
	// constant - and nothing here is ever compared against anything Claude
	// sends.
	//
	// It surfaced only when the hint line became a slice of (glyph, label)
	// pairs so the guard over it could be exact. As one long format string the
	// same word was invisible to this check, which is worth knowing about the
	// check as much as about the legend: this sees whole literals, so a wire
	// word embedded in a sentence is not policed.
	//
	// It moved here from internal/ui/composer.go when the legend was split out
	// of that file - same literal, same argument, one file over.
	//
	// "plan" was here too, as Mode.String's permission-mode value, excused as
	// argv rather than a decoder. **The excuse is gone because the naming is**:
	// ⇧⇥ made the mode real, so a mode word now travels as JSON in a
	// set_permission_mode request and comes back in its receipt, which is the
	// decoder half the excuse ruled out. internal/ui holds no mode word at all
	// now - core.PermissionModePlan is how it reaches one - so the exemption was
	// deleted rather than re-argued. An exemption nobody needs is a standing
	// permission for the whole file.
	"internal/ui/legend.go": {"interrupt": true},
	// The one argument every acting MCP tool takes, and the name a *model*
	// reads in the schema. Claude's wire carries agent_id too - on a
	// permission request, naming the subagent that asked - and the two never
	// meet: this one is a key in Wake's own MCP schema, filled by a model from
	// what list_agents printed and validated as a Wake session UUID by
	// mcp.isSessionID before anything reads it. Nothing in internal/mcp
	// decodes a byte of Claude's JSON; it is an rpc client.
	//
	// Worth stating because the plan predicted this collision at the tool
	// *names* and missed it at the argument, and the fix is the same either
	// way: pay the entry rather than spell the word differently, because a
	// model choosing between agent_status and send_to_agent should see one
	// word for the thing both of them take.
	//
	// "interrupt" is the tool that stops a turn, and it is the collision the
	// plan did predict. Wake's own verb, spelled to match rpc.FrameInterrupt
	// and core.Session.Interrupt so one word means one thing from the
	// manager's tool list down to the pipe - the same case as FrameAllow /
	// FrameDeny two entries up, and as the legend's own label. Nothing here
	// reaches Claude: core.EncodeInterrupt builds that frame from its own
	// constant, and this string only ever appears in a tool definition a model
	// reads and in the name a tools/call arrives under.
	"internal/mcp/tools.go": {"agent_id": true, "interrupt": true},
	// The MCP server-configuration schema's own key, in the file the daemon
	// writes for a manager session: {"mcpServers":{"wake":{"command":…}}}.
	//
	// It is the agent_id case one file over and it is if anything further from
	// Claude's JSON than that one. Claude's wire carries `command` on a
	// command_lifecycle frame; this is a key in a *configuration file* that
	// claude's own MCP client reads at startup, whose shape is MCP's rather
	// than Wake's or Claude's - so it is not spelled at Wake's discretion at
	// all, and renaming it to dodge this check would produce a manager with no
	// tools and nothing anywhere saying why.
	//
	// internal/daemon decodes no Claude JSON: everything it sees is a
	// core.Event that already crossed the airlock.
	"internal/daemon/manager.go": {"command": true},
}

// allowlistPairCount is the tripwire over the table above, and it exists for
// the reason the table does: 23 leaks accumulated in two files because nothing
// counted them. An entry added or removed has to update this number, so
// growing the exemption list is a deliberate two-place edit rather than
// something that happens quietly in a rebase. CLAUDE.md quotes the same
// figures and has to change with it.
const allowlistPairCount = 20

func TestTheAllowlistDoesNotGrowQuietly(t *testing.T) {
	pairs := 0
	for _, words := range allowed {
		pairs += len(words)
	}
	if pairs != allowlistPairCount {
		t.Errorf("the allowlist holds %d (file, literal) pairs across %d files, allowlistPairCount says %d: every entry is a place Wake deliberately reuses one of Claude's words, so add the reason, update this number, and update CLAUDE.md's own count",
			pairs, len(allowed), allowlistPairCount)
	}
}

// TestNoClaudeWireVocabularyOutsideTheAirlock walks the tree and fails on any
// string that names Claude's JSON outside protocol.go.
func TestNoClaudeWireVocabularyOutsideTheAirlock(t *testing.T) {
	files := goFiles(t)
	if len(files) < 20 {
		t.Fatalf("walked %d non-test .go files, want the whole tree - the walk is broken and this test is asserting nothing", len(files))
	}

	checked := 0
	for _, rel := range files {
		if airlockFiles[rel] {
			continue
		}
		checked++
		for _, lit := range stringsIn(t, filepath.Join(repoRoot, rel)) {
			if !claudeWireVocabulary[lit.text] || allowed[rel][lit.text] {
				continue
			}
			t.Errorf("%s:%d names Claude's wire literal %q outside the airlock - see the header of this file",
				rel, lit.line, lit.text)
		}
	}
	if checked == 0 {
		t.Fatal("no files checked")
	}
}

// notInTheCorpus is every vocabulary word no recording carries, each with the
// reason it is in the list anyway. It is checked in both directions below, so
// a word that becomes recorded has to be deleted from here rather than
// quietly outliving its excuse.
//
// This exists because "the bytes are the authority" is the rule this project
// keeps, and a lint whose word list is half invented would be enforcing
// somebody's memory of Claude rather than Claude.
var notInTheCorpus = map[string]string{
	// Not on init.tools on the machine that recorded the corpus, and never
	// called. They were in render/tool.go's map - the leak this test closes -
	// so dropping them would leave a closed hole unguarded.
	"Glob": "not advertised by init.tools here, and never called",
	"Grep": "not advertised by init.tools here, and never called",

	// Edit's input keys. Edit is advertised 46 times and called zero, so the
	// diff path has no fixture behind it and is exercised by hand-written
	// unit tests only. That is worth knowing: ToolCall.Diff is the one part
	// of the airlock ruling the corpus cannot vouch for. new_string does
	// occur, but only inside the English of an interrupt notice ("if it was
	// a file edit, the new_string was NOT written"), which is why the check
	// below matches quoted tokens rather than substrings.
	"old_string": "Edit is advertised but never called",

	// TodoWrite and its whole-list envelope. Retired in 2.1.240 (off unless
	// CLAUDE_CODE_ENABLE_TASKS is false) and never called in the corpus, so its
	// `todos` key stays transcribed from the shipped binary rather than recorded
	// - task-checklist.jsonl exercises the *replacement*, TaskCreate/TaskUpdate,
	// not this. "activeForm" and "in_progress" used to sit here for the same
	// reason and have moved out: the recorded checklist carries both.
	"todos":      "TodoWrite is retired in 2.1.240 and never called; its list is now TaskCreate/TaskUpdate",
	"TodoWrite":  "retired in 2.1.240 and never called in the corpus",
	"deleted":    "the fourth TaskUpdate status; the recorded session never deletes an item",
	"new_string": "Edit is advertised but never called; occurs only in prose",

	// primaryArg keys for tools the corpus never exercised, alongside Edit's.
	"url":     "WebFetch is advertised but never called",
	"pattern": "Glob and Grep are neither advertised here nor called",

	// The token stream's five words moved out of this list on 2026-08-21:
	// testdata/stream/partial-turn.jsonl is the one-turn recording with
	// --include-partial-messages that docs/live-testing.md §15 asked for, so
	// the airlock's last unrecorded inbound shape is now held to bytes like
	// every other word here. docs/superpowers/notes/2026-08-21-partial-messages-
	// findings.md records what the turn confirmed, and the one status value it
	// carried that the corpus had never seen.

	// The image block, its source type and its media-type key. Wake writes all
	// three and decodes the block type back, but the only recording of any of
	// them is testdata/input/image-block.stdin.jsonl, which this check does not
	// scan - it walks the stream and transcript corpora, and neither carries an
	// image (the recorded reply is text). See 2026-08-15-image-input-findings.md.
	"image":      "recorded only in testdata/input; no stream or transcript fixture carries one",
	"base64":     "the image source type; recorded only in testdata/input",
	"media_type": "the image source's media-type key; recorded only in testdata/input",

	// Outbound. Wake writes these and never reads them, so no recording of
	// stdout can contain them - encode.go's header says the same, and says
	// why that makes them the least trustworthy shapes in the airlock.
	"updatedInput":                "outbound only; a recording of stdout cannot contain it",
	"cancel_queued":               "outbound only; a recording of stdout cannot contain it",
	"interrupt":                   "outbound only; a recording of stdout cannot contain it",
	"set_permission_mode":         "outbound only; the corpus holds its receipts, not the requests",
	"allow":                       "outbound only; the behavior value Wake writes",
	"deny":                        "outbound only; the behavior value Wake writes",
	"rewind_conversation":         "outbound only; a recording of stdout cannot contain it",
	"target_message_uuid":         "outbound only; rewind request field Wake writes",
	"last_seen_user_message_uuid": "outbound only; rewind request field Wake writes",
	"interrupt_if_running":        "outbound only; rewind request field Wake writes",
}

// embeddedMarkers never appear as a JSON key or as a whole value: the
// local-command and cross-session envelope tags are delimiters *inside* a
// string, and "Current model:" is the leading phrase of a longer reply value -
// so the quoted check cannot see them and a substring check is the honest one.
var embeddedMarkers = map[string]bool{
	"<local-command-stdout>":   true,
	"</local-command-stdout>":  true,
	"<command-name>":           true,
	"</command-args>":          true,
	"<local-command-caveat>":   true,
	"</local-command-caveat>":  true,
	"Current model:":           true,
	"<cross-session-message":   true,
	"</cross-session-message>": true,
}

// The vocabulary has to be a real description of the corpus, or the test
// above is enforcing a list somebody invented. Every word must either appear
// in testdata/stream as a quoted key or value, or be named in notInTheCorpus
// with its reason.
func TestTheVocabularyDescribesTheRecordedCorpus(t *testing.T) {
	corpus := map[string]bool{}
	// Both recorded formats. The vocabulary polices words from claude's stream
	// *and* from its on-disk transcript, which DecodeTranscriptLine reads, and
	// a word recorded in either is a fact rather than a guess.
	for _, path := range append(fixtureFiles(t), transcriptFiles(t)...) {
		for _, line := range fixtureLines(t, path) {
			for word := range claudeWireVocabulary {
				if corpus[word] {
					continue
				}
				// Quoted, so a word mentioned in an assistant's prose does
				// not count as the wire carrying it.
				if strings.Contains(line, `"`+word+`"`) ||
					(embeddedMarkers[word] && strings.Contains(line, word)) {
					corpus[word] = true
				}
			}
		}
	}
	for word := range claudeWireVocabulary {
		_, excused := notInTheCorpus[word]
		switch {
		case corpus[word] && excused:
			t.Errorf("%q is excused as unrecorded (%q) but the corpus carries it: delete the excuse", word, notInTheCorpus[word])
		case !corpus[word] && !excused:
			t.Errorf("%q is in the vocabulary but appears nowhere in the recorded corpus: it is a guess, not a fact", word)
		}
	}
	if len(corpus) == 0 {
		t.Fatal("no vocabulary word matched any fixture: the corpus check is asserting nothing")
	}
}

// The detector has to see both shapes it claims to see. Without this the
// struct-tag half is untested - the tree has no non-airlock file with a
// Claude json tag, so deleting that code changes no result anywhere.
func TestTheDetectorSeesPlainLiteralsAndJSONTagsAlike(t *testing.T) {
	src := "package p\n" +
		"const plain = \"parent_tool_use_id\"\n" +
		"type T struct{ A string `json:\"agent_id,omitempty\"` }\n"

	found := map[string]bool{}
	for _, lit := range literalsIn(t, "planted.go", []byte(src)) {
		found[lit.text] = true
	}
	for _, want := range []string{"parent_tool_use_id", "agent_id"} {
		if !found[want] {
			t.Errorf("the detector did not see %q; a file naming it would pass", want)
		}
	}
	if found["nonsense"] {
		t.Error("the detector reports strings that are not there")
	}
}

// The vocabulary cannot be proved complete - no list of what a future Claude
// might emit can be. What it can be held to is the airlock's own struct tags:
// every wire field protocol.go decodes is either a word the tree may not name
// or one deliberately judged too generic to police. A field added there and
// classified as neither is a word the rest of the tree could start naming
// with nothing to stop it, and that is exactly how the vocabulary would decay.
func TestTheVocabularyCoversEveryStringTheAirlockNames(t *testing.T) {
	tags, literals := 0, 0
	for file := range airlockFiles {
		seen := map[string]bool{}
		for _, lit := range stringsIn(t, filepath.Join(repoRoot, file)) {
			if seen[lit.text] {
				continue
			}
			seen[lit.text] = true
			// A struct tag's own text is checked through the name pulled out
			// of it, which arrives as its own entry.
			if !lit.fromTag && jsonTagName(lit.text) != "" {
				continue
			}
			if lit.fromTag {
				tags++
			} else {
				literals++
			}
			if claudeWireVocabulary[lit.text] || deliberatelyGeneric[lit.text] || notWireVocabulary[lit.text] {
				continue
			}
			t.Errorf("%s:%d names %q, which is in none of the three lists (policed, generic, not-Claude): classify it",
				file, lit.line, lit.text)
		}
	}
	// Scan integrity. The floors sit just under what the airlock carries
	// today - 49 tags and 56 literals, measured - rather than at a round
	// number well below it: the first version used 30 and 20, which left
	// room for nineteen json tags to vanish from the airlock with the check
	// still green, and a check that cannot notice its own subject
	// disappearing is the failure this file keeps finding elsewhere.
	const (
		minTags     = 45
		minLiterals = 50
	)
	if tags < minTags {
		t.Errorf("found %d json tags across the airlock, want at least %d - the scan is broken or the airlock has shrunk", tags, minTags)
	}
	if literals < minLiterals {
		t.Errorf("found %d plain literals across the airlock, want at least %d - the scan is broken or the airlock has shrunk", literals, minLiterals)
	}
}

// The three lists are a partition, not three opinions. A word in two of them
// makes the classification meaningless - it would pass whatever anyone did -
// and worse, either excuse *disarms the leak check* for that word: the whole
// tree could then name it freely, silently, with the suite green.
//
// That is not hypothetical. A mutation that added "parent_tool_use_id" to the
// not-Claude list survived every other test in this file.
func TestTheThreeListsDoNotOverlap(t *testing.T) {
	lists := map[string]map[string]bool{
		"policed":    claudeWireVocabulary,
		"generic":    deliberatelyGeneric,
		"not-Claude": notWireVocabulary,
	}
	for aName, a := range lists {
		for bName, b := range lists {
			if aName >= bName {
				continue
			}
			for word := range a {
				if b[word] {
					t.Errorf("%q is in both the %s and %s lists: an excuse in either disarms the leak check for it", word, aName, bName)
				}
			}
		}
	}
}

// The airlock is a set of files in one package, and saying so is what stops
// the set being widened into an exemption for somewhere else.
func TestTheAirlockIsFourFilesInInternalCore(t *testing.T) {
	const want = 4
	if len(airlockFiles) != want {
		t.Errorf("the airlock is %d files, want %d - if that is deliberate, CLAUDE.md's rule and protocol.go's header both name the set and must change with it", len(airlockFiles), want)
	}
	for file := range airlockFiles {
		if filepath.Dir(file) != "internal/core" {
			t.Errorf("%q is in the airlock set but not in internal/core: the airlock is one package", file)
		}
	}
}

// An excuse for a word that is not in the vocabulary is dead text.
func TestEveryExcusedWordIsInTheVocabulary(t *testing.T) {
	for _, list := range []map[string]string{notInTheCorpus, notNamedByTheAirlock} {
		for word := range list {
			if !claudeWireVocabulary[word] {
				t.Errorf("%q is excused but is not in the vocabulary: the excuse is dead text", word)
			}
		}
	}
}

// Every policed word is either named by the airlock or excused here with a
// reason. Without this, a word the airlock never names can be deleted from
// the vocabulary and nothing fails - and the tree is then free to name it.
//
// Checked both ways: an excuse for a word the airlock *does* name is stale,
// and stale excuses are how a list stops describing anything.
func TestEveryPolicedWordIsNamedByTheAirlockOrExcused(t *testing.T) {
	named := map[string]bool{}
	for file := range airlockFiles {
		for _, lit := range stringsIn(t, filepath.Join(repoRoot, file)) {
			named[lit.text] = true
		}
	}
	if len(named) < 50 {
		t.Fatalf("only %d distinct strings found across the airlock: the scan is broken and this test is asserting nothing", len(named))
	}

	for word := range claudeWireVocabulary {
		_, excused := notNamedByTheAirlock[word]
		switch {
		case named[word] && excused:
			t.Errorf("%q is excused as unnamed (%q) but the airlock names it: delete the excuse", word, notNamedByTheAirlock[word])
		case !named[word] && !excused:
			t.Errorf("%q is policed but appears nowhere in the airlock and has no reason recorded: deleting it from the vocabulary would fail nothing, and the tree could then name it freely", word)
		}
	}

	// The tripwire. Its only job is to make a change to the list above
	// impossible to perform silently.
	if len(claudeWireVocabulary) != policedWordCount {
		t.Errorf("the vocabulary holds %d words, policedWordCount says %d: update it deliberately, and say why in the commit", len(claudeWireVocabulary), policedWordCount)
	}
}

// Nothing may be allowlisted that the vocabulary does not contain: a stale
// entry is a permanent hole nobody can see, because it exempts a word that
// would otherwise start failing the moment it is added.
func TestNothingIsAllowlistedThatIsNotInTheVocabulary(t *testing.T) {
	for file, words := range allowed {
		for word := range words {
			if !claudeWireVocabulary[word] {
				t.Errorf("%s allowlists %q, which is not in the vocabulary", file, word)
			}
		}
	}
}

type literal struct {
	text string
	line int

	// fromTag marks a name pulled out of a json struct tag rather than a
	// plain string constant. The distinction matters only to the coverage
	// test below, which reads the airlock's tags as the definition of what
	// Claude's field vocabulary is.
	fromTag bool
}

// stringsIn returns every string constant in a file - plain literals and the
// json name inside a struct tag both, because a tag naming Claude's JSON is
// the most direct way a second file could start decoding it.
func stringsIn(t *testing.T, path string) []literal {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return literalsIn(t, path, src)
}

// literalsIn is the detector proper, separated from the file system so a test
// can hand it source and watch it work.
func literalsIn(t *testing.T, path string, src []byte) []literal {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	// Import paths are string literals too, and no classification of them
	// would ever mean anything - skipped structurally rather than listed.
	imports := map[*ast.BasicLit]bool{}
	for _, spec := range f.Imports {
		imports[spec.Path] = true
	}

	var out []literal
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || imports[lit] {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		line := fset.Position(lit.Pos()).Line
		out = append(out, literal{text: s, line: line})
		if name := jsonTagName(s); name != "" {
			out = append(out, literal{text: name, line: line, fromTag: true})
		}
		return true
	})
	return out
}

// jsonTagName pulls the field name out of a struct tag, or "" for a string
// that is not one.
func jsonTagName(s string) string {
	tag, ok := reflect.StructTag(s).Lookup("json")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	return name
}

// goFiles lists every non-test .go file in the repository, as paths relative
// to the root. Test files are excluded because the rule is about non-test
// files: the airlock's own tests have to name the wire to prove it decodes.
func goFiles(t *testing.T) []string {
	t.Helper()
	skipDir := map[string]bool{
		".git": true, ".worktrees": true, "testdata": true,
		"docs": true, "notes": true, "bin": true, "dist": true,
		".superpowers": true,
	}
	// A NESTED CHECKOUT IS NOT THIS TREE, wherever somebody put it. The list
	// above names `.worktrees` because that is where this project's own
	// convention puts them - and a parallel lane created one under
	// `.claude/worktrees/`, which walked straight past every entry here and
	// reported thirty airlock violations in a *copy* of the airlock. The
	// second time a hand-written skip list has been wrong about a location
	// nobody had thought of; the first silently skipped `docs/notes/` after
	// the working notes moved there.
	//
	// So the rule is derived rather than listed: a directory holding a `.git`
	// entry is a checkout of its own and belongs to whoever made it. That is
	// what `git worktree add` writes, so it catches every future one without
	// anybody having to remember this.
	//
	// A vendored dependency is the same case arriving a different way: it is
	// somebody else's code sitting in this tree, and it answers to their
	// conventions rather than to ours. `go list ./...` already excludes it,
	// because a directory with its own `go.mod` is its own module - so the
	// walk that stands in for the toolchain should exclude it too, or the
	// 800-line rule and the two leak checks start reporting on source nobody
	// here wrote. Derived from the same kind of on-disk marker as above, so a
	// second vendored module needs no edit.
	notThisTree := func(dir string) bool {
		if dir == repoRoot {
			return false
		}
		for _, marker := range []string{".git", "go.mod"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return true
			}
		}
		return false
	}

	var files []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if skipDir[d.Name()] || notThisTree(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", repoRoot, err)
	}
	return files
}
