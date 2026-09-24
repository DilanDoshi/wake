package ui

// fold's room-admission decision for a workflow's own ending, split out of
// fleet.go to keep that file under the hard max. chat_blocks.go's KindSystem
// case draws whatever this admits.
//
// The wire frame that ends a dispatch carries no marker of its own kind -
// task_type arrives only on task_started - so this reads Kind off the row's
// own enrichment (Fleet.Observe calls Fleet.named before fold ever sees the
// event) rather than off the raw frame, which is TaskKindUnknown on every
// recorded ending regardless of what started it.

import "github.com/DilanDoshi/wake/internal/core"

// workflowRoomEvent is fold's KindSystem answer: the event itself for a
// workflow's ending naming its dispatch - task_notification, taskLine's own
// discriminator, so the room and the transcript can never disagree about
// which frame speaks - and nil for every other system frame, an ordinary
// subagent's or a shell's ending included, both of which stay
// conversation-only.
func workflowRoomEvent(ev core.Event) []core.Event {
	u := ev.Task
	if u == nil || u.Kind != core.TaskWorkflow || u.Phase != core.TaskEnded || u.Dispatch == "" {
		return nil
	}
	return []core.Event{ev}
}
