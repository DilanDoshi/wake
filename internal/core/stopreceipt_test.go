package core

// A workflow stop's receipt is as bare as a mode change's, so only the request
// id the session minted can say what it answers - answeredMCP's mechanism,
// shared rather than written twice.

import "testing"

func TestAStopsReceiptIsLabelledAStopReceipt(t *testing.T) {
	s, buf := mcpSession(t)
	id, err := s.StopTask("wbu5972hq")
	if err != nil {
		t.Fatal(err)
	}
	if sent, req := sentRequest(t, buf); sent != id || req["subtype"] != "stop_task" {
		t.Fatalf("StopTask wrote %q %v, want its own request id %q", sent, req, id)
	}
	refusal := `{"type":"control_response","response":{"subtype":"error","request_id":"` + id + `","error":"No task found"}}`
	ev := s.attribute(onlyEvent(t, refusal, 0))
	if ev.Kind != KindStopReceipt || ev.Control == nil || ev.Control.Error != "No task found" {
		t.Fatalf("the stop's refusal decoded as %q control %+v, want a stop receipt carrying the error", ev.Kind, ev.Control)
	}
	if again := s.attribute(onlyEvent(t, refusal, 0)); again.Kind != KindControlReceipt {
		t.Errorf("a second receipt for the same id was labelled %q; a stop is answered once", again.Kind)
	}
}

func TestAnUnwrittenStopIsNotRemembered(t *testing.T) {
	s := NewSession(Config{SessionID: "s1"})
	if _, err := s.StopTask("wbu5972hq"); err == nil {
		t.Fatal("a session that never started accepted a write")
	}
	if n := s.pendingAsks(); n != 0 {
		t.Errorf("%d asks remembered after a failed write", n)
	}
}
