package notifier

import "testing"

// BenchmarkRenderTitle measures the title-template path. text/template
// is mildly allocation-heavy (one bytes.Buffer + one slice grow per
// call); this is the per-event cost when a channel falls back to
// the default title.
func BenchmarkRenderTitle(b *testing.B) {
	e := Event{Type: EventRunFailed, PipelineName: "checkout-api"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderTitle(e)
	}
}

// BenchmarkRenderBody includes the conditional URL / Error blocks
// to exercise the template engine's range/with path.
func BenchmarkRenderBody(b *testing.B) {
	e := Event{
		Type:         EventRunFailed,
		PipelineName: "checkout-api",
		RunID:        "run_01HZX9YQ",
		Error:        "context deadline exceeded",
		URL:          "https://cooker.example.com/runs/run_01HZX9YQ",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderBody(e)
	}
}
