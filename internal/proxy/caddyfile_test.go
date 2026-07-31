package proxy

import (
	"strings"
	"testing"
)

func TestParseCaddyfile_Empty(t *testing.T) {
	blocks := parseCaddyfile("")
	if len(blocks) != 0 {
		t.Errorf("empty input should produce 0 blocks, got %d", len(blocks))
	}
}

func TestParseCaddyfile_GlobalBlockOnly(t *testing.T) {
	input := "{\n    email test@example.com\n}\n"
	blocks := parseCaddyfile(input)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if !blocks[0].isGlobal {
		t.Error("block should be global")
	}
	if len(blocks[0].body) != 1 {
		t.Fatalf("global block body should have 1 line, got %d", len(blocks[0].body))
	}
	if strings.TrimSpace(blocks[0].body[0]) != "email test@example.com" {
		t.Errorf("body = %q", blocks[0].body[0])
	}
}

func TestParseCaddyfile_SingleSite(t *testing.T) {
	input := "{\n    email a@b.c\n}\n\napp.example.com {\n    reverse_proxy qd-app:3000\n}\n"
	blocks := parseCaddyfile(input)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (global + site), got %d", len(blocks))
	}
	if !blocks[0].isGlobal {
		t.Error("first block should be global")
	}
	if blocks[1].address != "app.example.com" {
		t.Errorf("site address = %q, want %q", blocks[1].address, "app.example.com")
	}
	if len(blocks[1].body) != 1 {
		t.Fatalf("site body should have 1 line, got %d", len(blocks[1].body))
	}
}

func TestParseCaddyfile_MultipleSites(t *testing.T) {
	input := "{\n    email a@b.c\n}\n\na.com {\n    reverse_proxy qd-a:3000\n}\n\nb.com {\n    reverse_proxy qd-b:8080\n}\n"
	blocks := parseCaddyfile(input)

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[1].address != "a.com" {
		t.Errorf("block 1 address = %q", blocks[1].address)
	}
	if blocks[2].address != "b.com" {
		t.Errorf("block 2 address = %q", blocks[2].address)
	}
}

func TestParseCaddyfile_NestedHandleBlocks(t *testing.T) {
	// The webhook route has nested handle blocks — the parser must track
	// depth so the closing `}` of the inner handle doesn't terminate the
	// outer site block prematurely.
	input := `{
    email a@b.c
}

base.com {
    handle /_qd/* {
        reverse_proxy host.docker.internal:9000
    }
    handle {
        respond "Not Found" 404
    }
}
`
	blocks := parseCaddyfile(input)

	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[1].address != "base.com" {
		t.Errorf("address = %q", blocks[1].address)
	}
	// The site body should contain all lines including the nested blocks.
	// 6 lines: handle, reverse_proxy, }, handle, respond, }
	if len(blocks[1].body) != 6 {
		t.Errorf("site body should have 6 lines (nested handles), got %d: %v",
			len(blocks[1].body), blocks[1].body)
	}
}

func TestParseCaddyfile_NoGlobalBlock(t *testing.T) {
	input := "app.example.com {\n    reverse_proxy qd-app:3000\n}\n"
	blocks := parseCaddyfile(input)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].isGlobal {
		t.Error("block should not be global")
	}
}

func TestParseCaddyfile_BlankLinesBetweenBlocks(t *testing.T) {
	input := "{\n    email a@b.c\n}\n\n\n\napp.com {\n    reverse_proxy qd-app:3000\n}\n"
	blocks := parseCaddyfile(input)

	if len(blocks) != 2 {
		t.Fatalf("blank lines between blocks should be skipped; expected 2, got %d", len(blocks))
	}
}

func TestParseCaddyfile_UnterminatedBlock(t *testing.T) {
	input := "app.com {\n    reverse_proxy qd-app:3000\n"
	blocks := parseCaddyfile(input)

	if len(blocks) != 1 {
		t.Fatalf("unterminated block should be preserved; expected 1, got %d", len(blocks))
	}
	if blocks[0].address != "app.com" {
		t.Errorf("address = %q", blocks[0].address)
	}
}

func TestParseCaddyfile_AddressWithNoSpace(t *testing.T) {
	// "app.com{" (no space before brace) should parse correctly.
	input := "app.com{\n    reverse_proxy qd-app:3000\n}\n"
	blocks := parseCaddyfile(input)

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].address != "app.com" {
		t.Errorf("address = %q, want %q", blocks[0].address, "app.com")
	}
}

func TestRenderCaddyBlocks_BasicRoundTrip(t *testing.T) {
	input := "{\n    email a@b.c\n}\n\napp.example.com {\n    reverse_proxy qd-app:3000\n}\n"
	blocks := parseCaddyfile(input)
	rendered := renderCaddyBlocks(blocks)

	// The rendered output should contain the same essential content.
	if !strings.Contains(rendered, "email a@b.c") {
		t.Errorf("rendered output missing email: %s", rendered)
	}
	if !strings.Contains(rendered, "app.example.com {") {
		t.Errorf("rendered output missing site block: %s", rendered)
	}
	if !strings.Contains(rendered, "reverse_proxy qd-app:3000") {
		t.Errorf("rendered output missing reverse_proxy: %s", rendered)
	}
}

func TestRenderCaddyBlocks_SitesSortedByAddress(t *testing.T) {
	input := `{
    email a@b.c
}

zebra.com {
    reverse_proxy qd-zebra:3000
}

alpha.com {
    reverse_proxy qd-alpha:8080
}

mid.com {
    reverse_proxy qd-mid:4000
}
`
	blocks := parseCaddyfile(input)
	rendered := renderCaddyBlocks(blocks)

	// The site blocks must appear in sorted order: alpha, mid, zebra.
	alphaPos := strings.Index(rendered, "alpha.com")
	midPos := strings.Index(rendered, "mid.com")
	zebraPos := strings.Index(rendered, "zebra.com")

	if alphaPos == -1 || midPos == -1 || zebraPos == -1 {
		t.Fatalf("missing site in rendered output:\n%s", rendered)
	}
	if !(alphaPos < midPos && midPos < zebraPos) {
		t.Errorf("sites not in sorted order: alpha=%d, mid=%d, zebra=%d\n%s",
			alphaPos, midPos, zebraPos, rendered)
	}
}

func TestRenderCaddyBlocks_GlobalAlwaysFirst(t *testing.T) {
	input := "z.com {\n    reverse_proxy qd-z:3000\n}\n\n{\n    email a@b.c\n}\n\na.com {\n    reverse_proxy qd-a:8080\n}\n"
	blocks := parseCaddyfile(input)
	rendered := renderCaddyBlocks(blocks)

	// Global block must come before any site block regardless of input order.
	globalPos := strings.Index(rendered, "{\n")
	sitePos := strings.Index(rendered, "a.com")

	if globalPos == -1 {
		t.Fatalf("global block missing:\n%s", rendered)
	}
	if sitePos == -1 {
		t.Fatalf("site block missing:\n%s", rendered)
	}
	if globalPos > sitePos {
		t.Errorf("global block should come before site blocks:\n%s", rendered)
	}
}

func TestRemoveBlock_RemovesTargetOnly(t *testing.T) {
	blocks := []caddyBlock{
		{isGlobal: true, body: []string{"    email a@b.c"}},
		{address: "a.com", body: []string{"    reverse_proxy qd-a:3000"}},
		{address: "b.com", body: []string{"    reverse_proxy qd-b:8080"}},
		{address: "c.com", body: []string{"    reverse_proxy qd-c:9000"}},
	}

	result := removeBlock(blocks, "b.com")

	if len(result) != 3 {
		t.Fatalf("expected 3 blocks after removal, got %d", len(result))
	}
	for _, blk := range result {
		if blk.address == "b.com" {
			t.Error("b.com should have been removed")
		}
	}
}

func TestRemoveBlock_PreservesGlobalAndOthers(t *testing.T) {
	blocks := []caddyBlock{
		{isGlobal: true, body: []string{"    email a@b.c"}},
		{address: "a.com", body: []string{"    reverse_proxy qd-a:3000"}},
	}

	result := removeBlock(blocks, "a.com")

	if len(result) != 1 {
		t.Fatalf("expected 1 block (global only), got %d", len(result))
	}
	if !result[0].isGlobal {
		t.Error("global block should survive removal of a site block")
	}
}

func TestRemoveBlock_NonExistentIsNoOp(t *testing.T) {
	blocks := []caddyBlock{
		{isGlobal: true, body: []string{"    email a@b.c"}},
		{address: "a.com", body: []string{"    reverse_proxy qd-a:3000"}},
	}

	result := removeBlock(blocks, "nonexistent.com")

	if len(result) != len(blocks) {
		t.Errorf("non-existent removal should be a no-op: got %d, want %d", len(result), len(blocks))
	}
}

func TestUpsertBlock_NewBlock(t *testing.T) {
	blocks := []caddyBlock{
		{isGlobal: true, body: []string{"    email a@b.c"}},
		{address: "a.com", body: []string{"    reverse_proxy qd-a:3000"}},
	}

	result := upsertBlock(blocks, "b.com", []string{"    reverse_proxy qd-b:8080"})

	if len(result) != 3 {
		t.Fatalf("expected 3 blocks after upsert, got %d", len(result))
	}
	// The new block should be present.
	found := false
	for _, blk := range result {
		if blk.address == "b.com" {
			found = true
			if len(blk.body) != 1 || strings.TrimSpace(blk.body[0]) != "reverse_proxy qd-b:8080" {
				t.Errorf("new block body incorrect: %v", blk.body)
			}
		}
	}
	if !found {
		t.Error("upserted block b.com not found in result")
	}
}

func TestUpsertBlock_ReplaceExisting(t *testing.T) {
	blocks := []caddyBlock{
		{isGlobal: true, body: []string{"    email a@b.c"}},
		{address: "a.com", body: []string{"    reverse_proxy qd-a:3000"}},
	}

	newBody := []string{"    reverse_proxy qd-a:8080"}
	result := upsertBlock(blocks, "a.com", newBody)

	if len(result) != 2 {
		t.Fatalf("replace should not add blocks; expected 2, got %d", len(result))
	}
	// The replaced block should have the new body.
	for _, blk := range result {
		if blk.address == "a.com" {
			if len(blk.body) != 1 || strings.TrimSpace(blk.body[0]) != "reverse_proxy qd-a:8080" {
				t.Errorf("replaced body incorrect: %v", blk.body)
			}
			return
		}
	}
	t.Error("replaced block not found")
}

func TestUpsertBlock_DeduplicatesStackedDuplicates(t *testing.T) {
	// Stacked duplicates can occur from hand-edits or pre-parser bugs.
	blocks := []caddyBlock{
		{isGlobal: true, body: []string{"    email a@b.c"}},
		{address: "dup.com", body: []string{"    reverse_proxy qd-dup:3000"}},
		{address: "dup.com", body: []string{"    reverse_proxy qd-dup:4000"}},
		{address: "dup.com", body: []string{"    reverse_proxy qd-dup:5000"}},
	}

	result := upsertBlock(blocks, "dup.com", []string{"    reverse_proxy qd-dup:9999"})

	count := 0
	for _, blk := range result {
		if blk.address == "dup.com" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("stacked duplicates should collapse to 1 after upsert, got %d", count)
	}
}

func TestUpsertBlock_PreservesGlobal(t *testing.T) {
	blocks := []caddyBlock{
		{isGlobal: true, body: []string{"    email a@b.c"}},
	}

	result := upsertBlock(blocks, "new.com", []string{"    reverse_proxy qd-new:3000"})

	found := false
	for _, blk := range result {
		if blk.isGlobal {
			found = true
		}
	}
	if !found {
		t.Error("global block should survive upsert of a new site block")
	}
}

func TestParseRender_RoundTrip(t *testing.T) {
	// Parse then render should produce valid Caddyfile structure.
	// The exact text may differ (sites get sorted), but the essential
	// content must survive.
	input := `{
    email a@b.c
}

z.com {
    reverse_proxy qd-z:3000
}

a.com {
    reverse_proxy qd-a:8080
    header X-Frame-Options "DENY"
}
`
	blocks := parseCaddyfile(input)
	rendered := renderCaddyBlocks(blocks)

	// Re-parse the rendered output — it should be structurally identical.
	reBlocks := parseCaddyfile(rendered)

	if len(reBlocks) != len(blocks) {
		t.Fatalf("round-trip changed block count: %d -> %d", len(blocks), len(reBlocks))
	}

	// Global block must survive.
	if !reBlocks[0].isGlobal {
		t.Error("global block not first after round-trip")
	}

	// Both sites must be present.
	addresses := map[string]bool{}
	for _, blk := range reBlocks[1:] {
		addresses[blk.address] = true
	}
	if !addresses["a.com"] || !addresses["z.com"] {
		t.Errorf("sites lost in round-trip: %v", addresses)
	}
}
