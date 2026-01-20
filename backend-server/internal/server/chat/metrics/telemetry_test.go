package metrics

import "testing"

func TestConversationMetricsSnapshotAddsSentenceSeparator(t *testing.T) {
	metrics := &ConversationMetrics{}

	metrics.AddOutput("第一句")
	metrics.AddOutput("第二句")

	snapshot := metrics.Snapshot()

	want := "第一句" + outputSentenceSeparator + "第二句"
	if snapshot.OutputText != want {
		t.Fatalf("expected outputText to be %q, got %q", want, snapshot.OutputText)
	}
}
