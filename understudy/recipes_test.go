package understudy

import (
	"context"
	"testing"
)

// A book that half-decodes must not answer like a book that is simply small.
//
// This is the failure the 1.21.4 port surfaced: its recipe book decodes 102 of
// 1358 entries, and every lookup that missed said "no recipe known for that" —
// the same words a complete book uses for an item that genuinely has none. The
// count of undecoded entries is what tells those apart.
func TestAPartialRecipeBookSaysSo(t *testing.T) {
	c := newTestClient(t)
	c.recipes = map[string]RecipeID{"minecraft:stick": 1}
	c.recipesMissing = 1256

	err := c.CraftRecipeFor(context.Background(), "chest", false)
	if err == nil {
		t.Fatal("crafting an unknown recipe should fail")
	}
	for _, want := range []string{"1256", "could not be decoded", "not one that is absent"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	// With a complete book the wording must not hedge: there the answer really
	// is that no such recipe exists.
	c.recipesMissing = 0
	err = c.CraftRecipeFor(context.Background(), "chest", false)
	if err == nil {
		t.Fatal("crafting an unknown recipe should fail")
	}
	if contains(err.Error(), "could not be decoded") {
		t.Errorf("a complete book should answer plainly, got %q", err)
	}
}
