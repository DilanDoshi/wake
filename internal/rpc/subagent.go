package rpc

// A plain subagent's own transcript, asked for and answered the way a workflow
// agent's is (workflow.go). A subagent's forwarded frames reach only the
// clients attached while it spoke, so a pane opened on one later reads what it
// said off claude's own file instead. Both kinds carry the agent's id on
// WorkflowFrame.Agent and the transcript on Frame.Events.

const (
	FrameSubagent      = "subagent_transcript"       // client → daemon: one subagent's transcript (WorkflowFrame.Agent)
	FrameSubagentReply = "subagent_transcript_reply" // daemon → client: Frame.Events, WorkflowFrame.Agent echoed
)
