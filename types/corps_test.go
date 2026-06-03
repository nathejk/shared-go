package types_test

import (
	"testing"

	"github.com/nathejk/shared-go/types"
	"github.com/stretchr/testify/assert"
)

func TestCorpsSlugListAsObject(t *testing.T) {
	assert := assert.New(t)

	var actual = types.CorpsSlugList{types.CorpsSlugDDS, types.CorpsSlugKFUM}.AsObjects()
	var expected = []types.SlugLabel{
		{Slug: "dds", Label: "Det Danske Spejderkorps"},
		{Slug: "kfum", Label: "KFUM-Spejderne"},
	}

	assert.Equal(expected, actual, "Mismatched array")
}

func TestCoprsSlugLabel(t *testing.T) {
	assert := assert.New(t)

	assert.Equal(types.CorpsSlugDDS.Label(), "Det Danske Spejderkorps", "Label failed")
	const unknown types.CorpsSlug = "unknown"
	assert.Equal(unknown.Label(), "", "Label failed")

	/*
		assert.True(types.Slug("test").Valid(), "Slug validation failed")
		assert.True(types.Slug("2026").Valid(), "Slug validation failed")
		assert.False(types.Slug("").Valid(), "Emoty slug not valid")
		assert.False(types.Slug("-2026").Valid(), "Slug validation failed")
		assert.False(types.Slug("hello world").Valid(), "Spaces not allowed")
		assert.False(types.Slug("æøå").Valid(), "Only latin1 characters allowed")

		assert.True(types.YearSlug("test").Valid(), "Slug validation failed")
		//ID := memberID.New()
		//assert.Equal("  ", memberID, "Year calculation failed")
		//assert.Equal(ID, memberID, "Year calculation failed")
	*/
}
