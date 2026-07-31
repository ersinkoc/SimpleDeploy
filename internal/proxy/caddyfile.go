package proxy

import (
	"sort"
	"strings"
)

// caddyBlock represents one top-level block in a Caddyfile: either the global
// options block (the one starting with a bare `{`) or a site block (starting
// with an address like `app.example.com {`).
type caddyBlock struct {
	// address is the site address for site blocks, or "" for the global block.
	address string
	// isGlobal is true for the bare `{ ... }` options block.
	isGlobal bool
	// body is the block's content lines (everything between the opening
	// `address {` and the closing `}`).
	body []string
}

// parseCaddyfile splits a Caddyfile into top-level blocks. Unlike
// filterCaddyDomain's per-line brace counting, this tracks nesting depth
// across the full file so braces inside header values (which are quoted in
// the emitted output) do not corrupt block boundaries.
//
// The parser is intentionally minimal: it understands only the structure
// SimpleDeploy generates (global block + flat site blocks with reverse_proxy
// and header directives). It does not handle Caddyfile comments, nested
// blocks, or snippets — those are outside what the generator emits, and the
// "generated file — do not edit" contract makes hand-editing unsupported.
func parseCaddyfile(content string) []caddyBlock {
	lines := strings.Split(content, "\n")
	var blocks []caddyBlock
	var current *caddyBlock
	depth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if current == nil {
			// Not inside any block. Skip blank lines between blocks.
			if trimmed == "" {
				continue
			}
			// Global block: starts with a bare `{`.
			if trimmed == "{" {
				current = &caddyBlock{isGlobal: true}
				depth = 1
				continue
			}
			// Site block: starts with `address {` or `address{`.
			if strings.HasSuffix(trimmed, "{") {
				addr := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
				current = &caddyBlock{address: addr}
				depth = 1
				continue
			}
			// A line outside a block that isn't a block opener. This can
			// happen with malformed input; preserve it as a standalone
			// pseudo-block so renderCaddyBlocks does not drop it.
			current = &caddyBlock{address: trimmed, body: []string{}}
			depth = 0
			blocks = append(blocks, *current)
			current = nil
			continue
		}

		// Inside a block: collect body lines and track depth.
		if trimmed == "}" && depth == 1 {
			blocks = append(blocks, *current)
			current = nil
			depth = 0
			continue
		}
		current.body = append(current.body, line)
		// Track brace depth so nested handle blocks don't prematurely close.
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth <= 0 && current != nil {
			blocks = append(blocks, *current)
			current = nil
			depth = 0
		}
	}
	// Unterminated block: preserve it so nothing is silently dropped.
	if current != nil {
		blocks = append(blocks, *current)
	}
	return blocks
}

// renderCaddyBlocks renders blocks back into Caddyfile text. Site blocks are
// sorted by address for deterministic output; the global block always comes
// first.
func renderCaddyBlocks(blocks []caddyBlock) string {
	var global *caddyBlock
	var sites []caddyBlock
	var other []caddyBlock

	for i := range blocks {
		if blocks[i].isGlobal {
			global = &blocks[i]
		} else if blocks[i].address != "" {
			sites = append(sites, blocks[i])
		} else {
			other = append(other, blocks[i])
		}
	}

	sort.Slice(sites, func(i, j int) bool {
		return sites[i].address < sites[j].address
	})

	var b strings.Builder

	if global != nil {
		b.WriteString("{\n")
		for _, line := range global.body {
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("}\n")
	}

	for _, blk := range other {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(blk.address)
		b.WriteString("\n")
	}

	for _, blk := range sites {
		b.WriteString("\n")
		b.WriteString(blk.address)
		b.WriteString(" {\n")
		for _, line := range blk.body {
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("}\n")
	}

	return b.String()
}

// removeBlock returns the blocks with the site block for the given address
// removed. The global block and all other site blocks are preserved. An
// address that matches nothing is a no-op.
func removeBlock(blocks []caddyBlock, address string) []caddyBlock {
	result := make([]caddyBlock, 0, len(blocks))
	for _, blk := range blocks {
		if !blk.isGlobal && blk.address == address {
			continue
		}
		result = append(result, blk)
	}
	return result
}

// upsertBlock adds or replaces the site block for address. If a block with
// the same address exists, it is replaced; otherwise the new block is
// appended.
func upsertBlock(blocks []caddyBlock, address string, body []string) []caddyBlock {
	result := make([]caddyBlock, 0, len(blocks)+1)
	replaced := false
	for _, blk := range blocks {
		if !blk.isGlobal && blk.address == address {
			result = append(result, caddyBlock{address: address, body: body})
			replaced = true
			continue
		}
		result = append(result, blk)
	}
	if !replaced {
		result = append(result, caddyBlock{address: address, body: body})
	}
	return result
}
