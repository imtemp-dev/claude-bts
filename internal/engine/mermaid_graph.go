package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Mermaid graph analysis — deterministic path enumeration for /bts-verify.
//
// The verifier LLM used to enumerate diagram paths itself, which is
// error-prone on large diagrams (miscounted or skipped paths). This
// module extracts mermaid blocks from a document, parses the two
// diagram families bts documents actually use (flowchart/graph and
// stateDiagram/stateDiagram-v2), and enumerates:
//   - all simple paths from start nodes to terminal nodes (capped)
//   - cycles (capped)
//   - dead-end states (stateDiagram: no outgoing transition at all)
//   - orphan states (stateDiagram: no incoming transition)
//
// Lines that look like edges but fail to parse are counted and surfaced
// so the verifier can fall back to manual enumeration for that diagram
// instead of silently trusting an incomplete graph.

const (
	// MaxEnumeratedPaths bounds DFS path enumeration per diagram.
	MaxEnumeratedPaths = 100
	// MaxReportedCycles bounds distinct cycles reported per diagram.
	MaxReportedCycles = 20
)

// MermaidBlock is one fenced ```mermaid block extracted from a document.
type MermaidBlock struct {
	Kind      string // "flowchart", "stateDiagram", or the raw keyword if unsupported
	StartLine int    // 1-based line number of the ```mermaid fence
	Body      []string
}

type mermaidEdge struct {
	From, To string
}

// MermaidGraphAnalysis is the analysis result for one diagram.
type MermaidGraphAnalysis struct {
	Kind            string
	StartLine       int
	NodeCount       int
	EdgeCount       int
	Paths           [][]string
	PathsTruncated  bool
	Cycles          [][]string
	CyclesTruncated bool
	DeadEnds        []string // stateDiagram only
	Orphans         []string // stateDiagram only
	MultiEntry      []string // flowchart: >1 zero-in-degree root (informational)
	UnparsedEdges   int      // arrow-looking lines that failed to parse
	Notes           []string // parse caveats (composites flattened, unsupported type, ...)
}

// startNode / endNode are pseudo-nodes for stateDiagram [*] markers.
const (
	startNode = "[*start*]"
	endNode   = "[*end*]"
)

// ExtractMermaidBlocks scans document content for ```mermaid fences.
func ExtractMermaidBlocks(content string) []MermaidBlock {
	var blocks []MermaidBlock
	lines := strings.Split(content, "\n")
	inBlock := false
	var cur MermaidBlock
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if strings.HasPrefix(trimmed, "```mermaid") {
				inBlock = true
				cur = MermaidBlock{StartLine: i + 1}
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			cur.Kind = detectMermaidKind(cur.Body)
			blocks = append(blocks, cur)
			inBlock = false
			continue
		}
		cur.Body = append(cur.Body, line)
	}
	return blocks
}

func detectMermaidKind(body []string) string {
	for _, line := range body {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		keyword := strings.Fields(t)[0]
		switch {
		case keyword == "flowchart" || keyword == "graph":
			return "flowchart"
		case strings.HasPrefix(keyword, "stateDiagram"):
			return "stateDiagram"
		default:
			return keyword
		}
	}
	return ""
}

var (
	// stateDiagram edge: A --> B : label
	stateEdgeRe = regexp.MustCompile(`^(\[\*\]|[A-Za-z_][\w.]*)\s*-->\s*(\[\*\]|[A-Za-z_][\w.]*)\s*(?::.*)?$`)
	// state "description" as id
	stateAliasRe = regexp.MustCompile(`^state\s+"[^"]*"\s+as\s+([A-Za-z_][\w.]*)`)
	// state id { — composite state open
	stateCompositeRe = regexp.MustCompile(`^state\s+([A-Za-z_][\w.]*)\s*\{`)
	// flowchart arrow tokens: -->, --->, ---, -.->, ==>, --x, --o, with optional |label|
	flowArrowRe = regexp.MustCompile(`\s*(?:-{2,3}>?|-\.+->|={2,}>|--[xo])(?:\s*\|[^|]*\|)?\s*`)
	// flowchart node token: id optionally followed by a bracketed label
	flowNodeRe = regexp.MustCompile(`^\s*([A-Za-z_][\w.-]*)`)
	// arrow-ish detector for unparsed-edge accounting
	arrowishRe = regexp.MustCompile(`-->|---|-\.->|==>|--x|--o`)
)

// skip prefixes for non-edge mermaid statements
var mermaidSkipPrefixes = []string{
	"direction ", "classDef ", "class ", "style ", "linkStyle ", "click ",
	"accTitle", "accDescr", "title ",
}

// parseMermaidGraph builds an edge list from a supported block.
// Returns edges, node set, unparsed-edge count, and notes.
func parseMermaidGraph(b MermaidBlock) ([]mermaidEdge, map[string]bool, int, []string) {
	var edges []mermaidEdge
	nodes := make(map[string]bool)
	unparsed := 0
	var notes []string
	compositeSeen := false
	inNoteBlock := false

	addEdge := func(from, to string) {
		edges = append(edges, mermaidEdge{From: from, To: to})
		nodes[from] = true
		nodes[to] = true
	}

	for _, raw := range b.Body {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "%%") {
			continue
		}
		// note blocks (stateDiagram): "note right of X ... end" / single-line with ":"
		if inNoteBlock {
			if line == "end" {
				inNoteBlock = false
			}
			continue
		}
		if strings.HasPrefix(line, "note ") {
			if !strings.Contains(line, ":") {
				inNoteBlock = true
			}
			continue
		}
		first := strings.Fields(line)[0]
		if first == "flowchart" || first == "graph" || strings.HasPrefix(first, "stateDiagram") {
			continue
		}
		if line == "}" || line == "end" || strings.HasPrefix(line, "subgraph") {
			continue
		}
		skip := false
		for _, p := range mermaidSkipPrefixes {
			if strings.HasPrefix(line, p) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		switch b.Kind {
		case "stateDiagram":
			if m := stateCompositeRe.FindStringSubmatch(line); m != nil {
				nodes[m[1]] = true
				compositeSeen = true
				continue
			}
			if m := stateAliasRe.FindStringSubmatch(line); m != nil {
				nodes[m[1]] = true
				continue
			}
			if m := stateEdgeRe.FindStringSubmatch(line); m != nil {
				from, to := m[1], m[2]
				if from == "[*]" {
					from = startNode
				}
				if to == "[*]" {
					to = endNode
				}
				addEdge(from, to)
				continue
			}
			if arrowishRe.MatchString(line) {
				unparsed++
			}
		case "flowchart":
			if parseFlowchartLine(line, addEdge) {
				continue
			}
			if arrowishRe.MatchString(line) {
				unparsed++
			}
		}
	}

	if compositeSeen {
		notes = append(notes, "composite states flattened — [*] inside composites treated as the global start/end")
	}
	return edges, nodes, unparsed, notes
}

// parseFlowchartLine handles chained edges (A --> B --> C) and &-lists
// (A & B --> C). Returns true if at least one edge was extracted.
func parseFlowchartLine(line string, addEdge func(from, to string)) bool {
	if !arrowishRe.MatchString(line) {
		// Standalone node definition like `A[Label]` — register nothing;
		// nodes are collected from edges, and isolated nodes don't affect paths.
		return true
	}
	segments := flowArrowRe.Split(line, -1)
	if len(segments) < 2 {
		return false
	}
	var groups [][]string
	for _, seg := range segments {
		var ids []string
		for _, part := range strings.Split(seg, "&") {
			if m := flowNodeRe.FindStringSubmatch(part); m != nil {
				ids = append(ids, m[1])
			}
		}
		if len(ids) == 0 {
			return false
		}
		groups = append(groups, ids)
	}
	for i := 0; i+1 < len(groups); i++ {
		for _, from := range groups[i] {
			for _, to := range groups[i+1] {
				addEdge(from, to)
			}
		}
	}
	return true
}

// AnalyzeMermaidBlock parses and analyzes one extracted block.
func AnalyzeMermaidBlock(b MermaidBlock) MermaidGraphAnalysis {
	a := MermaidGraphAnalysis{Kind: b.Kind, StartLine: b.StartLine}
	if b.Kind != "flowchart" && b.Kind != "stateDiagram" {
		a.Notes = append(a.Notes, fmt.Sprintf("unsupported diagram type %q — not analyzed", b.Kind))
		return a
	}

	edges, nodes, unparsed, notes := parseMermaidGraph(b)
	a.UnparsedEdges = unparsed
	a.Notes = append(a.Notes, notes...)
	a.EdgeCount = len(edges)

	adj := make(map[string][]string)
	inDeg := make(map[string]int)
	for _, e := range edges {
		adj[e.From] = append(adj[e.From], e.To)
		inDeg[e.To]++
	}

	// Real nodes exclude the [*] pseudo-nodes.
	var realNodes []string
	for n := range nodes {
		if n != startNode && n != endNode {
			realNodes = append(realNodes, n)
		}
	}
	sort.Strings(realNodes)
	a.NodeCount = len(realNodes)

	starts, terminals := findBoundaries(b.Kind, adj, inDeg, realNodes, &a)
	if len(starts) == 0 {
		a.Notes = append(a.Notes, "no start node identified — path enumeration skipped")
	} else {
		a.Paths, a.PathsTruncated = enumerateSimplePaths(adj, starts, terminals)
	}
	a.Cycles, a.CyclesTruncated = findCycles(adj, realNodes)

	if b.Kind == "stateDiagram" {
		for _, n := range realNodes {
			if len(adj[n]) == 0 {
				a.DeadEnds = append(a.DeadEnds, n)
			}
			if inDeg[n] == 0 {
				a.Orphans = append(a.Orphans, n)
			}
		}
	}
	return a
}

// findBoundaries determines start and terminal nodes per diagram kind.
func findBoundaries(kind string, adj map[string][]string, inDeg map[string]int, realNodes []string, a *MermaidGraphAnalysis) ([]string, map[string]bool) {
	terminals := make(map[string]bool)
	var starts []string
	if kind == "stateDiagram" {
		if len(adj[startNode]) > 0 {
			starts = []string{startNode}
		} else {
			// No [*] entry — fall back to zero-in-degree states.
			for _, n := range realNodes {
				if inDeg[n] == 0 {
					starts = append(starts, n)
				}
			}
			a.Notes = append(a.Notes, "no [*] entry transition — using zero-in-degree states as starts")
		}
		terminals[endNode] = true
		// States with no outgoing edges also end paths (reported as dead-ends).
		for _, n := range realNodes {
			if len(adj[n]) == 0 {
				terminals[n] = true
			}
		}
	} else {
		for _, n := range realNodes {
			if inDeg[n] == 0 {
				starts = append(starts, n)
			}
			if len(adj[n]) == 0 {
				terminals[n] = true
			}
		}
		if len(starts) > 1 {
			a.MultiEntry = append(a.MultiEntry, starts...)
		}
	}
	return starts, terminals
}

// enumerateSimplePaths runs DFS from each start, collecting simple paths
// (no node revisited within a path) that end at a terminal node.
func enumerateSimplePaths(adj map[string][]string, starts []string, terminals map[string]bool) ([][]string, bool) {
	var paths [][]string
	truncated := false
	onPath := make(map[string]bool)
	var path []string

	var dfs func(n string)
	dfs = func(n string) {
		if truncated {
			return
		}
		path = append(path, n)
		onPath[n] = true
		if terminals[n] && len(path) > 1 {
			if len(paths) >= MaxEnumeratedPaths {
				truncated = true
			} else {
				cp := make([]string, len(path))
				copy(cp, path)
				paths = append(paths, cp)
			}
		} else {
			for _, next := range adj[n] {
				if onPath[next] {
					continue
				}
				dfs(next)
			}
		}
		onPath[n] = false
		path = path[:len(path)-1]
	}
	for _, s := range starts {
		dfs(s)
	}
	return paths, truncated
}

// findCycles reports distinct cycles found via DFS back-edges.
func findCycles(adj map[string][]string, realNodes []string) ([][]string, bool) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int)
	var stack []string
	var cycles [][]string
	seen := make(map[string]bool)
	truncated := false

	var dfs func(n string)
	dfs = func(n string) {
		if truncated {
			return
		}
		color[n] = gray
		stack = append(stack, n)
		for _, next := range adj[n] {
			if color[next] == gray {
				// Back-edge: extract the cycle from the stack.
				var cycle []string
				for i := len(stack) - 1; i >= 0; i-- {
					cycle = append([]string{stack[i]}, cycle...)
					if stack[i] == next {
						break
					}
				}
				key := cycleKey(cycle)
				if !seen[key] {
					seen[key] = true
					if len(cycles) >= MaxReportedCycles {
						truncated = true
					} else {
						cycles = append(cycles, cycle)
					}
				}
			} else if color[next] == white {
				dfs(next)
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}

	// Deterministic order: sorted real nodes, then pseudo start.
	if len(adj[startNode]) > 0 {
		dfs(startNode)
	}
	for _, n := range realNodes {
		if color[n] == white {
			dfs(n)
		}
	}
	return cycles, truncated
}

// cycleKey normalizes a cycle to a rotation-independent signature.
func cycleKey(cycle []string) string {
	sorted := make([]string, len(cycle))
	copy(sorted, cycle)
	sort.Strings(sorted)
	return strings.Join(sorted, "→")
}

// displayName renders pseudo-nodes back to mermaid notation.
func displayName(n string) string {
	if n == startNode || n == endNode {
		return "[*]"
	}
	return n
}

// RenderMermaidAnalysisReport renders analyses as a prompt-friendly
// deterministic report. The verifier consumes this instead of
// enumerating paths itself; paths_total feeds the <bts-findings> block.
func RenderMermaidAnalysisReport(analyses []MermaidGraphAnalysis) string {
	var b strings.Builder
	b.WriteString("## Mermaid Graph Analysis (deterministic — computed by bts, not the LLM)\n")

	supported := 0
	totalPaths := 0
	for _, a := range analyses {
		if a.Kind == "flowchart" || a.Kind == "stateDiagram" {
			supported++
			totalPaths += len(a.Paths)
		}
	}
	if supported == 0 {
		b.WriteString("\nNo flowchart/stateDiagram mermaid blocks found. paths_total: 0\n")
		return b.String()
	}
	fmt.Fprintf(&b, "\npaths_total: %d (across %d diagram(s))\n", totalPaths, supported)

	diagramNo := 0
	for _, a := range analyses {
		if a.Kind != "flowchart" && a.Kind != "stateDiagram" {
			continue
		}
		diagramNo++
		fmt.Fprintf(&b, "\n### Diagram %d — %s (document line %d): %d nodes, %d edges\n",
			diagramNo, a.Kind, a.StartLine, a.NodeCount, a.EdgeCount)

		if a.UnparsedEdges > 0 {
			fmt.Fprintf(&b, "⚠ %d edge-like line(s) could not be parsed — this enumeration may be "+
				"INCOMPLETE. Enumerate this diagram manually as a fallback.\n", a.UnparsedEdges)
		}
		for _, n := range a.Notes {
			fmt.Fprintf(&b, "Note: %s\n", n)
		}

		if len(a.Paths) > 0 {
			fmt.Fprintf(&b, "Paths (%d):\n", len(a.Paths))
			for i, p := range a.Paths {
				names := make([]string, len(p))
				for j, n := range p {
					names[j] = displayName(n)
				}
				fmt.Fprintf(&b, "  P%d: %s\n", i+1, strings.Join(names, " → "))
			}
			if a.PathsTruncated {
				fmt.Fprintf(&b, "  ⚠ TRUNCATED at %d paths — diagram has more; treat enumeration as partial.\n", MaxEnumeratedPaths)
			}
		} else {
			b.WriteString("Paths: none enumerated\n")
		}

		if len(a.Cycles) > 0 {
			b.WriteString("Cycles (each needs an exit condition specified):\n")
			for _, c := range a.Cycles {
				names := make([]string, 0, len(c)+1)
				for _, n := range c {
					names = append(names, displayName(n))
				}
				names = append(names, displayName(c[0]))
				fmt.Fprintf(&b, "  %s\n", strings.Join(names, " → "))
			}
			if a.CyclesTruncated {
				fmt.Fprintf(&b, "  ⚠ TRUNCATED at %d cycles.\n", MaxReportedCycles)
			}
		}
		if len(a.DeadEnds) > 0 {
			fmt.Fprintf(&b, "Dead-end states (no exit transition): %s\n", strings.Join(a.DeadEnds, ", "))
		}
		if len(a.Orphans) > 0 {
			fmt.Fprintf(&b, "Orphan states (no entry transition): %s\n", strings.Join(a.Orphans, ", "))
		}
		if len(a.MultiEntry) > 0 {
			fmt.Fprintf(&b, "Multiple entry nodes (verify intentional): %s\n", strings.Join(a.MultiEntry, ", "))
		}
	}
	return b.String()
}

// AnalyzeMermaidDocument is the CLI entry: extract, analyze, render.
func AnalyzeMermaidDocument(content string) string {
	blocks := ExtractMermaidBlocks(content)
	analyses := make([]MermaidGraphAnalysis, 0, len(blocks))
	for _, blk := range blocks {
		analyses = append(analyses, AnalyzeMermaidBlock(blk))
	}
	return RenderMermaidAnalysisReport(analyses)
}
