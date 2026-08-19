package tmux

import (
	"fmt"
	"os/exec"
	"testing"
)

// Preview cost at scale is the risk this milestone exists to bound (SPEC §14,
// risk 1): dozens of nodes multiplied by an uncapped capture rate. These measure
// the two numbers that decide whether the bound holds — what one capture costs,
// and what a whole viewport of them costs in the one batch a tick allows.

// benchSessions puts n live sessions on a private server and gives back their
// names, so nothing here can touch the sessions the user is working in.
func benchSessions(b *testing.B, n int) (CLI, []string) {
	b.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		b.Skip("tmux is not installed")
	}
	c := CLI{Socket: testSocket(b.Name())}
	b.Cleanup(func() { _ = exec.Command("tmux", "-L", c.Socket, "kill-server").Run() })

	names := make([]string, 0, n)
	for i := range n {
		name := SessionName("bench", fmt.Sprintf("n%03d", i))
		if err := c.Create(name, b.TempDir(), nil); err != nil {
			b.Fatalf("Create %s: %v", name, err)
		}
		// Something to capture: an empty pane is not the case that costs.
		if err := c.run("send-keys", "-t", "="+name+":", "printf 'the quick brown fox\\n%.0s' {1..200}", "Enter"); err != nil {
			b.Fatalf("send-keys: %v", err)
		}
		names = append(names, name)
	}
	return c, names
}

func BenchmarkCaptureOneCard(b *testing.B) {
	c, names := benchSessions(b, 1)
	b.ResetTimer()
	for b.Loop() {
		if _, err := c.Capture(names[0], 4); err != nil {
			b.Fatalf("Capture: %v", err)
		}
	}
}

// BenchmarkCaptureAViewportOfCards is one tick's worth of work: every visible
// card on a map of several dozen nodes, captured in the single batch the
// debounce coalesces into.
func BenchmarkCaptureAViewportOfCards(b *testing.B) {
	const nodes = 40
	c, names := benchSessions(b, nodes)
	b.ResetTimer()
	for b.Loop() {
		for _, name := range names {
			if _, err := c.Capture(name, 4); err != nil {
				b.Fatalf("Capture: %v", err)
			}
		}
	}
}

// BenchmarkCaptureALongPreview is the same batch at the largest card size, which
// is where a preview costs the most: ten lines rather than four.
func BenchmarkCaptureALongPreview(b *testing.B) {
	const nodes = 40
	c, names := benchSessions(b, nodes)
	b.ResetTimer()
	for b.Loop() {
		for _, name := range names {
			if _, err := c.Capture(name, 10); err != nil {
				b.Fatalf("Capture: %v", err)
			}
		}
	}
}
