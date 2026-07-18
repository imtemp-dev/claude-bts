package engine

import (
	"fmt"
	"strings"
	"testing"
)

func mdWithMermaid(kind, body string) string {
	return "# Doc\n\nText.\n\n```mermaid\n" + kind + "\n" + body + "\n```\n\nMore text.\n"
}

func analyzeFirst(t *testing.T, content string) MermaidGraphAnalysis {
	t.Helper()
	blocks := ExtractMermaidBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	return AnalyzeMermaidBlock(blocks[0])
}

func pathStrings(a MermaidGraphAnalysis) []string {
	var out []string
	for _, p := range a.Paths {
		names := make([]string, len(p))
		for i, n := range p {
			names[i] = displayName(n)
		}
		out = append(out, strings.Join(names, "→"))
	}
	return out
}

func TestExtractMermaidBlocks_KindAndLine(t *testing.T) {
	content := "line1\n```mermaid\nstateDiagram-v2\nA --> B\n```\n\n```mermaid\nflowchart TD\nX --> Y\n```\n"
	blocks := ExtractMermaidBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Kind != "stateDiagram" || blocks[0].StartLine != 2 {
		t.Errorf("block 0: got kind=%s line=%d", blocks[0].Kind, blocks[0].StartLine)
	}
	if blocks[1].Kind != "flowchart" {
		t.Errorf("block 1: got kind=%s", blocks[1].Kind)
	}
}

func TestStateDiagram_SimplePaths(t *testing.T) {
	a := analyzeFirst(t, mdWithMermaid("stateDiagram-v2", `
[*] --> Idle
Idle --> Loading : fetch
Loading --> Ready : ok
Loading --> Error : fail
Ready --> [*]
Error --> [*]`))
	paths := pathStrings(a)
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}
	want := map[string]bool{
		"[*]→Idle→Loading→Ready→[*]": true,
		"[*]→Idle→Loading→Error→[*]": true,
	}
	for _, p := range paths {
		if !want[p] {
			t.Errorf("unexpected path %q", p)
		}
	}
	if a.NodeCount != 4 || a.EdgeCount != 6 {
		t.Errorf("got nodes=%d edges=%d", a.NodeCount, a.EdgeCount)
	}
}

func TestStateDiagram_DeadEndAndOrphan(t *testing.T) {
	a := analyzeFirst(t, mdWithMermaid("stateDiagram-v2", `
[*] --> A
A --> Stuck
Ghost --> A
A --> [*]`))
	if len(a.DeadEnds) != 1 || a.DeadEnds[0] != "Stuck" {
		t.Errorf("dead-ends: %v", a.DeadEnds)
	}
	if len(a.Orphans) != 1 || a.Orphans[0] != "Ghost" {
		t.Errorf("orphans: %v", a.Orphans)
	}
}

func TestStateDiagram_CycleDetected(t *testing.T) {
	a := analyzeFirst(t, mdWithMermaid("stateDiagram-v2", `
[*] --> Loading
Loading --> Retry : fail
Retry --> Loading : again
Loading --> Done : ok
Done --> [*]`))
	if len(a.Cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d: %v", len(a.Cycles), a.Cycles)
	}
	// Cycle nodes should be Loading and Retry in some rotation.
	key := cycleKey(a.Cycles[0])
	if key != "Loading→Retry" {
		t.Errorf("cycle key: %s", key)
	}
	// Paths must still terminate despite the cycle.
	if len(a.Paths) == 0 {
		t.Error("expected paths despite cycle")
	}
}

func TestStateDiagram_LabelsAliasCompositeNotes(t *testing.T) {
	a := analyzeFirst(t, mdWithMermaid("stateDiagram-v2", `
state "Waiting for input" as Waiting
[*] --> Waiting
state Active {
  Waiting --> Running : start
}
note right of Running : hot loop
Running --> [*]`))
	if len(a.Notes) == 0 || !strings.Contains(a.Notes[0], "composite") {
		t.Errorf("expected composite note, got %v", a.Notes)
	}
	paths := pathStrings(a)
	if len(paths) != 1 || paths[0] != "[*]→Waiting→Running→[*]" {
		t.Errorf("paths: %v", paths)
	}
}

func TestFlowchart_ChainAndAmpersand(t *testing.T) {
	a := analyzeFirst(t, mdWithMermaid("flowchart TD", `
A[Start] --> B{Check} --> C(Done)
B -->|no| D[Alt]
E & F --> G`))
	paths := pathStrings(a)
	// Roots: A, E, F. Terminals: C, D, G.
	want := map[string]bool{"A→B→C": true, "A→B→D": true, "E→G": true, "F→G": true}
	if len(paths) != len(want) {
		t.Fatalf("expected %d paths, got %d: %v", len(want), len(paths), paths)
	}
	for _, p := range paths {
		if !want[p] {
			t.Errorf("unexpected path %q", p)
		}
	}
	if len(a.MultiEntry) != 3 {
		t.Errorf("multi-entry: %v", a.MultiEntry)
	}
}

func TestFlowchart_DottedThickAndCrossArrows(t *testing.T) {
	a := analyzeFirst(t, mdWithMermaid("flowchart LR", `
A -.-> B
B ==> C
C --x D
D --o E`))
	if a.EdgeCount != 4 {
		t.Errorf("edges: %d (unparsed=%d)", a.EdgeCount, a.UnparsedEdges)
	}
	if a.UnparsedEdges != 0 {
		t.Errorf("unparsed: %d", a.UnparsedEdges)
	}
}

func TestUnparsedEdgeLinesAreCounted(t *testing.T) {
	// stateDiagram parser does not understand flowchart-style labels —
	// the arrow-ish line must be counted, not silently dropped.
	a := analyzeFirst(t, mdWithMermaid("stateDiagram-v2", `
[*] --> A
A -->|weird| B`))
	if a.UnparsedEdges != 1 {
		t.Errorf("expected 1 unparsed edge, got %d", a.UnparsedEdges)
	}
}

func TestPathTruncationCap(t *testing.T) {
	// Layered diamond graph: 2^8 = 256 paths > MaxEnumeratedPaths.
	var b strings.Builder
	b.WriteString("[*] --> L0\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&b, "L%d --> A%d\nL%d --> B%d\nA%d --> L%d\nB%d --> L%d\n", i, i, i, i, i, i+1, i, i+1)
	}
	b.WriteString("L8 --> [*]\n")
	a := analyzeFirst(t, mdWithMermaid("stateDiagram-v2", b.String()))
	if !a.PathsTruncated {
		t.Fatal("expected truncation")
	}
	if len(a.Paths) != MaxEnumeratedPaths {
		t.Errorf("expected %d paths, got %d", MaxEnumeratedPaths, len(a.Paths))
	}
}

func TestUnsupportedKindNoted(t *testing.T) {
	a := analyzeFirst(t, mdWithMermaid("sequenceDiagram", "A->>B: hi"))
	if len(a.Notes) == 0 || !strings.Contains(a.Notes[0], "unsupported") {
		t.Errorf("notes: %v", a.Notes)
	}
}

func TestRenderReport_TotalsAndWarnings(t *testing.T) {
	content := mdWithMermaid("stateDiagram-v2", `
[*] --> A
A --> [*]`)
	report := AnalyzeMermaidDocument(content)
	if !strings.Contains(report, "paths_total: 1") {
		t.Errorf("report missing paths_total: %s", report)
	}
	if !strings.Contains(report, "P1: [*] → A → [*]") {
		t.Errorf("report missing path line: %s", report)
	}
}

func TestRenderReport_NoMermaid(t *testing.T) {
	report := AnalyzeMermaidDocument("# Plain doc\nNo diagrams here.\n")
	if !strings.Contains(report, "paths_total: 0") {
		t.Errorf("report: %s", report)
	}
}
